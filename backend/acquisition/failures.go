package acquisition

// Failure summaries aggregate terminal resource failures into bounded lineage diagnostics.

import (
	"context"
	"errors"
	"log/slog"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

const (
	stageQueue       = "queue"
	stageRate        = "rate"
	stageConcurrency = "concurrency"
	stageRequest     = "request"
	stageBody        = "body"
	stageDecode      = "decode"
	stageRetry       = "retry"
)

type failureKey struct {
	resource  string
	stage     string
	errorType string
	causeType string
	status    int
	attempt   int
	exhausted bool
}

type failureSummary struct {
	lineageID string
	logger    *slog.Logger
	counter   metric.Int64Counter
	counts    map[failureKey]int64
}

func newFailureSummary(lineageID string, logger *slog.Logger, counter metric.Int64Counter) *failureSummary {
	return &failureSummary{
		lineageID: lineageID,
		logger:    logger,
		counter:   counter,
		counts:    make(map[failureKey]int64),
	}
}

func (summary *failureSummary) add(resource string, err error) {
	var failure *requestError
	if !errors.As(err, &failure) {
		failure = &requestError{kind: "unknown", cause: err, stage: stageRequest}
	}

	key := failureKey{
		resource:  resource,
		stage:     failure.stage,
		errorType: failure.kind,
		causeType: requestErrorCauseType(failure),
		status:    boundedHTTPStatus(failure.status),
		attempt:   failure.attempt,
		exhausted: failure.exhausted,
	}
	summary.counts[key]++
}

func (summary *failureSummary) addCount(resource string, failure *requestError, count int) {
	if count <= 0 {
		return
	}

	key := failureKey{
		resource:  resource,
		stage:     failure.stage,
		errorType: failure.kind,
		causeType: requestErrorCauseType(failure),
		status:    boundedHTTPStatus(failure.status),
		attempt:   failure.attempt,
		exhausted: failure.exhausted,
	}
	summary.counts[key] += int64(count)
}

func (summary *failureSummary) flush(ctx context.Context) {
	for failure, count := range summary.counts {
		attributes := []attribute.KeyValue{
			attribute.String("resource.type", failure.resource),
			attribute.String("failure.stage", failure.stage),
			attribute.String("error.type", failure.errorType),
			attribute.String("error.cause.type", failure.causeType),
			attribute.Int("http.response.status_code", failure.status),
			attribute.Int("retry.attempt", failure.attempt),
			attribute.Bool("retry.exhausted", failure.exhausted),
		}
		summary.counter.Add(ctx, count, metric.WithAttributes(attributes...))
		summary.logger.ErrorContext(ctx, "upstream acquisition failures summarized",
			slog.String("lineage.id", summary.lineageID),
			slog.String("resource.type", failure.resource),
			slog.String("failure.stage", failure.stage),
			slog.String("error.type", failure.errorType),
			slog.String("error.cause.type", failure.causeType),
			slog.Int("http.response.status_code", failure.status),
			slog.Int("retry.attempt", failure.attempt),
			slog.Bool("retry.exhausted", failure.exhausted),
			slog.Int64("failure.count", count),
		)
	}
}

func boundedHTTPStatus(status int) int {
	if status < 100 || status > 599 {
		return 0
	}

	return status
}
