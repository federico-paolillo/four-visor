package lineage

import (
	"context"
	"errors"
	"fmt"

	"git.disroot.org/federico-paolillo/four-visor.git/snapshot"
	"github.com/bradfitz/gomemcache/memcache"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/trace"
)

var (
	errCorruptSnapshot = errors.New("corrupt active snapshot")
	errLineageMismatch = errors.New("active pointer and snapshot lineage differ")
)

type snapshotError struct {
	component string
	lineageID string
	cause     error
}

func (failure *snapshotError) Error() string {
	return "snapshot " + failure.component + " failed"
}

func (failure *snapshotError) Unwrap() error {
	return failure.cause
}

type snapshotRead struct {
	data      []byte
	lineageID string
}

type snapshotReader struct {
	cache  *instrumentedCache
	tracer trace.Tracer
}

// readSnapshot resolves and validates the lineage pinned by the active pointer without exposing its cache layout.
func (reader *snapshotReader) readSnapshot(ctx context.Context) (snapshotRead, error) {
	lineageID, err := reader.readPointer(ctx)
	if err != nil {
		return snapshotRead{}, err
	}

	metadata, err := reader.readCompletion(ctx, lineageID)
	if err != nil {
		return snapshotRead{}, err
	}

	data := make([]byte, 0, min(metadata.ByteLength, blockSize))
	for index := range metadata.BlockCount {
		data, err = reader.readBlock(ctx, lineageID, metadata, index, data)
		if err != nil {
			return snapshotRead{}, err
		}
	}

	data, err = reader.validate(ctx, lineageID, data)
	if err != nil {
		return snapshotRead{}, err
	}

	return snapshotRead{data: data, lineageID: lineageID}, nil
}

func (reader *snapshotReader) readPointer(ctx context.Context) (string, error) {
	ctx, span := reader.tracer.Start(ctx, "active-lineage.lookup")
	defer span.End()

	item, err := reader.get(ctx, activePointerKey)
	if err != nil {
		failure := wrapSnapshotFailure("pointer", "", err)
		finishSnapshotSpan(span, failure)

		return "", wrapSnapshotFailure("pointer", "", fmt.Errorf("reading active lineage pointer: %w", err))
	}

	lineageID := string(item.Value)
	if !snapshot.ValidLineageID(lineageID) {
		err = errors.Join(errCorruptSnapshot, errors.New("invalid active lineage identifier"))
		err = wrapSnapshotFailure("pointer", "", err)
		finishSnapshotSpan(span, err)

		return "", err
	}

	span.SetAttributes(
		attribute.String("snapshot.component", "pointer"),
		attribute.String("lineage.id", lineageID),
	)

	return lineageID, nil
}

func (reader *snapshotReader) readCompletion(ctx context.Context, lineageID string) (completion, error) {
	ctx, span := reader.tracer.Start(ctx, "lineage.completion.read",
		trace.WithAttributes(attribute.String("lineage.id", lineageID)),
	)
	defer span.End()

	item, err := reader.get(ctx, completionKey(lineageID))
	if err != nil {
		failure := wrapSnapshotFailure("completion", lineageID, err)
		finishSnapshotSpan(span, failure)

		return completion{}, wrapSnapshotFailure(
			"completion",
			lineageID,
			fmt.Errorf("reading lineage completion: %w", err),
		)
	}

	metadata, err := decodeCompletion(item.Value)
	if err != nil {
		failure := wrapSnapshotFailure("completion", lineageID, errors.Join(errCorruptSnapshot, err))
		finishSnapshotSpan(span, failure)

		return completion{}, failure
	}

	span.SetAttributes(attribute.String("snapshot.component", "completion"))

	return metadata, nil
}

func (reader *snapshotReader) readBlock(
	ctx context.Context,
	lineageID string,
	metadata completion,
	index int,
	data []byte,
) ([]byte, error) {
	ctx, span := reader.tracer.Start(ctx, "lineage.block.read",
		trace.WithAttributes(attribute.String("lineage.id", lineageID)),
	)
	defer span.End()

	item, err := reader.get(ctx, blockKey(lineageID, index))
	if err != nil {
		failure := wrapSnapshotFailure("block", lineageID, err)
		finishSnapshotSpan(span, failure)

		return nil, wrapSnapshotFailure(
			"block",
			lineageID,
			fmt.Errorf("reading lineage block: %w", err),
		)
	}

	data, err = appendBlock(data, metadata, index, item.Value)
	if err != nil {
		failure := wrapSnapshotFailure("block", lineageID, errors.Join(errCorruptSnapshot, err))
		finishSnapshotSpan(span, failure)

		return nil, failure
	}

	span.SetAttributes(attribute.String("snapshot.component", "block"))

	return data, nil
}

func (reader *snapshotReader) validate(ctx context.Context, lineageID string, data []byte) ([]byte, error) {
	ctx, span := reader.tracer.Start(ctx, "serialize.snapshot",
		trace.WithAttributes(attribute.String("lineage.id", lineageID)),
	)
	defer span.End()

	err := snapshotContextError(ctx)
	if err != nil {
		failure := wrapSnapshotFailure("serialization", lineageID, err)
		finishSnapshotSpan(span, failure)

		return nil, failure
	}

	parsed, err := snapshot.Parse(data)
	if err != nil {
		failure := wrapSnapshotFailure("serialization", lineageID, errors.Join(errCorruptSnapshot, err))
		finishSnapshotSpan(span, failure)

		return nil, failure
	}

	if parsed.LineageID != lineageID {
		failure := wrapSnapshotFailure(
			"serialization",
			lineageID,
			errors.Join(errCorruptSnapshot, errLineageMismatch),
		)
		finishSnapshotSpan(span, failure)

		return nil, failure
	}

	err = snapshotContextError(ctx)
	if err != nil {
		failure := wrapSnapshotFailure("serialization", lineageID, err)
		finishSnapshotSpan(span, failure)

		return nil, failure
	}

	span.SetAttributes(attribute.String("snapshot.component", "serialization"))

	return data, nil
}

func (reader *snapshotReader) get(ctx context.Context, key string) (*memcache.Item, error) {
	err := snapshotContextError(ctx)
	if err != nil {
		return nil, err
	}

	item, operationErr := reader.cache.get(ctx, key)
	contextErr := snapshotContextError(ctx)

	if operationErr != nil && contextErr != nil {
		return nil, errors.Join(operationErr, contextErr)
	}

	if operationErr != nil {
		return nil, operationErr
	}

	if contextErr != nil {
		return nil, contextErr
	}

	return item, nil
}

func snapshotContextError(ctx context.Context) error {
	if ctx.Err() == nil {
		return nil
	}

	return errors.Join(ctx.Err(), context.Cause(ctx))
}

func finishSnapshotSpan(span trace.Span, err error) {
	component, _ := snapshotErrorFields(err)
	span.SetAttributes(
		attribute.String("snapshot.component", component),
		attribute.String("error.type", snapshotErrorType(err)),
	)
	span.RecordError(err)
	span.SetStatus(codes.Error, "snapshot operation failed")
}

func wrapSnapshotFailure(component, lineageID string, err error) error {
	if err == nil {
		return nil
	}

	var failure *snapshotError
	if errors.As(err, &failure) {
		return err
	}

	return &snapshotError{component: component, lineageID: lineageID, cause: err}
}

func snapshotErrorFields(err error) (string, string) {
	var failure *snapshotError
	if errors.As(err, &failure) {
		return failure.component, failure.lineageID
	}

	return "response", ""
}

func snapshotErrorType(err error) string {
	switch {
	case errors.Is(err, context.Canceled):
		return "canceled"
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, memcache.ErrCacheMiss):
		return "cache_miss"
	case errors.Is(err, errCorruptSnapshot):
		return "corrupt"
	}

	var cacheFailure *cacheOperationError
	if errors.As(err, &cacheFailure) {
		return "unavailable"
	}

	return "invalid"
}
