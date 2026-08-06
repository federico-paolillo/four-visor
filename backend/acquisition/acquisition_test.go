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
			case "/z/thread/9.json":
				return response(http.StatusOK, `{"posts":[{"no":9,"com":"<b>kept</b>","tim":123}]}`, nil), nil
			case "/z/thread/8.json":
				return response(http.StatusNotFound, `gone`, nil), nil
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

		first, err := client.Observe(ctx, testLineageID)
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

		second, err := client.Observe(ctx, testLineageID)
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

		boards, err := client.Observe(ctx, testLineageID)
		root.End()
		if err != nil || (*boards.Items)[0].Catalog.State != snapshot.StateFailed {
			t.Fatalf("Observe() = %#v, %v", boards, err)
		}

		assertAcquisitionSpans(t, spanExporter.GetSpans(), root.SpanContext())
		assertAcquisitionMetrics(t, reader, 1)
		assertTerminalLogs(t, logs.Bytes())
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

func TestTerminalFailuresAggregatePerLineage(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var logs bytes.Buffer
		const secret = "resource-identifier-must-not-leak"
		transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
			if request.URL.Path == "/boards.json" {
				return response(http.StatusOK,
					`{"boards":[{"board":"`+secret+`-1"},{"board":"`+secret+`-2"},{"board":"`+secret+`-3"}]}`,
					nil,
				), nil
			}

			return response(http.StatusServiceUnavailable, "response-value-must-not-leak", nil), nil
		})
		policy := defaultPolicy()
		policy.MaxRetries = 0
		client := fakeClient(t, policy, transport, &logs, nil, nil)
		ctx, cancel := context.WithTimeout(t.Context(), time.Hour)
		defer cancel()

		boards, err := client.Observe(ctx, testLineageID)
		if err != nil || boards.FailedResourceCount() != 3 {
			t.Fatalf("Observe() = %#v, %v", boards, err)
		}
		if count := strings.Count(strings.TrimSpace(logs.String()), "\n") + 1; count != 4 ||
			strings.Count(logs.String(), `"msg":"upstream acquisition failed"`) != 3 {
			t.Fatalf("terminal log count = %d: %s", count, logs.String())
		}

		lines := bytes.Split(bytes.TrimSpace(logs.Bytes()), []byte{'\n'})
		var record map[string]any
		if err := json.Unmarshal(lines[len(lines)-1], &record); err != nil {
			t.Fatalf("decode aggregate log: %v", err)
		}
		if record["failure.count"] != float64(3) || record["resource.type"] != catalogResource ||
			record["failure.stage"] != stageRequest || record["error.type"] != errorHTTP ||
			record["error.cause.type"] != causeHTTPStatus ||
			record["http.response.status_code"] != float64(http.StatusServiceUnavailable) ||
			record["retry.attempt"] != float64(0) || record["retry.exhausted"] != false {
			t.Fatalf("aggregate fields = %#v", record)
		}
		for _, forbidden := range []string{secret, "response-value-must-not-leak", "example.test", "/catalog.json"} {
			if strings.Contains(logs.String(), forbidden) {
				t.Fatalf("aggregate disclosed %q: %s", forbidden, logs.String())
			}
		}
	})
}

func TestTerminalFetchWarningCardinality(t *testing.T) {
	t.Run("boards failure", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			var logs bytes.Buffer
			const responseSecret = "boards-response-must-not-leak"
			transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
				return response(http.StatusServiceUnavailable, responseSecret, nil), nil
			})
			policy := defaultPolicy()
			policy.MaxRetries = 0
			client := fakeClient(t, policy, transport, &logs, nil, nil)
			ctx, cancel := context.WithTimeout(t.Context(), time.Hour)
			defer cancel()

			boards, err := client.Observe(ctx, testLineageID)
			if err != nil || boards.State != snapshot.StateFailed {
				t.Fatalf("Observe() = %#v, %v", boards, err)
			}
			if strings.Count(logs.String(), `"msg":"upstream acquisition failed"`) != 1 ||
				strings.Count(logs.String(), `"msg":"upstream acquisition failures summarized"`) != 1 ||
				!strings.Contains(logs.String(), `"resource.type":"boards"`) ||
				strings.Contains(logs.String(), responseSecret) {
				t.Fatalf("boards failure logs = %s", logs.String())
			}
		})
	})

	t.Run("thread failure", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			var logs bytes.Buffer
			const boardID = "thread-board-must-not-leak"
			const threadID = "424242"
			const responseSecret = "thread-response-must-not-leak"
			var threadCalls atomic.Int64
			transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
				switch request.URL.Path {
				case "/boards.json":
					return response(http.StatusOK, `{"boards":[{"board":"`+boardID+`"}]}`, nil), nil
				case "/" + boardID + "/catalog.json":
					return response(http.StatusOK, `[{"page":1,"threads":[{"no":`+threadID+`}]}]`, nil), nil
				case "/" + boardID + "/thread/" + threadID + ".json":
					threadCalls.Add(1)

					return response(http.StatusNotFound, responseSecret, nil), nil
				default:
					t.Fatalf("unexpected request path %q", request.URL.Path)

					return nil, nil
				}
			})
			policy := defaultPolicy()
			policy.MaxRetries = 0
			client := fakeClient(t, policy, transport, &logs, nil, nil)
			ctx, cancel := context.WithTimeout(t.Context(), time.Hour)
			defer cancel()

			boards, err := client.Observe(ctx, testLineageID)
			if err != nil || boards.FailedResourceCount() != 1 || threadCalls.Load() != 1 {
				t.Fatalf("Observe() = %#v, %v failures=%d calls=%d",
					boards, err, boards.FailedResourceCount(), threadCalls.Load())
			}
			if strings.Count(logs.String(), `"msg":"upstream acquisition failed"`) != 1 ||
				strings.Count(logs.String(), `"msg":"upstream acquisition failures summarized"`) != 1 ||
				!strings.Contains(logs.String(), `"resource.type":"thread"`) ||
				!strings.Contains(logs.String(), `"http.response.status_code":404`) {
				t.Fatalf("thread failure logs = %s", logs.String())
			}
			for _, forbidden := range []string{boardID, threadID, responseSecret, "/thread/"} {
				if strings.Contains(logs.String(), forbidden) {
					t.Fatalf("thread failure log disclosed %q: %s", forbidden, logs.String())
				}
			}
		})
	})

	t.Run("malformed summary is aggregate only", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			var logs bytes.Buffer
			const boardID = "malformed-board-must-not-leak"
			const summarySecret = "malformed-summary-must-not-leak"
			var threadCalls atomic.Int64
			transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
				switch request.URL.Path {
				case "/boards.json":
					return response(http.StatusOK, `{"boards":[{"board":"`+boardID+`"}]}`, nil), nil
				case "/" + boardID + "/catalog.json":
					return response(http.StatusOK,
						`[{"page":1,"threads":[{"marker":"`+summarySecret+`"}]}]`, nil), nil
				default:
					threadCalls.Add(1)

					return response(http.StatusInternalServerError, "must not be called", nil), nil
				}
			})
			policy := defaultPolicy()
			policy.MaxRetries = 0
			client := fakeClient(t, policy, transport, &logs, nil, nil)
			ctx, cancel := context.WithTimeout(t.Context(), time.Hour)
			defer cancel()

			boards, err := client.Observe(ctx, testLineageID)
			if err != nil || boards.FailedResourceCount() != 1 || threadCalls.Load() != 0 {
				t.Fatalf("Observe() = %#v, %v failures=%d calls=%d",
					boards, err, boards.FailedResourceCount(), threadCalls.Load())
			}
			if strings.Contains(logs.String(), `"msg":"upstream acquisition failed"`) ||
				strings.Count(logs.String(), `"msg":"upstream acquisition failures summarized"`) != 1 ||
				!strings.Contains(logs.String(), `"resource.type":"thread"`) ||
				!strings.Contains(logs.String(), `"failure.stage":"decode"`) ||
				!strings.Contains(logs.String(), `"failure.count":1`) {
				t.Fatalf("malformed summary logs = %s", logs.String())
			}
			for _, forbidden := range []string{boardID, summarySecret, "must not be called"} {
				if strings.Contains(logs.String(), forbidden) {
					t.Fatalf("malformed summary log disclosed %q: %s", forbidden, logs.String())
				}
			}
		})
	})
}

func TestFailureSummaryUsesControlledStagesAndCauses(t *testing.T) {
	summary := newFailureSummary(testLineageID, nil, nil)
	summary.add(boardsResource, lineageDeadlineFailure(stageQueue))
	summary.add(catalogResource, lineageDeadlineFailure(stageRate))
	summary.add(threadResource, lineageDeadlineFailure(stageConcurrency))
	summary.add(threadResource, &requestError{
		kind: errorHTTP, cause: errUnexpectedStatus, stage: stageRequest, status: http.StatusNotFound,
	})
	summary.add(threadResource, &requestError{
		kind: errorNetwork, cause: io.ErrUnexpectedEOF, stage: stageBody, retryable: true, attempt: 2, exhausted: true,
	})
	summary.add(threadResource, &requestError{
		kind: errorInvalid, cause: &json.SyntaxError{Offset: 1}, stage: stageDecode,
	})
	summary.add(threadResource, &requestError{
		kind: errorDeadline, cause: errLineageDeadline, stage: stageRetry, attempt: 1, exhausted: true,
	})

	seenStages := make(map[string]bool)
	for failure := range summary.counts {
		seenStages[failure.stage] = true
		if failure.errorType == "" || failure.causeType == "" {
			t.Fatalf("unclassified failure = %#v", failure)
		}
	}
	for _, stage := range []string{
		stageQueue, stageRate, stageConcurrency, stageRequest, stageBody, stageDecode, stageRetry,
	} {
		if !seenStages[stage] {
			t.Errorf("failure stage %q missing from %#v", stage, summary.counts)
		}
	}
}

func TestThreadCapacityWarningUsesRemainingRateBudget(t *testing.T) {
	t.Run("idle gate includes immediate and fractional slots", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			var logs bytes.Buffer
			client := fakeClient(t, defaultPolicy(), blockingTransport(), &logs, nil, nil)
			ctx, cancel := context.WithTimeout(t.Context(), 2500*time.Millisecond)
			defer cancel()

			client.warnThreadCapacity(ctx, testLineageID, make([]threadJob, 3))
			client.warnThreadCapacity(ctx, testLineageID, make([]threadJob, 4))
			if strings.Count(logs.String(), "thread acquisition exceeds remaining rate capacity") != 1 ||
				!strings.Contains(logs.String(), `"resource.queued.count":4`) ||
				!strings.Contains(logs.String(), `"resource.rate_capacity.count":3`) ||
				!strings.Contains(logs.String(), `"lineage.id":"`+testLineageID+`"`) {
				t.Fatalf("capacity logs = %s", logs.String())
			}
		})
	})

	t.Run("exact deadline boundary is excluded", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			var logs bytes.Buffer
			client := fakeClient(t, defaultPolicy(), blockingTransport(), &logs, nil, nil)
			client.lastStart = time.Now()
			ctx, cancel := context.WithDeadline(t.Context(), client.lastStart.Add(3*time.Second))
			defer cancel()

			client.warnThreadCapacity(ctx, testLineageID, make([]threadJob, 3))
			if !strings.Contains(logs.String(), `"resource.queued.count":3`) ||
				!strings.Contains(logs.String(), `"resource.rate_capacity.count":2`) {
				t.Fatalf("capacity logs = %s", logs.String())
			}
		})
	})

	t.Run("prefailed jobs are not runnable", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			var logs bytes.Buffer
			client := fakeClient(t, defaultPolicy(), blockingTransport(), &logs, nil, nil)
			ctx, cancel := context.WithTimeout(t.Context(), 1500*time.Millisecond)
			defer cancel()
			jobs := []threadJob{
				{},
				{err: &requestError{kind: errorInvalid, stage: stageDecode}},
				{},
			}

			client.warnThreadCapacity(ctx, testLineageID, jobs)
			if logs.Len() != 0 {
				t.Fatalf("capacity logs = %s", logs.String())
			}
		})
	})
}

func TestRequestTimeoutSpanUsesRequestDeadlineCause(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		spanExporter := tracetest.NewInMemoryExporter()
		tracerProvider := tracesdk.NewTracerProvider(tracesdk.WithSyncer(spanExporter))
		t.Cleanup(func() { _ = tracerProvider.Shutdown(t.Context()) })
		policy := defaultPolicy()
		policy.MaxRetries = 0
		client := fakeClient(t, policy, blockingTransport(), io.Discard,
			tracerProvider.Tracer("test/acquisition"), nil)
		ctx, cancel := context.WithTimeout(t.Context(), time.Hour)
		defer cancel()

		boards, err := client.Observe(ctx, testLineageID)
		if err != nil || boards.State != snapshot.StateFailed {
			t.Fatalf("Observe() = %#v, %v", boards, err)
		}

		for _, span := range spanExporter.GetSpans() {
			if span.Name != "fetch.boards" {
				continue
			}
			attributes := attributeValues(span.Attributes)
			if attributes["error.type"] != errorTimeout ||
				attributes["error.cause.type"] != causeRequestDeadline {
				t.Fatalf("timeout span attributes = %#v", attributes)
			}
			if len(span.Events) != 1 ||
				attributeValues(span.Events[0].Attributes)["error.cause.type"] != causeRequestDeadline {
				t.Fatalf("timeout span events = %#v", span.Events)
			}

			return
		}

		t.Fatal("fetch.boards span missing")
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

		boards, err := client.Observe(ctx, testLineageID)
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

		boards, err := client.Observe(ctx, testLineageID)
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

		boards, err := client.Observe(ctx, testLineageID)
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

		boards, err := client.Observe(ctx, testLineageID)
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

		boards, err := client.Observe(ctx, testLineageID)
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

		boards, err := client.Observe(ctx, testLineageID)
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
			boards, err := client.Observe(ctx, testLineageID)
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

func TestHTTPServerThreadBoundaries(t *testing.T) {
	tests := []struct {
		name      string
		postCount int
		wantState snapshot.State
	}{
		{name: "zero posts", postCount: 0, wantState: snapshot.StatePresent},
		{name: "250 posts", postCount: snapshot.MaximumThreadPosts, wantState: snapshot.StatePresent},
		{name: "251 posts", postCount: snapshot.MaximumThreadPosts + 1, wantState: snapshot.StateOversize},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			var threadRequests atomic.Int64
			server := loopbackServer(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				switch request.URL.Path {
				case "/boards.json":
					_, _ = io.WriteString(writer, `{"boards":[{"board":"a"}]}`)
				case "/a/catalog.json":
					_, _ = io.WriteString(writer, `[{"page":1,"threads":[{"no":42,"com":"summary"}]}]`)
				case "/a/thread/42.json":
					threadRequests.Add(1)
					_, _ = writer.Write(threadDocument(t, test.postCount))
				default:
					t.Errorf("unexpected remainder or media request %q", request.URL.Path)
					http.NotFound(writer, request)
				}
			}))
			policy := defaultPolicy()
			policy.MaxRetries = 0
			client := serverClient(t, server, policy, io.Discard)
			ctx, cancel := context.WithTimeout(t.Context(), 4*time.Second)
			defer cancel()

			boards, err := client.Observe(ctx, testLineageID)
			if err != nil {
				t.Fatalf("Observe() error = %v", err)
			}
			thread := (*(*(*boards.Items)[0].Catalog.Pages)[0].Threads[0].Thread)
			if thread.State != test.wantState || thread.Posts == nil ||
				len(*thread.Posts) != min(test.postCount, snapshot.MaximumThreadPosts) {
				t.Fatalf("thread = %#v", thread)
			}
			if threadRequests.Load() != 1 {
				t.Fatalf("thread requests = %d, want 1", threadRequests.Load())
			}
			if test.postCount > snapshot.MaximumThreadPosts &&
				bytes.Contains((*thread.Posts)[snapshot.MaximumThreadPosts-1], []byte(`"no":251`)) {
				t.Fatal("post 251 was exposed")
			}
		})
	}
}

func TestHTTPServerThreadFailures(t *testing.T) {
	t.Run("rate limit retries through shared policy", func(t *testing.T) {
		var calls atomic.Int64
		server := loopbackServer(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			switch request.URL.Path {
			case "/boards.json":
				_, _ = io.WriteString(writer, `{"boards":[{"board":"a"}]}`)
			case "/a/catalog.json":
				_, _ = io.WriteString(writer, `[{"page":1,"threads":[{"no":42}]}]`)
			case "/a/thread/42.json":
				if calls.Add(1) == 1 {
					writer.Header().Set("Retry-After", "0")
					writer.WriteHeader(http.StatusTooManyRequests)

					return
				}
				_, _ = io.WriteString(writer, `{"posts":[]}`)
			default:
				http.NotFound(writer, request)
			}
		}))
		policy := defaultPolicy()
		policy.MaxRetries = 1
		client := serverClient(t, server, policy, io.Discard)
		ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
		defer cancel()

		boards, err := client.Observe(ctx, testLineageID)
		thread := (*(*boards.Items)[0].Catalog.Pages)[0].Threads[0].Thread
		if err != nil || calls.Load() != 2 || thread == nil || thread.State != snapshot.StatePresent {
			t.Fatalf("Observe() = %#v, %v calls=%d", boards, err, calls.Load())
		}
	})

	t.Run("permanent error fails known thread", func(t *testing.T) {
		var calls atomic.Int64
		server := loopbackServer(t, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
			switch request.URL.Path {
			case "/boards.json":
				_, _ = io.WriteString(writer, `{"boards":[{"board":"a"}]}`)
			case "/a/catalog.json":
				_, _ = io.WriteString(writer, `[{"page":1,"threads":[{"no":42}]}]`)
			case "/a/thread/42.json":
				calls.Add(1)
				http.Error(writer, "private", http.StatusNotFound)
			default:
				http.NotFound(writer, request)
			}
		}))
		policy := defaultPolicy()
		policy.MaxRetries = 2
		client := serverClient(t, server, policy, io.Discard)
		ctx, cancel := context.WithTimeout(t.Context(), 4*time.Second)
		defer cancel()

		boards, err := client.Observe(ctx, testLineageID)
		thread := (*(*boards.Items)[0].Catalog.Pages)[0].Threads[0].Thread
		if err != nil || calls.Load() != 1 || thread == nil || thread.State != snapshot.StateFailed || thread.Posts != nil {
			t.Fatalf("Observe() = %#v, %v calls=%d", boards, err, calls.Load())
		}
	})
}

func TestThreadOversizeTelemetryIsBoundedAndSecretFree(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		spanExporter := tracetest.NewInMemoryExporter()
		tracerProvider := tracesdk.NewTracerProvider(tracesdk.WithSyncer(spanExporter))
		reader := metricsdk.NewManualReader()
		meterProvider := metricsdk.NewMeterProvider(metricsdk.WithReader(reader))
		var logs bytes.Buffer
		const boardID = "board-identifier-must-not-leak"
		const threadID = "424242"
		const content = "post-content-must-not-leak"

		transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
			switch request.URL.Path {
			case "/boards.json":
				return response(http.StatusOK, `{"boards":[{"board":"`+boardID+`"}]}`, nil), nil
			case "/" + boardID + "/catalog.json":
				return response(http.StatusOK, `[{"page":1,"threads":[{"no":`+threadID+`}]}]`, nil), nil
			case "/" + boardID + "/thread/" + threadID + ".json":
				posts := threadDocument(t, snapshot.MaximumThreadPosts+1)
				posts = bytes.Replace(posts, []byte("post 1"), []byte(content), 1)

				return response(http.StatusOK, string(posts), nil), nil
			default:
				t.Fatalf("unexpected request path %q", request.URL.Path)

				return nil, nil
			}
		})
		client := fakeClient(
			t,
			defaultPolicy(),
			transport,
			&logs,
			tracerProvider.Tracer("test/acquisition"),
			meterProvider.Meter("test/acquisition"),
		)
		client.logger = slog.New(slog.NewJSONHandler(&logs, &slog.HandlerOptions{Level: slog.LevelDebug}))
		ctx, cancel := context.WithTimeout(t.Context(), time.Hour)
		defer cancel()
		ctx, root := tracerProvider.Tracer("test/root").Start(ctx, "lineage.sync")

		boards, err := client.Observe(ctx, testLineageID)
		root.End()
		if err != nil || (*(*(*boards.Items)[0].Catalog.Pages)[0].Threads[0].Thread).State != snapshot.StateOversize {
			t.Fatalf("Observe() = %#v, %v", boards, err)
		}

		foundThreadSpan := false
		for _, span := range spanExporter.GetSpans() {
			if span.Name != "fetch.thread" {
				continue
			}
			foundThreadSpan = true
			attributes := attributeValues(span.Attributes)
			if attributes["resource.state"] != string(snapshot.StateOversize) ||
				attributes["posts.limit"] != fmt.Sprint(snapshot.MaximumThreadPosts) {
				t.Fatalf("thread span attributes = %#v", attributes)
			}
			assertValuesExclude(t, attributes, boardID, threadID, content)
		}
		if !foundThreadSpan {
			t.Fatal("fetch.thread span not found")
		}
		if strings.Count(strings.TrimSpace(logs.String()), "\n") != 0 ||
			!strings.Contains(logs.String(), `"level":"DEBUG"`) ||
			!strings.Contains(logs.String(), "oversized thread detected") {
			t.Fatalf("oversize logs = %q", logs.String())
		}
		for _, forbidden := range []string{boardID, threadID, content, "/thread/", "example.test"} {
			if strings.Contains(logs.String(), forbidden) {
				t.Fatalf("oversize log disclosed %q: %s", forbidden, logs.String())
			}
		}

		assertAcquisitionMetrics(t, reader, 0)
		if err := meterProvider.Shutdown(t.Context()); err != nil {
			t.Fatalf("meter shutdown error = %v", err)
		}
		if err := tracerProvider.Shutdown(t.Context()); err != nil {
			t.Fatalf("tracer shutdown error = %v", err)
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

func assertTerminalLogs(t *testing.T, data []byte) {
	t.Helper()
	lines := bytes.Split(bytes.TrimSpace(data), []byte{'\n'})
	if len(lines) != 2 {
		t.Fatalf("terminal log count = %d: %s", len(lines), data)
	}

	for index, line := range lines {
		var record map[string]any
		if err := json.Unmarshal(line, &record); err != nil {
			t.Fatalf("decode terminal log: %v", err)
		}
		if record["resource.type"] != catalogResource || record["error.type"] != errorNetwork ||
			record["error.cause.type"] != causeNetwork || record["failure.stage"] != stageRequest ||
			record["http.response.status_code"] != float64(0) || record["retry.attempt"] != float64(1) ||
			record["retry.exhausted"] != true || record["lineage.id"] != testLineageID {
			t.Fatalf("terminal log fields = %#v", record)
		}
		if _, exists := record["error"]; exists {
			t.Fatalf("terminal log exported uncontrolled error value: %#v", record)
		}

		if index == 0 && (record["level"] != "WARN" || record["msg"] != "upstream acquisition failed" ||
			record["failure.count"] != nil) {
			t.Fatalf("individual warning = %#v", record)
		}
		if index == 1 && (record["level"] != "ERROR" ||
			record["msg"] != "upstream acquisition failures summarized" || record["failure.count"] != float64(1)) {
			t.Fatalf("aggregate error = %#v", record)
		}
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

func assertAcquisitionMetrics(t *testing.T, reader *metricsdk.ManualReader, wantFailures uint64) {
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
					assertMetricAttributes(t, item.Name, point.Attributes.ToSlice())
				}
			case metricdata.Histogram[float64]:
				for _, point := range data.DataPoints {
					pointCount += point.Count
					assertMetricAttributes(t, item.Name, point.Attributes.ToSlice())
				}
			default:
				t.Fatalf("unexpected metric data type %T", item.Data)
			}
		}
	}
	wantMetrics := 2
	if wantFailures > 0 {
		wantMetrics++
	}
	if metricCount != wantMetrics || pointCount != 6+wantFailures {
		t.Fatalf("metric count=%d total points=%d, want %d metrics and %d points",
			metricCount, pointCount, wantMetrics, 6+wantFailures)
	}
}

func assertMetricAttributes(t *testing.T, name string, attributes []attribute.KeyValue) {
	t.Helper()
	allowed := map[attribute.Key]bool{
		"resource.type": true, "error.type": true, "http.response.status_code": true,
	}
	want := 3
	if name == "lineage.resource.failure.count" {
		allowed["failure.stage"] = true
		allowed["error.cause.type"] = true
		allowed["retry.attempt"] = true
		allowed["retry.exhausted"] = true
		want = 7
	}
	if len(attributes) != want {
		t.Fatalf("metric %q attributes = %#v, want exactly %d", name, attributes, want)
	}

	for _, item := range attributes {
		if !allowed[item.Key] {
			t.Fatalf("unexpected %q metric attribute %q", name, item.Key)
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
