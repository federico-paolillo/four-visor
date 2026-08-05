package main

import (
	"fmt"
	"log/slog"
	"net/http"

	"git.disroot.org/federico-paolillo/four-visor.git/acquisition"
	"git.disroot.org/federico-paolillo/four-visor.git/config"
	"git.disroot.org/federico-paolillo/four-visor.git/lineage"
	"git.disroot.org/federico-paolillo/four-visor.git/synchronization"
	"git.disroot.org/federico-paolillo/four-visor.git/telemetry"
	"go.opentelemetry.io/otel"
)

// application groups the HTTP and scheduled lifecycles created by the composition root.
type application struct {
	handler   http.Handler
	scheduler *synchronization.Scheduler
}

func newApplication(cfg config.Config, providers *telemetry.Providers) (application, error) {
	lineageTracer := providers.Tracer.Tracer("four-visor/lineage")
	lineageMeter := providers.Meter.Meter("four-visor/lineage")

	snapshotHandler, err := lineage.NewSnapshotHandler(
		cfg.MemcachedAddress,
		providers.Slog,
		lineageTracer,
		lineageMeter,
	)
	if err != nil {
		return application{}, fmt.Errorf("creating snapshot handler: %w", err)
	}

	acquisitionClient, err := acquisition.New(
		acquisition.Policy{
			RateInterval:   cfg.Acquisition.RateInterval,
			MaxConcurrency: cfg.Acquisition.MaxConcurrency,
			RequestTimeout: cfg.Acquisition.RequestTimeout,
			MaxRetries:     cfg.Acquisition.MaxRetries,
			RetryBackoff:   cfg.Acquisition.RetryBackoff,
		},
		cfg.Acquisition.UserAgent,
		http.DefaultTransport,
		providers.Slog,
		providers.Tracer.Tracer("four-visor/acquisition"),
		providers.Meter.Meter("four-visor/acquisition"),
	)
	if err != nil {
		return application{}, fmt.Errorf("creating acquisition client: %w", err)
	}

	publisher, err := lineage.NewPublisher(cfg.MemcachedAddress, providers.Slog, lineageTracer, lineageMeter)
	if err != nil {
		return application{}, fmt.Errorf("creating lineage publisher: %w", err)
	}

	scheduler, err := synchronization.New(
		cfg.Synchronization.Interval,
		cfg.Acquisition.Deadline,
		cfg.Synchronization.FailedResourceTolerance,
		acquisitionClient,
		publisher,
		providers.Slog,
		providers.Tracer.Tracer("four-visor/synchronization"),
		providers.Meter.Meter("four-visor/synchronization"),
	)
	if err != nil {
		return application{}, fmt.Errorf("creating synchronization scheduler: %w", err)
	}

	logEffectivePolicy(providers.Slog, cfg)

	mux := http.NewServeMux()
	mux.Handle("/snapshot", snapshotHandler)

	handler, err := telemetry.HTTPHandler(mux, providers.Tracer, providers.Meter, otel.GetTextMapPropagator())
	if err != nil {
		return application{}, fmt.Errorf("instrumenting HTTP handler: %w", err)
	}

	return application{handler: handler, scheduler: scheduler}, nil
}

func logEffectivePolicy(logger *slog.Logger, cfg config.Config) {
	logger.Info("effective backend policy configured",
		slog.Duration("acquisition.rate_interval", cfg.Acquisition.RateInterval),
		slog.Int("acquisition.max_concurrency", cfg.Acquisition.MaxConcurrency),
		slog.Duration("acquisition.request_timeout", cfg.Acquisition.RequestTimeout),
		slog.Int("acquisition.max_retries", cfg.Acquisition.MaxRetries),
		slog.Duration("acquisition.retry_backoff", cfg.Acquisition.RetryBackoff),
		slog.Duration("lineage.deadline", cfg.Acquisition.Deadline),
		slog.Duration("synchronization.interval", cfg.Synchronization.Interval),
		slog.Int("resource.failed.tolerance", cfg.Synchronization.FailedResourceTolerance),
	)
}
