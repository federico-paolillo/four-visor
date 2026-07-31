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
