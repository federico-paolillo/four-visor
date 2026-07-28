# US-001: Run a configured and diagnosable backend health boundary

## Goal

Provide the Operator with a small Go `net/http` service whose configuration, dependency health, and telemetry-export behavior are explicit before snapshot work is added.

## User Value

The Operator can start the backend, detect whether its two required runtime dependencies are usable, and diagnose failures without exposing infrastructure details or secrets.

## Scope

- Establish a reusable `FOURVISOR_` environment parsing boundary and load only the server, health, Memcached, DNS, and OTLP settings needed by this story. Later stories add their own settings and defaults through the same boundary.
- Serve only `GET /health` at this stage, returning `200` when the process can respond, Memcached is reachable, and 4chan DNS resolves; otherwise return `503`.
- Keep the response body non-contractual and free of dependency details and secrets.
- Establish OpenTelemetry SDK/OTLP export for Go telemetry, inbound HTTP root spans, low-cardinality HTTP metrics, and structured error logging. Telemetry export failure must not fail health or request processing.
- Document environment variables, defaults, startup failures, health semantics, and the fact that no readiness endpoint exists.

## Out of Scope

- Snapshot routes, upstream HTTP acquisition, scheduling, lineage publication, Compose health checks, dashboards, alerting, or additional public backend endpoints.

## Dependencies

- None.

## Related MADRs

- None. The configuration source, OpenTelemetry topology, and health contract are locked; variable names, parsing, OTLP transport details, and resource naming are local implementation choices.

## Traceability

- `Full Requirements / Deployment` (`docs/SEED.md:191-210`): Go configuration uses only `FOURVISOR_`; health verifies backend responsiveness, Memcached, and 4chan DNS.
- `High-Level Architecture / HTTP routing` (`docs/SEED.md:375-394`): internal `GET /health`, `200`/`503`, non-contractual secret-free body, and no readiness or extra route.
- `Operational Flows / Backend component failure` and `/ Health check` (`docs/SEED.md:1027-1080`): required dependency failure fails the operation; health is intentionally shallow.
- `Deployment View / Failure model`, `/ Security`, and `/ Observability` (`docs/SEED.md:1266-1302`): telemetry is optional to application processing and diagnostics must respect the private network/security model.
- `Detailed Observability / OpenTelemetry`, `/ Tracing`, `/ Metrics`, `/ Logging`, and `/ Error handling` (`docs/SEED.md:1468-1604`): OTLP, HTTP roots, minimal metrics, meaningful logs, and failed spans.
- `Technology Stack / Backend` and `/ Configuration` (`docs/SEED.md:1771-1778`, `1852-1860`): Go, `net/http`, Memcached, OpenTelemetry, OTLP, and the environment prefix.

## Acceptance Criteria

1. With reachable Memcached and successful 4chan DNS resolution, `GET /health` returns `200`; either dependency failure produces `503` within a bounded request duration.
2. The response does not name dependency hosts, configuration values, cache keys, raw errors, or credentials.
3. Unsupported methods and undeclared routes are not treated as successful health requests; no readiness route exists.
4. Invalid or missing required settings fail startup with a cause-preserving, secret-free diagnostic; this story's defaults are documented and tested without speculative settings for later capabilities.
5. Every inbound request, including rejected methods/routes, creates an HTTP root span and updates low-cardinality request count/latency signals; errors mark the span and emit one meaningful structured error log, without routine begin/end logs.
6. An unavailable OTLP destination does not change the health result when required health dependencies remain available.

## Validation

- Unit-test configuration defaults, validation, and diagnostic redaction.
- Integration-test the handler with controllable Memcached reachability and DNS resolver seams, including cancellation and timeout behavior.
- Integration-test span status and metric attributes with an in-memory OpenTelemetry exporter, plus non-fatal exporter failure.
