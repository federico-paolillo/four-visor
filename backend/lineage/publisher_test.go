package lineage

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"

	"git.disroot.org/federico-paolillo/four-visor.git/snapshot"
	"github.com/bradfitz/gomemcache/memcache"
	"go.opentelemetry.io/otel/attribute"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	metricsdk "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

const (
	oldLineageID        = "01J1YQ7Y0M4S6R8T2V3W5X7Y9Z"
	newLineageID        = "01J1YQ7Y0M4S6R8T2V3W5X7Y9Y"
	unexpectedLineageID = "01J1YQ7Y0M4S6R8T2V3W5X7Y9X"
)

type fakeMemcache struct {
	mu           sync.Mutex
	items        map[string]*memcache.Item
	before       func(operation, key string) error
	after        func(operation, key string) error
	transformGet func(key string, value []byte) []byte
	deleted      []string
}

func newFakeMemcache() *fakeMemcache {
	return &fakeMemcache{items: make(map[string]*memcache.Item)}
}

func (cache *fakeMemcache) Add(item *memcache.Item) error {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	if err := cache.call(cache.before, "add", item.Key); err != nil {
		return err
	}
	if _, exists := cache.items[item.Key]; exists {
		return memcache.ErrNotStored
	}
	cache.items[item.Key] = cloneItem(item)

	return cache.call(cache.after, "add", item.Key)
}

func (cache *fakeMemcache) Set(item *memcache.Item) error {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	if err := cache.call(cache.before, "set", item.Key); err != nil {
		return err
	}
	cache.items[item.Key] = cloneItem(item)

	return cache.call(cache.after, "set", item.Key)
}

func (cache *fakeMemcache) Get(key string) (*memcache.Item, error) {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	if err := cache.call(cache.before, "get", key); err != nil {
		return nil, err
	}
	item, exists := cache.items[key]
	if !exists {
		return nil, memcache.ErrCacheMiss
	}
	result := cloneItem(item)
	if cache.transformGet != nil {
		result.Value = cache.transformGet(key, result.Value)
	}
	if err := cache.call(cache.after, "get", key); err != nil {
		return nil, err
	}

	return result, nil
}

func (cache *fakeMemcache) Delete(key string) error {
	cache.mu.Lock()
	defer cache.mu.Unlock()

	if err := cache.call(cache.before, "delete", key); err != nil {
		return err
	}
	if _, exists := cache.items[key]; !exists {
		return memcache.ErrCacheMiss
	}
	delete(cache.items, key)
	cache.deleted = append(cache.deleted, key)

	return cache.call(cache.after, "delete", key)
}

func (*fakeMemcache) call(hook func(string, string) error, operation, key string) error {
	if hook == nil {
		return nil
	}

	return hook(operation, key)
}

func cloneItem(item *memcache.Item) *memcache.Item {
	copy := *item
	copy.Value = bytes.Clone(item.Value)

	return &copy
}

func TestPublicationFailuresPreserveReadableActiveLineage(t *testing.T) {
	tests := []struct {
		name      string
		configure func(*fakeMemcache, context.CancelCauseFunc, error)
		want      error
	}{
		{
			name: "second block write",
			configure: func(cache *fakeMemcache, _ context.CancelCauseFunc, injected error) {
				cache.before = func(operation, key string) error {
					if operation == "add" && key == blockKey(newLineageID, 1) {
						return injected
					}
					return nil
				}
			},
			want: errInjected,
		},
		{
			name: "block verification mismatch",
			configure: func(cache *fakeMemcache, _ context.CancelCauseFunc, _ error) {
				cache.transformGet = func(key string, value []byte) []byte {
					if key == blockKey(newLineageID, 0) {
						return []byte("corrupt")
					}
					return value
				}
			},
			want: errVerificationFailed,
		},
		{
			name: "completion write",
			configure: func(cache *fakeMemcache, _ context.CancelCauseFunc, injected error) {
				cache.before = func(operation, key string) error {
					if operation == "add" && key == completionKey(newLineageID) {
						return injected
					}
					return nil
				}
			},
			want: errInjected,
		},
		{
			name: "completion verification mismatch",
			configure: func(cache *fakeMemcache, _ context.CancelCauseFunc, _ error) {
				cache.transformGet = func(key string, value []byte) []byte {
					if key == completionKey(newLineageID) {
						return []byte("corrupt")
					}
					return value
				}
			},
			want: errVerificationFailed,
		},
		{
			name: "pointer store",
			configure: func(cache *fakeMemcache, _ context.CancelCauseFunc, injected error) {
				cache.before = func(operation, key string) error {
					if operation == "set" && key == activePointerKey {
						return injected
					}
					return nil
				}
			},
			want: errInjected,
		},
		{
			name: "cancellation after block write",
			configure: func(cache *fakeMemcache, cancel context.CancelCauseFunc, injected error) {
				cache.after = func(operation, key string) error {
					if operation == "add" && key == blockKey(newLineageID, 0) {
						cancel(injected)
					}
					return nil
				}
			},
			want: errInjected,
		},
		{
			name: "cancellation after completion write",
			configure: func(cache *fakeMemcache, cancel context.CancelCauseFunc, injected error) {
				cache.after = func(operation, key string) error {
					if operation == "add" && key == completionKey(newLineageID) {
						cancel(injected)
					}
					return nil
				}
			},
			want: errInjected,
		},
		{
			name: "cancellation after completion verification",
			configure: func(cache *fakeMemcache, cancel context.CancelCauseFunc, injected error) {
				cache.after = func(operation, key string) error {
					if operation == "get" && key == completionKey(newLineageID) {
						cancel(injected)
					}
					return nil
				}
			},
			want: errInjected,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cache := newFakeMemcache()
			publisher := testPublisher(t, cache)
			old := testSnapshot(oldLineageID, 0)
			publish(t, publisher, t.Context(), old)

			ctx, cancel := context.WithCancelCause(t.Context())
			injected := errors.New("injected failure")
			test.configure(cache, cancel, injected)

			err := publisher.Publish(ctx, testSnapshot(newLineageID, blockSize+100), time.Hour)
			want := test.want
			if want == errInjected {
				want = injected
			}
			if !errors.Is(err, want) {
				t.Fatalf("Publish() error = %v, want cause %v", err, want)
			}
			assertActiveLineage(t, cache, oldLineageID, old)
		})
	}
}

var errInjected = errors.New("injected failure")

func TestValidationFailureDoesNotMutateCache(t *testing.T) {
	cache := newFakeMemcache()
	publisher := testPublisher(t, cache)
	invalid := testSnapshot(newLineageID, 0)
	invalid.SchemaVersion = 0

	if err := publisher.Publish(t.Context(), invalid, time.Hour); err == nil {
		t.Fatal("Publish() error = nil")
	}
	if len(cache.items) != 0 {
		t.Fatalf("validation failure wrote %d cache items", len(cache.items))
	}
}

func TestPublicationDiagnosticsUseControlledClassificationAndStage(t *testing.T) {
	tests := []struct {
		name      string
		err       error
		errorType string
		detail    string
	}{
		{name: "invalid JSON", err: snapshot.ErrInvalidJSON, errorType: "snapshot_invalid_json", detail: "invalid_json"},
		{name: "invalid contract", err: snapshot.ErrInvalidContract, errorType: "snapshot_invalid_contract", detail: "invalid_contract"},
		{name: "unsupported version", err: snapshot.ErrUnsupportedVersion, errorType: "snapshot_unsupported_version", detail: "unsupported_version"},
		{name: "collision", err: errLineageCollision, errorType: "conflict", detail: "lineage_collision"},
		{name: "unknown", err: errors.New("raw-value-must-not-leak"), errorType: "failed", detail: "other"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			failure := wrapPublicationFailure(publicationStageValidation, test.err)
			if classifyError(failure) != test.errorType || errorDetail(failure) != test.detail ||
				publicationStage(failure) != publicationStageValidation ||
				strings.Contains(failure.Error(), "raw-value-must-not-leak") {
				t.Fatalf("diagnostic = %q type=%q detail=%q stage=%q",
					failure, classifyError(failure), errorDetail(failure), publicationStage(failure))
			}
		})
	}

	cache := newFakeMemcache()
	var logs bytes.Buffer
	spanExporter := tracetest.NewInMemoryExporter()
	tracerProvider := tracesdk.NewTracerProvider(tracesdk.WithSyncer(spanExporter))
	t.Cleanup(func() { _ = tracerProvider.Shutdown(t.Context()) })
	publisher, err := newPublisher(
		cache,
		slog.New(slog.NewJSONHandler(&logs, nil)),
		tracerProvider.Tracer("test/lineage"),
		metricnoop.NewMeterProvider().Meter("test/lineage"),
	)
	if err != nil {
		t.Fatalf("newPublisher() error = %v", err)
	}
	invalid := testSnapshot(newLineageID, 0)
	invalid.ObservedAt = "response-value-must-not-leak"
	if err := publisher.Publish(t.Context(), invalid, time.Hour); !errors.Is(err, snapshot.ErrInvalidContract) {
		t.Fatalf("Publish() error = %v", err)
	}
	wantDetail := "snapshot.observedAt: must be a UTC RFC 3339 timestamp"
	if !strings.Contains(logs.String(), `"error.type":"snapshot_invalid_contract"`) ||
		!strings.Contains(logs.String(), `"error.detail":"`+wantDetail+`"`) ||
		!strings.Contains(logs.String(), `"publication.stage":"validation"`) ||
		strings.Contains(logs.String(), "response-value-must-not-leak") {
		t.Fatalf("publication logs = %s", logs.String())
	}

	for _, span := range spanExporter.GetSpans() {
		if span.Name != "lineage.validate" {
			continue
		}
		attributes := make(map[string]string, len(span.Attributes))
		for _, item := range span.Attributes {
			attributes[string(item.Key)] = item.Value.Emit()
		}
		if attributes["error.type"] != "snapshot_invalid_contract" ||
			attributes["error.detail"] != wantDetail ||
			attributes["publication.stage"] != publicationStageValidation {
			t.Fatalf("validation span attributes = %#v", attributes)
		}
		for _, item := range span.Attributes {
			if strings.Contains(item.Value.Emit(), invalid.ObservedAt) {
				t.Fatalf("validation span disclosed rejected value: %#v", span.Attributes)
			}
		}
		for _, event := range span.Events {
			for _, item := range event.Attributes {
				if strings.Contains(item.Value.Emit(), invalid.ObservedAt) {
					t.Fatalf("validation span event disclosed rejected value: %#v", event.Attributes)
				}
			}
		}

		return
	}

	t.Fatal("lineage.validate span missing")
}

func TestExpiryDuringPublicationDoesNotMutateLineageKeys(t *testing.T) {
	cache := newFakeMemcache()
	publisher := testPublisher(t, cache)
	started := publisher.now()
	calls := 0
	publisher.now = func() time.Time {
		calls++
		if calls == 1 {
			return started
		}

		return started.Add(3 * time.Hour)
	}

	err := publisher.Publish(t.Context(), testSnapshot(newLineageID, 0), time.Hour)
	if !errors.Is(err, errExpiredPublication) {
		t.Fatalf("Publish() error = %v, want expired publication", err)
	}
	for key := range cache.items {
		if strings.HasPrefix(key, lineageKeyPrefix+newLineageID) || key == activePointerKey {
			t.Fatalf("expired publication wrote key %q", key)
		}
	}
}

func TestLineageKeysAreImmutable(t *testing.T) {
	cache := newFakeMemcache()
	publisher := testPublisher(t, cache)
	value := testSnapshot(oldLineageID, 0)
	publish(t, publisher, t.Context(), value)

	if err := publisher.Publish(t.Context(), value, time.Hour); !errors.Is(err, errLineageCollision) {
		t.Fatalf("second Publish() error = %v, want collision", err)
	}
	assertActiveLineage(t, cache, oldLineageID, value)
}

func TestCandidateKeyCollisionPreservesActiveLineage(t *testing.T) {
	cache := newFakeMemcache()
	publisher := testPublisher(t, cache)
	old := testSnapshot(oldLineageID, 0)
	publish(t, publisher, t.Context(), old)
	cache.items[blockKey(newLineageID, 0)] = &memcache.Item{Value: []byte("orphan")}

	err := publisher.Publish(t.Context(), testSnapshot(newLineageID, 0), time.Hour)
	if !errors.Is(err, memcache.ErrNotStored) {
		t.Fatalf("Publish() error = %v, want immutable-key collision", err)
	}
	assertActiveLineage(t, cache, oldLineageID, old)
}

func TestCancellationImmediatelyBeforePointerSetPreservesActiveLineage(t *testing.T) {
	cache := newFakeMemcache()
	publisher := testPublisher(t, cache)
	old := testSnapshot(oldLineageID, 0)
	publish(t, publisher, t.Context(), old)

	ctx, cancel := context.WithCancelCause(t.Context())
	failure := errors.New("cancel before commit")
	now := publisher.now()
	calls := 0
	publisher.now = func() time.Time {
		calls++
		if calls == 4 {
			cancel(failure)
		}

		return now
	}

	err := publisher.Publish(ctx, testSnapshot(newLineageID, 0), time.Hour)
	if !errors.Is(err, failure) {
		t.Fatalf("Publish() error = %v, want cancellation cause", err)
	}
	assertActiveLineage(t, cache, oldLineageID, old)
}

func TestAppliedPointerSetErrorIsReconciled(t *testing.T) {
	cache := newFakeMemcache()
	publisher := testPublisher(t, cache)
	publish(t, publisher, t.Context(), testSnapshot(oldLineageID, 0))

	injected := errors.New("response lost")
	cache.after = func(operation, key string) error {
		if operation == "set" && key == activePointerKey {
			return injected
		}
		return nil
	}
	value := testSnapshot(newLineageID, 0)
	if err := publisher.Publish(t.Context(), value, time.Hour); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
	assertActiveLineage(t, cache, newLineageID, value)
}

func TestUncertainPointerCommitPreservesBothCauses(t *testing.T) {
	cache := newFakeMemcache()
	publisher := testPublisher(t, cache)
	publish(t, publisher, t.Context(), testSnapshot(oldLineageID, 0))

	setFailure := errors.New("set response lost")
	getFailure := errors.New("reconciliation unavailable")
	setAttempted := false
	cache.before = func(operation, key string) error {
		if operation == "set" && key == activePointerKey {
			setAttempted = true
			return setFailure
		}
		if operation == "get" && key == activePointerKey && setAttempted {
			return getFailure
		}
		return nil
	}

	err := publisher.Publish(t.Context(), testSnapshot(newLineageID, 0), time.Hour)
	var uncertain *UncertainCommitError
	if !errors.As(err, &uncertain) || !errors.Is(err, setFailure) || !errors.Is(err, getFailure) {
		t.Fatalf("Publish() error = %v, want uncertain commit with both causes", err)
	}
}

func TestAppliedPointerSetThenMissIsUncertain(t *testing.T) {
	cache := newFakeMemcache()
	publisher := testPublisher(t, cache)
	publish(t, publisher, t.Context(), testSnapshot(oldLineageID, 0))

	setFailure := errors.New("set response lost")
	setApplied := false
	cache.after = func(operation, key string) error {
		if operation == "set" && key == activePointerKey {
			setApplied = true

			return setFailure
		}

		return nil
	}
	cache.before = func(operation, key string) error {
		if operation == "get" && key == activePointerKey && setApplied {
			delete(cache.items, key)

			return memcache.ErrCacheMiss
		}

		return nil
	}

	err := publisher.Publish(t.Context(), testSnapshot(newLineageID, 0), time.Hour)
	var uncertain *UncertainCommitError
	if !errors.As(err, &uncertain) || !errors.Is(err, setFailure) || !errors.Is(err, memcache.ErrCacheMiss) {
		t.Fatalf("Publish() error = %v, want uncertain commit with Set and miss causes", err)
	}
	if strings.Contains(err.Error(), oldLineageID) || strings.Contains(err.Error(), newLineageID) ||
		strings.Contains(err.Error(), activePointerKey) {
		t.Fatalf("uncertain error disclosed a lineage value or cache key: %v", err)
	}
}

func TestUnexpectedPointerAfterSetFailureIsUncertainAndValueFree(t *testing.T) {
	cache := newFakeMemcache()
	old := testSnapshot(oldLineageID, 0)
	publish(t, testPublisher(t, cache), t.Context(), old)

	spanExporter := tracetest.NewInMemoryExporter()
	tracerProvider := tracesdk.NewTracerProvider(tracesdk.WithSyncer(spanExporter))
	t.Cleanup(func() { _ = tracerProvider.Shutdown(t.Context()) })
	var logs bytes.Buffer
	publisher, err := newPublisher(
		cache,
		slog.New(slog.NewJSONHandler(&logs, nil)),
		tracerProvider.Tracer("test/lineage"),
		metricnoop.NewMeterProvider().Meter("test/lineage"),
	)
	if err != nil {
		t.Fatalf("newPublisher() error = %v", err)
	}
	publisher.now = func() time.Time {
		return time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC)
	}

	setFailure := errors.New("set response lost")
	setAttempts := 0
	cache.before = func(operation, key string) error {
		if operation == "set" && key == activePointerKey {
			setAttempts++

			return setFailure
		}

		return nil
	}
	cache.transformGet = func(key string, value []byte) []byte {
		if key == activePointerKey && setAttempts > 0 {
			return []byte(unexpectedLineageID)
		}

		return value
	}

	err = publisher.Publish(t.Context(), testSnapshot(newLineageID, 0), time.Hour)
	var uncertain *UncertainCommitError
	if !errors.As(err, &uncertain) || !errors.Is(err, setFailure) || !errors.Is(err, errUnexpectedPointer) {
		t.Fatalf("Publish() error = %v, want uncertain commit with Set and unexpected-pointer causes", err)
	}
	for _, secret := range []string{activePointerKey, oldLineageID, newLineageID, unexpectedLineageID} {
		if strings.Contains(err.Error(), secret) {
			t.Fatalf("uncertain diagnostic disclosed pointer data %q: %v", secret, err)
		}
	}
	for _, span := range spanExporter.GetSpans() {
		if strings.HasPrefix(span.Name, "memcached.") {
			assertCacheAttributes(t, span.Attributes)
		}
		for _, item := range span.Attributes {
			if strings.Contains(item.Value.Emit(), oldLineageID) ||
				strings.Contains(item.Value.Emit(), unexpectedLineageID) ||
				strings.Contains(item.Value.Emit(), activePointerKey) {
				t.Fatalf("span %q disclosed reconciled pointer data: %#v", span.Name, span.Attributes)
			}
		}
		for _, event := range span.Events {
			for _, item := range event.Attributes {
				if strings.Contains(item.Value.Emit(), oldLineageID) ||
					strings.Contains(item.Value.Emit(), unexpectedLineageID) ||
					strings.Contains(item.Value.Emit(), activePointerKey) {
					t.Fatalf("span event disclosed reconciled pointer data: %#v", event.Attributes)
				}
			}
		}
	}
	if strings.Contains(logs.String(), oldLineageID) || strings.Contains(logs.String(), unexpectedLineageID) ||
		strings.Contains(logs.String(), lineageKeyPrefix) {
		t.Fatalf("lifecycle log disclosed reconciled pointer data or cache key: %s", logs.String())
	}

	assertActiveLineage(t, cache, oldLineageID, old)
	cache.mu.Lock()
	defer cache.mu.Unlock()
	if setAttempts != 1 {
		t.Fatalf("active pointer Set attempts = %d, want one without rollback", setAttempts)
	}
	if len(cache.deleted) != 0 {
		t.Fatalf("uncertain commit deleted previous lineage keys: %v", cache.deleted)
	}
	if _, exists := cache.items[completionKey(newLineageID)]; !exists {
		t.Fatal("uncertain commit rolled back candidate completion")
	}
	if _, exists := cache.items[blockKey(newLineageID, 0)]; !exists {
		t.Fatal("uncertain commit rolled back candidate block")
	}
}

func TestPublicationUsesOneAbsoluteExpirationWithAdvancingClock(t *testing.T) {
	base := time.Date(2026, time.July, 28, 12, 0, 0, 250_000_000, time.UTC)
	tests := []struct {
		name     string
		interval time.Duration
		advance  time.Duration
	}{
		{name: "minimum", interval: time.Second, advance: 100 * time.Millisecond},
		{name: "within thirty days", interval: time.Hour, advance: 250 * time.Millisecond},
		{name: "thirty day boundary", interval: 15 * 24 * time.Hour, advance: time.Second},
		{name: "above thirty days", interval: 16 * 24 * time.Hour, advance: time.Second},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			cache := newFakeMemcache()
			publisher := testPublisher(t, cache)
			calls := 0
			publisher.now = func() time.Time {
				now := base.Add(time.Duration(calls) * test.advance)
				calls++

				return now
			}

			err := publisher.Publish(
				t.Context(),
				testSnapshot(newLineageID, blockSize+100),
				test.interval,
			)
			if err != nil {
				t.Fatalf("Publish() error = %v", err)
			}

			deadline := base.Add(2 * test.interval)
			want := deadline.Unix()
			if len(cache.items) != 4 {
				t.Fatalf("stored item count = %d, want 4", len(cache.items))
			}
			for key, item := range cache.items {
				if item.Expiration != int32(want) {
					t.Fatalf("item %q expiration = %d, want common absolute %d", key, item.Expiration, want)
				}
			}
		})
	}
}

func TestPublicationRejectsSubsecondAndFractionalSecondIntervals(t *testing.T) {
	for _, interval := range []time.Duration{time.Nanosecond, 999 * time.Millisecond, 1500 * time.Millisecond} {
		cache := newFakeMemcache()
		publisher := testPublisher(t, cache)

		err := publisher.Publish(t.Context(), testSnapshot(newLineageID, 0), interval)
		if !errors.Is(err, errInvalidInterval) {
			t.Fatalf("Publish(interval=%s) error = %v, want invalid interval", interval, err)
		}
		if len(cache.items) != 0 {
			t.Fatalf("Publish(interval=%s) wrote %d cache items", interval, len(cache.items))
		}
	}
}

func TestCleanupFailureKeepsNewLineageActive(t *testing.T) {
	cache := newFakeMemcache()
	publisher := testPublisher(t, cache)
	publish(t, publisher, t.Context(), testSnapshot(oldLineageID, 0))

	cleanupFailure := errors.New("delete unavailable")
	cache.before = func(operation, key string) error {
		if operation == "delete" && key == blockKey(oldLineageID, 0) {
			return cleanupFailure
		}
		return nil
	}
	value := testSnapshot(newLineageID, 0)
	err := publisher.Publish(t.Context(), value, time.Hour)
	var cleanup *CleanupError
	if !errors.As(err, &cleanup) || !errors.Is(err, cleanupFailure) {
		t.Fatalf("Publish() error = %v, want cleanup failure", err)
	}
	assertActiveLineage(t, cache, newLineageID, value)
	if _, exists := cache.items[completionKey(oldLineageID)]; exists {
		t.Fatal("old completion metadata was not deleted first")
	}
	if _, exists := cache.items[blockKey(oldLineageID, 0)]; !exists {
		t.Fatal("failed old block deletion did not leave TTL fallback")
	}
	if len(cache.deleted) == 0 || cache.deleted[0] != completionKey(oldLineageID) {
		t.Fatalf("deletion order = %v", cache.deleted)
	}
}

func TestActiveSizeGaugeFollowsPointerCommit(t *testing.T) {
	cache := newFakeMemcache()
	reader := metricsdk.NewManualReader()
	meterProvider := metricsdk.NewMeterProvider(metricsdk.WithReader(reader))
	t.Cleanup(func() { _ = meterProvider.Shutdown(t.Context()) })
	publisher, err := newPublisher(
		cache,
		slog.New(slog.NewJSONHandler(io.Discard, nil)),
		tracenoop.NewTracerProvider().Tracer("test/lineage"),
		meterProvider.Meter("test/lineage"),
	)
	if err != nil {
		t.Fatalf("newPublisher() error = %v", err)
	}
	publisher.now = func() time.Time {
		return time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC)
	}

	old := testSnapshot(oldLineageID, 10)
	publish(t, publisher, t.Context(), old)
	assertActiveSize(t, reader, serializedSize(t, old))

	cache.before = func(operation, key string) error {
		if operation == "add" && key == blockKey(newLineageID, 0) {
			return errInjected
		}

		return nil
	}
	if err := publisher.Publish(t.Context(), testSnapshot(newLineageID, 100), time.Hour); !errors.Is(err, errInjected) {
		t.Fatalf("failed Publish() error = %v", err)
	}
	assertActiveSize(t, reader, serializedSize(t, old))

	cache.before = func(operation, key string) error {
		if operation == "delete" && key == blockKey(oldLineageID, 0) {
			return errInjected
		}

		return nil
	}
	activated := testSnapshot(unexpectedLineageID, 200)
	err = publisher.Publish(t.Context(), activated, time.Hour)
	var cleanup *CleanupError
	if !errors.As(err, &cleanup) {
		t.Fatalf("cleanup Publish() error = %v", err)
	}
	assertActiveSize(t, reader, serializedSize(t, activated))
}

func TestMissingPreviousCompletionReportsCleanupAfterActivation(t *testing.T) {
	cache := newFakeMemcache()
	publisher := testPublisher(t, cache)
	publish(t, publisher, t.Context(), testSnapshot(oldLineageID, 0))
	delete(cache.items, completionKey(oldLineageID))

	value := testSnapshot(newLineageID, 0)
	err := publisher.Publish(t.Context(), value, time.Hour)
	var cleanup *CleanupError
	if !errors.As(err, &cleanup) || !errors.Is(err, errPreviousIncomplete) {
		t.Fatalf("Publish() error = %v, want incomplete previous cleanup", err)
	}
	assertActiveLineage(t, cache, newLineageID, value)
}

func TestPublicationTelemetryIsBoundedAndKeyFree(t *testing.T) {
	spanExporter := tracetest.NewInMemoryExporter()
	tracerProvider := tracesdk.NewTracerProvider(tracesdk.WithSyncer(spanExporter))
	t.Cleanup(func() { _ = tracerProvider.Shutdown(t.Context()) })
	reader := metricsdk.NewManualReader()
	meterProvider := metricsdk.NewMeterProvider(metricsdk.WithReader(reader))
	t.Cleanup(func() { _ = meterProvider.Shutdown(t.Context()) })
	var logs bytes.Buffer

	publisher, err := newPublisher(
		newFakeMemcache(),
		slog.New(slog.NewJSONHandler(&logs, nil)),
		tracerProvider.Tracer("test/lineage"),
		meterProvider.Meter("test/lineage"),
	)
	if err != nil {
		t.Fatalf("newPublisher() error = %v", err)
	}
	publisher.now = func() time.Time {
		return time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC)
	}
	publish(t, publisher, t.Context(), testSnapshot(newLineageID, 0))

	cacheSpans := 0
	for _, span := range spanExporter.GetSpans() {
		if !strings.HasPrefix(span.Name, "memcached.") {
			continue
		}
		cacheSpans++
		if len(span.Attributes) != 2 {
			t.Fatalf("cache span %q attributes = %#v", span.Name, span.Attributes)
		}
		assertCacheAttributes(t, span.Attributes)
		for _, event := range span.Events {
			for _, item := range event.Attributes {
				if strings.Contains(item.Value.Emit(), lineageKeyPrefix) ||
					strings.Contains(item.Value.Emit(), newLineageID) {
					t.Fatalf("cache span event disclosed key or lineage ID: %#v", event.Attributes)
				}
			}
		}
	}
	if cacheSpans != 6 {
		t.Fatalf("cache span count = %d, want 6", cacheSpans)
	}
	if strings.Contains(logs.String(), lineageKeyPrefix) || strings.Contains(logs.String(), activePointerKey) {
		t.Fatalf("lifecycle log disclosed cache key: %s", logs.String())
	}
	if !strings.Contains(logs.String(), `"lineage.id":"`+newLineageID+`"`) {
		t.Fatalf("activation log omitted permitted lineage correlation: %s", logs.String())
	}

	var collected metricdata.ResourceMetrics
	if err := reader.Collect(t.Context(), &collected); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	cacheMetricCount := 0
	pointCount := uint64(0)
	for _, scope := range collected.ScopeMetrics {
		for _, item := range scope.Metrics {
			if item.Name == "lineage.active.size" {
				data, ok := item.Data.(metricdata.Gauge[int64])
				if !ok || item.Unit != "By" || len(data.DataPoints) != 1 || data.DataPoints[0].Attributes.Len() != 0 {
					t.Fatalf("active-size metric = %#v", item)
				}

				continue
			}

			cacheMetricCount++
			switch data := item.Data.(type) {
			case metricdata.Sum[int64]:
				for _, point := range data.DataPoints {
					pointCount += uint64(point.Value)
					assertCacheAttributes(t, point.Attributes.ToSlice())
				}
			case metricdata.Histogram[float64]:
				for _, point := range data.DataPoints {
					pointCount += point.Count
					assertCacheAttributes(t, point.Attributes.ToSlice())
				}
			default:
				t.Fatalf("unexpected cache metric type %T", item.Data)
			}
		}
	}
	if cacheMetricCount != 2 || pointCount != 12 {
		t.Fatalf("cache metrics=%d total points=%d, want 2 and 12", cacheMetricCount, pointCount)
	}
}

func serializedSize(t *testing.T, value snapshot.Snapshot) int64 {
	t.Helper()

	data, err := snapshot.Marshal(value)
	if err != nil {
		t.Fatalf("snapshot.Marshal() error = %v", err)
	}

	return int64(len(data))
}

func assertActiveSize(t *testing.T, reader *metricsdk.ManualReader, want int64) {
	t.Helper()

	var collected metricdata.ResourceMetrics
	if err := reader.Collect(t.Context(), &collected); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	for _, scope := range collected.ScopeMetrics {
		for _, item := range scope.Metrics {
			if item.Name != "lineage.active.size" {
				continue
			}

			data, ok := item.Data.(metricdata.Gauge[int64])
			if !ok || item.Unit != "By" || len(data.DataPoints) != 1 ||
				data.DataPoints[0].Value != want || data.DataPoints[0].Attributes.Len() != 0 {
				t.Fatalf("active size = %#v, want %d By without attributes", item, want)
			}

			return
		}
	}

	t.Fatal("lineage.active.size metric missing")
}

func assertCacheAttributes(t *testing.T, attributes []attribute.KeyValue) {
	t.Helper()
	if len(attributes) != 2 {
		t.Fatalf("cache attributes = %#v", attributes)
	}
	for _, item := range attributes {
		if item.Key != "cache.operation" && item.Key != "cache.outcome" {
			t.Fatalf("unexpected cache attribute %q", item.Key)
		}
		value := item.Value.Emit()
		if strings.Contains(value, lineageKeyPrefix) || strings.Contains(value, oldLineageID) ||
			strings.Contains(value, newLineageID) {
			t.Fatalf("cache attribute disclosed key or lineage ID: %s=%s", item.Key, value)
		}
	}
}

func testPublisher(t *testing.T, cache cacheClient) *Publisher {
	t.Helper()
	publisher, err := newPublisher(
		cache,
		slog.New(slog.NewJSONHandler(io.Discard, nil)),
		tracenoop.NewTracerProvider().Tracer("test/lineage"),
		metricnoop.NewMeterProvider().Meter("test/lineage"),
	)
	if err != nil {
		t.Fatalf("newPublisher() error = %v", err)
	}
	publisher.now = func() time.Time {
		return time.Date(2026, time.July, 28, 12, 0, 0, 0, time.UTC)
	}

	return publisher
}

func publish(t *testing.T, publisher *Publisher, ctx context.Context, value snapshot.Snapshot) {
	t.Helper()
	if err := publisher.Publish(ctx, value, time.Hour); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}
}

func testSnapshot(lineageID string, padding int) snapshot.Snapshot {
	items := []snapshot.BoardItem{{
		Board: json.RawMessage(fmt.Sprintf(`{"board":"a","padding":"%s"}`, strings.Repeat("x", padding))),
	}}

	return snapshot.Snapshot{
		SchemaVersion: snapshot.Version,
		LineageID:     lineageID,
		ObservedAt:    "2026-07-28T12:00:00Z",
		Boards:        snapshot.Boards{State: snapshot.StatePresent, Items: &items},
	}
}

func assertActiveLineage(t *testing.T, cache *fakeMemcache, lineageID string, want snapshot.Snapshot) {
	t.Helper()
	cache.mu.Lock()
	defer cache.mu.Unlock()

	pointer, exists := cache.items[activePointerKey]
	if !exists || string(pointer.Value) != lineageID {
		t.Fatalf("active pointer = %#v, want %s", pointer, lineageID)
	}
	metadataItem, exists := cache.items[completionKey(lineageID)]
	if !exists {
		t.Fatal("active completion metadata is missing")
	}
	metadata, err := decodeCompletion(metadataItem.Value)
	if err != nil {
		t.Fatalf("decodeCompletion() error = %v", err)
	}
	blocks := make([][]byte, metadata.BlockCount)
	for index := range metadata.BlockCount {
		item, exists := cache.items[blockKey(lineageID, index)]
		if !exists {
			t.Fatalf("active block %d is missing", index)
		}
		blocks[index] = item.Value
	}
	data, err := reassemble(metadata, blocks)
	if err != nil {
		t.Fatalf("reassemble() error = %v", err)
	}
	got, err := snapshot.Parse(data)
	if err != nil {
		t.Fatalf("snapshot.Parse() error = %v", err)
	}
	wantData, err := snapshot.Marshal(want)
	if err != nil {
		t.Fatalf("snapshot.Marshal() error = %v", err)
	}
	gotData, err := snapshot.Marshal(got)
	if err != nil {
		t.Fatalf("snapshot.Marshal(got) error = %v", err)
	}
	if !bytes.Equal(gotData, wantData) {
		t.Fatal("active lineage bytes changed")
	}
}
