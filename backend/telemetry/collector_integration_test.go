package telemetry

import (
	"bufio"
	"context"
	"crypto/sha256"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/metric"
	tracesdk "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

const (
	syntheticSuccessTraceCount = 1000
	syntheticErrorTraceCount   = 100
	wantSampledSuccessTraces   = 88
	forbiddenSentinel          = "raw-secret-board-thread-url-error"
)

func TestCollectorSignalPolicies(t *testing.T) {
	fixture := collectorFixtureFromEnvironment(t)
	providers, err := New(t.Context(), fixture.endpoint, io.Discard)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = providers.Shutdown(ctx)
	})

	emitCollectorTestMetrics(t, providers)
	emitCollectorTestLogs(t, providers)
	emitCollectorTestTraces(t, fixture.endpoint)

	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	traces, metrics, logs := waitForCollectorOutput(t, ctx, fixture)

	assertCapturedTraces(t, traces)
	assertCapturedMetrics(t, metrics)
	assertCapturedLogs(t, logs)
}

type collectorFixture struct {
	endpoint string
	traces   string
	metrics  string
	logs     string
}

func collectorFixtureFromEnvironment(t *testing.T) collectorFixture {
	t.Helper()
	fixture := collectorFixture{
		endpoint: os.Getenv("FOURVISOR_TEST_COLLECTOR_ENDPOINT"),
		traces:   os.Getenv("FOURVISOR_TEST_COLLECTOR_TRACES_OUTPUT"),
		metrics:  os.Getenv("FOURVISOR_TEST_COLLECTOR_METRICS_OUTPUT"),
		logs:     os.Getenv("FOURVISOR_TEST_COLLECTOR_LOGS_OUTPUT"),
	}
	values := []string{fixture.endpoint, fixture.traces, fixture.metrics, fixture.logs}
	configured := 0
	for _, value := range values {
		if value != "" {
			configured++
		}
	}
	if configured == 0 {
		t.Skip("Collector integration fixture is not configured")
	}
	if configured != len(values) {
		t.Fatal("Collector integration fixture is only partially configured")
	}

	return fixture
}

func emitCollectorTestTraces(t *testing.T, endpoint string) {
	t.Helper()
	exporter, err := otlptracegrpc.New(t.Context(),
		otlptracegrpc.WithEndpointURL(endpoint),
		otlptracegrpc.WithTimeout(5*time.Second),
	)
	if err != nil {
		t.Fatalf("creating test trace exporter: %v", err)
	}
	provider := tracesdk.NewTracerProvider(
		tracesdk.WithIDGenerator(new(deterministicIDGenerator)),
		tracesdk.WithSampler(tracesdk.AlwaysSample()),
		tracesdk.WithBatcher(exporter,
			tracesdk.WithMaxQueueSize(4096),
			tracesdk.WithMaxExportBatchSize(512),
			tracesdk.WithBatchTimeout(time.Hour),
		),
	)
	t.Cleanup(func() {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = provider.Shutdown(ctx)
	})

	tracer := provider.Tracer("four-visor/collector-policy-test")
	for index := range syntheticSuccessTraceCount {
		emitSyntheticTrace(tracer, fmt.Sprintf("synthetic.success.%04d", index), false)
	}
	for index := range syntheticErrorTraceCount {
		emitSyntheticTrace(tracer, fmt.Sprintf("synthetic.error.%04d", index), true)
	}
	if err := provider.ForceFlush(t.Context()); err != nil {
		t.Fatalf("flushing test traces: %v", err)
	}
}

func emitSyntheticTrace(tracer trace.Tracer, name string, failed bool) {
	ctx, root := tracer.Start(context.Background(), name+".root", trace.WithSpanKind(trace.SpanKindServer))
	_, child := tracer.Start(ctx, name+".child", trace.WithSpanKind(trace.SpanKindClient))
	if failed {
		child.RecordError(errors.New("synthetic failure"))
		child.SetStatus(codes.Error, "synthetic failure")
	}
	child.End()
	root.End()
}

type deterministicIDGenerator struct {
	trace atomic.Uint64
	span  atomic.Uint64
}

func (generator *deterministicIDGenerator) NewIDs(context.Context) (trace.TraceID, trace.SpanID) {
	var source [8]byte
	binary.BigEndian.PutUint64(source[:], generator.trace.Add(1))
	digest := sha256.Sum256(source[:])
	var traceID trace.TraceID
	copy(traceID[:], digest[:len(traceID)])

	return traceID, generator.newSpanID()
}

func (generator *deterministicIDGenerator) NewSpanID(context.Context, trace.TraceID) trace.SpanID {
	return generator.newSpanID()
}

func (generator *deterministicIDGenerator) newSpanID() trace.SpanID {
	var spanID trace.SpanID
	binary.BigEndian.PutUint64(spanID[:], generator.span.Add(1))

	return spanID
}

func emitCollectorTestMetrics(t *testing.T, providers *Providers) {
	t.Helper()
	meter := providers.Meter.Meter("four-visor/collector-policy-test")
	forbidden := []attribute.KeyValue{
		attribute.String("board.id", "a"),
		attribute.Int("thread.id", 42),
		attribute.Int("post.id", 84),
		attribute.String("lineage.id", "01J1YQ7Y0M4S6R8T2V3W5X7Y9Z"),
		attribute.String("url.full", "https://attacker.invalid/raw"),
		attribute.String("error.message", "upstream disclosed a secret"),
		attribute.String("forbidden.raw", forbiddenSentinel),
	}
	measurement := func(allowed ...attribute.KeyValue) metric.MeasurementOption {
		return metric.WithAttributes(slices.Concat(allowed, forbidden)...)
	}

	serverCount, err := meter.Int64Counter("http.server.request.count")
	if err != nil {
		t.Fatalf("creating server request counter: %v", err)
	}
	serverDuration, err := meter.Float64Histogram("http.server.request.duration", metric.WithUnit("s"))
	if err != nil {
		t.Fatalf("creating server duration histogram: %v", err)
	}
	clientCount, err := meter.Int64Counter("http.client.request.count")
	if err != nil {
		t.Fatalf("creating client request counter: %v", err)
	}
	clientDuration, err := meter.Float64Histogram("http.client.request.duration", metric.WithUnit("s"))
	if err != nil {
		t.Fatalf("creating client duration histogram: %v", err)
	}
	cacheCount, err := meter.Int64Counter("cache.operation.count")
	if err != nil {
		t.Fatalf("creating cache operation counter: %v", err)
	}
	cacheDuration, err := meter.Float64Histogram("cache.operation.duration", metric.WithUnit("s"))
	if err != nil {
		t.Fatalf("creating cache duration histogram: %v", err)
	}
	syncDuration, err := meter.Float64Histogram("lineage.synchronization.duration", metric.WithUnit("s"))
	if err != nil {
		t.Fatalf("creating synchronization duration histogram: %v", err)
	}
	activated, err := meter.Int64Counter("lineage.synchronization.activated")
	if err != nil {
		t.Fatalf("creating activation counter: %v", err)
	}
	failedResources, err := meter.Int64Histogram("lineage.failed_resource.count", metric.WithUnit("{resource}"))
	if err != nil {
		t.Fatalf("creating failed-resource histogram: %v", err)
	}
	_, err = meter.Float64ObservableGauge("lineage.active.age",
		metric.WithUnit("s"),
		metric.WithFloat64Callback(func(_ context.Context, observer metric.Float64Observer) error {
			observer.Observe(60, measurement())

			return nil
		}),
	)
	if err != nil {
		t.Fatalf("creating active-age gauge: %v", err)
	}
	forbiddenMetric, err := meter.Int64Counter("fourvisor.forbidden.raw.metric")
	if err != nil {
		t.Fatalf("creating forbidden counter: %v", err)
	}

	ctx := t.Context()
	server := measurement(
		attribute.String("http.request.method", "GET"),
		attribute.String("http.route", "/health"),
		attribute.Int("http.response.status_code", 200),
	)
	serverCount.Add(ctx, 1, server)
	serverDuration.Record(ctx, 0.01, server)
	client := measurement(
		attribute.String("resource.type", "boards"),
		attribute.String("error.type", "none"),
		attribute.Int("http.response.status_code", 200),
	)
	clientCount.Add(ctx, 1, client)
	clientDuration.Record(ctx, 0.02, client)
	cache := measurement(
		attribute.String("cache.operation", "get"),
		attribute.String("cache.outcome", "hit"),
	)
	cacheCount.Add(ctx, 1, cache)
	cacheDuration.Record(ctx, 0.001, cache)
	lineage := measurement(attribute.String("lineage.outcome", "success"))
	syncDuration.Record(ctx, 1, lineage)
	activated.Add(ctx, 1, lineage)
	failedResources.Record(ctx, 0, measurement())
	forbiddenMetric.Add(ctx, 1, measurement())

	if err := providers.Meter.ForceFlush(ctx); err != nil {
		t.Fatalf("flushing test metrics: %v", err)
	}
}

func emitCollectorTestLogs(t *testing.T, providers *Providers) {
	t.Helper()
	forbidden := []slog.Attr{
		slog.String("board.id", "a"),
		slog.Int("thread.id", 42),
		slog.Int("post.id", 84),
		slog.String("url.full", "https://attacker.invalid/raw"),
		slog.String("error.message", "upstream disclosed a secret"),
		slog.String("forbidden.raw", forbiddenSentinel),
	}
	log := func(level slog.Level, message string, allowed ...slog.Attr) {
		providers.Slog.LogAttrs(context.Background(), level, message, slices.Concat(allowed, forbidden)...)
	}

	log(slog.LevelInfo, "synchronization started",
		slog.String("lineage.id", "01J1YQ7Y0M4S6R8T2V3W5X7Y9Z"),
		slog.String("lineage.outcome", "success"),
	)
	log(slog.LevelInfo, "outbound acquisition completed", slog.String("resource.type", "boards"))
	log(slog.LevelInfo, "lineage activated", slog.String("lineage.id", "01J1YQ7Y0M4S6R8T2V3W5X7Y9Z"))
	log(slog.LevelInfo, "previous lineage evicted")
	log(slog.LevelInfo, "synchronization completed", slog.String("lineage.outcome", "success"))
	log(slog.LevelWarn, "oversized thread detected", slog.String("resource.type", "thread"))
	log(slog.LevelWarn, "synchronization tick skipped", slog.String("scheduler.reason", "synchronization_active"))
	log(slog.LevelError, "synthetic unlisted error", slog.String("error.type", "failed"))
	log(slog.LevelError, "successful cache GET", slog.String("error.type", "failed"))

	log(slog.LevelInfo, "successful request completed")
	log(slog.LevelInfo, "successful cache GET")
	log(slog.LevelInfo, "successful outbound request")
	log(slog.LevelInfo, "cache hit")
	log(slog.LevelWarn, "unrelated warning")

	if err := providers.Logger.ForceFlush(t.Context()); err != nil {
		t.Fatalf("flushing test logs: %v", err)
	}
}

type capturedSignals struct {
	traces  []capturedSpan
	metrics []capturedMetric
	logs    []capturedLog
}

func waitForCollectorOutput(
	t *testing.T,
	ctx context.Context,
	fixture collectorFixture,
) ([]capturedSpan, []capturedMetric, []capturedLog) {
	t.Helper()
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	var latest capturedSignals
	var latestError error
	for {
		latest.traces, latestError = readCapturedSpans(fixture.traces)
		if latestError == nil {
			latest.metrics, latestError = readCapturedMetrics(fixture.metrics)
		}
		if latestError == nil {
			latest.logs, latestError = readCapturedLogs(fixture.logs)
		}
		if latestError == nil && collectorOutputComplete(latest) {
			return latest.traces, latest.metrics, latest.logs
		}

		select {
		case <-ctx.Done():
			t.Fatalf("Collector output incomplete: traces=%d metrics=%d logs=%d last error=%v",
				len(latest.traces), len(latest.metrics), len(latest.logs), latestError)
		case <-ticker.C:
		}
	}
}

func collectorOutputComplete(signals capturedSignals) bool {
	errorRoots := 0
	errorChildren := 0
	for _, span := range signals.traces {
		if strings.HasPrefix(span.Name, "synthetic.error.") && strings.HasSuffix(span.Name, ".root") {
			errorRoots++
		}
		if strings.HasPrefix(span.Name, "synthetic.error.") && strings.HasSuffix(span.Name, ".child") {
			errorChildren++
		}
	}

	return errorRoots == syntheticErrorTraceCount && errorChildren == syntheticErrorTraceCount &&
		len(signals.metrics) >= 10 && len(signals.logs) >= 9
}

type otlpAttribute struct {
	Key   string       `json:"key"`
	Value otlpAnyValue `json:"value"`
}

type otlpAnyValue struct {
	StringValue string          `json:"stringValue"`
	IntValue    json.RawMessage `json:"intValue"`
	DoubleValue json.RawMessage `json:"doubleValue"`
	BoolValue   json.RawMessage `json:"boolValue"`
}

func (value otlpAnyValue) string() string {
	if value.StringValue != "" {
		return value.StringValue
	}
	for _, raw := range []json.RawMessage{value.IntValue, value.DoubleValue, value.BoolValue} {
		if len(raw) == 0 {
			continue
		}
		var text string
		if json.Unmarshal(raw, &text) == nil {
			return text
		}

		return string(raw)
	}

	return ""
}

func attributeMap(attributes []otlpAttribute) map[string]string {
	values := make(map[string]string, len(attributes))
	for _, item := range attributes {
		values[item.Key] = item.Value.string()
	}

	return values
}

type capturedSpan struct {
	TraceID      string          `json:"traceId"`
	SpanID       string          `json:"spanId"`
	ParentSpanID string          `json:"parentSpanId"`
	Name         string          `json:"name"`
	Status       json.RawMessage `json:"status"`
}

func readCapturedSpans(path string) ([]capturedSpan, error) {
	var spans []capturedSpan
	err := readOTLPJSONLines(path, func(data []byte) error {
		var batch struct {
			ResourceSpans []struct {
				ScopeSpans []struct {
					Spans []capturedSpan `json:"spans"`
				} `json:"scopeSpans"`
			} `json:"resourceSpans"`
		}
		if err := json.Unmarshal(data, &batch); err != nil {
			return err
		}
		for _, resource := range batch.ResourceSpans {
			for _, scope := range resource.ScopeSpans {
				spans = append(spans, scope.Spans...)
			}
		}

		return nil
	})

	return spans, err
}

type capturedMetric struct {
	Name       string              `json:"name"`
	Unit       string              `json:"unit"`
	Gauge      *capturedMetricData `json:"gauge"`
	Sum        *capturedMetricData `json:"sum"`
	Histogram  *capturedMetricData `json:"histogram"`
	Attributes map[string]string   `json:"-"`
	Resource   map[string]string   `json:"-"`
}

type capturedMetricData struct {
	DataPoints []struct {
		Attributes []otlpAttribute `json:"attributes"`
	} `json:"dataPoints"`
}

func readCapturedMetrics(path string) ([]capturedMetric, error) {
	var metrics []capturedMetric
	err := readOTLPJSONLines(path, func(data []byte) error {
		var batch struct {
			ResourceMetrics []struct {
				Resource struct {
					Attributes []otlpAttribute `json:"attributes"`
				} `json:"resource"`
				ScopeMetrics []struct {
					Metrics []capturedMetric `json:"metrics"`
				} `json:"scopeMetrics"`
			} `json:"resourceMetrics"`
		}
		if err := json.Unmarshal(data, &batch); err != nil {
			return err
		}
		for _, resource := range batch.ResourceMetrics {
			resourceAttributes := attributeMap(resource.Resource.Attributes)
			for _, scope := range resource.ScopeMetrics {
				for _, item := range scope.Metrics {
					item.Resource = resourceAttributes
					points := item.dataPoints()
					if len(points) == 1 {
						item.Attributes = attributeMap(points[0].Attributes)
					}
					metrics = append(metrics, item)
				}
			}
		}

		return nil
	})

	return metrics, err
}

func (item capturedMetric) dataPoints() []struct {
	Attributes []otlpAttribute `json:"attributes"`
} {
	switch {
	case item.Gauge != nil:
		return item.Gauge.DataPoints
	case item.Sum != nil:
		return item.Sum.DataPoints
	case item.Histogram != nil:
		return item.Histogram.DataPoints
	default:
		return nil
	}
}

type capturedLog struct {
	SeverityText string          `json:"severityText"`
	Body         otlpAnyValue    `json:"body"`
	Attributes   []otlpAttribute `json:"attributes"`
	TraceID      string          `json:"traceId"`
	SpanID       string          `json:"spanId"`
}

func readCapturedLogs(path string) ([]capturedLog, error) {
	var logs []capturedLog
	err := readOTLPJSONLines(path, func(data []byte) error {
		var batch struct {
			ResourceLogs []struct {
				ScopeLogs []struct {
					LogRecords []capturedLog `json:"logRecords"`
				} `json:"scopeLogs"`
			} `json:"resourceLogs"`
		}
		if err := json.Unmarshal(data, &batch); err != nil {
			return err
		}
		for _, resource := range batch.ResourceLogs {
			for _, scope := range resource.ScopeLogs {
				logs = append(logs, scope.LogRecords...)
			}
		}

		return nil
	})

	return logs, err
}

func readOTLPJSONLines(path string, consume func([]byte) error) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close() //nolint:errcheck // Read-only test fixture has no close-error recovery action.

	scanner := bufio.NewScanner(file)
	scanner.Buffer(make([]byte, 64*1024), 16*1024*1024)
	for scanner.Scan() {
		if err := consume(scanner.Bytes()); err != nil {
			return err
		}
	}

	return scanner.Err()
}

func assertCapturedTraces(t *testing.T, spans []capturedSpan) {
	t.Helper()
	byTrace := make(map[string][]capturedSpan)
	for _, span := range spans {
		if strings.HasPrefix(span.Name, "synthetic.") {
			byTrace[span.TraceID] = append(byTrace[span.TraceID], span)
		}
	}
	successes := 0
	errorsFound := 0
	for _, traceSpans := range byTrace {
		if len(traceSpans) != 2 {
			t.Fatalf("captured trace has %d spans, want 2: %#v", len(traceSpans), traceSpans)
		}
		var root, child capturedSpan
		for _, span := range traceSpans {
			if strings.HasSuffix(span.Name, ".root") {
				root = span
			} else if strings.HasSuffix(span.Name, ".child") {
				child = span
			}
		}
		if root.Name == "" || child.Name == "" || child.ParentSpanID != root.SpanID {
			t.Fatalf("captured topology root=%#v child=%#v", root, child)
		}
		if strings.HasPrefix(root.Name, "synthetic.error.") {
			errorsFound++
			if statusIsError(root.Status) || !statusIsError(child.Status) {
				t.Fatalf("error trace statuses root=%s child=%s", root.Status, child.Status)
			}
		} else {
			successes++
		}
	}
	if errorsFound != syntheticErrorTraceCount {
		t.Fatalf("retained error traces = %d, want %d", errorsFound, syntheticErrorTraceCount)
	}
	if successes < 80 || successes > 120 {
		t.Fatalf("retained successful traces = %d, want approximately 100", successes)
	}
	if successes != wantSampledSuccessTraces {
		t.Fatalf("retained successful traces = %d, want pinned deterministic count %d", successes, wantSampledSuccessTraces)
	}
	t.Logf("pinned Collector retained %d/%d deterministic successful traces", successes, syntheticSuccessTraceCount)
}

func statusIsError(status json.RawMessage) bool {
	return strings.Contains(string(status), "STATUS_CODE_ERROR") || strings.Contains(string(status), `"code":2`)
}

func assertCapturedMetrics(t *testing.T, metrics []capturedMetric) {
	t.Helper()
	type expectedMetric struct {
		kind       string
		unit       string
		attributes map[string]string
	}
	expected := map[string]expectedMetric{
		"http.server.request.count":         {kind: "sum", attributes: map[string]string{"http.request.method": "GET", "http.route": "/health", "http.response.status_code": "200"}},
		"http.server.request.duration":      {kind: "histogram", unit: "s", attributes: map[string]string{"http.request.method": "GET", "http.route": "/health", "http.response.status_code": "200"}},
		"http.client.request.count":         {kind: "sum", attributes: map[string]string{"resource.type": "boards", "error.type": "none", "http.response.status_code": "200"}},
		"http.client.request.duration":      {kind: "histogram", unit: "s", attributes: map[string]string{"resource.type": "boards", "error.type": "none", "http.response.status_code": "200"}},
		"cache.operation.count":             {kind: "sum", attributes: map[string]string{"cache.operation": "get", "cache.outcome": "hit"}},
		"cache.operation.duration":          {kind: "histogram", unit: "s", attributes: map[string]string{"cache.operation": "get", "cache.outcome": "hit"}},
		"lineage.synchronization.duration":  {kind: "histogram", unit: "s", attributes: map[string]string{"lineage.outcome": "success"}},
		"lineage.synchronization.activated": {kind: "sum", attributes: map[string]string{"lineage.outcome": "success"}},
		"lineage.failed_resource.count":     {kind: "histogram", unit: "{resource}", attributes: map[string]string{}},
		"lineage.active.age":                {kind: "gauge", unit: "s", attributes: map[string]string{}},
	}
	seen := make(map[string]bool, len(expected))
	for _, item := range metrics {
		want, ok := expected[item.Name]
		if !ok {
			t.Fatalf("unexpected exported metric %q", item.Name)
		}
		if seen[item.Name] {
			t.Fatalf("metric %q exported more than once", item.Name)
		}
		seen[item.Name] = true
		if item.kind() != want.kind || item.Unit != want.unit {
			t.Fatalf("metric %q kind/unit = %q/%q, want %q/%q", item.Name, item.kind(), item.Unit, want.kind, want.unit)
		}
		if !mapsEqual(item.Attributes, want.attributes) {
			t.Fatalf("metric %q attributes = %#v, want %#v", item.Name, item.Attributes, want.attributes)
		}
		if !mapsEqual(item.Resource, map[string]string{"service.name": serviceName}) {
			t.Fatalf("metric %q resource = %#v", item.Name, item.Resource)
		}
	}
	if len(seen) != len(expected) {
		t.Fatalf("exported metrics = %#v, want all %d allowlisted instruments", seen, len(expected))
	}
}

func (item capturedMetric) kind() string {
	switch {
	case item.Gauge != nil:
		return "gauge"
	case item.Sum != nil:
		return "sum"
	case item.Histogram != nil:
		return "histogram"
	default:
		return ""
	}
}

func mapsEqual(left, right map[string]string) bool {
	if len(left) != len(right) {
		return false
	}
	for key, value := range left {
		if right[key] != value || strings.Contains(value, forbiddenSentinel) {
			return false
		}
	}

	return true
}

func assertCapturedLogs(t *testing.T, logs []capturedLog) {
	t.Helper()
	expected := map[string]map[string]string{
		"INFO synchronization started":        {"lineage.id": "01J1YQ7Y0M4S6R8T2V3W5X7Y9Z", "lineage.outcome": "success"},
		"INFO outbound acquisition completed": {"resource.type": "boards"},
		"INFO lineage activated":              {"lineage.id": "01J1YQ7Y0M4S6R8T2V3W5X7Y9Z"},
		"INFO previous lineage evicted":       {},
		"INFO synchronization completed":      {"lineage.outcome": "success"},
		"WARN oversized thread detected":      {"resource.type": "thread"},
		"WARN synchronization tick skipped":   {"scheduler.reason": "synchronization_active"},
		"ERROR synthetic unlisted error":      {"error.type": "failed"},
		"ERROR successful cache GET":          {"error.type": "failed"},
	}
	seen := make(map[string]bool, len(expected))
	for _, record := range logs {
		key := record.SeverityText + " " + record.Body.string()
		want, ok := expected[key]
		if !ok {
			t.Fatalf("unexpected exported log %q", key)
		}
		if seen[key] {
			t.Fatalf("log %q exported more than once", key)
		}
		seen[key] = true
		attributes := attributeMap(record.Attributes)
		if !mapsEqual(attributes, want) {
			t.Fatalf("log %q attributes = %#v, want %#v", key, attributes, want)
		}
	}
	if len(seen) != len(expected) {
		t.Fatalf("exported logs = %#v, want all %d retained records", seen, len(expected))
	}
}
