// This module proves OTLP log filtering does not alter local stderr diagnostics.
package telemetry

import (
	"bytes"
	"log/slog"
	"strings"
	"testing"
)

func TestOTLPLogHandlerFiltersRecordsAndAttributesOnlyFromExport(t *testing.T) {
	var exported bytes.Buffer
	var stderr bytes.Buffer
	logger := slog.New(slog.NewMultiHandler(
		slog.NewJSONHandler(&stderr, nil),
		otlpLogHandler{handler: slog.NewJSONHandler(&exported, nil)},
	)).With(
		"dependency", "memcached",
		"forbidden.context", "context-secret",
	)

	logger.Info("routine chatter", "forbidden.record", "routine-secret")
	logger.Info("lineage activated",
		"lineage.id", "01J1YQ7Y0M4S6R8T2V3W5X7Y9Z",
		"forbidden.record", "lineage-secret",
	)
	logger.Error("synthetic failure",
		"error.type", "failed",
		"forbidden.record", "error-secret",
	)

	if strings.Count(exported.String(), "\n") != 2 || strings.Contains(exported.String(), "routine chatter") {
		t.Fatalf("exported logs did not retain only the lifecycle and error records: %s", exported.String())
	}
	for _, forbidden := range []string{"forbidden.context", "context-secret", "forbidden.record", "routine-secret", "lineage-secret", "error-secret"} {
		if strings.Contains(exported.String(), forbidden) {
			t.Fatalf("exported logs contain %q: %s", forbidden, exported.String())
		}
	}
	for _, retained := range []string{"dependency", "lineage.id", "error.type"} {
		if !strings.Contains(exported.String(), retained) {
			t.Fatalf("exported logs are missing %q: %s", retained, exported.String())
		}
	}

	if strings.Count(stderr.String(), "\n") != 3 {
		t.Fatalf("stderr record count changed: %s", stderr.String())
	}
	for _, unfiltered := range []string{"routine chatter", "context-secret", "routine-secret", "lineage-secret", "error-secret"} {
		if !strings.Contains(stderr.String(), unfiltered) {
			t.Fatalf("stderr is missing %q: %s", unfiltered, stderr.String())
		}
	}
}
