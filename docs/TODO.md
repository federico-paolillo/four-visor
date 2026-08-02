# Implementation Plan

## Planning assumptions

- `docs/SEED.md` is authoritative. Read-only, anonymous, frozen-snapshot behavior and every explicit exclusion remain global scope guardrails.
- Version 1 initially uses one logical JSON response. Public fixed batches remain a measured future option, but activation always covers one complete lineage.
- Browser lineage records use distinct local generation keys and fixed-size serialized records; upstream `lineageId` is validated metadata, not local storage identity.
- Backend acquisition defaults to the official 4chan limit of one request per second, at most two transient retries with one- then two-second backoff, and `Retry-After` when longer.
- Excessive degradation defaults to more than 10 failed resources. This changes telemetry severity only and never blocks an otherwise valid lineage.
- The backend synchronization interval defaults to one hour and is configurable through `FOURVISOR_`; cache TTL is always twice the configured interval.
- Every project-controlled local or Compose service listener, proxy target, container, health-check, and published port uses 65100–65199. Remote upstream, ingress, and operator exporter destinations are outside this range rule. Test-only containers may use dependency-native container ports and Docker-assigned ephemeral host ports, but every published test port binds explicitly to `127.0.0.1`. Only edge Caddy publishes a deployment host port, explicitly on `127.0.0.1`.
- Backend stories run `mise run be:build`, `mise run be:test`, and `mise run be:lint`. Frontend stories run `mise run fe:build`, `mise run fe:test`, `mise run fe:lint`, and `mise run fe:check`; US-002 aligns the current task name before its frontend work. Stories spanning both areas run both sets.
- Automated validation is limited to unit and integration tests. Smoke, end-to-end, and deployment tests remain excluded.
- Basic semantic and keyboard accessibility is an implementation-quality requirement, not additional product scope.

## User Stories

- [x] US-001 Run a configured and diagnosable backend health boundary
- [x] US-002 Enforce the exact snapshot version 1 contract at both boundaries
- [x] US-003 Observe every board and its first 250 catalog threads
- [x] US-004 Complete lineages with bounded thread acquisition
- [x] US-005 Publish one immutable lineage atomically through Memcached
- [x] US-006 Serve the active lineage as one logical snapshot
- [x] US-007 Build and activate lineages on the backend schedule
- [x] US-008 Install and reopen the application shell offline
- [x] US-009 Start from, fail on, or reset local snapshot storage
- [x] US-010 Replace the local lineage only after complete synchronization
- [x] US-011 Refresh with stable installation-local jitter
- [x] US-012 Browse ordered boards and compact catalogs from the local lineage
- [x] US-013 Render upstream post markup safely with canonical links
- [x] US-014 Read nested, collapsible threads with honest degradation
- [x] US-015 Load media directly and only with the required user intent
- [x] US-016 Package the backend and PWA as hardened first-party images
- [x] US-017 Deploy one loopback edge with private internal services
- [x] US-018 Retain failed traces and a sample of successful operation
- [x] US-019 Export a complete snapshot from the CLI
- [x] US-020 Make production acquisition failures diagnosable
