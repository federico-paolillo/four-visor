package telemetry

import (
	"context"
	"fmt"
	"log/slog"
)

var retainedLogMessages = map[string]struct{}{
	"synchronization started":        {},
	"outbound acquisition completed": {},
	"lineage activated":              {},
	"previous lineage evicted":       {},
	"synchronization completed":      {},
	"oversized thread detected":      {},
	"synchronization tick skipped":   {},
}

var retainedLogAttributes = map[string]struct{}{
	"dependency":                    {},
	"scheduler.reason":              {},
	"lineage.id":                    {},
	"lineage.observed_at":           {},
	lineageOutcomeKey:               {},
	"lineage.degradation.excessive": {},
	resourceTypeKey:                 {},
	"resource.state":                {},
	"resource.board.count":          {},
	"resource.catalog.count":        {},
	"resource.thread.count":         {},
	"resource.failed.count":         {},
	"resource.failed.tolerance":     {},
	"posts.limit":                   {},
	errorTypeKey:                    {},
	"error.cause.type":              {},
}

// otlpLogHandler applies the application log policy only to the OTLP branch.
type otlpLogHandler struct {
	handler slog.Handler
}

func (handler otlpLogHandler) Enabled(ctx context.Context, level slog.Level) bool {
	return handler.handler.Enabled(ctx, level)
}

func (handler otlpLogHandler) Handle(ctx context.Context, record slog.Record) error {
	if record.Level < slog.LevelError {
		if _, retained := retainedLogMessages[record.Message]; !retained {
			return nil
		}
	}

	filtered := slog.NewRecord(record.Time, record.Level, record.Message, record.PC)
	record.Attrs(func(attribute slog.Attr) bool {
		if _, retained := retainedLogAttributes[attribute.Key]; retained {
			filtered.AddAttrs(attribute)
		}

		return true
	})

	err := handler.handler.Handle(ctx, filtered)
	if err != nil {
		return fmt.Errorf("exporting OpenTelemetry log: %w", err)
	}

	return nil
}

func (handler otlpLogHandler) WithAttrs(attributes []slog.Attr) slog.Handler {
	filtered := make([]slog.Attr, 0, len(attributes))
	for _, attribute := range attributes {
		if _, retained := retainedLogAttributes[attribute.Key]; retained {
			filtered = append(filtered, attribute)
		}
	}

	return otlpLogHandler{handler: handler.handler.WithAttrs(filtered)}
}

func (handler otlpLogHandler) WithGroup(name string) slog.Handler {
	return otlpLogHandler{handler: handler.handler.WithGroup(name)}
}
