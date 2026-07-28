package acquisition

import (
	"bytes"
	"context"
	"errors"
	"io"
	"log/slog"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"git.disroot.org/federico-paolillo/four-visor.git/snapshot"
	"go.opentelemetry.io/otel/metric"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (roundTrip roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return roundTrip(request)
}

func TestRetryBackoffAndRetryAfter(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var starts []time.Time
		attempt := 0
		transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
			starts = append(starts, time.Now())
			attempt++
			switch attempt {
			case 1:
				return nil, errors.New("temporary disconnect")
			case 2:
				return response(http.StatusTooManyRequests, ``, http.Header{"Retry-After": []string{"2"}}), nil
			default:
				return response(http.StatusOK, `{"boards":[]}`, nil), nil
			}
		})
		client := fakeClient(t, defaultPolicy(), transport, io.Discard, nil, nil)
		ctx, cancel := context.WithTimeout(t.Context(), time.Hour)
		defer cancel()

		if _, err := client.fetchBoards(ctx); err != nil {
			t.Fatalf("fetchBoards() error = %v", err)
		}
		if len(starts) != 3 {
			t.Fatalf("attempt count = %d, want 3", len(starts))
		}
		if got := starts[1].Sub(starts[0]); got != time.Second {
			t.Fatalf("first retry delay = %s, want 1s", got)
		}
		if got := starts[2].Sub(starts[1]); got != 2*time.Second {
			t.Fatalf("Retry-After delay = %s, want 2s", got)
		}
	})
}

func TestOneClientSharesRateAndConcurrencyAcrossObserveCalls(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		const boardsPerObservation = 12
		catalogRelease := make(chan struct{})
		catalogEntered := make(chan struct{}, boardsPerObservation*2)
		var active atomic.Int64
		var maximum atomic.Int64
		var startsMu sync.Mutex
		var starts []time.Time

		transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
			startsMu.Lock()
			starts = append(starts, time.Now())
			startsMu.Unlock()

			if request.URL.Path == "/boards.json" {
				return response(http.StatusOK, boardsJSON(t, boardsPerObservation), nil), nil
			}

			current := active.Add(1)
			for current > maximum.Load() && !maximum.CompareAndSwap(maximum.Load(), current) {
			}
			catalogEntered <- struct{}{}
			<-catalogRelease
			active.Add(-1)

			return response(http.StatusOK, `[]`, nil), nil
		})
		client := fakeClient(t, defaultPolicy(), transport, io.Discard, nil, nil)
		ctx, cancel := context.WithTimeout(t.Context(), time.Hour)
		defer cancel()

		results := make(chan error, 2)
		var observations sync.WaitGroup
		for range 2 {
			observations.Go(func() {
				_, err := client.Observe(ctx)
				results <- err
			})
		}

		for range client.policy.MaxConcurrency {
			<-catalogEntered
		}
		synctest.Wait()
		if got := active.Load(); got != int64(client.policy.MaxConcurrency) {
			t.Fatalf("active requests = %d, want %d", got, client.policy.MaxConcurrency)
		}
		if got := maximum.Load(); got != int64(client.policy.MaxConcurrency) {
			t.Fatalf("maximum concurrency = %d, want %d", got, client.policy.MaxConcurrency)
		}

		close(catalogRelease)
		observations.Wait()
		close(results)
		for err := range results {
			if err != nil {
				t.Fatalf("Observe() error = %v", err)
			}
		}

		startsMu.Lock()
		sort.Slice(starts, func(left, right int) bool { return starts[left].Before(starts[right]) })
		for index := 1; index < len(starts); index++ {
			if spacing := starts[index].Sub(starts[index-1]); spacing < time.Second {
				t.Fatalf("shared request spacing = %s, want at least 1s", spacing)
			}
		}
		startsMu.Unlock()
	})
}

func TestTimeoutDeadlineAndExternalCancellation(t *testing.T) {
	t.Run("request timeout is technical degradation", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			policy := defaultPolicy()
			policy.MaxRetries = 0
			client := fakeClient(t, policy, blockingTransport(), io.Discard, nil, nil)
			ctx, cancel := context.WithTimeout(t.Context(), time.Hour)
			defer cancel()

			boards, err := client.Observe(ctx)
			if err != nil || boards.State != snapshot.StateFailed {
				t.Fatalf("Observe() = %#v, %v; want failed technical degradation", boards, err)
			}
		})
	})

	t.Run("lineage deadline is degradation", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			policy := defaultPolicy()
			policy.MaxRetries = 0
			client := fakeClient(t, policy, blockingTransport(), io.Discard, nil, nil)
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()

			boards, err := client.Observe(ctx)
			if err != nil || boards.State != snapshot.StateFailed {
				t.Fatalf("Observe() = %#v, %v; want failed deadline degradation", boards, err)
			}
		})
	})

	t.Run("lineage deadline fails every known unfinished catalog", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
				if request.URL.Path == "/boards.json" {
					return response(http.StatusOK, `{"boards":[{"board":"a"},{"board":"b"}]}`, nil), nil
				}
				<-request.Context().Done()

				return nil, request.Context().Err()
			})
			policy := defaultPolicy()
			policy.MaxRetries = 0
			client := fakeClient(t, policy, transport, io.Discard, nil, nil)
			ctx, cancel := context.WithTimeout(t.Context(), 2*time.Second)
			defer cancel()

			boards, err := client.Observe(ctx)
			if err != nil || boards.State != snapshot.StatePresent {
				t.Fatalf("Observe() = %#v, %v; want present boards", boards, err)
			}
			for index, item := range *boards.Items {
				if item.Catalog == nil || item.Catalog.State != snapshot.StateFailed {
					t.Fatalf("catalog %d = %#v, want failed", index, item.Catalog)
				}
			}
		})
	})

	t.Run("external cancellation aborts partial result", func(t *testing.T) {
		entered := make(chan struct{})
		transport := roundTripFunc(func(request *http.Request) (*http.Response, error) {
			close(entered)
			<-request.Context().Done()

			return nil, request.Context().Err()
		})
		policy := defaultPolicy()
		policy.MaxRetries = 0
		client := fakeClient(t, policy, transport, io.Discard, nil, nil)
		deadlineCtx, deadlineCancel := context.WithTimeout(t.Context(), time.Hour)
		defer deadlineCancel()
		ctx, cancel := context.WithCancelCause(deadlineCtx)
		cause := errors.New("shutdown requested")

		type result struct {
			boards snapshot.Boards
			err    error
		}
		resultChannel := make(chan result, 1)
		go func() {
			boards, err := client.Observe(ctx)
			resultChannel <- result{boards: boards, err: err}
		}()

		<-entered
		cancel(cause)
		got := <-resultChannel
		if !errors.Is(got.err, cause) || got.boards.State != "" || got.boards.Items != nil {
			t.Fatalf("Observe() = %#v, %v; want no usable partial result", got.boards, got.err)
		}
	})
}

func TestPreCanceledContextDoesNotEnterTransport(t *testing.T) {
	var calls atomic.Int64
	transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls.Add(1)

		return response(http.StatusOK, `{"boards":[]}`, nil), nil
	})
	policy := defaultPolicy()
	policy.MaxRetries = 0
	client := fakeClient(t, policy, transport, io.Discard, nil, nil)
	deadlineCtx, deadlineCancel := context.WithTimeout(t.Context(), time.Hour)
	defer deadlineCancel()
	ctx, cancel := context.WithCancelCause(deadlineCtx)
	cause := errors.New("shutdown requested")
	cancel(cause)

	for range 64 {
		boards, err := client.Observe(ctx)
		if !errors.Is(err, cause) || boards.State != "" || boards.Items != nil {
			t.Fatalf("Observe() = %#v, %v; want no result and preserved cancellation", boards, err)
		}
	}
	if calls.Load() != 0 {
		t.Fatalf("transport calls = %d, want 0", calls.Load())
	}
}

func TestInvalidUpstreamResponseIsNotRetriedAndPreservesCause(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var calls atomic.Int64
		transport := roundTripFunc(func(*http.Request) (*http.Response, error) {
			calls.Add(1)

			return response(http.StatusOK, `{"boards":[`, nil), nil
		})
		client := fakeClient(t, defaultPolicy(), transport, io.Discard, nil, nil)
		ctx, cancel := context.WithTimeout(t.Context(), time.Hour)
		defer cancel()

		_, err := client.fetchBoards(ctx)
		if err == nil || errorType(err) != errorInvalid || calls.Load() != 1 {
			t.Fatalf("fetchBoards() error = %v calls=%d, want one invalid-response attempt", err, calls.Load())
		}
		if !errors.Is(err, io.ErrUnexpectedEOF) {
			t.Fatalf("fetchBoards() error = %v, want preserved unexpected-EOF cause", err)
		}
	})
}

func TestParseRetryAfter(t *testing.T) {
	now := time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name  string
		value string
		want  time.Duration
	}{
		{name: "delta seconds", value: "3", want: 3 * time.Second},
		{name: "future HTTP date", value: "Tue, 28 Jul 2026 12:00:05 GMT", want: 5 * time.Second},
		{name: "past HTTP date", value: "Tue, 28 Jul 2026 11:59:55 GMT", want: 0},
		{name: "duration overflow", value: "9223372036854775807", want: time.Duration(1<<63 - 1)},
		{name: "integer overflow", value: "18446744073709551616", want: time.Duration(1<<63 - 1)},
		{name: "invalid text", value: "invalid", want: 0},
		{name: "negative sign", value: "-1", want: 0},
		{name: "positive sign", value: "+1", want: 0},
		{name: "leading whitespace", value: " 1", want: 0},
		{name: "trailing whitespace", value: "1 ", want: 0},
		{name: "decimal", value: "1.0", want: 0},
		{name: "non-ASCII digit", value: "١", want: 0},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := parseRetryAfter(test.value, now); got != test.want {
				t.Errorf("parseRetryAfter(%q) = %s, want %s", test.value, got, test.want)
			}
		})
	}
}

func defaultPolicy() Policy {
	return Policy{
		RateInterval:   time.Second,
		MaxConcurrency: 10,
		RequestTimeout: 5 * time.Second,
		MaxRetries:     2,
		RetryBackoff:   time.Second,
	}
}

func fakeClient(
	t *testing.T,
	policy Policy,
	transport http.RoundTripper,
	logs io.Writer,
	tracer trace.Tracer,
	meter metric.Meter,
) *Client {
	t.Helper()
	if tracer == nil {
		tracer = tracenoop.NewTracerProvider().Tracer("test/acquisition")
	}
	if meter == nil {
		meter = metricnoop.NewMeterProvider().Meter("test/acquisition")
	}

	client, err := newClient(
		policy,
		"4Visor/0123456789abcdef0123456789abcdef01234567",
		"https://example.test",
		&http.Client{Transport: transport},
		slog.New(slog.NewJSONHandler(logs, nil)),
		tracer,
		meter,
	)
	if err != nil {
		t.Fatalf("newClient() error = %v", err)
	}

	return client
}

func response(status int, body string, header http.Header) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     header,
		Body:       io.NopCloser(strings.NewReader(body)),
	}
}

func blockingTransport() http.RoundTripper {
	return roundTripFunc(func(request *http.Request) (*http.Response, error) {
		<-request.Context().Done()

		return nil, request.Context().Err()
	})
}

func boardsJSON(t *testing.T, count int) string {
	t.Helper()
	var output bytes.Buffer
	output.WriteString(`{"boards":[`)
	for index := range count {
		if index > 0 {
			output.WriteByte(',')
		}
		_, _ = output.WriteString(`{"board":"b` + strconv.Itoa(index) + `"}`)
	}
	output.WriteString(`]}`)

	return output.String()
}
