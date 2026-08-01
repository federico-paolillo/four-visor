package lineage

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net"
	"time"

	"git.disroot.org/federico-paolillo/four-visor.git/snapshot"
	"github.com/bradfitz/gomemcache/memcache"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

var (
	errInvalidPublisher   = errors.New("invalid lineage publisher")
	errVerificationFailed = errors.New("memcached lineage verification failed")
	errLineageCollision   = errors.New("lineage identifier is already active")
	errPreviousIncomplete = errors.New("previous lineage is incomplete")
	errUnexpectedPointer  = errors.New("active lineage reconciliation returned an unexpected value")
)

// CleanupError reports cleanup failure after the new lineage has become active.
type CleanupError struct {
	cause error
}

func (*CleanupError) Error() string {
	return "previous lineage cleanup failed after activation"
}

func (failure *CleanupError) Unwrap() error {
	return failure.cause
}

// UncertainCommitError reports a pointer write whose outcome could not be reconciled.
type UncertainCommitError struct {
	setError error
	getError error
}

func (*UncertainCommitError) Error() string {
	return "active lineage commit outcome is uncertain"
}

func (failure *UncertainCommitError) Unwrap() []error {
	return []error{failure.setError, failure.getError}
}

// Publisher validates and atomically activates immutable lineages in one Memcached server.
type Publisher struct {
	cache  *instrumentedCache
	logger *slog.Logger
	tracer trace.Tracer
	now    func() time.Time
}

type previousLineage struct {
	id         string
	metadata   completion
	found      bool
	cleanupErr error
}

type preparedLineage struct {
	id            string
	data          []byte
	blocks        [][]byte
	metadata      completion
	metadataBytes []byte
	deadline      time.Time
}

// NewPublisher creates a publisher with bounded Memcached socket operations.
func NewPublisher(
	address string,
	logger *slog.Logger,
	tracer trace.Tracer,
	meter metric.Meter,
) (*Publisher, error) {
	client := memcache.New(address)
	client.Timeout = memcache.DefaultTimeout

	return newPublisher(client, logger, tracer, meter)
}

func newPublisher(
	client cacheClient,
	logger *slog.Logger,
	tracer trace.Tracer,
	meter metric.Meter,
) (*Publisher, error) {
	if client == nil || logger == nil || tracer == nil || meter == nil {
		return nil, errInvalidPublisher
	}

	cache, err := newInstrumentedCache(client, tracer, meter)
	if err != nil {
		return nil, err
	}

	return &Publisher{cache: cache, logger: logger, tracer: tracer, now: time.Now}, nil
}

// Publish stores and verifies value before atomically replacing the active pointer.
func (publisher *Publisher) Publish(
	ctx context.Context,
	value snapshot.Snapshot,
	synchronizationInterval time.Duration,
) error {
	lineage, err := publisher.prepare(ctx, value, synchronizationInterval)
	if err != nil {
		publisher.logFailure(ctx, "lineage publication failed", value.LineageID, err)

		return err
	}

	previous, err := publisher.previous(ctx)
	if err != nil {
		publisher.logFailure(ctx, "lineage publication failed", lineage.id, err)

		return err
	}

	if previous.found && previous.id == lineage.id {
		publisher.logFailure(ctx, "lineage publication failed", lineage.id, errLineageCollision)

		return errLineageCollision
	}

	err = publisher.storeAndVerify(ctx, lineage)
	if err != nil {
		publisher.logFailure(ctx, "lineage publication failed", lineage.id, err)

		return err
	}

	expiration, err := publisher.activationExpiration(ctx, lineage.deadline)
	if err != nil {
		publisher.logFailure(ctx, "lineage publication failed", lineage.id, err)

		return err
	}

	err = publisher.activate(ctx, lineage.id, expiration, previous)
	if err != nil {
		publisher.logFailure(ctx, "lineage activation failed", lineage.id, err)

		return err
	}

	publisher.logger.InfoContext(ctx, "lineage activated", slog.String("lineage.id", lineage.id))

	return publisher.evictPrevious(context.WithoutCancel(ctx), previous)
}

func (publisher *Publisher) prepare(
	ctx context.Context,
	value snapshot.Snapshot,
	synchronizationInterval time.Duration,
) (preparedLineage, error) {
	data, err := publisher.validate(ctx, value)
	if err != nil {
		return preparedLineage{}, err
	}

	deadline, err := publicationDeadline(publisher.now(), synchronizationInterval)
	if err != nil {
		return preparedLineage{}, fmt.Errorf("calculating lineage expiry: %w", err)
	}

	blocks, metadata, err := splitBlocks(data)
	if err != nil {
		return preparedLineage{}, fmt.Errorf("splitting lineage blocks: %w", err)
	}

	metadataBytes, err := encodeCompletion(metadata)
	if err != nil {
		return preparedLineage{}, fmt.Errorf("encoding lineage completion: %w", err)
	}

	return preparedLineage{
		id:            value.LineageID,
		data:          data,
		blocks:        blocks,
		metadata:      metadata,
		metadataBytes: metadataBytes,
		deadline:      deadline,
	}, nil
}

func (publisher *Publisher) storeAndVerify(ctx context.Context, lineage preparedLineage) error {
	for index, block := range lineage.blocks {
		expiration, err := memcachedExpiration(publisher.now(), lineage.deadline)
		if err != nil {
			return fmt.Errorf("calculating block expiry: %w", err)
		}

		item := &memcache.Item{Key: blockKey(lineage.id, index), Value: block, Expiration: expiration}

		err = publisher.addBeforeActivation(ctx, item)
		if err != nil {
			return fmt.Errorf("writing lineage block: %w", err)
		}
	}

	verified := make([][]byte, len(lineage.blocks))
	for index, want := range lineage.blocks {
		item, err := publisher.getBeforeActivation(ctx, blockKey(lineage.id, index))
		if err != nil {
			return fmt.Errorf("verifying lineage block: %w", err)
		}

		if !bytes.Equal(item.Value, want) {
			return errVerificationFailed
		}

		verified[index] = item.Value
	}

	reassembled, err := reassemble(lineage.metadata, verified)
	if err != nil {
		return fmt.Errorf("%w: %w", errVerificationFailed, err)
	}

	if !bytes.Equal(reassembled, lineage.data) {
		return errVerificationFailed
	}

	return publisher.storeAndVerifyCompletion(ctx, lineage)
}

func (publisher *Publisher) storeAndVerifyCompletion(ctx context.Context, lineage preparedLineage) error {
	expiration, err := memcachedExpiration(publisher.now(), lineage.deadline)
	if err != nil {
		return fmt.Errorf("calculating completion expiry: %w", err)
	}

	item := &memcache.Item{
		Key:        completionKey(lineage.id),
		Value:      lineage.metadataBytes,
		Expiration: expiration,
	}

	err = publisher.addBeforeActivation(ctx, item)
	if err != nil {
		return fmt.Errorf("writing lineage completion: %w", err)
	}

	stored, err := publisher.getBeforeActivation(ctx, item.Key)
	if err != nil {
		return fmt.Errorf("verifying lineage completion: %w", err)
	}

	if !bytes.Equal(stored.Value, lineage.metadataBytes) {
		return errVerificationFailed
	}

	return nil
}

func (publisher *Publisher) activationExpiration(ctx context.Context, deadline time.Time) (int32, error) {
	err := cancellationError(ctx)
	if err != nil {
		return 0, err
	}

	expiration, err := memcachedExpiration(publisher.now(), deadline)
	if err != nil {
		return 0, fmt.Errorf("calculating active pointer expiry: %w", err)
	}

	return expiration, nil
}

func (publisher *Publisher) validate(ctx context.Context, value snapshot.Snapshot) ([]byte, error) {
	ctx, span := publisher.tracer.Start(ctx, "lineage.validate")
	defer span.End()

	data, err := snapshot.Marshal(value)
	if err != nil {
		finishLifecycleSpan(span, "validation_failed", err)

		return nil, fmt.Errorf("validating lineage: %w", err)
	}

	span.SetAttributes(attribute.String("lineage.id", value.LineageID))

	err = cancellationError(ctx)
	if err != nil {
		finishLifecycleSpan(span, "canceled", err)

		return nil, err
	}

	span.SetAttributes(attribute.String("lineage.outcome", "success"))

	return data, nil
}

func (publisher *Publisher) previous(ctx context.Context) (previousLineage, error) {
	item, err := publisher.getBeforeActivation(ctx, activePointerKey)
	if errors.Is(err, memcache.ErrCacheMiss) {
		return previousLineage{}, nil
	}

	if err != nil {
		return previousLineage{}, fmt.Errorf("reading active lineage pointer: %w", err)
	}

	id := string(item.Value)

	previous := previousLineage{id: id, found: true}
	if !snapshot.ValidLineageID(id) {
		previous.cleanupErr = errPreviousIncomplete

		return previous, nil
	}

	item, err = publisher.getBeforeActivation(ctx, completionKey(id))
	if errors.Is(err, memcache.ErrCacheMiss) {
		previous.cleanupErr = errPreviousIncomplete

		return previous, nil
	}

	if err != nil {
		return previousLineage{}, fmt.Errorf("reading active lineage completion: %w", err)
	}

	previous.metadata, err = decodeCompletion(item.Value)
	if err != nil {
		previous.cleanupErr = err
	}

	return previous, nil
}

func (publisher *Publisher) addBeforeActivation(ctx context.Context, item *memcache.Item) error {
	err := cancellationError(ctx)
	if err != nil {
		return err
	}

	err = publisher.cache.add(ctx, item)

	return operationOrCancellationError(ctx, err)
}

func (publisher *Publisher) getBeforeActivation(ctx context.Context, key string) (*memcache.Item, error) {
	err := cancellationError(ctx)
	if err != nil {
		return nil, err
	}

	item, err := publisher.cache.get(ctx, key)

	combined := operationOrCancellationError(ctx, err)
	if combined != nil {
		return nil, combined
	}

	return item, nil
}

func (publisher *Publisher) activate(
	ctx context.Context,
	lineageID string,
	expiration int32,
	previous previousLineage,
) error {
	ctx, span := publisher.tracer.Start(ctx, "lineage.activate",
		trace.WithAttributes(attribute.String("lineage.id", lineageID)),
	)
	defer span.End()

	item := &memcache.Item{Key: activePointerKey, Value: []byte(lineageID), Expiration: expiration}

	err := cancellationError(ctx)
	if err != nil {
		finishLifecycleSpan(span, "canceled", err)

		return err
	}

	// Publication relies on one active-pointer writer, so no compare-and-swap coordination is needed.
	setError := publisher.cache.set(ctx, item)
	if setError == nil {
		span.SetAttributes(attribute.String("lineage.outcome", "success"))

		return nil
	}

	reconcileCtx := context.WithoutCancel(ctx)

	observed, getError := publisher.cache.get(reconcileCtx, activePointerKey)
	if getError == nil {
		switch {
		case bytes.Equal(observed.Value, item.Value):
			span.SetAttributes(
				attribute.Bool("lineage.activation.reconciled", true),
				attribute.String("lineage.outcome", "success"),
			)

			return nil
		case previous.found && string(observed.Value) == previous.id:
			finishLifecycleSpan(span, "failed", setError)

			return fmt.Errorf("setting active lineage pointer: %w", setError)
		default:
			getError = errUnexpectedPointer
		}
	}

	uncertain := &UncertainCommitError{setError: setError, getError: getError}
	finishLifecycleSpan(span, "uncertain", uncertain)

	return uncertain
}

func (publisher *Publisher) evictPrevious(ctx context.Context, previous previousLineage) error {
	if !previous.found {
		return nil
	}

	ctx, span := publisher.tracer.Start(ctx, "lineage.evict.previous")
	defer span.End()

	if snapshot.ValidLineageID(previous.id) {
		span.SetAttributes(attribute.String("lineage.id", previous.id))
	}

	cleanupErr := previous.cleanupErr
	if cleanupErr == nil {
		keys, err := evictionKeys(previous.id, previous.metadata)
		if err != nil {
			cleanupErr = err
		} else {
			for _, key := range keys {
				deleteError := publisher.cache.delete(ctx, key)
				if deleteError != nil && !errors.Is(deleteError, memcache.ErrCacheMiss) {
					cleanupErr = errors.Join(cleanupErr, deleteError)
				}
			}
		}
	}

	if cleanupErr != nil {
		failure := &CleanupError{cause: cleanupErr}
		finishLifecycleSpan(span, "failed", failure)
		publisher.logger.ErrorContext(ctx, "previous lineage eviction failed",
			slog.String("error.type", classifyError(failure)),
		)

		return failure
	}

	publisher.logger.InfoContext(ctx, "previous lineage evicted")
	span.SetAttributes(attribute.String("lineage.outcome", "success"))

	return nil
}

func cancellationError(ctx context.Context) error {
	cause := context.Cause(ctx)
	if cause == nil {
		return nil
	}

	return fmt.Errorf("lineage publication canceled: %w", cause)
}

func operationOrCancellationError(ctx context.Context, operationError error) error {
	cancellation := cancellationError(ctx)
	if operationError != nil && cancellation != nil {
		return errors.Join(operationError, cancellation)
	}

	if operationError != nil {
		return operationError
	}

	return cancellation
}

func finishLifecycleSpan(span trace.Span, outcome string, err error) {
	span.SetAttributes(
		attribute.String("lineage.outcome", outcome),
		attribute.String("error.type", classifyError(err)),
	)
	span.RecordError(errors.New("lineage lifecycle operation failed"))
	span.SetStatus(codes.Error, "lineage lifecycle operation failed")
}

func (publisher *Publisher) logFailure(ctx context.Context, message, lineageID string, err error) {
	attributes := []any{slog.String("error.type", classifyError(err))}
	if snapshot.ValidLineageID(lineageID) {
		attributes = append(attributes, slog.String("lineage.id", lineageID))
	}

	publisher.logger.ErrorContext(ctx, message, attributes...)
}

func classifyError(err error) string {
	var uncertain *UncertainCommitError
	if errors.As(err, &uncertain) {
		return "uncertain_commit"
	}

	switch {
	case err == nil:
		return "none"
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, memcache.ErrCacheMiss):
		return "cache_miss"
	case errors.Is(err, memcache.ErrNotStored):
		return "not_stored"
	case errors.Is(err, errVerificationFailed):
		return "verification_failed"
	case errors.Is(err, errInvalidInterval), errors.Is(err, errExpiredPublication):
		return "invalid_expiry"
	}

	var networkError net.Error
	if errors.As(err, &networkError) {
		return "unavailable"
	}

	return "invalid"
}
