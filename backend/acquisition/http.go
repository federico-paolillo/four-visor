package acquisition

import (
	"context"
	"encoding/json"
	"errors"
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"time"

	"git.disroot.org/federico-paolillo/four-visor.git/snapshot"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

const (
	boardsResource       = "boards"
	catalogResource      = "catalog"
	threadResource       = "thread"
	errorNone            = "none"
	errorNetwork         = "network"
	errorTimeout         = "timeout"
	errorRateLimit       = "rate_limited"
	errorHTTP            = "http"
	errorInvalid         = "invalid_response"
	errorDeadline        = "lineage_deadline"
	errorCanceled        = "canceled"
	causeNone            = "none"
	causeCanceled        = "context_canceled"
	causeDeadline        = "context_deadline_exceeded"
	causeHTTPStatus      = "http_status"
	causeInvalidJSON     = "invalid_json"
	causeLineageDeadline = "lineage_deadline"
	causeNetwork         = "network"
	causeOther           = "other"
	causeRequestDeadline = "request_deadline"
	causeUnexpectedEOF   = "unexpected_eof"
)

var (
	errRequestTimeout   = errors.New("upstream request timeout")
	errLineageDeadline  = errors.New("lineage deadline prevents acquisition")
	errUnexpectedStatus = errors.New("unexpected upstream HTTP status")
)

type requestError struct {
	kind       string
	cause      error
	retryable  bool
	retryAfter time.Duration
	status     int
	stage      string
	attempt    int
	exhausted  bool
}

func (failure *requestError) Error() string {
	return "upstream acquisition " + failure.kind
}

func (failure *requestError) Unwrap() error {
	return failure.cause
}

func (client *Client) fetchBoards(ctx context.Context, lineageID string) ([]observedBoard, error) {
	var boards []observedBoard

	err := client.fetch(ctx, lineageID, boardsResource, client.boardsURL(), func(_ context.Context, data []byte) error {
		var parseError error

		boards, parseError = parseBoards(data)

		return parseError
	})

	return boards, err
}

func (client *Client) fetchCatalog(ctx context.Context, lineageID, board string) ([]snapshot.Page, error) {
	var pages []snapshot.Page

	err := client.fetch(
		ctx,
		lineageID,
		catalogResource,
		client.catalogURL(board),
		func(_ context.Context, data []byte) error {
			var parseError error

			pages, parseError = parseCatalog(data)

			return parseError
		},
	)

	return pages, err
}

func (client *Client) fetchThread(
	ctx context.Context,
	lineageID, board string,
	number uint64,
) (snapshot.Thread, error) {
	var thread snapshot.Thread

	err := client.fetch(
		ctx,
		lineageID,
		threadResource,
		client.threadURL(board, number),
		func(attemptCtx context.Context, data []byte) error {
			var parseError error

			thread, parseError = parseThread(data)
			if parseError == nil && thread.State == snapshot.StateOversize {
				attributes := []attribute.KeyValue{
					attribute.String("resource.state", string(snapshot.StateOversize)),
					attribute.Int("posts.limit", snapshot.MaximumThreadPosts),
				}
				trace.SpanFromContext(attemptCtx).SetAttributes(attributes...)
				client.logger.WarnContext(attemptCtx, "oversized thread detected",
					slog.String("lineage.id", lineageID),
					slog.String("resource.type", threadResource),
					slog.String("resource.state", string(snapshot.StateOversize)),
					slog.Int("posts.limit", snapshot.MaximumThreadPosts),
				)
			}

			return parseError
		},
	)

	return thread, err
}

func (client *Client) fetch(
	ctx context.Context,
	lineageID, resource, target string,
	decode func(context.Context, []byte) error,
) error {
	for attempt := 0; attempt <= client.policy.MaxRetries; attempt++ {
		failure := client.attempt(ctx, lineageID, resource, target, attempt, decode)
		if failure == nil {
			return nil
		}

		if !failure.retryable || attempt == client.policy.MaxRetries {
			failure.exhausted = failure.retryable

			return failure
		}

		delay := max(time.Duration(attempt+1)*client.policy.RetryBackoff, failure.retryAfter)

		waitFailure := waitUntilRetry(ctx, delay)
		if waitFailure != nil {
			waitFailure.attempt = attempt + 1
			waitFailure.exhausted = true

			return waitFailure
		}
	}

	panic("bounded retry loop exhausted without returning")
}

func (client *Client) attempt(
	ctx context.Context,
	lineageID, resource, target string,
	attempt int,
	decode func(context.Context, []byte) error,
) *requestError {
	attemptCtx, span := client.tracer.Start(ctx, "fetch."+resource,
		trace.WithSpanKind(trace.SpanKindClient),
		trace.WithAttributes(
			attribute.String("lineage.id", lineageID),
			attribute.String("resource.type", resource),
			attribute.String("http.request.method", http.MethodGet),
			attribute.Int("retry.attempt", attempt),
		),
	)
	defer span.End()

	release, failure := client.beginAttempt(attemptCtx)
	if failure != nil {
		failure.attempt = attempt
		finishSpan(span, failure)

		return failure
	}
	defer release()

	attemptCtx, cancel := context.WithTimeoutCause(attemptCtx, client.policy.RequestTimeout, errRequestTimeout)
	defer cancel()

	// Each lineage is built from scratch, so no prior representation exists for conditional requests.
	request, err := http.NewRequestWithContext(attemptCtx, http.MethodGet, target, http.NoBody)
	if err != nil {
		failure = &requestError{kind: errorInvalid, cause: err, stage: stageRequest, attempt: attempt}
		finishSpan(span, failure)

		return failure
	}

	request.Header.Set("User-Agent", client.userAgent)

	request = request.WithContext(attemptCtx)

	started := time.Now()

	result := client.perform(attemptCtx, request, decode)
	if result != nil {
		result.attempt = attempt
		result.exhausted = result.retryable && attempt == client.policy.MaxRetries
	}

	client.recordAttempt(attemptCtx, resource, time.Since(started), result)
	finishSpan(span, result)

	return result
}

func (client *Client) perform(
	ctx context.Context,
	request *http.Request,
	decode func(context.Context, []byte) error,
) *requestError {
	response, err := client.httpClient.Do(request)
	if err != nil {
		return interruptedRequest(ctx, err, stageRequest)
	}
	defer response.Body.Close() //nolint:errcheck // A completed GET has no close-error recovery action.

	if response.StatusCode != http.StatusOK {
		failure := &requestError{
			kind: errorHTTP, cause: errUnexpectedStatus, status: response.StatusCode, stage: stageRequest,
		}
		if response.StatusCode == http.StatusTooManyRequests {
			failure.kind = errorRateLimit
			failure.retryable = true
			failure.retryAfter = parseRetryAfter(response.Header.Get("Retry-After"), time.Now())
		}

		return failure
	}

	body, err := io.ReadAll(response.Body)
	if err != nil {
		return interruptedRequest(ctx, err, stageBody)
	}

	cause := context.Cause(ctx)
	if cause != nil {
		return interruptedRequest(ctx, cause, stageBody)
	}

	err = decode(ctx, body)

	cause = context.Cause(ctx)
	if cause != nil {
		return interruptedRequest(ctx, cause, stageDecode)
	}

	if err != nil {
		return &requestError{kind: errorInvalid, cause: err, status: response.StatusCode, stage: stageDecode}
	}

	return nil
}

func (client *Client) beginAttempt(ctx context.Context) (func(), *requestError) {
	select {
	case <-client.rateGate:
	case <-ctx.Done():
		return nil, contextFailure(ctx, stageQueue)
	}

	releaseGate := func() { client.rateGate <- struct{}{} }
	if ctx.Err() != nil {
		releaseGate()

		return nil, contextFailure(ctx, stageQueue)
	}

	delay := time.Until(client.lastStart.Add(client.policy.RateInterval))

	if delay > 0 {
		if !fitsDeadline(ctx, delay) {
			releaseGate()

			return nil, lineageDeadlineFailure(stageRate)
		}

		failure := wait(ctx, delay, stageRate)
		if failure != nil {
			releaseGate()

			return nil, failure
		}
	}

	select {
	case client.concurrency <- struct{}{}:
		if ctx.Err() != nil {
			<-client.concurrency
			releaseGate()

			return nil, contextFailure(ctx, stageConcurrency)
		}

		client.lastStart = time.Now()

		releaseGate()

		return func() { <-client.concurrency }, nil
	case <-ctx.Done():
		releaseGate()

		return nil, contextFailure(ctx, stageConcurrency)
	}
}

func waitUntilRetry(ctx context.Context, delay time.Duration) *requestError {
	if !fitsDeadline(ctx, delay) {
		return lineageDeadlineFailure(stageRetry)
	}

	return wait(ctx, delay, stageRetry)
}

func wait(ctx context.Context, delay time.Duration, stage string) *requestError {
	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-timer.C:
		return nil
	case <-ctx.Done():
		return contextFailure(ctx, stage)
	}
}

func fitsDeadline(ctx context.Context, delay time.Duration) bool {
	deadline, ok := ctx.Deadline()

	return !ok || time.Now().Add(delay).Before(deadline)
}

func contextFailure(ctx context.Context, stage string) *requestError {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return lineageDeadlineFailure(stage)
	}

	return &requestError{kind: errorCanceled, cause: context.Cause(ctx), stage: stage}
}

func interruptedRequest(ctx context.Context, fallback error, stage string) *requestError {
	if errors.Is(ctx.Err(), context.DeadlineExceeded) {
		if errors.Is(context.Cause(ctx), errRequestTimeout) {
			return &requestError{kind: errorTimeout, cause: fallback, retryable: true, stage: stage}
		}

		return lineageDeadlineFailure(stage)
	}

	if errors.Is(ctx.Err(), context.Canceled) {
		return &requestError{kind: errorCanceled, cause: context.Cause(ctx), stage: stage}
	}

	return &requestError{kind: errorNetwork, cause: fallback, retryable: true, stage: stage}
}

func lineageDeadlineFailure(stage string) *requestError {
	return &requestError{kind: errorDeadline, cause: errLineageDeadline, stage: stage}
}

func parseRetryAfter(value string, now time.Time) time.Duration {
	if isASCIIDigits(value) {
		const maximumSeconds = uint64((1<<63 - 1) / time.Second)

		seconds, err := strconv.ParseUint(value, 10, 64)
		if err != nil || seconds > maximumSeconds {
			return time.Duration(1<<63 - 1)
		}

		return time.Duration(seconds) * time.Second
	}

	when, err := http.ParseTime(value)
	if err != nil {
		return 0
	}

	return max(when.Sub(now), 0)
}

func isASCIIDigits(value string) bool {
	if value == "" {
		return false
	}

	for index := range len(value) {
		if value[index] < '0' || value[index] > '9' {
			return false
		}
	}

	return true
}

func errorCauseType(err error) string {
	switch {
	case err == nil:
		return causeNone
	case errors.Is(err, errLineageDeadline):
		return causeLineageDeadline
	case errors.Is(err, errUnexpectedStatus):
		return causeHTTPStatus
	case errors.Is(err, context.Canceled):
		return causeCanceled
	case errors.Is(err, context.DeadlineExceeded):
		return causeDeadline
	case errors.Is(err, io.ErrUnexpectedEOF):
		return causeUnexpectedEOF
	}

	var syntaxError *json.SyntaxError

	var typeError *json.UnmarshalTypeError
	if errors.As(err, &syntaxError) || errors.As(err, &typeError) {
		return causeInvalidJSON
	}

	var networkError net.Error
	if errors.As(err, &networkError) {
		return causeNetwork
	}

	return causeOther
}

func (client *Client) recordAttempt(
	ctx context.Context,
	resource string,
	duration time.Duration,
	failure *requestError,
) {
	status := 0
	kind := errorNone

	if failure != nil {
		status = failure.status
		kind = failure.kind
	}

	attributes := metric.WithAttributes(
		attribute.String("resource.type", resource),
		attribute.String("error.type", kind),
		attribute.Int("http.response.status_code", status),
	)
	client.requests.Add(ctx, 1, attributes)
	client.duration.Record(ctx, duration.Seconds(), attributes)
}

func finishSpan(span trace.Span, failure *requestError) {
	if failure == nil {
		return
	}

	causeType := requestErrorCauseType(failure)
	span.RecordError(failure, trace.WithAttributes(attribute.String("error.cause.type", causeType)))
	span.SetStatus(codes.Error, "upstream acquisition failed")
	span.SetAttributes(
		attribute.String("failure.stage", failure.stage),
		attribute.String("error.type", failure.kind),
		attribute.String("error.cause.type", causeType),
		attribute.Int("http.response.status_code", failure.status),
		attribute.Int("retry.attempt", failure.attempt),
		attribute.Bool("retry.exhausted", failure.exhausted),
	)
}

func requestErrorCauseType(err error) string {
	var failure *requestError
	if errors.As(err, &failure) {
		if failure.kind == errorTimeout {
			return causeRequestDeadline
		}

		return errorCauseType(failure.cause)
	}

	return causeOther
}
