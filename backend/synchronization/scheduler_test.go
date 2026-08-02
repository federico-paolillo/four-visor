package synchronization

import (
	"bytes"
	"context"
	"io"
	"log/slog"
	"strings"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"git.disroot.org/federico-paolillo/four-visor.git/snapshot"
	metricnoop "go.opentelemetry.io/otel/metric/noop"
	tracenoop "go.opentelemetry.io/otel/trace/noop"
)

func TestStartupJitterRangeAndInstanceReuse(t *testing.T) {
	minimum, err := startupJitter(bytes.NewReader([]byte{0}))
	if err != nil || minimum != minimumStartupJitter {
		t.Fatalf("startupJitter(minimum) = %s, %v", minimum, err)
	}

	maximum, err := startupJitter(bytes.NewReader([]byte{55}))
	if err != nil || maximum != maximumStartupJitter {
		t.Fatalf("startupJitter(maximum) = %s, %v", maximum, err)
	}

	scheduler := testScheduler(t, time.Hour, io.Discard, func(context.Context, string) (snapshot.Boards, error) {
		return failedBoards(), nil
	})
	if scheduler.jitter != minimumStartupJitter {
		t.Fatalf("stored jitter = %s, want %s", scheduler.jitter, minimumStartupJitter)
	}
	for range 10 {
		if scheduler.jitter != minimumStartupJitter {
			t.Fatalf("stored jitter changed to %s", scheduler.jitter)
		}
	}
}

func TestFixedCadenceStartsAfterJitter(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		starts := make(chan time.Time, 3)
		scheduler := testScheduler(t, 10*time.Second, io.Discard, func(context.Context, string) (snapshot.Boards, error) {
			starts <- time.Now()

			return failedBoards(), nil
		})
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})
		go func() {
			scheduler.Run(ctx)
			close(done)
		}()

		initial := time.Now()
		first := <-starts
		second := <-starts
		third := <-starts
		if first.Sub(initial) != minimumStartupJitter || second.Sub(first) != 10*time.Second ||
			third.Sub(second) != 10*time.Second {
			t.Fatalf("starts = [%s %s %s] from %s", first, second, third, initial)
		}

		cancel()
		<-done
	})
}

func TestActiveTicksAreSkippedWithoutCatchUp(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var logs bytes.Buffer
		entered := make(chan time.Time, 2)
		release := make(chan struct{})
		var calls atomic.Int64
		scheduler := testScheduler(t, 10*time.Second, &logs, func(ctx context.Context, _ string) (snapshot.Boards, error) {
			call := calls.Add(1)
			entered <- time.Now()
			if call == 1 {
				select {
				case <-release:
				case <-ctx.Done():
					return snapshot.Boards{}, context.Cause(ctx)
				}
			}

			return failedBoards(), nil
		})
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})
		go func() {
			scheduler.Run(ctx)
			close(done)
		}()

		first := <-entered
		time.Sleep(21 * time.Second)
		synctest.Wait()
		if got := strings.Count(logs.String(), "synchronization tick skipped"); got != 2 {
			t.Fatalf("skip logs = %d, want 2: %s", got, logs.String())
		}

		close(release)
		second := <-entered
		if got := second.Sub(first); got != 30*time.Second {
			t.Fatalf("next start after skipped ticks = %s, want 30s", got)
		}
		if calls.Load() != 2 {
			t.Fatalf("run calls = %d, want 2", calls.Load())
		}

		cancel()
		<-done
	})
}

func TestShutdownCancelsAndWaitsForActiveRun(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		entered := make(chan struct{})
		canceled := make(chan struct{})
		release := make(chan struct{})
		scheduler := testScheduler(t, time.Hour, io.Discard, func(ctx context.Context, _ string) (snapshot.Boards, error) {
			close(entered)
			<-ctx.Done()
			close(canceled)
			<-release

			return snapshot.Boards{}, context.Cause(ctx)
		})
		ctx, cancel := context.WithCancel(t.Context())
		done := make(chan struct{})
		go func() {
			scheduler.Run(ctx)
			close(done)
		}()

		<-entered
		cancel()
		<-canceled
		synctest.Wait()
		select {
		case <-done:
			t.Fatal("Scheduler.Run returned before active synchronization stopped")
		default:
		}

		close(release)
		<-done
	})
}

func TestStaleActiveTickIsSkippedAfterCompletionWasObserved(t *testing.T) {
	var logs bytes.Buffer
	started := false
	scheduler := testScheduler(t, time.Hour, &logs, func(context.Context, string) (snapshot.Boards, error) {
		return failedBoards(), nil
	})
	completedAt := time.Now()

	active, gotCompletedAt := scheduler.consumeTick(
		t.Context(),
		completedAt.Add(-time.Second),
		false,
		completedAt,
		nil,
		func() { started = true },
	)
	if active || started || gotCompletedAt != completedAt {
		t.Fatalf("stale tick active=%t started=%t completedAt=%s", active, started, gotCompletedAt)
	}
	if got := strings.Count(logs.String(), "synchronization tick skipped"); got != 1 {
		t.Fatalf("skip logs = %d, want 1: %s", got, logs.String())
	}
}

func testScheduler(
	t *testing.T,
	interval time.Duration,
	logs io.Writer,
	observe func(context.Context, string) (snapshot.Boards, error),
) *Scheduler {
	t.Helper()

	scheduler, err := newScheduler(interval, 10, schedulerDependencies{
		observe: observe,
		publish: func(context.Context, snapshot.Snapshot, time.Duration) error {
			return nil
		},
		logger:         slog.New(slog.NewJSONHandler(logs, nil)),
		tracer:         tracenoop.NewTracerProvider().Tracer("test/synchronization"),
		meter:          metricnoop.NewMeterProvider().Meter("test/synchronization"),
		jitterEntropy:  bytes.NewReader([]byte{0}),
		lineageEntropy: bytes.NewReader(make([]byte, 256)),
		deadline:       time.Hour,
	})
	if err != nil {
		t.Fatalf("newScheduler() error = %v", err)
	}

	return scheduler
}

func failedBoards() snapshot.Boards {
	return snapshot.Boards{State: snapshot.StateFailed}
}
