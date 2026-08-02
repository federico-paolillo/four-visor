// This module proves the metric catalogue drops unapproved data before export.
package telemetry

import (
	"testing"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	metricsdk "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
)

func TestMetricViewDropsUnknownAndWrongKindMetricsAndFiltersAttributes(t *testing.T) {
	reader := metricsdk.NewManualReader()
	provider := metricsdk.NewMeterProvider(
		metricsdk.WithReader(reader),
		metricsdk.WithView(metricView),
	)
	t.Cleanup(func() { _ = provider.Shutdown(t.Context()) })
	meter := provider.Meter("test/metric-policy")

	approved, err := HTTPServerRequestCount.Int64Counter(meter)
	if err != nil {
		t.Fatalf("approved counter: %v", err)
	}
	unknown, err := meter.Int64Counter("unknown.metric")
	if err != nil {
		t.Fatalf("unknown counter: %v", err)
	}
	wrongKind, err := meter.Float64Histogram(metricCatalogue[HTTPServerRequestCount].name)
	if err != nil {
		t.Fatalf("wrong-kind histogram: %v", err)
	}

	attributes := metric.WithAttributes(
		attribute.String("http.request.method", "GET"),
		attribute.String("http.route", "/health"),
		attribute.Int(httpResponseStatusCodeKey, 200),
		attribute.String("forbidden.attribute", "secret-value"),
	)
	approved.Add(t.Context(), 1, attributes)
	unknown.Add(t.Context(), 1, attributes)
	wrongKind.Record(t.Context(), 1, attributes)

	var collected metricdata.ResourceMetrics
	if err := reader.Collect(t.Context(), &collected); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	metricCount := 0
	for _, scope := range collected.ScopeMetrics {
		for _, item := range scope.Metrics {
			metricCount++
			if item.Name != metricCatalogue[HTTPServerRequestCount].name {
				t.Fatalf("exported metric = %q, want approved counter", item.Name)
			}
			sum, ok := item.Data.(metricdata.Sum[int64])
			if !ok || len(sum.DataPoints) != 1 {
				t.Fatalf("approved metric data = %#v", item.Data)
			}
			got := sum.DataPoints[0].Attributes.ToSlice()
			if len(got) != 3 {
				t.Fatalf("exported attributes = %#v, want three allowed attributes", got)
			}
			for _, value := range got {
				if value.Key == "forbidden.attribute" {
					t.Fatalf("forbidden metric attribute exported: %#v", got)
				}
			}
		}
	}
	if metricCount != 1 {
		t.Fatalf("exported metric count = %d, want 1", metricCount)
	}
}

func TestFailureCounterAndActiveSizeGaugePolicies(t *testing.T) {
	reader := metricsdk.NewManualReader()
	provider := metricsdk.NewMeterProvider(
		metricsdk.WithReader(reader),
		metricsdk.WithView(metricView),
	)
	t.Cleanup(func() { _ = provider.Shutdown(t.Context()) })
	meter := provider.Meter("test/lineage-policy")

	failures, err := LineageResourceFailureCount.Int64Counter(meter)
	if err != nil {
		t.Fatalf("failure counter: %v", err)
	}
	activeSize, err := LineageActiveSize.Int64Gauge(meter)
	if err != nil {
		t.Fatalf("active size gauge: %v", err)
	}
	measurements := metric.WithAttributes(
		attribute.String("resource.type", "thread"),
		attribute.String("failure.stage", "request"),
		attribute.String("error.type", "http"),
		attribute.String("error.cause.type", "http_status"),
		attribute.Int("http.response.status_code", 503),
		attribute.Int("retry.attempt", 2),
		attribute.Bool("retry.exhausted", true),
		attribute.String("lineage.id", "must-be-dropped"),
	)
	failures.Add(t.Context(), 4, measurements)
	activeSize.Record(t.Context(), 1234, measurements)

	var collected metricdata.ResourceMetrics
	if err := reader.Collect(t.Context(), &collected); err != nil {
		t.Fatalf("Collect() error = %v", err)
	}
	seen := make(map[string]bool)
	for _, scope := range collected.ScopeMetrics {
		for _, item := range scope.Metrics {
			seen[item.Name] = true
			switch item.Name {
			case "lineage.resource.failure.count":
				data := item.Data.(metricdata.Sum[int64])
				if data.DataPoints[0].Value != 4 || data.DataPoints[0].Attributes.Len() != 7 {
					t.Fatalf("failure counter = %#v", data.DataPoints[0])
				}
			case "lineage.active.size":
				data := item.Data.(metricdata.Gauge[int64])
				if item.Unit != "By" || data.DataPoints[0].Value != 1234 || data.DataPoints[0].Attributes.Len() != 0 {
					t.Fatalf("active size gauge = %#v", item)
				}
			default:
				t.Fatalf("unexpected metric %q", item.Name)
			}
		}
	}
	if len(seen) != 2 {
		t.Fatalf("lineage metric policy exported %v", seen)
	}
}
