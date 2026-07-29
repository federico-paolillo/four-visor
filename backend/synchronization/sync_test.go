package synchronization

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"strings"
	"testing"
	"testing/synctest"
	"time"

	"git.disroot.org/federico-paolillo/four-visor.git/lineage"
	"git.disroot.org/federico-paolillo/four-visor.git/snapshot"
	"github.com/oklog/ulid/v2"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	metricsdk "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
)

func TestSynchronizeAssignsFreshULIDAndOneStartTime(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var values []snapshot.Snapshot
		entropy := append(make([]byte, 10), append(make([]byte, 9), 1)...)
		scheduler := unitScheduler(t, schedulerOptions{
			lineageEntropy: bytes.NewReader(entropy),
			publish: func(_ context.Context, value snapshot.Snapshot, interval time.Duration) error {
				if interval != time.Hour {
					t.Fatalf("publication interval = %s, want 1h", interval)
				}
				if err := snapshot.Validate(value); err != nil {
					t.Fatalf("published snapshot invalid: %v", err)
				}
				values = append(values, value)

				return nil
			},
		})

		scheduler.synchronize(t.Context())
		scheduler.synchronize(t.Context())
		if len(values) != 2 || values[0].LineageID == values[1].LineageID {
			t.Fatalf("lineage IDs = %q, %q", values[0].LineageID, values[1].LineageID)
		}
		for _, value := range values {
			identifier, err := ulid.ParseStrict(value.LineageID)
			if err != nil || !snapshot.ValidLineageID(value.LineageID) {
				t.Fatalf("generated lineage ID %q is invalid: %v", value.LineageID, err)
			}
			observedAt, err := time.Parse(time.RFC3339Nano, value.ObservedAt)
			if err != nil || observedAt.Location() != time.UTC {
				t.Fatalf("observedAt %q is not UTC RFC3339: %v", value.ObservedAt, err)
			}
			if !ulid.Time(identifier.Time()).Equal(observedAt.Truncate(time.Millisecond)) {
				t.Fatalf("ULID time %s differs from observedAt %s", ulid.Time(identifier.Time()), observedAt)
			}
		}
	})
}

func TestAcquisitionDeadlineFinalizesButExternalCancellationAborts(t *testing.T) {
	t.Run("deadline publishes failed wrappers", func(t *testing.T) {
		synctest.Test(t, func(t *testing.T) {
			published := false
			scheduler := unitScheduler(t, schedulerOptions{
				deadline: 10 * time.Second,
				observe: func(ctx context.Context) (snapshot.Boards, error) {
					<-ctx.Done()
					if !errors.Is(ctx.Err(), context.DeadlineExceeded) {
						t.Fatalf("acquisition context error = %v", ctx.Err())
					}

					return failedBoards(), nil
				},
				publish: func(ctx context.Context, value snapshot.Snapshot, _ time.Duration) error {
					if ctx.Err() != nil || value.Boards.State != snapshot.StateFailed {
						t.Fatalf("publication context=%v boards=%#v", ctx.Err(), value.Boards)
					}
					published = true

					return nil
				},
			})

			scheduler.synchronize(t.Context())
			if !published {
				t.Fatal("deadline-degraded lineage was not published")
			}
		})
	})

	t.Run("external cancellation preserves active lineage", func(t *testing.T) {
		harness := newTelemetryHarness(t)
		var logs bytes.Buffer
		published := false
		cause := errors.New("shutdown requested")
		ctx, cancel := context.WithCancelCause(t.Context())
		cancel(cause)
		scheduler := harness.scheduler(t, schedulerOptions{
			logs: &logs,
			observe: func(ctx context.Context) (snapshot.Boards, error) {
				return snapshot.Boards{}, context.Cause(ctx)
			},
			publish: func(context.Context, snapshot.Snapshot, time.Duration) error {
				published = true

				return nil
			},
		})

		scheduler.synchronize(ctx)
		if published {
			t.Fatal("external cancellation reached publication")
		}
		if strings.Count(logs.String(), `"level":"ERROR"`) != 1 ||
			!strings.Contains(logs.String(), `"error.type":"canceled"`) ||
			!strings.Contains(logs.String(), `"lineage.id":"`) {
			t.Fatalf("cancellation logs = %s", logs.String())
		}
	})

	t.Run("construction failure remains logged", func(t *testing.T) {
		var logs bytes.Buffer
		scheduler := unitScheduler(t, schedulerOptions{
			logs: &logs,
			observe: func(context.Context) (snapshot.Boards, error) {
				return snapshot.Boards{}, errors.New("acquisition unavailable")
			},
		})

		scheduler.synchronize(t.Context())
		if strings.Count(logs.String(), `"level":"ERROR"`) != 1 ||
			!strings.Contains(logs.String(), `"error.type":"failed"`) ||
			!strings.Contains(logs.String(), `"lineage.id":"`) {
			t.Fatalf("construction logs = %s", logs.String())
		}
	})
}

func TestSynchronizationOutcomeTelemetry(t *testing.T) {
	tests := []struct {
		name       string
		failures   int
		want       string
		wantStatus codes.Code
	}{
		{name: "successful", failures: 0, want: outcomeSuccess, wantStatus: codes.Unset},
		{name: "degraded within tolerance", failures: 1, want: outcomeDegraded, wantStatus: codes.Unset},
		{name: "degraded at tolerance", failures: 10, want: outcomeDegraded, wantStatus: codes.Unset},
		{name: "excessively degraded", failures: 11, want: outcomeDegraded, wantStatus: codes.Error},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				harness := newTelemetryHarness(t)
				var logs bytes.Buffer
				scheduler := harness.scheduler(t, schedulerOptions{
					logs:    &logs,
					observe: func(context.Context) (snapshot.Boards, error) { return boardsWithFailures(test.failures), nil },
				})

				harness.assertActiveAgeAbsent(t)
				scheduler.synchronize(t.Context())
				root := synchronizationRoot(t, harness.spans.GetSpans())
				if root.Status.Code != test.wantStatus || spanAttribute(root.Attributes, "lineage.outcome") != test.want {
					t.Fatalf("root status=%v attributes=%#v", root.Status, root.Attributes)
				}
				assertStageHierarchy(t, harness.spans.GetSpans(), root)
				harness.assertFourMetrics(t, test.want, int64(test.failures))
				if test.failures > 10 && !strings.Contains(logs.String(), "excessively degraded lineage activated") {
					t.Fatalf("excessive degradation log missing: %s", logs.String())
				}
			})
		})
	}
}

func TestPublicationFailureAndCleanupErrorHaveDistinctCommitSemantics(t *testing.T) {
	t.Run("publication failure does not activate", func(t *testing.T) {
		harness := newTelemetryHarness(t)
		var logs bytes.Buffer
		publisherLogger := slog.New(slog.NewJSONHandler(&logs, nil))
		scheduler := harness.scheduler(t, schedulerOptions{
			logs: &logs,
			publish: func(ctx context.Context, value snapshot.Snapshot, _ time.Duration) error {
				publisherLogger.ErrorContext(ctx, "lineage publication failed",
					slog.String("lineage.id", value.LineageID),
					slog.String("error.type", "unavailable"),
				)

				return errors.New("cache unavailable")
			},
		})

		scheduler.synchronize(t.Context())
		root := synchronizationRoot(t, harness.spans.GetSpans())
		if root.Status.Code != codes.Error || spanAttribute(root.Attributes, "lineage.outcome") != outcomeFailed ||
			scheduler.activeObservedAt.Load() != 0 {
			t.Fatalf("failed publication root=%#v active=%d", root, scheduler.activeObservedAt.Load())
		}
		harness.assertFailedAttemptMetrics(t)
		if strings.Count(logs.String(), `"level":"ERROR"`) != 1 ||
			!strings.Contains(logs.String(), "lineage publication failed") ||
			!strings.Contains(logs.String(), `"lineage.id":"`) {
			t.Fatalf("publication logs = %s", logs.String())
		}
	})

	t.Run("cleanup error is committed activation", func(t *testing.T) {
		harness := newTelemetryHarness(t)
		var logs bytes.Buffer
		scheduler := harness.scheduler(t, schedulerOptions{
			logs: &logs,
			publish: func(context.Context, snapshot.Snapshot, time.Duration) error {
				return new(lineage.CleanupError)
			},
		})

		scheduler.synchronize(t.Context())
		root := synchronizationRoot(t, harness.spans.GetSpans())
		if root.Status.Code != codes.Error || spanAttribute(root.Attributes, "lineage.outcome") != outcomeSuccess ||
			scheduler.activeObservedAt.Load() == 0 {
			t.Fatalf("cleanup root=%#v active=%d", root, scheduler.activeObservedAt.Load())
		}
		if !strings.Contains(logs.String(), "cleanup failure") || strings.Contains(logs.String(), "previous lineage evicted") {
			t.Fatalf("cleanup logs = %s", logs.String())
		}
		harness.assertFourMetrics(t, outcomeSuccess, 0)
	})

	t.Run("excessive degradation and cleanup failure remain committed", func(t *testing.T) {
		harness := newTelemetryHarness(t)
		var logs bytes.Buffer
		scheduler := harness.scheduler(t, schedulerOptions{
			logs:    &logs,
			observe: func(context.Context) (snapshot.Boards, error) { return boardsWithFailures(11), nil },
			publish: func(context.Context, snapshot.Snapshot, time.Duration) error {
				return new(lineage.CleanupError)
			},
		})

		scheduler.synchronize(t.Context())
		root := synchronizationRoot(t, harness.spans.GetSpans())
		lineageID := spanAttribute(root.Attributes, "lineage.id")
		failed, failedFound := spanIntAttribute(root.Attributes, "lineage.failed_resource.count")
		tolerance, toleranceFound := spanIntAttribute(root.Attributes, "lineage.failed_resource.tolerance")
		if root.Status.Code != codes.Error || spanAttribute(root.Attributes, "lineage.outcome") != outcomeDegraded ||
			!snapshot.ValidLineageID(lineageID) || !failedFound || failed != 11 || !toleranceFound || tolerance != 10 ||
			scheduler.activeObservedAt.Load() == 0 {
			t.Fatalf("combined cleanup root=%#v active=%d", root, scheduler.activeObservedAt.Load())
		}
		harness.assertFourMetrics(t, outcomeDegraded, 11)
		if strings.Count(logs.String(), "excessively degraded lineage activated with cleanup failure") != 1 ||
			!strings.Contains(logs.String(), `"lineage.id":"`+lineageID+`"`) ||
			!strings.Contains(logs.String(), `"resource.failed.count":11`) ||
			!strings.Contains(logs.String(), `"resource.failed.tolerance":10`) ||
			!strings.Contains(logs.String(), `"lineage.degradation.excessive":true`) {
			t.Fatalf("combined cleanup logs = %s", logs.String())
		}
	})
}

type schedulerOptions struct {
	logs           io.Writer
	observe        func(context.Context) (snapshot.Boards, error)
	publish        func(context.Context, snapshot.Snapshot, time.Duration) error
	lineageEntropy io.Reader
	deadline       time.Duration
}

func unitScheduler(t *testing.T, options schedulerOptions) *Scheduler {
	t.Helper()
	return newTelemetryHarness(t).scheduler(t, options)
}

type telemetryHarness struct {
	spans  *tracetest.InMemoryExporter
	tracer *tracesdk.TracerProvider
	reader *metricsdk.ManualReader
	meter  *metricsdk.MeterProvider
}

func newTelemetryHarness(t *testing.T) *telemetryHarness {
	t.Helper()
	spans := tracetest.NewInMemoryExporter()
	tracer := tracesdk.NewTracerProvider(tracesdk.WithSyncer(spans))
	t.Cleanup(func() { _ = tracer.Shutdown(t.Context()) })
	reader := metricsdk.NewManualReader()
	meter := metricsdk.NewMeterProvider(metricsdk.WithReader(reader))
	t.Cleanup(func() { _ = meter.Shutdown(t.Context()) })

	return &telemetryHarness{spans: spans, tracer: tracer, reader: reader, meter: meter}
}

func (harness *telemetryHarness) scheduler(t *testing.T, options schedulerOptions) *Scheduler {
	t.Helper()
	if options.logs == nil {
		options.logs = io.Discard
	}
	if options.observe == nil {
		options.observe = func(context.Context) (snapshot.Boards, error) { return boardsWithFailures(0), nil }
	}
	if options.publish == nil {
		options.publish = func(context.Context, snapshot.Snapshot, time.Duration) error { return nil }
	}
	if options.lineageEntropy == nil {
		options.lineageEntropy = bytes.NewReader(make([]byte, 256))
	}
	if options.deadline == 0 {
		options.deadline = time.Hour
	}

	scheduler, err := newScheduler(time.Hour, 10, schedulerDependencies{
		observe:        options.observe,
		publish:        options.publish,
		logger:         slog.New(slog.NewJSONHandler(options.logs, nil)),
		tracer:         harness.tracer.Tracer("test/synchronization"),
		meter:          harness.meter.Meter("test/synchronization"),
		jitterEntropy:  bytes.NewReader([]byte{0}),
		lineageEntropy: options.lineageEntropy,
		deadline:       options.deadline,
	})
	if err != nil {
		t.Fatalf("newScheduler() error = %v", err)
	}

	return scheduler
}

func (harness *telemetryHarness) collect(t *testing.T) metricdata.ResourceMetrics {
	t.Helper()
	var collected metricdata.ResourceMetrics
	if err := harness.reader.Collect(t.Context(), &collected); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	return collected
}

func (harness *telemetryHarness) assertActiveAgeAbsent(t *testing.T) {
	t.Helper()
	collected := harness.collect(t)
	for _, scope := range collected.ScopeMetrics {
		for _, item := range scope.Metrics {
			if item.Name != "lineage.active.age" {
				continue
			}
			data, ok := item.Data.(metricdata.Gauge[float64])
			if !ok || len(data.DataPoints) != 0 {
				t.Fatalf("active age before activation = %#v", item.Data)
			}
		}
	}
}

func (harness *telemetryHarness) assertFourMetrics(t *testing.T, outcome string, failed int64) {
	t.Helper()
	collected := harness.collect(t)
	seen := make(map[string]bool)
	for _, scope := range collected.ScopeMetrics {
		for _, item := range scope.Metrics {
			seen[item.Name] = true
			switch item.Name {
			case "lineage.synchronization.duration":
				data := item.Data.(metricdata.Histogram[float64])
				assertOnlyOutcome(t, data.DataPoints[0].Attributes.ToSlice(), outcome)
			case "lineage.synchronization.activated":
				data := item.Data.(metricdata.Sum[int64])
				if data.DataPoints[0].Value != 1 {
					t.Fatalf("activated count = %d", data.DataPoints[0].Value)
				}
				assertOnlyOutcome(t, data.DataPoints[0].Attributes.ToSlice(), outcome)
			case "lineage.failed_resource.count":
				data := item.Data.(metricdata.Histogram[int64])
				if data.DataPoints[0].Sum != failed || data.DataPoints[0].Attributes.Len() != 0 {
					t.Fatalf("failed-resource metric = %#v", data.DataPoints[0])
				}
			case "lineage.active.age":
				data := item.Data.(metricdata.Gauge[float64])
				if len(data.DataPoints) != 1 || data.DataPoints[0].Attributes.Len() != 0 {
					t.Fatalf("active-age metric = %#v", data)
				}
			default:
				t.Fatalf("unexpected lineage metric %q", item.Name)
			}
		}
	}
	if len(seen) != 4 {
		t.Fatalf("lineage metrics = %v, want four concepts", seen)
	}
}

func (harness *telemetryHarness) assertFailedAttemptMetrics(t *testing.T) {
	t.Helper()
	collected := harness.collect(t)
	durationSeen := false

	for _, scope := range collected.ScopeMetrics {
		for _, item := range scope.Metrics {
			switch item.Name {
			case "lineage.synchronization.duration":
				data := item.Data.(metricdata.Histogram[float64])
				assertOnlyOutcome(t, data.DataPoints[0].Attributes.ToSlice(), outcomeFailed)
				durationSeen = true
			case "lineage.synchronization.activated", "lineage.failed_resource.count":
				t.Fatalf("failed attempt recorded activation metric %q", item.Name)
			case "lineage.active.age":
				data := item.Data.(metricdata.Gauge[float64])
				if len(data.DataPoints) != 0 {
					t.Fatalf("failed attempt recorded active age: %#v", data)
				}
			}
		}
	}

	if !durationSeen {
		t.Fatal("failed attempt duration metric missing")
	}
}

func assertOnlyOutcome(t *testing.T, attributes []attribute.KeyValue, want string) {
	t.Helper()
	if len(attributes) != 1 || attributes[0].Key != "lineage.outcome" || attributes[0].Value.AsString() != want {
		t.Fatalf("metric attributes = %#v, want outcome %q", attributes, want)
	}
}

func synchronizationRoot(t *testing.T, spans tracetest.SpanStubs) tracetest.SpanStub {
	t.Helper()
	for _, span := range spans {
		if span.Name == "lineage.synchronize" {
			if span.Parent.IsValid() {
				t.Fatalf("synchronization root has parent %v", span.Parent)
			}

			return span
		}
	}
	t.Fatalf("synchronization root missing: %#v", spans)

	return tracetest.SpanStub{}
}

func assertStageHierarchy(t *testing.T, spans tracetest.SpanStubs, root tracetest.SpanStub) {
	t.Helper()
	found := map[string]bool{"lineage.acquire": false, "lineage.publish": false}
	for _, span := range spans {
		if _, ok := found[span.Name]; !ok {
			continue
		}
		found[span.Name] = true
		if span.Parent.SpanID() != root.SpanContext.SpanID() || span.Parent.TraceID() != root.SpanContext.TraceID() {
			t.Fatalf("stage %q parent = %v, want %v", span.Name, span.Parent, root.SpanContext)
		}
	}
	if !found["lineage.acquire"] || !found["lineage.publish"] {
		t.Fatalf("stage spans = %v", found)
	}
}

func spanAttribute(attributes []attribute.KeyValue, key attribute.Key) string {
	for _, item := range attributes {
		if item.Key == key {
			return item.Value.AsString()
		}
	}

	return ""
}

func spanIntAttribute(attributes []attribute.KeyValue, key attribute.Key) (int64, bool) {
	for _, item := range attributes {
		if item.Key == key {
			return item.Value.AsInt64(), true
		}
	}

	return 0, false
}

func boardsWithFailures(count int) snapshot.Boards {
	items := make([]snapshot.BoardItem, count)
	for index := range items {
		items[index] = snapshot.BoardItem{
			Board:   json.RawMessage(`{"board":"test"}`),
			Catalog: &snapshot.Catalog{State: snapshot.StateFailed},
		}
	}

	return snapshot.Boards{State: snapshot.StatePresent, Items: &items}
}
