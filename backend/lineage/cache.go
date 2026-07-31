package lineage

import (
	"context"
	"errors"
	"fmt"
	"time"

	"git.disroot.org/federico-paolillo/four-visor.git/telemetry"
	"github.com/bradfitz/gomemcache/memcache"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

const (
	cacheOutcomeSuccess = "success"
	cacheOutcomeHit     = "hit"
	cacheOutcomeMiss    = "miss"
	cacheOutcomeError   = "error"
)

type cacheClient interface {
	Add(item *memcache.Item) error
	Set(item *memcache.Item) error
	Get(key string) (*memcache.Item, error)
	Delete(key string) error
}

type instrumentedCache struct {
	client     cacheClient
	tracer     trace.Tracer
	operations metric.Int64Counter
	duration   metric.Float64Histogram
}

type cacheOperationError struct {
	operation string
	cause     error
}

func (failure *cacheOperationError) Error() string {
	return "Memcached " + failure.operation + " failed"
}

func (failure *cacheOperationError) Unwrap() error {
	return failure.cause
}

func newInstrumentedCache(client cacheClient, tracer trace.Tracer, meter metric.Meter) (*instrumentedCache, error) {
	operations, err := telemetry.CacheOperationCount.Int64Counter(meter)
	if err != nil {
		return nil, fmt.Errorf("creating cache operation counter: %w", err)
	}

	duration, err := telemetry.CacheOperationDuration.Float64Histogram(meter)
	if err != nil {
		return nil, fmt.Errorf("creating cache operation duration: %w", err)
	}

	return &instrumentedCache{client: client, tracer: tracer, operations: operations, duration: duration}, nil
}

func (cache *instrumentedCache) add(ctx context.Context, item *memcache.Item) error {
	return cache.mutate(ctx, "add", func() error { return cache.client.Add(item) })
}

func (cache *instrumentedCache) set(ctx context.Context, item *memcache.Item) error {
	return cache.mutate(ctx, "set", func() error { return cache.client.Set(item) })
}

func (cache *instrumentedCache) get(ctx context.Context, key string) (*memcache.Item, error) {
	ctx, span := cache.tracer.Start(ctx, "memcached.get", trace.WithSpanKind(trace.SpanKindClient))
	started := time.Now()

	item, err := cache.client.Get(key)

	outcome := cacheOutcomeHit
	if errors.Is(err, memcache.ErrCacheMiss) {
		outcome = cacheOutcomeMiss
	} else if err != nil {
		outcome = cacheOutcomeError
	}

	cache.finish(ctx, span, "get", outcome, time.Since(started), err)

	if err != nil {
		return nil, &cacheOperationError{operation: "get", cause: err}
	}

	return item, nil
}

func (cache *instrumentedCache) delete(ctx context.Context, key string) error {
	return cache.mutate(ctx, "delete", func() error { return cache.client.Delete(key) })
}

func (cache *instrumentedCache) mutate(ctx context.Context, operation string, call func() error) error {
	ctx, span := cache.tracer.Start(ctx, "memcached."+operation, trace.WithSpanKind(trace.SpanKindClient))
	started := time.Now()

	err := call()

	outcome := cacheOutcomeSuccess
	if errors.Is(err, memcache.ErrCacheMiss) {
		outcome = cacheOutcomeMiss
	} else if err != nil {
		outcome = cacheOutcomeError
	}

	cache.finish(ctx, span, operation, outcome, time.Since(started), err)

	if err != nil {
		return &cacheOperationError{operation: operation, cause: err}
	}

	return nil
}

func (cache *instrumentedCache) finish(
	ctx context.Context,
	span trace.Span,
	operation, outcome string,
	duration time.Duration,
	err error,
) {
	attributes := []attribute.KeyValue{
		attribute.String("cache.operation", operation),
		attribute.String("cache.outcome", outcome),
	}

	span.SetAttributes(attributes...)

	if err != nil && !errors.Is(err, memcache.ErrCacheMiss) {
		failure := &cacheOperationError{operation: operation, cause: err}
		span.RecordError(failure)
		span.SetStatus(codes.Error, "Memcached operation failed")
	}

	span.End()

	measurement := metric.WithAttributes(attributes...)
	cache.operations.Add(ctx, 1, measurement)
	cache.duration.Record(ctx, duration.Seconds(), measurement)
}
