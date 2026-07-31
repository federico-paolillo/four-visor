package telemetry

import (
	"context"
	"fmt"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
	metricsdk "go.opentelemetry.io/otel/sdk/metric"
)

// Metric identifies one application metric and its SDK policy.
type Metric uint8

const (
	HTTPServerRequestCount Metric = iota
	HTTPServerRequestDuration
	HTTPClientRequestCount
	HTTPClientRequestDuration
	CacheOperationCount
	CacheOperationDuration
	LineageSynchronizationDuration
	LineageSynchronizationActivated
	LineageFailedResourceCount
	LineageActiveAge
)

const (
	httpResponseStatusCodeKey = "http.response.status_code"
	resourceTypeKey           = "resource.type"
	errorTypeKey              = "error.type"
	lineageOutcomeKey         = "lineage.outcome"
)

type metricDefinition struct {
	name        string
	kind        metricsdk.InstrumentKind
	unit        string
	description string
	attributes  []attribute.Key
}

var metricCatalogue = [...]metricDefinition{
	HTTPServerRequestCount: {
		name:        "http.server.request.count",
		kind:        metricsdk.InstrumentKindCounter,
		description: "Number of inbound HTTP requests.",
		attributes:  []attribute.Key{"http.request.method", "http.route", httpResponseStatusCodeKey},
	},
	HTTPServerRequestDuration: {
		name:        "http.server.request.duration",
		kind:        metricsdk.InstrumentKindHistogram,
		unit:        "s",
		description: "Inbound HTTP request duration.",
		attributes:  []attribute.Key{"http.request.method", "http.route", httpResponseStatusCodeKey},
	},
	HTTPClientRequestCount: {
		name:        "http.client.request.count",
		kind:        metricsdk.InstrumentKindCounter,
		description: "Number of outbound acquisition request attempts.",
		attributes:  []attribute.Key{resourceTypeKey, errorTypeKey, httpResponseStatusCodeKey},
	},
	HTTPClientRequestDuration: {
		name:        "http.client.request.duration",
		kind:        metricsdk.InstrumentKindHistogram,
		unit:        "s",
		description: "Outbound acquisition request attempt duration.",
		attributes:  []attribute.Key{resourceTypeKey, errorTypeKey, httpResponseStatusCodeKey},
	},
	CacheOperationCount: {
		name:        "cache.operation.count",
		kind:        metricsdk.InstrumentKindCounter,
		description: "Number of Memcached operations.",
		attributes:  []attribute.Key{"cache.operation", "cache.outcome"},
	},
	CacheOperationDuration: {
		name:        "cache.operation.duration",
		kind:        metricsdk.InstrumentKindHistogram,
		unit:        "s",
		description: "Memcached operation duration.",
		attributes:  []attribute.Key{"cache.operation", "cache.outcome"},
	},
	LineageSynchronizationDuration: {
		name:        "lineage.synchronization.duration",
		kind:        metricsdk.InstrumentKindHistogram,
		unit:        "s",
		description: "Duration of scheduled lineage synchronization attempts.",
		attributes:  []attribute.Key{lineageOutcomeKey},
	},
	LineageSynchronizationActivated: {
		name:        "lineage.synchronization.activated",
		kind:        metricsdk.InstrumentKindCounter,
		description: "Number of activated successful or degraded lineages.",
		attributes:  []attribute.Key{lineageOutcomeKey},
	},
	LineageFailedResourceCount: {
		name:        "lineage.failed_resource.count",
		kind:        metricsdk.InstrumentKindHistogram,
		unit:        "{resource}",
		description: "Number of failed resources in an activated lineage.",
	},
	LineageActiveAge: {
		name:        "lineage.active.age",
		kind:        metricsdk.InstrumentKindObservableGauge,
		unit:        "s",
		description: "Age of the lineage activated by this process.",
	},
}

// Int64Counter creates this catalogue metric as an integer counter.
//
//nolint:ireturn // OpenTelemetry instruments are interfaces.
func (item Metric) Int64Counter(meter metric.Meter) (metric.Int64Counter, error) {
	definition := metricCatalogue[item]

	instrument, err := meter.Int64Counter(definition.name,
		metric.WithDescription(definition.description),
		metric.WithUnit(definition.unit),
	)
	if err != nil {
		return nil, fmt.Errorf("creating metric %q: %w", definition.name, err)
	}

	return instrument, nil
}

// Float64Histogram creates this catalogue metric as a floating-point histogram.
//
//nolint:ireturn // OpenTelemetry instruments are interfaces.
func (item Metric) Float64Histogram(meter metric.Meter) (metric.Float64Histogram, error) {
	definition := metricCatalogue[item]

	instrument, err := meter.Float64Histogram(definition.name,
		metric.WithDescription(definition.description),
		metric.WithUnit(definition.unit),
	)
	if err != nil {
		return nil, fmt.Errorf("creating metric %q: %w", definition.name, err)
	}

	return instrument, nil
}

// Int64Histogram creates this catalogue metric as an integer histogram.
//
//nolint:ireturn // OpenTelemetry instruments are interfaces.
func (item Metric) Int64Histogram(meter metric.Meter) (metric.Int64Histogram, error) {
	definition := metricCatalogue[item]

	instrument, err := meter.Int64Histogram(definition.name,
		metric.WithDescription(definition.description),
		metric.WithUnit(definition.unit),
	)
	if err != nil {
		return nil, fmt.Errorf("creating metric %q: %w", definition.name, err)
	}

	return instrument, nil
}

// Float64ObservableGauge creates this catalogue metric as a floating-point observable gauge.
//
//nolint:ireturn // OpenTelemetry instruments are interfaces.
func (item Metric) Float64ObservableGauge(
	meter metric.Meter,
	callback func(context.Context, metric.Float64Observer) error,
) (metric.Float64ObservableGauge, error) {
	definition := metricCatalogue[item]

	instrument, err := meter.Float64ObservableGauge(definition.name,
		metric.WithDescription(definition.description),
		metric.WithUnit(definition.unit),
		metric.WithFloat64Callback(callback),
	)
	if err != nil {
		return nil, fmt.Errorf("creating metric %q: %w", definition.name, err)
	}

	return instrument, nil
}

func metricView(instrument metricsdk.Instrument) (metricsdk.Stream, bool) {
	for _, definition := range metricCatalogue {
		if instrument.Name == definition.name && instrument.Kind == definition.kind {
			return metricsdk.Stream{
				Name:            instrument.Name,
				Description:     instrument.Description,
				Unit:            instrument.Unit,
				Aggregation:     metricsdk.AggregationDefault{},
				AttributeFilter: attribute.NewAllowKeysFilter(definition.attributes...),
			}, true
		}
	}

	return metricsdk.Stream{Aggregation: metricsdk.AggregationDrop{}}, true
}
