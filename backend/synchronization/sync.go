package synchronization

import (
	"context"
	cryptorand "crypto/rand"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/big"
	"sync/atomic"
	"time"

	"git.disroot.org/federico-paolillo/four-visor.git/acquisition"
	"git.disroot.org/federico-paolillo/four-visor.git/lineage"
	"git.disroot.org/federico-paolillo/four-visor.git/snapshot"
	"git.disroot.org/federico-paolillo/four-visor.git/telemetry"
	"github.com/oklog/ulid/v2"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

const (
	outcomeSuccess  = "success"
	outcomeDegraded = "degraded"
	outcomeFailed   = "failed"
)

var (
	errInvalidScheduler     = errors.New("invalid synchronization scheduler")
	errIdentifierGeneration = errors.New("lineage identifier generation failed")
)

type schedulerDependencies struct {
	observe        func(context.Context) (snapshot.Boards, error)
	publish        func(context.Context, snapshot.Snapshot, time.Duration) error
	logger         *slog.Logger
	tracer         trace.Tracer
	meter          metric.Meter
	jitterEntropy  io.Reader
	lineageEntropy io.Reader
	deadline       time.Duration
}

// Scheduler owns the instance-local cadence and one active lineage construction.
type Scheduler struct {
	interval         time.Duration
	tolerance        int
	jitter           time.Duration
	deadline         time.Duration
	observe          func(context.Context) (snapshot.Boards, error)
	publish          func(context.Context, snapshot.Snapshot, time.Duration) error
	logger           *slog.Logger
	tracer           trace.Tracer
	lineageEntropy   io.Reader
	duration         metric.Float64Histogram
	activations      metric.Int64Counter
	failedResources  metric.Int64Histogram
	activeObservedAt atomic.Int64
}

// New creates a production scheduler with cryptographic instance jitter and monotonic ULID entropy.
func New(
	interval time.Duration,
	tolerance int,
	client *acquisition.Client,
	publisher *lineage.Publisher,
	logger *slog.Logger,
	tracer trace.Tracer,
	meter metric.Meter,
) (*Scheduler, error) {
	if client == nil || publisher == nil {
		return nil, errInvalidScheduler
	}

	return newScheduler(interval, tolerance, schedulerDependencies{
		observe:        client.Observe,
		publish:        publisher.Publish,
		logger:         logger,
		tracer:         tracer,
		meter:          meter,
		jitterEntropy:  cryptorand.Reader,
		lineageEntropy: ulid.DefaultEntropy(),
		deadline:       lineageDeadline,
	})
}

func newScheduler(interval time.Duration, tolerance int, dependencies schedulerDependencies) (*Scheduler, error) {
	if tolerance < 0 || !dependencies.valid() {
		return nil, errInvalidScheduler
	}

	err := lineage.ValidateSynchronizationInterval(interval)
	if err != nil {
		return nil, fmt.Errorf("%w: synchronization interval: %w", errInvalidScheduler, err)
	}

	jitter, err := startupJitter(dependencies.jitterEntropy)
	if err != nil {
		return nil, fmt.Errorf("%w: deriving startup jitter: %w", errInvalidScheduler, err)
	}

	scheduler := &Scheduler{
		interval:       interval,
		tolerance:      tolerance,
		jitter:         jitter,
		deadline:       dependencies.deadline,
		observe:        dependencies.observe,
		publish:        dependencies.publish,
		logger:         dependencies.logger,
		tracer:         dependencies.tracer,
		lineageEntropy: dependencies.lineageEntropy,
	}

	err = scheduler.createMetrics(dependencies.meter)
	if err != nil {
		return nil, err
	}

	return scheduler, nil
}

func (dependencies schedulerDependencies) valid() bool {
	return dependencies.observe != nil && dependencies.publish != nil && dependencies.logger != nil &&
		dependencies.tracer != nil && dependencies.meter != nil && dependencies.jitterEntropy != nil &&
		dependencies.lineageEntropy != nil && dependencies.deadline > 0
}

func startupJitter(entropy io.Reader) (time.Duration, error) {
	seconds, err := cryptorand.Int(entropy, big.NewInt(int64((maximumStartupJitter-minimumStartupJitter)/time.Second)+1))
	if err != nil {
		return 0, fmt.Errorf("reading randomness: %w", err)
	}

	return minimumStartupJitter + time.Duration(seconds.Int64())*time.Second, nil
}

func (scheduler *Scheduler) createMetrics(meter metric.Meter) error {
	var err error

	scheduler.duration, err = telemetry.LineageSynchronizationDuration.Float64Histogram(meter)
	if err != nil {
		return fmt.Errorf("creating lineage synchronization duration: %w", err)
	}

	scheduler.activations, err = telemetry.LineageSynchronizationActivated.Int64Counter(meter)
	if err != nil {
		return fmt.Errorf("creating lineage activation counter: %w", err)
	}

	scheduler.failedResources, err = telemetry.LineageFailedResourceCount.Int64Histogram(meter)
	if err != nil {
		return fmt.Errorf("creating failed resource count: %w", err)
	}

	_, err = telemetry.LineageActiveAge.Float64ObservableGauge(meter,
		func(_ context.Context, observer metric.Float64Observer) error {
			observedAt := scheduler.activeObservedAt.Load()
			if observedAt == 0 {
				return nil
			}

			age := time.Since(time.Unix(0, observedAt)).Seconds()
			observer.Observe(max(age, 0))

			return nil
		},
	)
	if err != nil {
		return fmt.Errorf("creating active lineage age: %w", err)
	}

	return nil
}

func (scheduler *Scheduler) synchronize(parent context.Context) {
	ctx, root := scheduler.tracer.Start(parent, "lineage.synchronize", trace.WithNewRoot())
	startedAt := time.Now().UTC()
	outcome := outcomeFailed

	defer func() {
		scheduler.duration.Record(ctx, time.Since(startedAt).Seconds(), metric.WithAttributes(
			attribute.String("lineage.outcome", outcome),
		))
		root.SetAttributes(attribute.String("lineage.outcome", outcome))
		root.End()
	}()

	value, counts, err := scheduler.construct(ctx, root, startedAt)
	if err != nil {
		scheduler.fail(ctx, root, synchronizationErrorType(err), value.LineageID)

		return
	}

	publishError := scheduler.publishLineage(ctx, value)

	var cleanupError *lineage.CleanupError

	if publishError != nil && !errors.As(publishError, &cleanupError) {
		markSynchronizationFailed(root, synchronizationErrorType(publishError))

		return
	}

	outcome = outcomeSuccess
	if counts.failed > 0 {
		outcome = outcomeDegraded
	}

	scheduler.recordActivation(ctx, startedAt, outcome, counts.failed)
	scheduler.complete(ctx, root, value.LineageID, outcome, counts.failed, cleanupError)
}

func (scheduler *Scheduler) construct(
	ctx context.Context,
	root trace.Span,
	startedAt time.Time,
) (snapshot.Snapshot, resourceCounts, error) {
	identifier, err := ulid.New(ulid.Timestamp(startedAt), scheduler.lineageEntropy)
	if err != nil {
		return snapshot.Snapshot{}, resourceCounts{}, fmt.Errorf("%w: %w", errIdentifierGeneration, err)
	}

	lineageID := identifier.String()
	root.SetAttributes(
		attribute.String("lineage.id", lineageID),
		attribute.Int("lineage.failed_resource.tolerance", scheduler.tolerance),
	)
	scheduler.logger.InfoContext(ctx, "synchronization started",
		slog.String("lineage.id", lineageID),
		slog.String("lineage.observed_at", startedAt.Format(time.RFC3339Nano)),
	)

	value := snapshot.Snapshot{
		SchemaVersion: snapshot.Version,
		LineageID:     lineageID,
		ObservedAt:    startedAt.Format(time.RFC3339Nano),
	}
	boards, err := scheduler.acquire(ctx)

	contextError := context.Cause(ctx)
	if contextError != nil {
		return value, resourceCounts{}, fmt.Errorf("acquisition canceled: %w", errors.Join(ctx.Err(), contextError))
	}

	if err != nil {
		return value, resourceCounts{}, err
	}

	counts := countResources(boards)
	root.SetAttributes(
		attribute.Int("resource.board.count", counts.boards),
		attribute.Int("resource.catalog.count", counts.catalogs),
		attribute.Int("resource.thread.count", counts.threads),
		attribute.Int("lineage.failed_resource.count", counts.failed),
	)
	scheduler.logger.InfoContext(ctx, "outbound acquisition completed",
		slog.String("lineage.id", lineageID),
		slog.Int("resource.board.count", counts.boards),
		slog.Int("resource.catalog.count", counts.catalogs),
		slog.Int("resource.thread.count", counts.threads),
		slog.Int("resource.failed.count", counts.failed),
	)

	value.Boards = boards

	return value, counts, nil
}

func (scheduler *Scheduler) recordActivation(
	ctx context.Context,
	startedAt time.Time,
	outcome string,
	failedResources int,
) {
	scheduler.activeObservedAt.Store(startedAt.UnixNano())

	measurement := metric.WithAttributes(attribute.String("lineage.outcome", outcome))
	scheduler.activations.Add(ctx, 1, measurement)
	scheduler.failedResources.Record(ctx, int64(failedResources))
}

func (scheduler *Scheduler) complete(
	ctx context.Context,
	root trace.Span,
	lineageID string,
	outcome string,
	failedResources int,
	cleanupError *lineage.CleanupError,
) {
	excessive := failedResources > scheduler.tolerance

	if cleanupError != nil {
		markRootError(root, "lineage cleanup failed")

		message := "synchronization completed with cleanup failure"
		if excessive {
			message = "excessively degraded lineage activated with cleanup failure"
		}

		scheduler.logger.ErrorContext(ctx, message,
			slog.String("lineage.id", lineageID),
			slog.String("lineage.outcome", outcome),
			slog.Int("resource.failed.count", failedResources),
			slog.Int("resource.failed.tolerance", scheduler.tolerance),
			slog.Bool("lineage.degradation.excessive", excessive),
			slog.String("error.type", "cleanup_failed"),
		)

		return
	}

	if excessive {
		markRootError(root, "lineage degradation exceeded tolerance")
		scheduler.logger.ErrorContext(ctx, "excessively degraded lineage activated",
			slog.String("lineage.id", lineageID),
			slog.String("lineage.outcome", outcome),
			slog.Int("resource.failed.count", failedResources),
			slog.Int("resource.failed.tolerance", scheduler.tolerance),
		)

		return
	}

	scheduler.logger.InfoContext(ctx, "synchronization completed",
		slog.String("lineage.id", lineageID),
		slog.String("lineage.outcome", outcome),
		slog.Int("resource.failed.count", failedResources),
		slog.Int("resource.failed.tolerance", scheduler.tolerance),
	)
}

func (scheduler *Scheduler) acquire(ctx context.Context) (snapshot.Boards, error) {
	acquisitionCtx, acquisitionSpan := scheduler.tracer.Start(ctx, "lineage.acquire")

	acquisitionCtx, cancel := context.WithTimeout(acquisitionCtx, scheduler.deadline)
	defer cancel()
	defer acquisitionSpan.End()

	boards, err := scheduler.observe(acquisitionCtx)
	if err != nil {
		finishStageSpan(acquisitionSpan, "acquisition_failed")
	}

	return boards, err
}

func (scheduler *Scheduler) publishLineage(ctx context.Context, value snapshot.Snapshot) error {
	publicationCtx, publicationSpan := scheduler.tracer.Start(ctx, "lineage.publish")
	defer publicationSpan.End()

	err := scheduler.publish(publicationCtx, value, scheduler.interval)
	if err != nil {
		finishStageSpan(publicationSpan, "publication_failed")
	}

	return err
}

func (scheduler *Scheduler) fail(ctx context.Context, root trace.Span, errorType, lineageID string) {
	markSynchronizationFailed(root, errorType)

	attributes := []any{slog.String("error.type", errorType)}
	if snapshot.ValidLineageID(lineageID) {
		attributes = append(attributes, slog.String("lineage.id", lineageID))
	}

	scheduler.logger.ErrorContext(ctx, "synchronization failed", attributes...)
}

func markSynchronizationFailed(root trace.Span, errorType string) {
	root.SetAttributes(attribute.String("error.type", errorType))
	markRootError(root, "lineage synchronization failed")
}

func finishStageSpan(span trace.Span, description string) {
	span.RecordError(errors.New(description))
	span.SetStatus(codes.Error, description)
}

func markRootError(span trace.Span, description string) {
	span.RecordError(errors.New(description))
	span.SetStatus(codes.Error, description)
}

func synchronizationErrorType(err error) string {
	switch {
	case errors.Is(err, errIdentifierGeneration):
		return "identifier_generation"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	}

	return "failed"
}

type resourceCounts struct {
	boards   int
	catalogs int
	threads  int
	failed   int
}

func countResources(boards snapshot.Boards) resourceCounts {
	counts := resourceCounts{failed: boards.FailedResourceCount()}
	if boards.Items == nil {
		return counts
	}

	counts.boards = len(*boards.Items)
	for _, board := range *boards.Items {
		if board.Catalog == nil {
			continue
		}

		counts.catalogs++

		if board.Catalog.Pages == nil {
			continue
		}

		for _, page := range *board.Catalog.Pages {
			counts.threads += len(page.Threads)
		}
	}

	return counts
}
