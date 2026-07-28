package acquisition

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"git.disroot.org/federico-paolillo/four-visor.git/snapshot"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	metricsdk "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

func TestObserveConstructsFreshContractResources(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var boardCalls atomic.Int64
		transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if request.Header.Get("User-Agent") != "4Visor/0123456789abcdef0123456789abcdef01234567" {
				t.Errorf("User-Agent = %q", request.Header.Get("User-Agent"))
			}

			switch request.URL.Path {
			case "/boards.json":
				if boardCalls.Add(1) == 1 {
					return response(http.StatusOK, `{"boards":[{"board":"z","title":"Z"},{"board":"a","title":"A"}]}`, nil), nil
				}

				return response(http.StatusOK, `{"boards":[{"board":"q","title":"Q"}]}`, nil), nil
			case "/z/catalog.json":
				return response(http.StatusOK, `[{"page":1,"extra":{"kept":true},"threads":[{"no":9},{"no":8}]}]`, nil), nil
			case "/a/catalog.json":
				return response(http.StatusServiceUnavailable, `never logged`, nil), nil
			case "/q/catalog.json":
				return response(http.StatusOK, `[]`, nil), nil
			default:
				t.Fatalf("unexpected request path %q", request.URL.Path)

				return nil, nil
			}
		})
		policy := defaultPolicy()
		policy.MaxRetries = 0
		client := fakeClient(t, policy, transport, io.Discard, nil, nil)
		ctx, cancel := context.WithTimeout(t.Context(), time.Hour)
		defer cancel()

		first, err := client.Observe(ctx)
		if err != nil {
			t.Fatalf("first Observe() error = %v", err)
		}
		if err := snapshot.Validate(completeSnapshot(first)); err != nil {
			t.Fatalf("first snapshot contract error = %v", err)
		}
		firstItems := *first.Items
		if string(firstItems[0].Board) != `{"board":"z","title":"Z"}` ||
			firstItems[0].Catalog.State != snapshot.StatePresent ||
			len(*firstItems[0].Catalog.Pages) != 1 ||
			firstItems[1].Catalog.State != snapshot.StateFailed {
			t.Fatalf("first Observe() = %#v", firstItems)
		}

		second, err := client.Observe(ctx)
		if err != nil {
			t.Fatalf("second Observe() error = %v", err)
		}
		if err := snapshot.Validate(completeSnapshot(second)); err != nil {
			t.Fatalf("second snapshot contract error = %v", err)
		}
		secondItems := *second.Items
		if len(secondItems) != 1 || string(secondItems[0].Board) != `{"board":"q","title":"Q"}` {
			t.Fatalf("second observation reused prior resources: %#v", secondItems)
		}
	})
}

func TestAcquisitionTelemetryIsBoundedAndSecretFree(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		spanExporter := tracetest.NewInMemoryExporter()
		tracerProvider := tracesdk.NewTracerProvider(tracesdk.WithSyncer(spanExporter))
		reader := metricsdk.NewManualReader()
		meterProvider := metricsdk.NewMeterProvider(metricsdk.WithReader(reader))
		var logs bytes.Buffer
		const boardID = "identifier-must-not-leak"
		const responseSecret = "response-body-secret"
		const transportSecret = "transport-error-secret"
		var catalogCalls atomic.Int64

		transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if request.URL.Path == "/boards.json" {
				return response(http.StatusOK, `{"boards":[{"board":"`+boardID+`"}]}`, nil), nil
			}

			if catalogCalls.Add(1) == 1 {
				return response(http.StatusTooManyRequests, responseSecret, http.Header{"Retry-After": []string{"0"}}), nil
			}

			return nil, errors.New(transportSecret)
		})
		policy := defaultPolicy()
		policy.MaxRetries = 1
		client := fakeClient(
			t,
			policy,
			transport,
			&logs,
			tracerProvider.Tracer("test/acquisition"),
			meterProvider.Meter("test/acquisition"),
		)
		ctx, cancel := context.WithTimeout(t.Context(), time.Hour)
		defer cancel()
		ctx, root := tracerProvider.Tracer("test/root").Start(ctx, "lineage.sync")

		boards, err := client.Observe(ctx)
		root.End()
		if err != nil || (*boards.Items)[0].Catalog.State != snapshot.StateFailed {
			t.Fatalf("Observe() = %#v, %v", boards, err)
		}

		assertAcquisitionSpans(t, spanExporter.GetSpans(), root.SpanContext())
		assertAcquisitionMetrics(t, reader)
		if strings.Count(strings.TrimSpace(logs.String()), "\n") != 0 || strings.TrimSpace(logs.String()) == "" {
			t.Fatalf("terminal log count != 1: %q", logs.String())
		}
		assertTerminalLog(t, logs.Bytes())
		for _, forbidden := range []string{boardID, responseSecret, transportSecret, "example.test", "/catalog.json"} {
			if strings.Contains(logs.String(), forbidden) {
				t.Fatalf("log disclosed %q: %s", forbidden, logs.String())
			}
		}

		if err := meterProvider.Shutdown(t.Context()); err != nil {
			t.Fatalf("meter shutdown error = %v", err)
		}
		if err := tracerProvider.Shutdown(t.Context()); err != nil {
			t.Fatalf("tracer shutdown error = %v", err)
		}
	})
}

func TestHTTPServerFailurePaths(t *testing.T) {
	t.Run("ordered catalog succeeds", func(t *testing.T) {
		server := loopbackServer(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			switch request.URL.Path {
			case "/boards.json":
				_, _ = io.WriteString(writer, `{"boards":[{"board":"a","nested":{"kept":true}}]}`)
			case "/a/catalog.json":
				_, _ = io.WriteString(writer, `[{"page":1,"label":"first","threads":[{"no":2}]},{"page":2,"label":"second","threads":[{"no":1}]}]`)
			default:
				http.NotFound(writer, request)
			}
		}))
		policy := defaultPolicy()
		policy.MaxRetries = 0
		client := serverClient(t, server, policy, io.Discard)
		ctx, cancel := context.WithTimeout(t.Context(), 3*time.Second)
		defer cancel()

		boards, err := client.Observe(ctx)
		if err != nil || boards.State != snapshot.StatePresent {
			t.Fatalf("Observe() = %#v, %v", boards, err)
		}
		pages := *(*boards.Items)[0].Catalog.Pages
		if len(pages) != 2 || len(pages[0].Threads) != 1 || len(pages[1].Threads) != 1 ||
			!bytes.Contains(pages[0].Metadata, []byte(`"label":"first"`)) ||
			!bytes.Contains(pages[1].Metadata, []byte(`"label":"second"`)) {
			t.Fatalf("catalog pages changed: %#v", pages)
		}
	})

	t.Run("rate limit retries with User-Agent", func(t *testing.T) {
		var calls atomic.Int64
		server := loopbackServer(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			if request.Header.Get("User-Agent") != "4Visor/0123456789abcdef0123456789abcdef01234567" {
				t.Errorf("User-Agent = %q", request.Header.Get("User-Agent"))
			}
			if calls.Add(1) == 1 {
				writer.Header().Set("Retry-After", "0")
				writer.WriteHeader(http.StatusTooManyRequests)

				return
			}
			_, _ = io.WriteString(writer, `{"boards":[]}`)
		}))
		policy := defaultPolicy()
		policy.MaxRetries = 1
		client := serverClient(t, server, policy, io.Discard)
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()

		boards, err := client.Observe(ctx)
		if err != nil || boards.State != snapshot.StatePresent || calls.Load() != 2 {
			t.Fatalf("Observe() = %#v, %v calls=%d", boards, err, calls.Load())
		}
	})

	t.Run("permanent HTTP failure degrades", func(t *testing.T) {
		server := loopbackServer(t, http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			http.Error(writer, "private body", http.StatusServiceUnavailable)
		}))
		policy := defaultPolicy()
		policy.MaxRetries = 0
		client := serverClient(t, server, policy, io.Discard)
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()

		boards, err := client.Observe(ctx)
		if err != nil || boards.State != snapshot.StateFailed {
			t.Fatalf("Observe() = %#v, %v", boards, err)
		}
	})

	t.Run("slow response times out technically", func(t *testing.T) {
		server := loopbackServer(t, http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
			<-request.Context().Done()
		}))
		policy := defaultPolicy()
		policy.MaxRetries = 0
		policy.RequestTimeout = 20 * time.Millisecond
		client := serverClient(t, server, policy, io.Discard)
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()

		boards, err := client.Observe(ctx)
		if err != nil || boards.State != snapshot.StateFailed {
			t.Fatalf("Observe() = %#v, %v", boards, err)
		}
	})

	t.Run("lineage deadline degrades", func(t *testing.T) {
		server := loopbackServer(t, http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
			<-request.Context().Done()
		}))
		policy := defaultPolicy()
		policy.MaxRetries = 0
		client := serverClient(t, server, policy, io.Discard)
		ctx, cancel := context.WithTimeout(t.Context(), 20*time.Millisecond)
		defer cancel()

		boards, err := client.Observe(ctx)
		if err != nil || boards.State != snapshot.StateFailed {
			t.Fatalf("Observe() = %#v, %v", boards, err)
		}
	})

	t.Run("disconnect degrades", func(t *testing.T) {
		server := loopbackServer(t, http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
			connection, _, err := writer.(http.Hijacker).Hijack()
			if err != nil {
				t.Errorf("Hijack() error = %v", err)

				return
			}
			_ = connection.Close()
		}))
		policy := defaultPolicy()
		policy.MaxRetries = 0
		client := serverClient(t, server, policy, io.Discard)
		ctx, cancel := context.WithTimeout(t.Context(), time.Second)
		defer cancel()

		boards, err := client.Observe(ctx)
		if err != nil || boards.State != snapshot.StateFailed {
			t.Fatalf("Observe() = %#v, %v", boards, err)
		}
	})

	t.Run("external cancellation aborts", func(t *testing.T) {
		entered := make(chan struct{})
		server := loopbackServer(t, http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
			close(entered)
			<-request.Context().Done()
		}))
		policy := defaultPolicy()
		policy.MaxRetries = 0
		client := serverClient(t, server, policy, io.Discard)
		deadlineCtx, deadlineCancel := context.WithTimeout(t.Context(), time.Second)
		defer deadlineCancel()
		ctx, cancel := context.WithCancelCause(deadlineCtx)
		cause := errors.New("shutdown")
		result := make(chan error, 1)
		go func() {
			boards, err := client.Observe(ctx)
			if boards.State != "" || boards.Items != nil {
				result <- errors.New("external cancellation returned partial boards")

				return
			}
			result <- err
		}()

		<-entered
		cancel(cause)
		if err := <-result; !errors.Is(err, cause) {
			t.Fatalf("Observe() error = %v, want cancellation cause", err)
		}
	})
}

func assertAcquisitionSpans(t *testing.T, spans tracetest.SpanStubs, root trace.SpanContext) {
	t.Helper()
	children := 0
	failed := 0
	for _, span := range spans {
		if span.Name == "lineage.sync" {
			continue
		}
		children++
		if span.Parent.SpanID() != root.SpanID() || span.Parent.TraceID() != root.TraceID() {
			t.Fatalf("span %q parent = %v, want lineage root %v", span.Name, span.Parent, root)
		}
		if span.Status.Code == codes.Error {
			failed++
			attributes := attributeValues(span.Attributes)
			wantCause, ok := map[string]string{
				errorRateLimit: causeHTTPStatus,
				errorNetwork:   causeNetwork,
			}[attributes["error.type"]]
			if !ok {
				t.Fatalf("span %q error type = %q", span.Name, attributes["error.type"])
			}
			if attributes["error.cause.type"] != wantCause {
				t.Fatalf("span %q cause type = %q, want %q", span.Name, attributes["error.cause.type"], wantCause)
			}
			if len(span.Events) != 1 || span.Events[0].Name != "exception" {
				t.Fatalf("span %q events = %#v, want one exception", span.Name, span.Events)
			}
			eventAttributes := attributeValues(span.Events[0].Attributes)
			if eventAttributes["error.cause.type"] != wantCause ||
				eventAttributes["exception.type"] != "*acquisition.requestError" ||
				!strings.HasPrefix(eventAttributes["exception.message"], "upstream acquisition ") {
				t.Fatalf("span %q exception attributes = %#v", span.Name, eventAttributes)
			}
			assertValuesExclude(t, eventAttributes,
				"identifier-must-not-leak", "response-body-secret", "transport-error-secret", "example.test")
		}
		for _, item := range span.Attributes {
			value := item.Value.Emit()
			if strings.Contains(value, "identifier-must-not-leak") || strings.Contains(value, "example.test") {
				t.Fatalf("span attribute disclosed request identifier: %s=%s", item.Key, value)
			}
		}
	}
	if children != 3 || failed != 2 {
		t.Fatalf("child spans=%d failed=%d, want 3 and 2; spans=%v", children, failed, spans)
	}
}

func assertTerminalLog(t *testing.T, data []byte) {
	t.Helper()
	var record map[string]any
	if err := json.Unmarshal(data, &record); err != nil {
		t.Fatalf("decode terminal log: %v", err)
	}
	if record["resource.type"] != catalogResource || record["error.type"] != errorNetwork ||
		record["error.cause.type"] != causeNetwork {
		t.Fatalf("terminal log fields = %#v", record)
	}
	if _, exists := record["error"]; exists {
		t.Fatalf("terminal log exported uncontrolled error value: %#v", record)
	}
}

func attributeValues(attributes []attribute.KeyValue) map[string]string {
	values := make(map[string]string, len(attributes))
	for _, item := range attributes {
		values[string(item.Key)] = item.Value.Emit()
	}

	return values
}

func assertValuesExclude(t *testing.T, values map[string]string, forbidden ...string) {
	t.Helper()
	for key, value := range values {
		for _, secret := range forbidden {
			if strings.Contains(value, secret) {
				t.Fatalf("attribute %q disclosed %q: %q", key, secret, value)
			}
		}
	}
}

func assertAcquisitionMetrics(t *testing.T, reader *metricsdk.ManualReader) {
	t.Helper()
	var collected metricdata.ResourceMetrics
	if err := reader.Collect(t.Context(), &collected); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}

	metricCount := 0
	pointCount := uint64(0)
	for _, scope := range collected.ScopeMetrics {
		for _, item := range scope.Metrics {
			metricCount++
			switch data := item.Data.(type) {
			case metricdata.Sum[int64]:
				for _, point := range data.DataPoints {
					pointCount += uint64(point.Value)
					assertMetricAttributes(t, point.Attributes.ToSlice())
				}
			case metricdata.Histogram[float64]:
				for _, point := range data.DataPoints {
					pointCount += point.Count
					assertMetricAttributes(t, point.Attributes.ToSlice())
				}
			default:
				t.Fatalf("unexpected metric data type %T", item.Data)
			}
		}
	}
	if metricCount != 2 || pointCount != 6 {
		t.Fatalf("metric count=%d total points=%d, want 2 metrics across 3 attempts", metricCount, pointCount)
	}
}

func assertMetricAttributes(t *testing.T, attributes []attribute.KeyValue) {
	t.Helper()
	if len(attributes) != 3 {
		t.Fatalf("metric attributes = %#v, want exactly 3", attributes)
	}
	for _, item := range attributes {
		if item.Key != "resource.type" && item.Key != "error.type" && item.Key != "http.response.status_code" {
			t.Fatalf("unexpected metric attribute %q", item.Key)
		}
	}
}

func loopbackServer(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	var listener net.Listener
	var err error
	for port := 65180; port <= 65189; port++ {
		listener, err = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			break
		}
	}
	if err != nil {
		t.Fatalf("listen for acquisition test server: %v", err)
	}

	server := httptest.NewUnstartedServer(handler)
	server.Listener = listener
	server.Start()
	t.Cleanup(server.Close)

	return server
}

func serverClient(t *testing.T, server *httptest.Server, policy Policy, logs io.Writer) *Client {
	t.Helper()
	client, err := newClient(
		policy,
		"4Visor/0123456789abcdef0123456789abcdef01234567",
		server.URL,
		server.Client(),
		slog.New(slog.NewJSONHandler(logs, nil)),
		tracenoop.NewTracerProvider().Tracer("test/acquisition"),
		metricnoop.NewMeterProvider().Meter("test/acquisition"),
	)
	if err != nil {
		t.Fatalf("newClient() error = %v", err)
	}

	return client
}
