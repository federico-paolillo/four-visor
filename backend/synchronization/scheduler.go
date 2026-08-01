// Package synchronization schedules and observes one-at-a-time lineage replacement.
package synchronization

import (
	"context"
	"log/slog"
	"sync"
	"time"
)

const (
	minimumStartupJitter = 5 * time.Second
	maximumStartupJitter = 60 * time.Second
	// LineageDeadline bounds every complete snapshot observation.
	LineageDeadline = 30 * time.Minute
)

// Run waits for the instance jitter, then consumes a fixed cadence until cancellation.
func (scheduler *Scheduler) Run(ctx context.Context) {
	if !waitForStartup(ctx, scheduler.jitter) {
		return
	}

	scheduler.runCadence(ctx)
}

func waitForStartup(ctx context.Context, jitter time.Duration) bool {
	startup := time.NewTimer(jitter)
	defer startup.Stop()

	select {
	case <-ctx.Done():
		return false
	case <-startup.C:
		return true
	}
}

func (scheduler *Scheduler) runCadence(ctx context.Context) {
	ticker := time.NewTicker(scheduler.interval)
	defer ticker.Stop()

	completed := make(chan time.Time, 1)
	active := false

	var completedAt time.Time

	var runs sync.WaitGroup

	start := func() {
		active = true

		runs.Go(func() {
			scheduler.synchronize(ctx)

			completed <- time.Now()
		})
	}

	start()

	for {
		select {
		case <-ctx.Done():
			runs.Wait()

			return
		case completedAt = <-completed:
			active = false
		case tick := <-ticker.C:
			active, completedAt = scheduler.consumeTick(ctx, tick, active, completedAt, completed, start)
		}
	}
}

func (scheduler *Scheduler) consumeTick(
	ctx context.Context,
	tick time.Time,
	active bool,
	completedAt time.Time,
	completed <-chan time.Time,
	start func(),
) (bool, time.Time) {
	if active {
		select {
		case completedAt = <-completed:
			active = false
		default:
		}
	}

	// A tick delivered before completion stays stale even when this select observes completion first.
	if active || (!completedAt.IsZero() && !tick.After(completedAt)) {
		scheduler.logger.WarnContext(ctx, "synchronization tick skipped",
			slog.String("scheduler.reason", "synchronization_active"),
		)

		return active, completedAt
	}

	if ctx.Err() != nil {
		return false, completedAt
	}

	start()

	return true, completedAt
}
