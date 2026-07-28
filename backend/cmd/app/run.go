package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"git.disroot.org/federico-paolillo/four-visor.git/config"
	"git.disroot.org/federico-paolillo/four-visor.git/health"
	"git.disroot.org/federico-paolillo/four-visor.git/telemetry"
	"go.opentelemetry.io/otel"
)

const (
	readHeaderTimeout = 5 * time.Second
	readTimeout       = 10 * time.Second
	writeTimeout      = 30 * time.Second
	idleTimeout       = 120 * time.Second
	shutdownTimeout   = 10 * time.Second
)

// applicationError preserves an operational cause behind a value-free process diagnostic.
type applicationError struct {
	operation string
	cause     error
}

func (err *applicationError) Error() string {
	return err.operation
}

func (err *applicationError) Unwrap() error {
	return err.cause
}

// run composes the health service and owns its bounded server lifecycle.
func run(parent context.Context, stderr io.Writer) error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("loading configuration: %w", err)
	}

	providers, err := telemetry.New(parent, cfg.OTLPEndpoint, stderr)
	if err != nil {
		return &applicationError{operation: "creating OpenTelemetry", cause: err}
	}
	defer shutdownTelemetry(context.WithoutCancel(parent), providers)

	cache := health.NewMemcached(cfg.MemcachedAddress)
	dns := health.NewDNS(cfg.DNSName, net.DefaultResolver)
	healthHandler := health.NewHandler(
		cfg.HealthTimeout,
		providers.Slog,
		providers.Tracer.Tracer("four-visor/health"),
		cache,
		dns,
	)
	mux := http.NewServeMux()
	mux.Handle("/health", healthHandler)

	handler, err := telemetry.HTTPHandler(mux, providers.Tracer, providers.Meter, otel.GetTextMapPropagator())
	if err != nil {
		return &applicationError{operation: "creating HTTP instrumentation", cause: err}
	}

	server := &http.Server{
		Addr:              cfg.ServerAddress,
		Handler:           handler,
		ReadHeaderTimeout: readHeaderTimeout,
		ReadTimeout:       readTimeout,
		WriteTimeout:      writeTimeout,
		IdleTimeout:       idleTimeout,
	}

	ctx, stop := signal.NotifyContext(parent, os.Interrupt, syscall.SIGTERM)
	defer stop()

	serverError := make(chan error, 1)
	go func() {
		serverError <- server.ListenAndServe()
	}()

	select {
	case err := <-serverError:
		if !errors.Is(err, http.ErrServerClosed) {
			return &applicationError{operation: "serving HTTP", cause: err}
		}
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
		defer cancel()

		err = server.Shutdown(shutdownCtx)
		if err != nil {
			return &applicationError{operation: "shutting down HTTP", cause: err}
		}

		err = <-serverError
		if !errors.Is(err, http.ErrServerClosed) {
			return &applicationError{operation: "serving HTTP", cause: err}
		}
	}

	return nil
}

func shutdownTelemetry(parent context.Context, providers *telemetry.Providers) {
	ctx, cancel := context.WithTimeout(parent, shutdownTimeout)
	defer cancel()

	err := providers.Shutdown(ctx)
	if err != nil {
		providers.Slog.Error("OpenTelemetry shutdown failed",
			slog.String("error.type", "unavailable"),
		)
	}
}
