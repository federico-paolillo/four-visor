package synchronization

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"strings"
	"testing"
	"time"

	"git.disroot.org/federico-paolillo/four-visor.git/acquisition"
	"git.disroot.org/federico-paolillo/four-visor.git/lineage"
	"git.disroot.org/federico-paolillo/four-visor.git/snapshot"
	"github.com/bradfitz/gomemcache/memcache"
	"go.opentelemetry.io/otel/codes"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

const integrationOldLineageID = "01J1YQ7Y0M4S6R8T2V3W5X7Y9Z"

func TestSynchronizationRealAcquisitionAndMemcachedOutcomes(t *testing.T) {
	tests := []struct {
		name        string
		handler     http.Handler
		wantBoards  snapshot.State
		wantCatalog snapshot.State
		wantOutcome string
	}{
		{
			name: "complete",
			handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				switch request.URL.Path {
				case "/boards.json":
					_, _ = io.WriteString(writer, `{"boards":[{"board":"a"}]}`)
				case "/a/catalog.json":
					_, _ = io.WriteString(writer, `[]`)
				default:
					http.NotFound(writer, request)
				}
			}),
			wantBoards:  snapshot.StatePresent,
			wantCatalog: snapshot.StatePresent,
			wantOutcome: outcomeSuccess,
		},
		{
			name: "degraded catalog",
			handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.URL.Path == "/boards.json" {
					_, _ = io.WriteString(writer, `{"boards":[{"board":"a"}]}`)

					return
				}
				http.Error(writer, "unavailable", http.StatusServiceUnavailable)
			}),
			wantBoards:  snapshot.StatePresent,
			wantCatalog: snapshot.StateFailed,
			wantOutcome: outcomeDegraded,
		},
		{
			name: "total outage",
			handler: http.HandlerFunc(func(writer http.ResponseWriter, _ *http.Request) {
				http.Error(writer, "unavailable", http.StatusServiceUnavailable)
			}),
			wantBoards:  snapshot.StateFailed,
			wantOutcome: outcomeDegraded,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			address := integrationMemcachedAddress(t)
			flushMemcached(t, address)
			server := integrationUpstream(t, test.handler)
			harness := newTelemetryHarness(t)
			publisher := integrationPublisher(t, address, harness)
			publishOldLineage(t, publisher)
			scheduler := integrationScheduler(t, server, publisher, harness, 5*time.Second, time.Hour)

			scheduler.synchronize(t.Context())
			active := readActiveSnapshot(t, address, http.StatusOK)
			if active.LineageID == integrationOldLineageID || active.Boards.State != test.wantBoards {
				t.Fatalf("active lineage = %#v", active)
			}
			if test.wantCatalog != "" {
				catalog := (*active.Boards.Items)[0].Catalog
				if catalog == nil || catalog.State != test.wantCatalog {
					t.Fatalf("active catalog = %#v, want %s", catalog, test.wantCatalog)
				}
			}
			root := synchronizationRoot(t, harness.spans.GetSpans())
			if got := spanAttribute(root.Attributes, "lineage.outcome"); got != test.wantOutcome {
				t.Fatalf("root outcome = %q, want %q", got, test.wantOutcome)
			}
		})
	}
}

func TestSynchronizationDeadlineResourceFinalizationAgainstHTTP(t *testing.T) {
	tests := []struct {
		name     string
		deadline time.Duration
		handler  http.Handler
		assert   func(*testing.T, snapshot.Snapshot)
	}{
		{
			name:     "board deadline",
			deadline: 500 * time.Millisecond,
			handler:  blockingUpstream(),
			assert: func(t *testing.T, value snapshot.Snapshot) {
				if value.Boards.State != snapshot.StateFailed {
					t.Fatalf("boards = %#v, want failed", value.Boards)
				}
			},
		},
		{
			name:     "catalog deadline",
			deadline: 1500 * time.Millisecond,
			handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				if request.URL.Path == "/boards.json" {
					_, _ = io.WriteString(writer, `{"boards":[{"board":"a"}]}`)

					return
				}
				<-request.Context().Done()
			}),
			assert: func(t *testing.T, value snapshot.Snapshot) {
				catalog := (*value.Boards.Items)[0].Catalog
				if catalog == nil || catalog.State != snapshot.StateFailed {
					t.Fatalf("catalog = %#v, want failed", catalog)
				}
			},
		},
		{
			name:     "thread deadline",
			deadline: 2500 * time.Millisecond,
			handler: http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
				switch request.URL.Path {
				case "/boards.json":
					_, _ = io.WriteString(writer, `{"boards":[{"board":"a"}]}`)
				case "/a/catalog.json":
					_, _ = io.WriteString(writer, `[{"page":1,"threads":[{"no":1}]}]`)
				default:
					<-request.Context().Done()
				}
			}),
			assert: func(t *testing.T, value snapshot.Snapshot) {
				thread := (*(*value.Boards.Items)[0].Catalog.Pages)[0].Threads[0].Thread
				if thread == nil || thread.State != snapshot.StateFailed {
					t.Fatalf("thread = %#v, want failed", thread)
				}
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			address := integrationMemcachedAddress(t)
			flushMemcached(t, address)
			server := integrationUpstream(t, test.handler)
			harness := newTelemetryHarness(t)
			publisher := integrationPublisher(t, address, harness)
			scheduler := integrationScheduler(t, server, publisher, harness, test.deadline, time.Hour)

			scheduler.synchronize(t.Context())
			active := readActiveSnapshot(t, address, http.StatusOK)
			test.assert(t, active)
			root := synchronizationRoot(t, harness.spans.GetSpans())
			if got := spanAttribute(root.Attributes, "lineage.outcome"); got != outcomeDegraded {
				t.Fatalf("root outcome = %q, want degraded", got)
			}
		})
	}
}

func TestExternalCancellationAndPublicationFailurePreserveRealPointer(t *testing.T) {
	t.Run("external cancellation", func(t *testing.T) {
		address := integrationMemcachedAddress(t)
		flushMemcached(t, address)
		entered := make(chan struct{})
		server := integrationUpstream(t, http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
			close(entered)
			<-request.Context().Done()
		}))
		harness := newTelemetryHarness(t)
		publisher := integrationPublisher(t, address, harness)
		publishOldLineage(t, publisher)
		scheduler := integrationScheduler(t, server, publisher, harness, time.Hour, time.Hour)
		ctx, cancel := context.WithCancelCause(t.Context())
		done := make(chan struct{})
		go func() {
			scheduler.synchronize(ctx)
			close(done)
		}()

		<-entered
		cancel(errors.New("shutdown requested"))
		<-done
		active := readActiveSnapshot(t, address, http.StatusOK)
		if active.LineageID != integrationOldLineageID {
			t.Fatalf("active lineage = %s, want old lineage", active.LineageID)
		}
		root := synchronizationRoot(t, harness.spans.GetSpans())
		if root.Status.Code != codes.Error || spanAttribute(root.Attributes, "lineage.outcome") != outcomeFailed {
			t.Fatalf("canceled root = %#v", root)
		}
	})

	t.Run("publication failure", func(t *testing.T) {
		address := integrationMemcachedAddress(t)
		flushMemcached(t, address)
		server := integrationUpstream(t, completeUpstream())
		harness := newTelemetryHarness(t)
		publishOldLineage(t, integrationPublisher(t, address, harness))
		var logs bytes.Buffer
		logger := slog.New(slog.NewJSONHandler(&logs, nil))
		unavailable, err := lineage.NewPublisher(
			"127.0.0.1:65179",
			logger,
			harness.tracer.Tracer("test/lineage"),
			harness.meter.Meter("test/lineage"),
		)
		if err != nil {
			t.Fatalf("lineage.NewPublisher() error = %v", err)
		}
		scheduler := integrationScheduler(t, server, unavailable, harness, 5*time.Second, time.Hour)
		scheduler.logger = logger

		scheduler.synchronize(t.Context())
		active := readActiveSnapshot(t, address, http.StatusOK)
		if active.LineageID != integrationOldLineageID {
			t.Fatalf("active lineage = %s, want old lineage", active.LineageID)
		}
		root := synchronizationRoot(t, harness.spans.GetSpans())
		if root.Status.Code != codes.Error || spanAttribute(root.Attributes, "lineage.outcome") != outcomeFailed {
			t.Fatalf("publication failure root = %#v", root)
		}
		if strings.Count(logs.String(), `"level":"ERROR"`) != 1 ||
			!strings.Contains(logs.String(), "lineage publication failed") ||
			!strings.Contains(logs.String(), `"lineage.id":"`) {
			t.Fatalf("publication failure logs = %s", logs.String())
		}
	})
}

func TestCacheLossRecoversAtSynchronizationAndTTLUsesConfiguredInterval(t *testing.T) {
	address := integrationMemcachedAddress(t)
	flushMemcached(t, address)
	server := integrationUpstream(t, completeUpstream())
	harness := newTelemetryHarness(t)
	publisher := integrationPublisher(t, address, harness)
	publishOldLineage(t, publisher)
	flushMemcached(t, address)
	readActiveSnapshot(t, address, http.StatusGone)

	const interval = 5 * time.Second
	scheduler := integrationScheduler(t, server, publisher, harness, 5*time.Second, interval)
	originalPublish := scheduler.publish
	var publicationStarted, publicationCompleted time.Time
	scheduler.publish = func(ctx context.Context, value snapshot.Snapshot, interval time.Duration) error {
		publicationStarted = time.Now()
		err := originalPublish(ctx, value, interval)
		publicationCompleted = time.Now()

		return err
	}
	scheduler.synchronize(t.Context())
	readActiveSnapshot(t, address, http.StatusOK)

	conceptualEarliest := publicationStarted.Add(2 * interval)
	conceptualLatest := publicationCompleted.Add(2 * interval)
	const observationResolution = 100 * time.Millisecond
	// The protocol timestamp and Memcached's internal clock each quantize to whole seconds.
	const memcachedObservationWindow = 2 * time.Second
	earliestObservedExpiry := conceptualEarliest.Add(-memcachedObservationWindow - observationResolution)
	latestObservedExpiry := conceptualLatest.Add(memcachedObservationWindow + observationResolution)
	oneIntervalLatestExpiry := publicationCompleted.Add(interval + memcachedObservationWindow + observationResolution)
	if !oneIntervalLatestExpiry.Before(earliestObservedExpiry) {
		t.Fatalf("one-times-interval expiry range ends at %s; correct range starts at %s",
			oneIntervalLatestExpiry, earliestObservedExpiry)
	}
	deadline := time.NewTimer(time.Until(latestObservedExpiry))
	defer deadline.Stop()
	poll := time.NewTicker(observationResolution)
	defer poll.Stop()
	for {
		select {
		case <-poll.C:
			if snapshotStatus(t, address) == http.StatusGone {
				expiredAt := time.Now()
				if expiredAt.Before(earliestObservedExpiry) || expiredAt.After(latestObservedExpiry) {
					t.Fatalf("lineage expired at %s, conceptual deadline range %s..%s", expiredAt,
						conceptualEarliest, conceptualLatest)
				}

				return
			}
		case <-deadline.C:
			t.Fatal("active lineage exceeded twice the configured interval plus Memcached's quantization window")
		}
	}
}

func integrationScheduler(
	t *testing.T,
	server *httptest.Server,
	publisher *lineage.Publisher,
	harness *telemetryHarness,
	deadline, interval time.Duration,
) *Scheduler {
	t.Helper()
	client, err := acquisition.New(
		acquisition.Policy{
			RateInterval:   time.Second,
			MaxConcurrency: 1,
			RequestTimeout: 5 * time.Second,
			MaxRetries:     0,
			RetryBackoff:   time.Second,
		},
		"4Visor/0123456789abcdef0123456789abcdef01234567",
		rewriteToServer(t, server),
		slog.New(slog.NewJSONHandler(io.Discard, nil)),
		harness.tracer.Tracer("test/acquisition"),
		harness.meter.Meter("test/acquisition"),
	)
	if err != nil {
		t.Fatalf("acquisition.New() error = %v", err)
	}

	scheduler, err := newScheduler(interval, 10, schedulerDependencies{
		observe:        client.Observe,
		publish:        publisher.Publish,
		logger:         slog.New(slog.NewJSONHandler(io.Discard, nil)),
		tracer:         harness.tracer.Tracer("test/synchronization"),
		meter:          harness.meter.Meter("test/synchronization"),
		jitterEntropy:  bytes.NewReader([]byte{0}),
		lineageEntropy: bytes.NewReader(make([]byte, 32)),
		deadline:       deadline,
	})
	if err != nil {
		t.Fatalf("newScheduler() error = %v", err)
	}

	return scheduler
}

func integrationPublisher(t *testing.T, address string, harness *telemetryHarness) *lineage.Publisher {
	t.Helper()
	publisher, err := lineage.NewPublisher(
		address,
		slog.New(slog.NewJSONHandler(io.Discard, nil)),
		harness.tracer.Tracer("test/lineage"),
		harness.meter.Meter("test/lineage"),
	)
	if err != nil {
		t.Fatalf("lineage.NewPublisher() error = %v", err)
	}

	return publisher
}

func publishOldLineage(t *testing.T, publisher *lineage.Publisher) {
	t.Helper()
	items := []snapshot.BoardItem{}
	err := publisher.Publish(t.Context(), snapshot.Snapshot{
		SchemaVersion: snapshot.Version,
		LineageID:     integrationOldLineageID,
		ObservedAt:    "2026-07-29T00:00:00Z",
		Boards:        snapshot.Boards{State: snapshot.StatePresent, Items: &items},
	}, time.Hour)
	if err != nil {
		t.Fatalf("publishing old lineage: %v", err)
	}
}

func readActiveSnapshot(t *testing.T, address string, wantStatus int) snapshot.Snapshot {
	t.Helper()
	handler, err := lineage.NewSnapshotHandler(
		address,
		slog.New(slog.NewJSONHandler(io.Discard, nil)),
		tracenoop.NewTracerProvider().Tracer("test/lineage"),
		metricnoop.NewMeterProvider().Meter("test/lineage"),
	)
	if err != nil {
		t.Fatalf("lineage.NewSnapshotHandler() error = %v", err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/snapshot", http.NoBody))
	if response.Code != wantStatus {
		t.Fatalf("snapshot status = %d, want %d; body=%s", response.Code, wantStatus, response.Body.String())
	}
	if wantStatus != http.StatusOK {
		return snapshot.Snapshot{}
	}

	value, err := snapshot.Parse(response.Body.Bytes())
	if err != nil {
		t.Fatalf("snapshot.Parse() error = %v", err)
	}

	return value
}

func snapshotStatus(t *testing.T, address string) int {
	t.Helper()
	handler, err := lineage.NewSnapshotHandler(
		address,
		slog.New(slog.NewJSONHandler(io.Discard, nil)),
		tracenoop.NewTracerProvider().Tracer("test/lineage"),
		metricnoop.NewMeterProvider().Meter("test/lineage"),
	)
	if err != nil {
		t.Fatalf("lineage.NewSnapshotHandler() error = %v", err)
	}
	response := httptest.NewRecorder()
	handler.ServeHTTP(response, httptest.NewRequest(http.MethodGet, "/snapshot", http.NoBody))

	return response.Code
}

func integrationMemcachedAddress(t *testing.T) string {
	t.Helper()
	address := os.Getenv("FOURVISOR_TEST_MEMCACHED_ADDRESS")
	if address == "" {
		t.Skip("FOURVISOR_TEST_MEMCACHED_ADDRESS is not set")
	}

	host, port, err := net.SplitHostPort(address)
	if err != nil || host != "127.0.0.1" || port != "65198" {
		t.Fatalf("integration Memcached must use 127.0.0.1:65198, got %q", address)
	}

	return address
}

func flushMemcached(t *testing.T, address string) {
	t.Helper()
	client := memcache.New(address)
	client.Timeout = memcache.DefaultTimeout
	if err := client.FlushAll(); err != nil {
		t.Fatalf("Memcached FlushAll() error = %v", err)
	}
}

func integrationUpstream(t *testing.T, handler http.Handler) *httptest.Server {
	t.Helper()
	var listener net.Listener
	var err error
	for port := 65190; port <= 65197; port++ {
		listener, err = net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", port))
		if err == nil {
			break
		}
	}
	if err != nil {
		t.Fatalf("listening for integration upstream: %v", err)
	}

	server := httptest.NewUnstartedServer(handler)
	server.Listener = listener
	server.Start()
	t.Cleanup(server.Close)

	return server
}

type rewriteTransport struct {
	target *url.URL
	base   http.RoundTripper
}

func (transport rewriteTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	if request.URL.Scheme != "https" || request.URL.Host != "a.4cdn.org" {
		return nil, fmt.Errorf("acquisition request did not use the official upstream endpoint")
	}

	clone := request.Clone(request.Context())
	clone.URL.Scheme = transport.target.Scheme
	clone.URL.Host = transport.target.Host
	clone.Host = transport.target.Host

	return transport.base.RoundTrip(clone)
}

func rewriteToServer(t *testing.T, server *httptest.Server) http.RoundTripper {
	t.Helper()
	target, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parsing test server URL: %v", err)
	}

	return rewriteTransport{target: target, base: server.Client().Transport}
}

func completeUpstream() http.Handler {
	return http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/boards.json":
			_, _ = io.WriteString(writer, `{"boards":[{"board":"a"}]}`)
		case "/a/catalog.json":
			_, _ = io.WriteString(writer, `[]`)
		default:
			http.NotFound(writer, request)
		}
	})
}

func blockingUpstream() http.Handler {
	return http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		<-request.Context().Done()
	})
}
