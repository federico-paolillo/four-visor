package telemetry

import (
	"context"
	"errors"
	"maps"
	"net/http"
	"net/http/httptest"
	"slices"
	"testing"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/propagation"
	metricsdk "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
)

func TestHTTPHandlerSpansAndMetrics(t *testing.T) {
	spanExporter := tracetest.NewInMemoryExporter()
	tracerProvider := tracesdk.NewTracerProvider(tracesdk.WithSyncer(spanExporter))
	t.Cleanup(func() { _ = tracerProvider.Shutdown(t.Context()) })
	reader := metricsdk.NewManualReader()
	meterProvider := metricsdk.NewMeterProvider(metricsdk.WithReader(reader))
	t.Cleanup(func() { _ = meterProvider.Shutdown(t.Context()) })
	propagator := propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{})

	good := instrumentedSnapshot(t, tracerProvider, meterProvider, propagator, nil)
	bad := instrumentedSnapshot(t, tracerProvider, meterProvider, propagator, errors.New("private dependency detail"))

	requests := []struct {
		handler http.Handler
		method  string
		path    string
		status  int
	}{
		{handler: good, method: http.MethodGet, path: "/snapshot", status: http.StatusOK},
		{handler: bad, method: http.MethodGet, path: "/snapshot", status: http.StatusServiceUnavailable},
		{handler: good, method: http.MethodPost, path: "/snapshot", status: http.StatusMethodNotAllowed},
		{handler: good, method: http.MethodGet, path: "/unknown/attacker-value", status: http.StatusNotFound},
		{handler: good, method: "ATTACKER-CONTROLLED", path: "/snapshot", status: http.StatusMethodNotAllowed},
	}
	for _, request := range requests {
		response := httptest.NewRecorder()
		request.handler.ServeHTTP(response, httptest.NewRequest(request.method, request.path, http.NoBody))
		if response.Code != request.status {
			t.Fatalf("%s %s status = %d, want %d", request.method, request.path, response.Code, request.status)
		}
	}

	assertRootSpans(t, spanExporter.GetSpans())
	assertHTTPMetrics(t, reader)
}

func TestHTTPHandlerStartsPublicRootAndParentsSnapshotSpan(t *testing.T) {
	spanExporter := tracetest.NewInMemoryExporter()
	tracerProvider := tracesdk.NewTracerProvider(tracesdk.WithSyncer(spanExporter))
	t.Cleanup(func() { _ = tracerProvider.Shutdown(t.Context()) })
	meterProvider := metricsdk.NewMeterProvider()
	t.Cleanup(func() { _ = meterProvider.Shutdown(t.Context()) })
	propagator := propagation.NewCompositeTextMapPropagator(propagation.TraceContext{})
	handler := instrumentedSnapshot(t, tracerProvider, meterProvider, propagator, nil)

	request := httptest.NewRequest(http.MethodGet, "/snapshot", http.NoBody)
	request.Header.Set("traceparent", "00-4bf92f3577b34da6a3ce929d0e0e4736-00f067aa0ba902b7-01")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	var server *tracetest.SpanStub
	var child *tracetest.SpanStub
	spans := spanExporter.GetSpans()
	for index := range spans {
		span := &spans[index]
		if span.SpanKind == trace.SpanKindServer {
			server = span
		}
		if span.Name == "snapshot.read" {
			child = span
		}
	}
	if server == nil {
		t.Fatalf("server span missing; spans=%v", spanNames(spans))
	}
	if server.Parent.IsValid() {
		t.Fatalf("server parent = %v, want invalid root parent", server.Parent)
	}
	if len(server.Links) != 1 || !server.Links[0].SpanContext.IsRemote() ||
		server.Links[0].SpanContext.TraceID().String() != "4bf92f3577b34da6a3ce929d0e0e4736" ||
		server.Links[0].SpanContext.SpanID().String() != "00f067aa0ba902b7" {
		t.Fatalf("server links = %#v, want incoming remote trace context", server.Links)
	}
	if child == nil {
		t.Fatalf("snapshot.read span missing; spans=%v", spanNames(spans))
	}
	if child.Parent.TraceID() != server.SpanContext.TraceID() ||
		child.Parent.SpanID() != server.SpanContext.SpanID() {
		t.Fatalf("snapshot.read parent = %v, want server span %v", child.Parent, server.SpanContext)
	}
}

func TestHTTPHandlerExporterFailureIsNonFatal(t *testing.T) {
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(error) {}))
	tracerProvider := tracesdk.NewTracerProvider(tracesdk.WithSyncer(failingExporter{}))
	t.Cleanup(func() { _ = tracerProvider.Shutdown(t.Context()) })
	meterProvider := metricsdk.NewMeterProvider()
	t.Cleanup(func() { _ = meterProvider.Shutdown(t.Context()) })

	handler := instrumentedSnapshot(
		t,
		tracerProvider,
		meterProvider,
		propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}),
		nil,
	)
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/snapshot", http.NoBody))
	if response.Code != http.StatusOK {
		t.Fatalf("status after export failure = %d, want 200", response.Code)
	}
}

func TestNormalizedMethodBoundsCardinality(t *testing.T) {
	if got := normalizedMethod("ATTACKER-CONTROLLED"); got != "_OTHER" {
		t.Fatalf("normalizedMethod() = %q, want _OTHER", got)
	}
}

type failingExporter struct{}

func (failingExporter) ExportSpans(context.Context, []tracesdk.ReadOnlySpan) error {
	return errors.New("collector unavailable")
}

func (failingExporter) Shutdown(context.Context) error {
	return nil
}

func instrumentedSnapshot(
	t *testing.T,
	tracerProvider trace.TracerProvider,
	meterProvider *metricsdk.MeterProvider,
	propagator propagation.TextMapPropagator,
	readError error,
) http.Handler {
	t.Helper()
	tracer := tracerProvider.Tracer("test/snapshot")
	mux := http.NewServeMux()
	mux.HandleFunc("/snapshot", func(writer http.ResponseWriter, request *http.Request) {
		if request.Method != http.MethodGet {
			writer.Header().Set("Allow", http.MethodGet)
			writer.WriteHeader(http.StatusMethodNotAllowed)

			return
		}

		_, span := tracer.Start(request.Context(), "snapshot.read")
		defer span.End()
		if readError == nil {
			writer.WriteHeader(http.StatusOK)

			return
		}

		span.SetStatus(codes.Error, "snapshot unavailable")
		trace.SpanFromContext(request.Context()).SetStatus(codes.Error, "snapshot unavailable")
		writer.WriteHeader(http.StatusServiceUnavailable)
	})
	instrumented, err := HTTPHandler(mux, tracerProvider, meterProvider, propagator)
	if err != nil {
		t.Fatalf("HTTPHandler() error = %v", err)
	}

	return instrumented
}

func assertRootSpans(t *testing.T, spans tracetest.SpanStubs) {
	t.Helper()
	var roots tracetest.SpanStubs
	var failedSnapshotChildren int
	for _, span := range spans {
		if span.SpanKind == trace.SpanKindServer {
			roots = append(roots, span)
		}
		if span.Name == "snapshot.read" && span.Status.Code == codes.Error {
			failedSnapshotChildren++
		}
	}
	if len(roots) != 5 {
		t.Fatalf("root span count = %d, want 5; spans=%v", len(roots), spanNames(spans))
	}
	if failedSnapshotChildren != 1 {
		t.Fatalf("failed snapshot child spans = %d, want 1", failedSnapshotChildren)
	}

	wantNames := []string{"GET", "GET /snapshot", "GET /snapshot", "POST /snapshot", "_OTHER /snapshot"}
	gotNames := spanNames(roots)
	slices.Sort(gotNames)
	slices.Sort(wantNames)
	if !slices.Equal(gotNames, wantNames) {
		t.Fatalf("root span names = %v, want %v", gotNames, wantNames)
	}
	var errorRoots int
	for _, span := range roots {
		if span.Status.Code == codes.Error {
			errorRoots++
		}
	}
	if errorRoots != 1 {
		t.Fatalf("error root spans = %d, want 1", errorRoots)
	}
}

func assertHTTPMetrics(t *testing.T, reader *metricsdk.ManualReader) {
	t.Helper()
	var resourceMetrics metricdata.ResourceMetrics
	if err := reader.Collect(t.Context(), &resourceMetrics); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	want := map[metricAttributes]uint64{
		{method: http.MethodGet, route: "/snapshot", status: http.StatusOK}:                 1,
		{method: http.MethodGet, route: "/snapshot", status: http.StatusServiceUnavailable}: 1,
		{method: http.MethodPost, route: "/snapshot", status: http.StatusMethodNotAllowed}:  1,
		{method: http.MethodGet, route: "unmatched", status: http.StatusNotFound}:           1,
		{method: "_OTHER", route: "/snapshot", status: http.StatusMethodNotAllowed}:         1,
	}
	requestPoints := make(map[metricAttributes]uint64)
	durationPoints := make(map[metricAttributes]uint64)
	var metricCount int
	for _, scope := range resourceMetrics.ScopeMetrics {
		for _, metric := range scope.Metrics {
			metricCount++
			switch data := metric.Data.(type) {
			case metricdata.Sum[int64]:
				if metric.Name != "http.server.request.count" {
					t.Fatalf("unexpected sum metric %q", metric.Name)
				}
				for _, point := range data.DataPoints {
					requestPoints[exactMetricAttributes(t, point.Attributes.ToSlice())] += uint64(point.Value)
				}
			case metricdata.Histogram[float64]:
				if metric.Name != "http.server.request.duration" {
					t.Fatalf("unexpected histogram metric %q", metric.Name)
				}
				for _, point := range data.DataPoints {
					durationPoints[exactMetricAttributes(t, point.Attributes.ToSlice())] += point.Count
				}
			default:
				t.Fatalf("unexpected metric data type %T", metric.Data)
			}
		}
	}
	if metricCount != 2 {
		t.Fatalf("metric count = %d, want 2", metricCount)
	}
	if !maps.Equal(requestPoints, want) {
		t.Fatalf("request metric points = %#v, want %#v", requestPoints, want)
	}
	if !maps.Equal(durationPoints, want) {
		t.Fatalf("duration metric points = %#v, want %#v", durationPoints, want)
	}
}

type metricAttributes struct {
	method string
	route  string
	status int
}

func exactMetricAttributes(t *testing.T, attributes []attribute.KeyValue) metricAttributes {
	t.Helper()
	if len(attributes) != 3 {
		t.Fatalf("metric attribute count = %d, want 3: %#v", len(attributes), attributes)
	}

	var got metricAttributes
	for _, item := range attributes {
		switch item.Key {
		case "http.request.method":
			got.method = item.Value.AsString()
		case "http.route":
			got.route = item.Value.AsString()
		case "http.response.status_code":
			got.status = int(item.Value.AsInt64())
		default:
			t.Fatalf("unexpected metric attribute %q", item.Key)
		}
	}
	if got.method == "" || got.route == "" || got.status == 0 {
		t.Fatalf("metric attributes incomplete: %#v", got)
	}

	return got
}

func spanNames(spans tracetest.SpanStubs) []string {
	names := make([]string, 0, len(spans))
	for _, span := range spans {
		names = append(names, span.Name)
	}

	return names
}
