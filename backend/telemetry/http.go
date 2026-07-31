package telemetry

import (
	"fmt"
	"net/http"
	"time"

	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/trace"
)

// HTTPHandler instruments every inbound request with one root span and two bounded metrics.
func HTTPHandler(
	next http.Handler,
	tracerProvider trace.TracerProvider,
	meterProvider metric.MeterProvider,
	propagator propagation.TextMapPropagator,
) (http.Handler, error) {
	meter := meterProvider.Meter(serviceName)

	requests, err := HTTPServerRequestCount.Int64Counter(meter)
	if err != nil {
		return nil, fmtInstrumentError("request counter", err)
	}

	duration, err := HTTPServerRequestDuration.Float64Histogram(meter)
	if err != nil {
		return nil, fmtInstrumentError("request duration", err)
	}

	metrics := http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		started := time.Now()
		response := &responseWriter{ResponseWriter: writer, status: http.StatusOK}
		next.ServeHTTP(response, request)

		attributes := metric.WithAttributes(
			attribute.String("http.request.method", normalizedMethod(request.Method)),
			attribute.String("http.route", route(request)),
			attribute.Int("http.response.status_code", response.status),
		)
		requests.Add(request.Context(), 1, attributes)
		duration.Record(request.Context(), time.Since(started).Seconds(), attributes)
	})

	return otelhttp.NewHandler(metrics, "http.server",
		otelhttp.WithTracerProvider(tracerProvider),
		otelhttp.WithMeterProvider(metricnoop.NewMeterProvider()),
		otelhttp.WithPropagators(propagator),
		otelhttp.WithPublicEndpointFn(func(*http.Request) bool { return true }),
		otelhttp.WithSpanNameFormatter(spanName),
	), nil
}

type responseWriter struct {
	http.ResponseWriter

	status      int
	wroteHeader bool
}

func (writer *responseWriter) WriteHeader(status int) {
	if writer.wroteHeader {
		return
	}

	writer.status = status
	writer.wroteHeader = true
	writer.ResponseWriter.WriteHeader(status)
}

func (writer *responseWriter) Write(body []byte) (int, error) {
	if !writer.wroteHeader {
		writer.WriteHeader(http.StatusOK)
	}

	written, err := writer.ResponseWriter.Write(body)
	if err != nil {
		return written, fmt.Errorf("writing HTTP response: %w", err)
	}

	return written, nil
}

func (writer *responseWriter) Unwrap() http.ResponseWriter {
	return writer.ResponseWriter
}

func route(request *http.Request) string {
	if request.Pattern == "" {
		return "unmatched"
	}

	return request.Pattern
}

func spanName(_ string, request *http.Request) string {
	if request.Pattern == "" {
		return normalizedMethod(request.Method)
	}

	return normalizedMethod(request.Method) + " " + request.Pattern
}

func normalizedMethod(method string) string {
	switch method {
	case http.MethodConnect, http.MethodDelete, http.MethodGet, http.MethodHead,
		http.MethodOptions, http.MethodPatch, http.MethodPost, http.MethodPut, http.MethodTrace:
		return method
	default:
		return "_OTHER"
	}
}

func fmtInstrumentError(instrument string, cause error) error {
	return &instrumentError{instrument: instrument, cause: cause}
}

type instrumentError struct {
	instrument string
	cause      error
}

func (err *instrumentError) Error() string {
	return "creating HTTP " + err.instrument
}

func (err *instrumentError) Unwrap() error {
	return err.cause
}
