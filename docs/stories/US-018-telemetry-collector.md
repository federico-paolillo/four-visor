# US-018: Retain failed traces and a sample of successful operation

## Goal

Complete the trace-first operational path from the Go backend through the internal Collector to configurable metric, log, and trace destinations.

## User Value

The Operator can explain a failed request or degraded lineage from one trace while keeping telemetry volume and personal-project operations small.

## Scope

- Configure the Go SDK to enforce the application metric/log policy and the internal OpenTelemetry Collector to receive backend OTLP telemetry and perform tail-based trace sampling.
- Own and validate the SDK catalogue/View/log wrapper plus the Collector receiver, memory, batching, tail-sampling, authentication, and exporter semantics; reuse the service wiring introduced by US-017.
- Retain every trace containing an error and approximately 10% of fully successful traces; perform no application-side sampling.
- Verify, without reimplementing accepted instrumentation, that inbound request and scheduled synchronization root traces from earlier stories contain the required outbound HTTP, Memcached, validation/serialization, construction, activation, and eviction children where applicable.
- Export only the enumerated low-cardinality HTTP/cache/lineage metrics and meaningful lifecycle/error logs through one authenticated Grafana Cloud OTLP/HTTP destination.
- Keep Caddy and third-party container stdout outside the Go OpenTelemetry contract; Collector/exporter failure must not fail application operations.
- Document signal names/attributes, how to locate a degraded sync by lineage ULID, exporter configuration, expected single-node gaps, and a concise operator troubleshooting flow.

## Out of Scope

- A second observability stack, application-side sampling, local telemetry buffering, verbose request logs, high-cardinality metrics, audit/business/user analytics, session replay, or enterprise alerting/SLO machinery.

## Dependencies

- US-017.

## Related MADRs

- None. The telemetry topology and sampling behavior are locked; concrete exporter destinations and Collector syntax are operator configuration/implementation details.

## Traceability

- `Full Requirements / Observability` (`docs/SEED.md:218-229`): Go OpenTelemetry boundary, sparse metrics/logs, root/child traces, and sampling outcomes.
- `High-Level Architecture / Observability path` (`docs/SEED.md:548-596`): root/child topology and Collector tail sampling.
- `Operational Flows / Trace flow for inbound requests`, `/ Trace flow for scheduled synchronization`, and `/ Telemetry export` (`docs/SEED.md:1081-1153`): required spans, events, exported logs, and excluded labels.
- `Deployment View / Observability` (`docs/SEED.md:1292-1302`): backend-to-Collector path and third-party stdout boundary.
- `Detailed Observability` (`docs/SEED.md:1450-1629`): exact philosophy, signal set, error propagation, sampling, no buffering, and diagnostic questions.
- `Locked Decisions / Observability` (`docs/SEED.md:2206-2217`): trace-first, minimal telemetry, tail sampling, and retention percentages.
- `Out of Scope / Observability` (`docs/SEED.md:2353-2361`): verbose/high-cardinality/audit/analytics exclusions.

## Acceptance Criteria

1. Collector configuration receives OTLP from the backend and tail-samples 100% of traces containing an error plus approximately 10% of fully successful traces; backend SDK sampling is always-on/deferred to Collector.
2. Representative successful and failed `/health`, `/snapshot`, and scheduled-sync traces have one root and the applicable required child operations with errors propagated to relevant parents.
3. One typed Go SDK catalogue defines the ten allowed metrics and one catch-all View drops unknown or wrong-kind instruments while filtering datapoint attributes; trace exemplars and both service resource identifiers remain available.
4. The Go logger filters only OTLP to meaningful lineage lifecycle/acquisition summaries and all Error+ records with allowed attributes; stderr stays unfiltered.
5. Collector/exporter unavailability leaves health, snapshot serving, and synchronization behavior unchanged apart from lost telemetry.
6. Operator documentation can locate an excessive-degradation trace from lineage ID and explains expected personal-grade telemetry gaps without promising audit/SLO capabilities.

## Validation

- Integration-test backend telemetry through a test Collector pipeline with synthetic successful/failed traces, asserting tail sampling, catalogue metrics, unknown/wrong-kind drops, resource identity, OTLP filtering, unfiltered stderr, and failure non-interference.
- Validate the Collector configuration and tail-sampling policy deterministically against a local capture Collector; do not contact Grafana Cloud or perform deployment/external-backend tests.
