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

type snapshotReader struct {
	cache  *instrumentedCache
	tracer trace.Tracer
}

// read resolves and validates the lineage pinned by the active pointer without exposing its cache layout.
func (reader *snapshotReader) read(ctx context.Context) ([]byte, error) {
	lineageID, err := reader.readPointer(ctx)
	if err != nil {
		return nil, err
	}

	metadata, err := reader.readCompletion(ctx, lineageID)
	if err != nil {
		return nil, err
	}

	data := make([]byte, 0, min(metadata.ByteLength, blockSize))
	for index := range metadata.BlockCount {
		data, err = reader.readBlock(ctx, lineageID, metadata, index, data)
		if err != nil {
			return nil, err
		}
	}

	return reader.validate(ctx, lineageID, data)
}

func (reader *snapshotReader) readPointer(ctx context.Context) (string, error) {
	ctx, span := reader.tracer.Start(ctx, "active-lineage.lookup")
	defer span.End()

	item, err := reader.get(ctx, activePointerKey)
	if err != nil {
		finishSnapshotSpan(span, "pointer", err)

		return "", fmt.Errorf("reading active lineage pointer: %w", err)
	}

	lineageID := string(item.Value)
	if !snapshot.ValidLineageID(lineageID) {
		err = errors.Join(errCorruptSnapshot, errors.New("invalid active lineage identifier"))
		finishSnapshotSpan(span, "pointer", err)

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
		finishSnapshotSpan(span, "completion", err)

		return completion{}, fmt.Errorf("reading lineage completion: %w", err)
	}

	metadata, err := decodeCompletion(item.Value)
	if err != nil {
		failure := errors.Join(errCorruptSnapshot, err)
		finishSnapshotSpan(span, "completion", failure)

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
		finishSnapshotSpan(span, "block", err)

		return nil, fmt.Errorf("reading lineage block: %w", err)
	}

	data, err = appendBlock(data, metadata, index, item.Value)
	if err != nil {
		failure := errors.Join(errCorruptSnapshot, err)
		finishSnapshotSpan(span, "block", failure)

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
		finishSnapshotSpan(span, "serialization", err)

		return nil, err
	}

	parsed, err := snapshot.Parse(data)
	if err != nil {
		failure := errors.Join(errCorruptSnapshot, err)
		finishSnapshotSpan(span, "serialization", failure)

		return nil, failure
	}

	if parsed.LineageID != lineageID {
		failure := errors.Join(errCorruptSnapshot, errLineageMismatch)
		finishSnapshotSpan(span, "serialization", failure)

		return nil, failure
	}

	err = snapshotContextError(ctx)
	if err != nil {
		finishSnapshotSpan(span, "serialization", err)

		return nil, err
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

func finishSnapshotSpan(span trace.Span, component string, err error) {
	span.SetAttributes(
		attribute.String("snapshot.component", component),
		attribute.String("error.type", snapshotErrorType(err)),
	)
	span.RecordError(errors.New("snapshot operation failed"))
	span.SetStatus(codes.Error, "snapshot operation failed")
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
