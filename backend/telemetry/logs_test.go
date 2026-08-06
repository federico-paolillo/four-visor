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
		"error.detail", "other",
		"publication.stage", "activation",
		"snapshot.component", "block",
		"http.response.status_code", 503,
		"forbidden.record", "error-secret",
	)
	logger.Info("effective backend policy configured",
		"acquisition.max_concurrency", 10,
		"lineage.deadline", "30m0s",
		"forbidden.record", "policy-secret",
	)
	logger.Warn("thread acquisition exceeds remaining rate capacity",
		"lineage.id", "01J1YQ7Y0M4S6R8T2V3W5X7Y9Z",
		"resource.queued.count", 2000,
		"resource.rate_capacity.count", 1700,
		"forbidden.record", "capacity-secret",
	)
	logger.Warn("upstream acquisition failed",
		"lineage.id", "01J1YQ7Y0M4S6R8T2V3W5X7Y9Z",
		"resource.type", "thread",
		"failure.stage", "request",
		"error.type", "http",
		"error.cause.type", "http_status",
		"http.response.status_code", 404,
		"retry.attempt", 0,
		"retry.exhausted", false,
		"forbidden.record", "fetch-secret",
	)

	if strings.Count(exported.String(), "\n") != 5 || strings.Contains(exported.String(), "routine chatter") {
		t.Fatalf("exported logs did not retain only the lifecycle and error records: %s", exported.String())
	}
	for _, forbidden := range []string{
		"forbidden.context", "context-secret", "forbidden.record", "routine-secret", "lineage-secret",
		"error-secret", "policy-secret", "capacity-secret", "fetch-secret",
	} {
		if strings.Contains(exported.String(), forbidden) {
			t.Fatalf("exported logs contain %q: %s", forbidden, exported.String())
		}
	}
	for _, retained := range []string{
		"dependency", "lineage.id", "error.type", "error.detail", "publication.stage",
		"snapshot.component", "http.response.status_code", "acquisition.max_concurrency",
		"lineage.deadline", "resource.queued.count", "resource.rate_capacity.count", "failure.stage",
		"error.cause.type", "retry.attempt", "retry.exhausted",
	} {
		if !strings.Contains(exported.String(), retained) {
			t.Fatalf("exported logs are missing %q: %s", retained, exported.String())
		}
	}

	if strings.Count(stderr.String(), "\n") != 6 {
		t.Fatalf("stderr record count changed: %s", stderr.String())
	}
	for _, unfiltered := range []string{
		"routine chatter", "context-secret", "routine-secret", "lineage-secret", "error-secret",
		"policy-secret", "capacity-secret", "fetch-secret",
	} {
		if !strings.Contains(stderr.String(), unfiltered) {
			t.Fatalf("stderr is missing %q: %s", unfiltered, stderr.String())
		}
	}
}
