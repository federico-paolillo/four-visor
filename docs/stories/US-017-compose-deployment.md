# US-017: Deploy one loopback edge with private internal services

## Goal

Compose the edge, frontend, backend, Memcached, and Collector so the VPS ingress reaches exactly one loopback-bound service and browser API paths map to the correct internal routes.

## User Value

The Operator can deploy and upgrade the personal instance with a small topology that does not expose backend dependencies to the Internet.

## Scope

- Define Docker Compose services for dedicated edge Caddy, frontend Caddy, Go backend, one Memcached, and one OpenTelemetry Collector.
- Assign every edge, frontend, backend, Memcached, and Collector listener/target/container port from 65100–65199. Bind only edge Caddy to an explicit `127.0.0.1` host port; give no host port to frontend, backend, Memcached, or Collector.
- Route `/api/*` first, strip `/api`, and proxy to backend so `/api/snapshot` and `/api/health` become `/snapshot` and `/health`; route all other requests to frontend Caddy.
- Keep TLS termination and Brotli response compression at the existing VPS ingress only; both Caddy services forward uncompressed internal HTTP.
- Limit Memcached reachability to the backend-facing internal network and use native Caddy/Memcached/Collector configuration rather than `FOURVISOR_` variables for third-party services.
- Configure Compose health checks around backend responsiveness, Memcached availability, and 4chan DNS through the shallow backend health contract, without inventing readiness orchestration.
- Wire the Collector service, configuration mount, compliant local ports, and backend OTLP connection; US-018 alone owns receiver/pipeline/filtering/sampling/export semantics.
- Document loopback ingress wiring, configuration, start/stop/upgrade, accepted single-service outages, and recovery (including waiting for scheduled rebuild after Memcached loss).

## Out of Scope

- Public `0.0.0.0`/IPv6 host binds, Docker firewall changes, TLS certificates inside Compose, Brotli in either Caddy/backend, Kubernetes, replicas, autoscaling, service mesh, complex readiness/liveness, or automated deployment tests.
- Collector pipeline, filtering, tail-sampling, exporter behavior, or cross-capability telemetry validation, which belong to US-018.

## Dependencies

- US-016.

## Related MADRs

- None. The five-service topology, edge routing, and exposure constraints are locked; network names and the concrete compliant loopback port are local deployment details.

## Traceability

- `Full Requirements / Deployment` (`docs/SEED.md:191-210`): Compose, loopback-only edge, prefix stripping, separate services, native configuration, health, TLS, and hardening boundaries.
- `High-Level Architecture` and `/ HTTP routing` (`docs/SEED.md:230-268`, `375-394`): traffic topology and exact browser/backend route mapping.
- `Deployment View / Deployment philosophy`, `/ Container model`, `/ Health model`, `/ Traffic`, `/ Failure model`, and `/ Security` (`docs/SEED.md:1175-1291`): one exposed edge, internal services, health, direct media, accepted outages, and private Memcached.
- `Technology Stack / Networking` (`docs/SEED.md:1808-1817`): HTTPS ingress, loopback Caddy, prefix stripping, internal network, and upstream HTTP.
- `Locked Decisions / Backend` and `/ Out of scope` (`docs/SEED.md:2169-2191`, `2229-2245`): deployment/routing/hardening and distributed/platform exclusions.
- `AGENTS.md / Guidelines for Workers`: service ports must be 65100–65199 and Docker exposure must bind directly to `127.0.0.1`.

## Acceptance Criteria

1. Rendered Compose configuration has exactly one host-published port, explicitly bound to `127.0.0.1` in 65100–65199, belonging to edge Caddy; every internal listener, target, and health-check port is also in 65100–65199, and no other service has host exposure.
2. Edge Caddy precedence strips `/api` and maps the two browser routes to backend routes while every non-API request goes to frontend Caddy.
3. Backend alone can reach Memcached on an internal network; browser/host cannot address Memcached, backend, frontend, or Collector directly through published ports.
4. No Compose service terminates TLS or applies Brotli; documentation identifies VPS ingress as owner of both and uses loopback HTTP to edge.
5. Health configuration uses the existing shallow `/health` contract and adds no readiness endpoint or complex orchestration.
6. The documented upgrade/recovery path accepts temporary outages and never proposes replicas, persistent cache recovery, client-triggered acquisition, or firewall changes.

## Validation

- Render the Compose configuration, build the first-party images, and start the normal loopback-only stack for operator inspection.
- Do not add a deployment or smoke test.
