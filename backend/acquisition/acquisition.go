// Package acquisition observes 4chan boards and catalogs through one bounded client.
package acquisition

import (
	"context"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"git.disroot.org/federico-paolillo/four-visor.git/snapshot"
	"git.disroot.org/federico-paolillo/four-visor.git/telemetry"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

const officialBaseURL = "https://a.4cdn.org"

var (
	// ErrLineageDeadlineRequired reports a caller that omitted the construction deadline.
	ErrLineageDeadlineRequired = errors.New("lineage deadline is required")
	// ErrLineageIDRequired reports a caller that omitted a valid correlation identifier.
	ErrLineageIDRequired = errors.New("valid lineage identifier is required")
	errInvalidPolicy     = errors.New("invalid acquisition policy")
)

// Policy defines the bounded behavior shared by every request through a Client.
type Policy struct {
	RateInterval   time.Duration
	MaxConcurrency int
	RequestTimeout time.Duration
	MaxRetries     int
	RetryBackoff   time.Duration
}

// Client is the process-shared 4chan acquisition boundary.
type Client struct {
	policy      Policy
	userAgent   string
	baseURL     *url.URL
	httpClient  *http.Client
	logger      *slog.Logger
	tracer      trace.Tracer
	requests    metric.Int64Counter
	duration    metric.Float64Histogram
	failures    metric.Int64Counter
	rateGate    chan struct{}
	concurrency chan struct{}
	lastStart   time.Time
}

type clientMetrics struct {
	requests metric.Int64Counter
	duration metric.Float64Histogram
	failures metric.Int64Counter
}

type threadJob struct {
	board   string
	number  uint64
	entry   *snapshot.ThreadEntry
	thread  snapshot.Thread
	fetched bool
	err     error
}

// New creates a production client for the official 4chan API.
func New(
	policy Policy,
	userAgent string,
	transport http.RoundTripper,
	logger *slog.Logger,
	tracer trace.Tracer,
	meter metric.Meter,
) (*Client, error) {
	if transport == nil {
		return nil, fmt.Errorf("%w: missing HTTP transport", errInvalidPolicy)
	}

	httpClient := &http.Client{
		Transport: transport,
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return http.ErrUseLastResponse
		},
	}

	return newClient(policy, userAgent, officialBaseURL, httpClient, logger, tracer, meter)
}

func newClient(
	policy Policy,
	userAgent, baseURL string,
	httpClient *http.Client,
	logger *slog.Logger,
	tracer trace.Tracer,
	meter metric.Meter,
) (*Client, error) {
	err := validatePolicy(policy)
	if err != nil {
		return nil, err
	}

	if !validUserAgent(userAgent) {
		return nil, fmt.Errorf("%w: invalid deployed User-Agent", errInvalidPolicy)
	}

	if logger == nil || httpClient == nil {
		return nil, fmt.Errorf("%w: missing client dependency", errInvalidPolicy)
	}

	parsedBaseURL, err := url.Parse(baseURL)
	if err != nil || parsedBaseURL.Scheme == "" || parsedBaseURL.Host == "" {
		return nil, fmt.Errorf("%w: invalid upstream base URL", errInvalidPolicy)
	}

	metrics, err := createClientMetrics(meter)
	if err != nil {
		return nil, err
	}

	rateGate := make(chan struct{}, 1)
	rateGate <- struct{}{}

	return &Client{
		policy:      policy,
		userAgent:   userAgent,
		baseURL:     parsedBaseURL,
		httpClient:  httpClient,
		logger:      logger,
		tracer:      tracer,
		requests:    metrics.requests,
		duration:    metrics.duration,
		failures:    metrics.failures,
		rateGate:    rateGate,
		concurrency: make(chan struct{}, policy.MaxConcurrency),
	}, nil
}

func createClientMetrics(meter metric.Meter) (clientMetrics, error) {
	requests, err := telemetry.HTTPClientRequestCount.Int64Counter(meter)
	if err != nil {
		return clientMetrics{}, fmt.Errorf("creating HTTP client request counter: %w", err)
	}

	duration, err := telemetry.HTTPClientRequestDuration.Float64Histogram(meter)
	if err != nil {
		return clientMetrics{}, fmt.Errorf("creating HTTP client duration histogram: %w", err)
	}

	failures, err := telemetry.LineageResourceFailureCount.Int64Counter(meter)
	if err != nil {
		return clientMetrics{}, fmt.Errorf("creating lineage resource failure counter: %w", err)
	}

	return clientMetrics{requests: requests, duration: duration, failures: failures}, nil
}

// Observe constructs fresh board, catalog, and thread resources under the caller's lineage deadline.
func (client *Client) Observe(ctx context.Context, lineageID string) (snapshot.Boards, error) {
	err := validateObservation(ctx, lineageID)
	if err != nil {
		return snapshot.Boards{}, err
	}

	failures := newFailureSummary(lineageID, client.logger, client.failures)
	defer failures.flush(ctx)

	boards, err := client.fetchBoards(ctx, lineageID)
	if err != nil {
		cancellation := externalCancellation(ctx)
		if cancellation != nil {
			return snapshot.Boards{}, cancellation
		}

		failures.addFetch(ctx, boardsResource, err)

		return snapshot.Boards{State: snapshot.StateFailed}, nil
	}

	items := make([]snapshot.BoardItem, len(boards))
	catalogErrors := make([]error, len(boards))

	var wait sync.WaitGroup

	for index, board := range boards {
		items[index].Board = board.raw

		wait.Go(func() {
			pages, catalogError := client.fetchCatalog(ctx, lineageID, board.id)
			if catalogError != nil {
				catalogErrors[index] = catalogError

				return
			}

			items[index].Catalog = &snapshot.Catalog{State: snapshot.StatePresent, Pages: &pages}
		})
	}

	wait.Wait()

	cancellation := externalCancellation(ctx)
	if cancellation != nil {
		return snapshot.Boards{}, cancellation
	}

	for index, failure := range catalogErrors {
		if failure == nil {
			continue
		}

		items[index].Catalog = &snapshot.Catalog{State: snapshot.StateFailed}

		failures.addFetch(ctx, catalogResource, failure)
	}

	jobs := collectThreadJobs(boards, items)
	client.warnThreadCapacity(ctx, lineageID, jobs)
	client.acquireThreads(ctx, lineageID, jobs)

	cancellation = externalCancellation(ctx)
	if cancellation != nil {
		return snapshot.Boards{}, cancellation
	}

	client.applyThreadResults(ctx, jobs, failures)

	return snapshot.Boards{State: snapshot.StatePresent, Items: &items}, nil
}

func validateObservation(ctx context.Context, lineageID string) error {
	if _, ok := ctx.Deadline(); !ok {
		return ErrLineageDeadlineRequired
	}

	if !snapshot.ValidLineageID(lineageID) {
		return ErrLineageIDRequired
	}

	return nil
}

func (client *Client) applyThreadResults(ctx context.Context, jobs []threadJob, failures *failureSummary) {
	unfinished := 0

	for index := range jobs {
		job := &jobs[index]
		if job.err == nil && job.thread.State != "" {
			job.entry.Thread = &snapshot.Thread{State: job.thread.State, Posts: job.thread.Posts}

			continue
		}

		job.entry.Thread = &snapshot.Thread{State: snapshot.StateFailed}
		if job.err == nil {
			unfinished++

			continue
		}

		if job.fetched {
			failures.addFetch(ctx, threadResource, job.err)
		} else {
			failures.add(threadResource, job.err)
		}
	}

	failures.addCount(threadResource, lineageDeadlineFailure(stageQueue), unfinished)
}

func collectThreadJobs(boards []observedBoard, items []snapshot.BoardItem) []threadJob {
	jobs := make([]threadJob, 0)

	for boardIndex := range items {
		catalog := items[boardIndex].Catalog
		if catalog == nil || catalog.State != snapshot.StatePresent || catalog.Pages == nil {
			continue
		}

		for pageIndex := range *catalog.Pages {
			page := &(*catalog.Pages)[pageIndex]
			for threadIndex := range page.Threads {
				entry := &page.Threads[threadIndex]
				number, err := threadNumber(entry.Summary)
				job := threadJob{board: boards[boardIndex].id, number: number, entry: entry}

				if err != nil {
					job.err = &requestError{kind: errorInvalid, cause: err, stage: stageDecode}
				}

				jobs = append(jobs, job)
			}
		}
	}

	return jobs
}

func (client *Client) acquireThreads(ctx context.Context, lineageID string, jobs []threadJob) {
	workerCount := min(client.policy.MaxConcurrency, len(jobs))
	if workerCount == 0 {
		return
	}

	queue := make(chan *threadJob)

	var workers sync.WaitGroup

	for range workerCount {
		workers.Go(func() {
			for job := range queue {
				if ctx.Err() != nil {
					return
				}

				job.fetched = true
				job.thread, job.err = client.fetchThread(ctx, lineageID, job.board, job.number)
			}
		})
	}

dispatch:
	for index := range jobs {
		if jobs[index].err != nil {
			continue
		}

		select {
		case queue <- &jobs[index]:
		case <-ctx.Done():
			break dispatch
		}
	}

	close(queue)
	workers.Wait()
}

func (client *Client) warnThreadCapacity(ctx context.Context, lineageID string, jobs []threadJob) {
	deadline, ok := ctx.Deadline()
	if !ok {
		return
	}

	queued := 0

	for index := range jobs {
		if jobs[index].err == nil {
			queued++
		}
	}

	if queued == 0 {
		return
	}

	select {
	case <-client.rateGate:
	case <-ctx.Done():
		return
	}

	now := time.Now()
	nextStart := client.lastStart.Add(client.policy.RateInterval)

	if nextStart.Before(now) {
		nextStart = now
	}

	client.rateGate <- struct{}{}

	remaining := max(deadline.Sub(now), 0)

	capacity := int64(0)

	if nextStart.Before(deadline) {
		capacity = 1 + int64((deadline.Sub(nextStart)-1)/client.policy.RateInterval)
	}

	if int64(queued) <= capacity {
		return
	}

	client.logger.WarnContext(ctx, "thread acquisition exceeds remaining rate capacity",
		slog.String("lineage.id", lineageID),
		slog.String("resource.type", threadResource),
		slog.Int("resource.queued.count", queued),
		slog.Int64("resource.rate_capacity.count", capacity),
		slog.Duration("lineage.deadline.remaining", remaining),
	)
}

func validatePolicy(policy Policy) error {
	if policy.RateInterval < time.Second || policy.MaxConcurrency < 1 || policy.MaxConcurrency > 10 ||
		policy.RequestTimeout <= 0 || policy.MaxRetries < 0 || policy.MaxRetries > 2 || policy.RetryBackoff <= 0 {
		return errInvalidPolicy
	}

	return nil
}

func validUserAgent(value string) bool {
	hash, ok := strings.CutPrefix(value, "4Visor/")
	if !ok || len(hash) != 40 || hash != strings.ToLower(hash) {
		return false
	}

	_, err := hex.DecodeString(hash)

	return err == nil
}

func externalCancellation(ctx context.Context) error {
	if !errors.Is(ctx.Err(), context.Canceled) {
		return nil
	}

	return &requestError{kind: errorCanceled, cause: context.Cause(ctx)}
}
