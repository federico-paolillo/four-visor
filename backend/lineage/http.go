package lineage

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"strconv"

	"github.com/bradfitz/gomemcache/memcache"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"
)

var errInvalidSnapshotHandler = errors.New("invalid snapshot handler")

var (
	methodNotAllowedBody   = []byte("{\"error\":\"method not allowed\"}\n")
	snapshotGoneBody       = []byte("{\"error\":\"snapshot unavailable\"}\n")
	serviceUnavailableBody = []byte("{\"error\":\"service unavailable\"}\n")
	internalErrorBody      = []byte("{\"error\":\"internal server error\"}\n")
)

// SnapshotHandler serves the complete active lineage through one internal JSON route.
type SnapshotHandler struct {
	reader *snapshotReader
	logger *slog.Logger
}

// NewSnapshotHandler creates the active snapshot handler with bounded Memcached operations.
func NewSnapshotHandler(
	address string,
	logger *slog.Logger,
	tracer trace.Tracer,
	meter metric.Meter,
) (*SnapshotHandler, error) {
	client := memcache.New(address)
	client.Timeout = memcache.DefaultTimeout

	return newSnapshotHandler(client, logger, tracer, meter)
}

func newSnapshotHandler(
	client cacheClient,
	logger *slog.Logger,
	tracer trace.Tracer,
	meter metric.Meter,
) (*SnapshotHandler, error) {
	if client == nil || logger == nil || tracer == nil || meter == nil {
		return nil, errInvalidSnapshotHandler
	}

	cache, err := newInstrumentedCache(client, tracer, meter)
	if err != nil {
		return nil, fmt.Errorf("instrumenting snapshot cache: %w", err)
	}

	return &SnapshotHandler{
		reader: &snapshotReader{cache: cache, tracer: tracer},
		logger: logger,
	}, nil
}

// ServeHTTP accepts only GET and commits success after the complete pinned lineage validates.
func (handler *SnapshotHandler) ServeHTTP(writer http.ResponseWriter, request *http.Request) {
	if request.Method != http.MethodGet {
		writer.Header().Set("Allow", http.MethodGet)
		writeSnapshotJSON(writer, http.StatusMethodNotAllowed, methodNotAllowedBody)

		return
	}

	data, err := handler.reader.read(request.Context())
	if err != nil {
		handler.fail(writer, request, err)

		return
	}

	err = snapshotContextError(request.Context())
	if err != nil {
		handler.fail(writer, request, err)

		return
	}

	setSnapshotHeaders(writer.Header(), len(data))
	writer.WriteHeader(http.StatusOK)

	written, err := writer.Write(data)

	if err == nil && written != len(data) {
		err = io.ErrShortWrite
	}

	if err != nil {
		handler.reportFailure(request.Context(), fmt.Errorf("writing snapshot response: %w", err))
	}
}

func (handler *SnapshotHandler) fail(writer http.ResponseWriter, request *http.Request, err error) {
	status := snapshotFailureStatus(err)
	handler.reportFailure(request.Context(), err)

	switch status {
	case http.StatusGone:
		writeSnapshotJSON(writer, status, snapshotGoneBody)
	case http.StatusServiceUnavailable:
		writeSnapshotJSON(writer, status, serviceUnavailableBody)
	default:
		writeSnapshotJSON(writer, status, internalErrorBody)
	}
}

func (handler *SnapshotHandler) reportFailure(ctx context.Context, err error) {
	errorType := snapshotErrorType(err)
	root := trace.SpanFromContext(ctx)
	root.SetAttributes(attribute.String("error.type", errorType))
	root.RecordError(errors.New("snapshot request failed"))
	root.SetStatus(codes.Error, "snapshot request failed")

	handler.logger.ErrorContext(ctx, "snapshot request failed", slog.String("error.type", errorType))
}

func snapshotFailureStatus(err error) int {
	switch {
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return http.StatusServiceUnavailable
	case errors.Is(err, memcache.ErrCacheMiss):
		return http.StatusGone
	}

	var cacheFailure *cacheOperationError
	if errors.As(err, &cacheFailure) {
		return http.StatusServiceUnavailable
	}

	return http.StatusInternalServerError
}

func writeSnapshotJSON(writer http.ResponseWriter, status int, body []byte) {
	setSnapshotHeaders(writer.Header(), len(body))
	writer.WriteHeader(status)
	_, _ = writer.Write(body)
}

func setSnapshotHeaders(header http.Header, length int) {
	header.Set("Cache-Control", "no-store")
	header.Set("Content-Type", "application/json")
	header.Set("Content-Length", strconv.Itoa(length))
	header.Del("Content-Encoding")
	header.Del("Accept-Ranges")
	header.Del("Content-Range")
}
