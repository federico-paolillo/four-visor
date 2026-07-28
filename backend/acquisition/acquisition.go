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
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

const officialBaseURL = "https://a.4cdn.org"

var (
	// ErrLineageDeadlineRequired reports a caller that omitted the construction deadline.
	ErrLineageDeadlineRequired = errors.New("lineage deadline is required")
	errInvalidPolicy           = errors.New("invalid acquisition policy")
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
	rateGate    chan struct{}
	concurrency chan struct{}
	lastStart   time.Time
}

// New creates a production client for the official 4chan API.
func New(
	policy Policy,
	userAgent string,
	logger *slog.Logger,
	tracer trace.Tracer,
	meter metric.Meter,
) (*Client, error) {
	httpClient := &http.Client{
		Transport: http.DefaultTransport,
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

	requests, err := meter.Int64Counter("http.client.request.count",
		metric.WithDescription("Number of outbound acquisition request attempts."),
	)
	if err != nil {
		return nil, fmt.Errorf("creating HTTP client request counter: %w", err)
	}

	duration, err := meter.Float64Histogram("http.client.request.duration",
		metric.WithDescription("Outbound acquisition request attempt duration."),
		metric.WithUnit("s"),
	)
	if err != nil {
		return nil, fmt.Errorf("creating HTTP client duration histogram: %w", err)
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
		requests:    requests,
		duration:    duration,
		rateGate:    rateGate,
		concurrency: make(chan struct{}, policy.MaxConcurrency),
	}, nil
}

// Observe constructs fresh board and catalog resources under the caller's lineage deadline.
func (client *Client) Observe(ctx context.Context) (snapshot.Boards, error) {
	if _, ok := ctx.Deadline(); !ok {
		return snapshot.Boards{}, ErrLineageDeadlineRequired
	}

	boards, err := client.fetchBoards(ctx)
	if err != nil {
		cancellation := externalCancellation(ctx)
		if cancellation != nil {
			return snapshot.Boards{}, cancellation
		}

		client.logFailure(ctx, boardsResource, err)

		return snapshot.Boards{State: snapshot.StateFailed}, nil
	}

	items := make([]snapshot.BoardItem, len(boards))
	failures := make([]error, len(boards))

	var wait sync.WaitGroup

	for index, board := range boards {
		items[index].Board = board.raw

		wait.Go(func() {
			pages, catalogError := client.fetchCatalog(ctx, board.id)
			if catalogError != nil {
				failures[index] = catalogError

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

	for index, failure := range failures {
		if failure == nil {
			continue
		}

		items[index].Catalog = &snapshot.Catalog{State: snapshot.StateFailed}

		client.logFailure(ctx, catalogResource, failure)
	}

	return snapshot.Boards{State: snapshot.StatePresent, Items: &items}, nil
}

func (client *Client) logFailure(ctx context.Context, resource string, err error) {
	client.logger.ErrorContext(ctx, "upstream acquisition failed",
		slog.String("resource.type", resource),
		slog.String("error.type", errorType(err)),
		slog.String("error.cause.type", requestErrorCauseType(err)),
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
