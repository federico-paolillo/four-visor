package lineage

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"git.disroot.org/federico-paolillo/four-visor.git/snapshot"
	"git.disroot.org/federico-paolillo/four-visor.git/telemetry"
	"github.com/bradfitz/gomemcache/memcache"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/propagation"
	metricsdk "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

func TestSnapshotHandlerServesExactBytesWithoutEncodingOrRanges(t *testing.T) {
	data := []byte(`{
  "schemaVersion": 1,
  "lineageId": "01J1YQ7Y0M4S6R8T2V3W5X7Y9Y",
  "observedAt": "2026-07-28T12:00:00Z",
  "boards": {"state": "failed"}
}`)
	cache := newFakeMemcache()
	seedSerializedLineage(t, cache, newLineageID, data)
	var logs bytes.Buffer
	handler := testSnapshotHTTPHandler(t, cache, &logs)

	request := httptest.NewRequest(http.MethodGet, "/snapshot", http.NoBody)
	request.Header.Set("Accept-Encoding", "br")
	request.Header.Set("Range", "bytes=0-9")
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusOK {
		t.Fatalf("status = %d, want 200", response.Code)
	}
	if !bytes.Equal(response.Body.Bytes(), data) {
		t.Fatalf("body changed stored bytes:\n%s", response.Body.Bytes())
	}
	if _, err := snapshot.Parse(response.Body.Bytes()); err != nil {
		t.Fatalf("snapshot.Parse(response) error = %v", err)
	}
	assertSnapshotHeaders(t, response, len(data))
	if logs.Len() != 0 {
		t.Fatalf("successful request emitted log: %s", logs.String())
	}
}

func TestSnapshotHandlerRejectsMethodsAndLeavesOtherRoutesUndeclared(t *testing.T) {
	cache := newFakeMemcache()
	seedSerializedLineage(t, cache, newLineageID, mustMarshalSnapshot(t, testSnapshot(newLineageID, 0)))
	var reads atomic.Int64
	cache.before = func(operation, _ string) error {
		if operation == "get" {
			reads.Add(1)
		}

		return nil
	}
	handler := testSnapshotHTTPHandler(t, cache, io.Discard)
	mux := http.NewServeMux()
	mux.Handle("/snapshot", handler)

	for _, method := range []string{http.MethodHead, http.MethodPost, http.MethodPut, http.MethodOptions} {
		t.Run(method, func(t *testing.T) {
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, httptest.NewRequest(method, "/snapshot", http.NoBody))
			if response.Code != http.StatusMethodNotAllowed || response.Header().Get("Allow") != http.MethodGet {
				t.Fatalf("status=%d Allow=%q", response.Code, response.Header().Get("Allow"))
			}
			assertSnapshotHeaders(t, response, response.Body.Len())
		})
	}

	for _, path := range []string{
		"/api/snapshot",
		"/snapshot/",
		"/snapshot/manifest",
		"/manifest",
		"/blocks/0",
		"/boards",
		"/threads/1",
		"/resources/1",
	} {
		t.Run(path, func(t *testing.T) {
			response := httptest.NewRecorder()
			mux.ServeHTTP(response, httptest.NewRequest(http.MethodGet, path, http.NoBody))
			if response.Code != http.StatusNotFound {
				t.Fatalf("GET %s status = %d, want 404", path, response.Code)
			}
		})
	}

	if reads.Load() != 0 {
		t.Fatalf("unsupported requests performed %d cache reads", reads.Load())
	}
}

func TestSnapshotHandlerMapsOperationalFailuresSafely(t *testing.T) {
	const secret = "private-cache.example:65100/payload-secret"
	tests := []struct {
		name      string
		configure func(*fakeMemcache) context.Context
		status    int
		errorType string
		component string
		lineage   bool
	}{
		{
			name: "missing pointer",
			configure: func(cache *fakeMemcache) context.Context {
				return t.Context()
			},
			status:    http.StatusGone,
			errorType: "cache_miss",
			component: "pointer",
		},
		{
			name: "dependency unavailable",
			configure: func(cache *fakeMemcache) context.Context {
				cache.before = func(operation, _ string) error {
					if operation == "get" {
						return errors.New(secret)
					}

					return nil
				}

				return t.Context()
			},
			status:    http.StatusServiceUnavailable,
			errorType: "unavailable",
			component: "pointer",
		},
		{
			name: "present corruption",
			configure: func(cache *fakeMemcache) context.Context {
				cache.items[activePointerKey] = &memcache.Item{Value: []byte("invalid")}

				return t.Context()
			},
			status:    http.StatusInternalServerError,
			errorType: "corrupt",
			component: "pointer",
		},
		{
			name: "missing completion",
			configure: func(cache *fakeMemcache) context.Context {
				cache.items[activePointerKey] = &memcache.Item{Value: []byte(newLineageID)}

				return t.Context()
			},
			status:    http.StatusGone,
			errorType: "cache_miss",
			component: "completion",
			lineage:   true,
		},
		{
			name: "missing block",
			configure: func(cache *fakeMemcache) context.Context {
				data := mustMarshalSnapshot(t, testSnapshot(newLineageID, 0))
				seedSerializedLineage(t, cache, newLineageID, data)
				delete(cache.items, blockKey(newLineageID, 0))

				return t.Context()
			},
			status:    http.StatusGone,
			errorType: "cache_miss",
			component: "block",
			lineage:   true,
		},
		{
			name: "invalid serialization",
			configure: func(cache *fakeMemcache) context.Context {
				data := mustMarshalSnapshot(t, testSnapshot(newLineageID, 0))
				seedSerializedLineage(t, cache, newLineageID, data)
				cache.items[blockKey(newLineageID, 0)].Value[0] = '!'

				return t.Context()
			},
			status:    http.StatusInternalServerError,
			errorType: "corrupt",
			component: "serialization",
			lineage:   true,
		},
		{
			name: "request canceled",
			configure: func(cache *fakeMemcache) context.Context {
				ctx, cancel := context.WithCancel(t.Context())
				cancel()

				return ctx
			},
			status:    http.StatusServiceUnavailable,
			errorType: "canceled",
			component: "pointer",
		},
		{
			name: "request deadline",
			configure: func(cache *fakeMemcache) context.Context {
				ctx, cancel := context.WithDeadline(t.Context(), time.Now().Add(-time.Second))
				defer cancel()

				return ctx
			},
			status:    http.StatusServiceUnavailable,
			errorType: "timeout",
			component: "pointer",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cache := newFakeMemcache()
			ctx := test.configure(cache)
			var logs bytes.Buffer
			handler := testSnapshotHTTPHandler(t, cache, &logs)
			request := httptest.NewRequest(http.MethodGet, "/snapshot", http.NoBody).WithContext(ctx)
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			if response.Code != test.status {
				t.Fatalf("status = %d, want %d", response.Code, test.status)
			}
			assertSnapshotHeaders(t, response, response.Body.Len())
			if countLines(logs.String()) != 1 ||
				!strings.Contains(logs.String(), `"error.type":"`+test.errorType+`"`) ||
				!strings.Contains(logs.String(), `"snapshot.component":"`+test.component+`"`) ||
				!strings.Contains(logs.String(), `"http.response.status_code":`+strconv.Itoa(test.status)) {
				t.Fatalf("logs = %q, want one %s failure", logs.String(), test.errorType)
			}
			if got := strings.Contains(logs.String(), `"lineage.id":"`+newLineageID+`"`); got != test.lineage {
				t.Fatalf("lineage correlation present=%t, want %t: %s", got, test.lineage, logs.String())
			}
			for _, forbidden := range []string{secret, activePointerKey, lineageKeyPrefix, `"schemaVersion"`} {
				if strings.Contains(response.Body.String(), forbidden) || strings.Contains(logs.String(), forbidden) {
					t.Fatalf("response or log disclosed %q: body=%q log=%q", forbidden, response.Body, logs.String())
				}
			}
		})
	}
}

func TestSnapshotHandlerWriteFailureIsTelemetryOnly(t *testing.T) {
	cache := newFakeMemcache()
	data := mustMarshalSnapshot(t, testSnapshot(newLineageID, 0))
	seedSerializedLineage(t, cache, newLineageID, data)
	exporter := tracetest.NewInMemoryExporter()
	provider := tracesdk.NewTracerProvider(tracesdk.WithSyncer(exporter))
	t.Cleanup(func() { _ = provider.Shutdown(t.Context()) })
	var logs bytes.Buffer
	handler, err := newSnapshotHandler(
		cache,
		slog.New(slog.NewJSONHandler(&logs, nil)),
		provider.Tracer("test/snapshot"),
		metricnoop.NewMeterProvider().Meter("test/snapshot"),
	)
	if err != nil {
		t.Fatalf("newSnapshotHandler() error = %v", err)
	}

	ctx, cancel := context.WithCancel(t.Context())
	ctx, root := provider.Tracer("test/root").Start(ctx, "http.server", trace.WithSpanKind(trace.SpanKindServer))
	request := httptest.NewRequest(http.MethodGet, "/snapshot", http.NoBody).WithContext(ctx)
	writer := &shortResponseWriter{
		header:  make(http.Header),
		onWrite: cancel,
		failure: context.Canceled,
	}
	handler.ServeHTTP(writer, request)
	root.End()

	if writer.status != http.StatusOK || writer.writes != 1 {
		t.Fatalf("status=%d writes=%d, want committed 200 and one write", writer.status, writer.writes)
	}
	if countLines(logs.String()) != 1 || !strings.Contains(logs.String(), `"error.type":"canceled"`) {
		t.Fatalf("write failure logs = %q", logs.String())
	}
	if !strings.Contains(logs.String(), `"snapshot.component":"response"`) ||
		!strings.Contains(logs.String(), `"http.response.status_code":200`) ||
		!strings.Contains(logs.String(), `"lineage.id":"`+newLineageID+`"`) {
		t.Fatalf("write failure detail logs = %q", logs.String())
	}
	for _, span := range exporter.GetSpans() {
		if span.Name == "http.server" && span.Status.Code != codes.Error {
			t.Fatalf("root status = %v, want error", span.Status)
		}
	}
}

func TestSnapshotExporterFailureDoesNotChangeResponse(t *testing.T) {
	data := mustMarshalSnapshot(t, testSnapshot(newLineageID, 0))
	cache := newFakeMemcache()
	seedSerializedLineage(t, cache, newLineageID, data)
	provider := tracesdk.NewTracerProvider(tracesdk.WithSyncer(snapshotFailingExporter{}))
	t.Cleanup(func() { _ = provider.Shutdown(t.Context()) })
	handler, err := newSnapshotHandler(
		cache,
		slog.New(slog.NewJSONHandler(io.Discard, nil)),
		provider.Tracer("test/snapshot"),
		metricnoop.NewMeterProvider().Meter("test/snapshot"),
	)
	if err != nil {
		t.Fatalf("newSnapshotHandler() error = %v", err)
	}
	mux := http.NewServeMux()
	mux.Handle("/snapshot", handler)
	instrumented, err := telemetry.HTTPHandler(
		mux,
		provider,
		metricnoop.NewMeterProvider(),
		propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}),
	)
	if err != nil {
		t.Fatalf("telemetry.HTTPHandler() error = %v", err)
	}

	response := httptest.NewRecorder()
	instrumented.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/snapshot", http.NoBody))
	if response.Code != http.StatusOK || !bytes.Equal(response.Body.Bytes(), data) {
		t.Fatalf("response after export failure: status=%d body=%s", response.Code, response.Body)
	}
}

func TestSnapshotTelemetryUsesExistingServerRootAndBoundedMetrics(t *testing.T) {
	exporter := tracetest.NewInMemoryExporter()
	tracerProvider := tracesdk.NewTracerProvider(tracesdk.WithSyncer(exporter))
	t.Cleanup(func() { _ = tracerProvider.Shutdown(t.Context()) })
	reader := metricsdk.NewManualReader()
	meterProvider := metricsdk.NewMeterProvider(metricsdk.WithReader(reader))
	t.Cleanup(func() { _ = meterProvider.Shutdown(t.Context()) })
	cache := newFakeMemcache()
	data := mustMarshalSnapshot(t, testSnapshot(newLineageID, blockSize+100))
	metadata := seedSerializedLineage(t, cache, newLineageID, data)
	var logs bytes.Buffer
	handler, err := newSnapshotHandler(
		cache,
		slog.New(slog.NewJSONHandler(&logs, nil)),
		tracerProvider.Tracer("test/snapshot"),
		meterProvider.Meter("test/snapshot"),
	)
	if err != nil {
		t.Fatalf("newSnapshotHandler() error = %v", err)
	}
	mux := http.NewServeMux()
	mux.Handle("/snapshot", handler)
	instrumented, err := telemetry.HTTPHandler(
		mux,
		tracerProvider,
		meterProvider,
		propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}),
	)
	if err != nil {
		t.Fatalf("telemetry.HTTPHandler() error = %v", err)
	}

	success := httptest.NewRecorder()
	instrumented.ServeHTTP(success, httptest.NewRequest(http.MethodGet, "/snapshot", http.NoBody))
	delete(cache.items, blockKey(newLineageID, metadata.BlockCount-1))
	missing := httptest.NewRecorder()
	instrumented.ServeHTTP(missing, httptest.NewRequest(http.MethodGet, "/snapshot", http.NoBody))
	seedSerializedLineage(t, cache, newLineageID, data)
	cache.items[blockKey(newLineageID, 0)].Value[0] = '!'
	corrupt := httptest.NewRecorder()
	instrumented.ServeHTTP(corrupt, httptest.NewRequest(http.MethodGet, "/snapshot", http.NoBody))
	if success.Code != http.StatusOK || missing.Code != http.StatusGone || corrupt.Code != http.StatusInternalServerError {
		t.Fatalf("statuses success=%d missing=%d corrupt=%d", success.Code, missing.Code, corrupt.Code)
	}
	if countLines(logs.String()) != 2 {
		t.Fatalf("logs = %q, want two failed-request logs", logs.String())
	}

	spans := exporter.GetSpans()
	type spanIdentity struct {
		traceID trace.TraceID
		status  codes.Code
	}
	serverIDs := make(map[trace.SpanID]spanIdentity)
	semanticIDs := make(map[trace.SpanID]trace.TraceID)
	failedBlock := false
	failedSerialization := false
	for _, span := range spans {
		if span.SpanKind == trace.SpanKindServer {
			serverIDs[span.SpanContext.SpanID()] = spanIdentity{traceID: span.SpanContext.TraceID(), status: span.Status.Code}
		}
		switch span.Name {
		case "active-lineage.lookup", "lineage.completion.read", "lineage.block.read", "serialize.snapshot":
			semanticIDs[span.SpanContext.SpanID()] = span.SpanContext.TraceID()
			if span.Name == "lineage.block.read" && span.Status.Code == codes.Error {
				failedBlock = true
			}
			if span.Name == "serialize.snapshot" && span.Status.Code == codes.Error {
				failedSerialization = true
			}
		}

		for _, item := range span.Attributes {
			value := item.Value.Emit()
			if strings.Contains(value, lineageKeyPrefix) || strings.Contains(value, `"schemaVersion"`) {
				t.Fatalf("span %q disclosed key or payload: %s=%s", span.Name, item.Key, value)
			}
		}
	}
	for _, span := range spans {
		switch span.Name {
		case "active-lineage.lookup", "lineage.completion.read", "lineage.block.read", "serialize.snapshot":
			server, exists := serverIDs[span.Parent.SpanID()]
			if !exists || server.traceID != span.Parent.TraceID() || server.traceID != span.SpanContext.TraceID() {
				t.Fatalf("semantic span %q parent %v is not a server root", span.Name, span.Parent)
			}
		case "memcached.get":
			semanticTrace, exists := semanticIDs[span.Parent.SpanID()]
			if !exists || semanticTrace != span.Parent.TraceID() || semanticTrace != span.SpanContext.TraceID() {
				t.Fatalf("memcached.get parent %v is not a semantic read", span.Parent)
			}
		}
	}
	if len(serverIDs) != 3 || !failedBlock || !failedSerialization {
		t.Fatalf("server roots=%d failedBlock=%v failedSerialization=%v spans=%v",
			len(serverIDs), failedBlock, failedSerialization, snapshotSpanNames(spans))
	}
	var errorRoots int
	for _, server := range serverIDs {
		if server.status == codes.Error {
			errorRoots++
		}
	}
	if errorRoots != 2 {
		t.Fatalf("error server roots = %d, want 2", errorRoots)
	}

	assertSnapshotMetrics(t, reader)
}

type snapshotFailingExporter struct{}

func (snapshotFailingExporter) ExportSpans(context.Context, []tracesdk.ReadOnlySpan) error {
	return errors.New("collector unavailable")
}

func (snapshotFailingExporter) Shutdown(context.Context) error {
	return nil
}

type shortResponseWriter struct {
	header  http.Header
	status  int
	writes  int
	onWrite func()
	failure error
}

func (writer *shortResponseWriter) Header() http.Header {
	return writer.header
}

func (writer *shortResponseWriter) WriteHeader(status int) {
	if writer.status == 0 {
		writer.status = status
	}
}

func (writer *shortResponseWriter) Write(body []byte) (int, error) {
	writer.writes++
	if writer.onWrite != nil {
		writer.onWrite()
	}

	return len(body) / 2, writer.failure
}

func testSnapshotHTTPHandler(t *testing.T, cache cacheClient, logs io.Writer) *SnapshotHandler {
	t.Helper()
	handler, err := newSnapshotHandler(
		cache,
		slog.New(slog.NewJSONHandler(logs, nil)),
		tracenoop.NewTracerProvider().Tracer("test/snapshot"),
		metricnoop.NewMeterProvider().Meter("test/snapshot"),
	)
	if err != nil {
		t.Fatalf("newSnapshotHandler() error = %v", err)
	}

	return handler
}

func mustMarshalSnapshot(t *testing.T, value snapshot.Snapshot) []byte {
	t.Helper()
	data, err := snapshot.Marshal(value)
	if err != nil {
		t.Fatalf("snapshot.Marshal() error = %v", err)
	}

	return data
}

func assertSnapshotHeaders(t *testing.T, response *httptest.ResponseRecorder, length int) {
	t.Helper()
	if response.Header().Get("Cache-Control") != "no-store" ||
		response.Header().Get("Content-Type") != "application/json" ||
		response.Header().Get("Content-Length") != strconv.Itoa(length) {
		t.Fatalf("snapshot headers = %#v", response.Header())
	}
	for _, name := range []string{"Content-Encoding", "Accept-Ranges", "Content-Range"} {
		if response.Header().Get(name) != "" {
			t.Fatalf("%s = %q, want absent", name, response.Header().Get(name))
		}
	}
}

func assertSnapshotMetrics(t *testing.T, reader *metricsdk.ManualReader) {
	t.Helper()
	var collected metricdata.ResourceMetrics
	if err := reader.Collect(t.Context(), &collected); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	httpStatuses := make(map[int]int64)
	cacheOutcomes := make(map[string]int64)
	for _, scope := range collected.ScopeMetrics {
		for _, item := range scope.Metrics {
			sum, ok := item.Data.(metricdata.Sum[int64])
			if !ok {
				continue
			}
			for _, point := range sum.DataPoints {
				switch item.Name {
				case "http.server.request.count":
					attributes := point.Attributes.ToSlice()
					if attributeValue(attributes, "http.route") != "/snapshot" {
						t.Fatalf("HTTP metric route attributes = %#v", attributes)
					}
					status, err := strconv.Atoi(attributeValue(attributes, "http.response.status_code"))
					if err != nil {
						t.Fatalf("HTTP metric status: %v", err)
					}
					httpStatuses[status] += point.Value
				case "cache.operation.count":
					attributes := point.Attributes.ToSlice()
					if attributeValue(attributes, "cache.operation") != "get" {
						t.Fatalf("cache operation attributes = %#v", attributes)
					}
					cacheOutcomes[attributeValue(attributes, "cache.outcome")] += point.Value
				}
			}
		}
	}

	if httpStatuses[http.StatusOK] != 1 || httpStatuses[http.StatusGone] != 1 ||
		httpStatuses[http.StatusInternalServerError] != 1 {
		t.Fatalf("HTTP request statuses = %#v", httpStatuses)
	}
	if cacheOutcomes[cacheOutcomeHit] == 0 || cacheOutcomes[cacheOutcomeMiss] != 1 {
		t.Fatalf("cache outcomes = %#v", cacheOutcomes)
	}
}

func attributeValue(attributes []attribute.KeyValue, key attribute.Key) string {
	for _, item := range attributes {
		if item.Key == key {
			return item.Value.Emit()
		}
	}

	return ""
}

func snapshotSpanNames(spans tracetest.SpanStubs) []string {
	names := make([]string, 0, len(spans))
	for _, span := range spans {
		names = append(names, span.Name)
	}

	return names
}
