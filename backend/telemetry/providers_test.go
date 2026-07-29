package telemetry

import (
	"io"
	"os"
	"testing"
)

func TestNewNeutralizesAmbientOTelConfiguration(t *testing.T) {
	hostile := map[string]string{
		"OTEL_EXPORTER_OTLP_ENDPOINT":                              "https://attacker.invalid:1",
		"OTEL_EXPORTER_OTLP_HEADERS":                               "authorization=secret",
		"OTEL_EXPORTER_OTLP_COMPRESSION":                           "gzip",
		"OTEL_EXPORTER_OTLP_INSECURE":                              "false",
		"OTEL_EXPORTER_OTLP_CERTIFICATE":                           "/does/not/exist",
		"OTEL_EXPORTER_OTLP_CLIENT_CERTIFICATE":                    "/does/not/exist",
		"OTEL_EXPORTER_OTLP_CLIENT_KEY":                            "/does/not/exist",
		"OTEL_EXPORTER_OTLP_TIMEOUT":                               "1",
		"OTEL_EXPORTER_OTLP_TRACES_ENDPOINT":                       "https://attacker.invalid:2",
		"OTEL_EXPORTER_OTLP_TRACES_HEADERS":                        "trace-secret=value",
		"OTEL_EXPORTER_OTLP_TRACES_COMPRESSION":                    "gzip",
		"OTEL_EXPORTER_OTLP_TRACES_CERTIFICATE":                    "/does/not/exist",
		"OTEL_EXPORTER_OTLP_METRICS_ENDPOINT":                      "https://attacker.invalid:3",
		"OTEL_EXPORTER_OTLP_METRICS_HEADERS":                       "metric-secret=value",
		"OTEL_EXPORTER_OTLP_METRICS_COMPRESSION":                   "gzip",
		"OTEL_EXPORTER_OTLP_METRICS_CERTIFICATE":                   "/does/not/exist",
		"OTEL_EXPORTER_OTLP_METRICS_TEMPORALITY_PREFERENCE":        "delta",
		"OTEL_EXPORTER_OTLP_METRICS_DEFAULT_HISTOGRAM_AGGREGATION": "base2_exponential_bucket_histogram",
		"OTEL_EXPORTER_OTLP_LOGS_ENDPOINT":                         "https://attacker.invalid:4",
		"OTEL_EXPORTER_OTLP_LOGS_HEADERS":                          "log-secret=value",
		"OTEL_EXPORTER_OTLP_LOGS_COMPRESSION":                      "gzip",
		"OTEL_EXPORTER_OTLP_LOGS_CERTIFICATE":                      "/does/not/exist",
		"OTEL_BSP_SCHEDULE_DELAY":                                  "1",
		"OTEL_BSP_EXPORT_TIMEOUT":                                  "1",
		"OTEL_BSP_MAX_QUEUE_SIZE":                                  "1",
		"OTEL_BSP_MAX_EXPORT_BATCH_SIZE":                           "1",
		"OTEL_BLRP_SCHEDULE_DELAY":                                 "1",
		"OTEL_BLRP_EXPORT_TIMEOUT":                                 "1",
		"OTEL_BLRP_MAX_QUEUE_SIZE":                                 "1",
		"OTEL_BLRP_MAX_EXPORT_BATCH_SIZE":                          "1",
		"OTEL_METRIC_EXPORT_INTERVAL":                              "1",
		"OTEL_METRIC_EXPORT_TIMEOUT":                               "1",
		"OTEL_METRICS_EXEMPLAR_FILTER":                             "always_off",
		"OTEL_GO_X_CARDINALITY_LIMIT":                              "1",
		"OTEL_GO_X_OBSERVABILITY":                                  "true",
		"OTEL_GO_X_METRIC_EXPORT_BATCH_SIZE":                       "1",
		"OTEL_TRACES_SAMPLER":                                      "always_off",
		"OTEL_SPAN_ATTRIBUTE_COUNT_LIMIT":                          "1",
		"OTEL_SPAN_ATTRIBUTE_VALUE_LENGTH_LIMIT":                   "1",
		"OTEL_SPAN_EVENT_COUNT_LIMIT":                              "1",
		"OTEL_SPAN_LINK_COUNT_LIMIT":                               "1",
		"OTEL_EVENT_ATTRIBUTE_COUNT_LIMIT":                         "1",
		"OTEL_LINK_ATTRIBUTE_COUNT_LIMIT":                          "1",
		"OTEL_LOGRECORD_ATTRIBUTE_COUNT_LIMIT":                     "1",
		"OTEL_LOGRECORD_ATTRIBUTE_VALUE_LENGTH_LIMIT":              "1",
		"OTEL_RESOURCE_ATTRIBUTES":                                 "host.name=attacker",
		"OTEL_SERVICE_NAME":                                        "attacker-service",
	}
	for key, value := range hostile {
		t.Setenv(key, value)
	}
	t.Setenv("FOURVISOR_TEST_SENTINEL", "preserved")

	providers, err := New(t.Context(), "http://127.0.0.1:4317", io.Discard)
	if err != nil {
		t.Fatalf("New() error with hostile OTEL_ environment = %v", err)
	}
	t.Cleanup(func() { _ = providers.Shutdown(t.Context()) })

	for key := range hostile {
		if _, exists := os.LookupEnv(key); exists {
			t.Errorf("%s remained in the SDK environment", key)
		}
	}
	if got := os.Getenv("FOURVISOR_TEST_SENTINEL"); got != "preserved" {
		t.Fatalf("FOURVISOR_ setting = %q, want preserved", got)
	}
	_, span := providers.Tracer.Tracer("test/providers").Start(t.Context(), "always-on")
	if !span.IsRecording() || !span.SpanContext().IsSampled() {
		t.Fatalf("hostile sampler changed always-on span: recording=%v context=%v", span.IsRecording(), span.SpanContext())
	}
	span.End()
}
