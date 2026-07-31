// Package telemetry configures the backend's OpenTelemetry signals and HTTP boundary.
package telemetry

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strings"
	"time"

	"go.opentelemetry.io/contrib/bridges/otelslog"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploggrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetricgrpc"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	otellogglobal "go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/propagation"
	logsdk "go.opentelemetry.io/otel/sdk/log"
	metricsdk "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/exemplar"
	"go.opentelemetry.io/otel/sdk/resource"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.41.0"
)

const (
	serviceName   = "four-visor-backend"
	exportTimeout = 5 * time.Second
)

// Error preserves a telemetry setup failure behind a value-free diagnostic.
type Error struct {
	cause error
}

// Error returns a stable diagnostic that does not disclose the exporter endpoint.
func (*Error) Error() string {
	return "initializing OpenTelemetry"
}

// Unwrap preserves the underlying SDK or exporter cause.
func (err *Error) Unwrap() error {
	return err.cause
}

// Providers owns the three OpenTelemetry SDK providers and application logger.
type Providers struct {
	Tracer *tracesdk.TracerProvider
	Meter  *metricsdk.MeterProvider
	Logger *logsdk.LoggerProvider
	Slog   *slog.Logger
}

// New creates asynchronous OTLP exporters and a JSON-plus-OTLP structured logger.
func New(ctx context.Context, endpoint string, stderr io.Writer) (*Providers, error) {
	err := clearOTelEnvironment()
	if err != nil {
		return nil, &Error{cause: fmt.Errorf("neutralizing OpenTelemetry environment: %w", err)}
	}

	instanceID, err := randomInstanceID()
	if err != nil {
		return nil, &Error{cause: fmt.Errorf("generating service instance ID: %w", err)}
	}

	res := resource.NewWithAttributes(semconv.SchemaURL,
		semconv.ServiceName(serviceName),
		semconv.ServiceInstanceID(instanceID),
	)

	tracerProvider, err := newTracerProvider(ctx, endpoint, res)
	if err != nil {
		return nil, &Error{cause: err}
	}

	meterProvider, err := newMeterProvider(ctx, endpoint, res)
	if err != nil {
		_ = tracerProvider.Shutdown(ctx)

		return nil, &Error{cause: err}
	}

	loggerProvider, err := newLoggerProvider(ctx, endpoint, res)
	if err != nil {
		_ = meterProvider.Shutdown(ctx)
		_ = tracerProvider.Shutdown(ctx)

		return nil, &Error{cause: err}
	}

	jsonHandler := slog.NewJSONHandler(stderr, nil)
	otlpHandler := otlpLogHandler{handler: otelslog.NewHandler(
		serviceName,
		otelslog.WithLoggerProvider(loggerProvider),
	)}
	logger := slog.New(slog.NewMultiHandler(
		jsonHandler,
		otlpHandler,
	))

	providers := &Providers{Tracer: tracerProvider, Meter: meterProvider, Logger: loggerProvider, Slog: logger}
	providers.installGlobals(context.WithoutCancel(ctx), jsonHandler)

	return providers, nil
}

func newTracerProvider(ctx context.Context, endpoint string, res *resource.Resource) (*tracesdk.TracerProvider, error) {
	exporter, err := otlptracegrpc.New(ctx,
		otlptracegrpc.WithEndpointURL(endpoint),
		otlptracegrpc.WithTimeout(exportTimeout),
	)
	if err != nil {
		return nil, fmt.Errorf("creating trace exporter: %w", err)
	}

	tracerProvider := tracesdk.NewTracerProvider(
		tracesdk.WithResource(res),
		tracesdk.WithSampler(tracesdk.AlwaysSample()),
		tracesdk.WithRawSpanLimits(tracesdk.SpanLimits{
			AttributeValueLengthLimit:   -1,
			AttributeCountLimit:         tracesdk.DefaultAttributeCountLimit,
			EventCountLimit:             tracesdk.DefaultEventCountLimit,
			LinkCountLimit:              tracesdk.DefaultLinkCountLimit,
			AttributePerEventCountLimit: tracesdk.DefaultAttributePerEventCountLimit,
			AttributePerLinkCountLimit:  tracesdk.DefaultAttributePerLinkCountLimit,
		}),
		tracesdk.WithBatcher(exporter,
			tracesdk.WithMaxQueueSize(tracesdk.DefaultMaxQueueSize),
			tracesdk.WithMaxExportBatchSize(tracesdk.DefaultMaxExportBatchSize),
			tracesdk.WithBatchTimeout(tracesdk.DefaultScheduleDelay*time.Millisecond),
			tracesdk.WithExportTimeout(tracesdk.DefaultExportTimeout*time.Millisecond),
		),
	)

	return tracerProvider, nil
}

func newMeterProvider(ctx context.Context, endpoint string, res *resource.Resource) (*metricsdk.MeterProvider, error) {
	exporter, err := otlpmetricgrpc.New(ctx,
		otlpmetricgrpc.WithEndpointURL(endpoint),
		otlpmetricgrpc.WithTimeout(exportTimeout),
	)
	if err != nil {
		return nil, fmt.Errorf("creating metric exporter: %w", err)
	}

	provider := metricsdk.NewMeterProvider(
		metricsdk.WithResource(res),
		metricsdk.WithExemplarFilter(exemplar.TraceBasedFilter),
		metricsdk.WithCardinalityLimit(2000),
		metricsdk.WithView(metricView),
		metricsdk.WithReader(metricsdk.NewPeriodicReader(exporter,
			metricsdk.WithInterval(60*time.Second),
			metricsdk.WithTimeout(30*time.Second),
		)),
	)

	return provider, nil
}

func newLoggerProvider(ctx context.Context, endpoint string, res *resource.Resource) (*logsdk.LoggerProvider, error) {
	exporter, err := otlploggrpc.New(ctx,
		otlploggrpc.WithEndpointURL(endpoint),
		otlploggrpc.WithTimeout(exportTimeout),
	)
	if err != nil {
		return nil, fmt.Errorf("creating log exporter: %w", err)
	}

	provider := logsdk.NewLoggerProvider(
		logsdk.WithResource(res),
		logsdk.WithAttributeCountLimit(128),
		logsdk.WithAttributeValueLengthLimit(-1),
		logsdk.WithProcessor(logsdk.NewBatchProcessor(exporter,
			logsdk.WithMaxQueueSize(2048),
			logsdk.WithExportInterval(time.Second),
			logsdk.WithExportTimeout(30*time.Second),
			logsdk.WithExportMaxBatchSize(512),
			logsdk.WithExportBufferSize(1),
		)),
	)

	return provider, nil
}

// Shutdown flushes and releases all SDK providers.
func (providers *Providers) Shutdown(ctx context.Context) error {
	return errors.Join(
		providers.Logger.Shutdown(ctx),
		providers.Meter.Shutdown(ctx),
		providers.Tracer.Shutdown(ctx),
	)
}

// clearOTelEnvironment keeps the process-wide telemetry SDK inside the FOURVISOR_ configuration boundary.
func clearOTelEnvironment() error {
	for _, setting := range os.Environ() {
		key, _, _ := strings.Cut(setting, "=")
		if strings.HasPrefix(key, "OTEL_") {
			err := os.Unsetenv(key)
			if err != nil {
				return fmt.Errorf("unsetting %s: %w", key, err)
			}
		}
	}

	return nil
}

func (providers *Providers) installGlobals(ctx context.Context, diagnostics slog.Handler) {
	otel.SetTracerProvider(providers.Tracer)
	otel.SetMeterProvider(providers.Meter)
	otellogglobal.SetLoggerProvider(providers.Logger)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(error) {
		_ = diagnostics.Handle(ctx, slog.NewRecord(
			time.Now(), slog.LevelError, "OpenTelemetry export failed", 0,
		))
	}))
}

func randomInstanceID() (string, error) {
	identifier := make([]byte, 16)

	_, err := rand.Read(identifier)
	if err != nil {
		return "", fmt.Errorf("reading randomness: %w", err)
	}

	return hex.EncodeToString(identifier), nil
}
