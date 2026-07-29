# 4Visor

> 4Visor is a read-only anonymous Progressive Web App that presents 4chan
> through a modern, content-focused interface while preserving the ordering,
> content and philosophy of the original platform.

## Disclaimer

I don't own any content, trademarks, names, whatever. I just happen to make
viewer to present data available online in a way I like. I don't moderate,
change, own or otherwise have any association with whatever data I display and
cache. If you have complaints go complain to the actual owners. You cannot blame
a pair of binoculars for showing you something you don't wish to see.

## AI Disclosure

AI and related technologies bring strong opinions from many people. To ensure
transparency I will disclose that this repository is mainly developed using LLM
agents (specifically OpenAI Codex). I am one person and I don't have enough wake
time to follow my projects. I will tell that I personally setup the project
structure, [`README.md`](README.md). I personally designed the architecture,
constraints and deployment models. I have also thouroughly supervised the agent
output, ensuring it matches my expecatation. You can say I have indeed vibecoded
this project but you cannot say I have not designed and architected the project.
All automatic guardrails (linters, formatters, etc.) have been configured and
prepared personally by me.

## Getting started

> Use [Mise-en-Place](https://mise.jdx.dev) to setup all necessary components.

- `backend/` contains backend code. Written in Go
- `frontend/` containes frontend code. Written in TypeScript

The backend and frontend enforce the same fixture-backed
[snapshot version 1 contract](docs/snapshot-v1-contract.md).

### Progressive Web App shell

4Visor supports Chrome for Android 150 and newer. Other browsers are outside
the support contract. Installation requires the production application to be
served from a secure context; the deployment ingress provides TLS. After one
successful online load has finished installing the Service Worker, Chrome's
normal **Install app** or **Add to Home screen** action can install 4Visor, and
the application shell can be reopened without a network connection. The Vite
development server deliberately does not register the production worker.

The production build derives one application-shell cache revision from the
actual manifest and icon bytes plus the generated HTML and content-hashed Vite
assets. Installation precaches that exact set before the worker activates. A
new worker removes only obsolete caches named `four-visor-shell-*`; it leaves
unrelated origin caches and browser-managed HTTP caches untouched.

Cache Storage contains only `index.html`, the Web App Manifest, the two PWA
icons, and Vite-generated application assets. Snapshot responses and IndexedDB
records never enter Cache Storage. 4chan images, thumbnails, video, audio, and
other content media are never explicitly cached by 4Visor, although the browser
may retain them independently in its ordinary HTTP cache. Offline support
therefore promises the application shell and the last complete local snapshot,
not media.

### Local snapshot storage and reset

IndexedDB is mandatory and is the exclusive local home for snapshot payloads.
At startup, 4Visor opens `four-visor-snapshots`, audits its local lineage
ownership, and loads the active snapshot before any backend request is needed.
A valid active snapshot becomes available immediately; a fresh installation
shows an explicit empty state. Unavailable or corrupt IndexedDB produces a
blocking storage error with no memory-only or online-only fallback.

The database stores at most one active and one inactive incoming lineage. Each
uses a distinct installation-local key even when both payloads contain the same
upstream `lineageId`. UTF-8 snapshot documents are stored as ordered 65,536-byte
records with completion length, count, and SHA-256 metadata. The final record is
the exact remaining length. Upstream lineage identifiers remain inside payloads
and never identify local records. Cache Storage remains restricted to the
application shell; it never contains these payload records.

**Reset local data** asks for destructive confirmation, then deletes the whole
4Visor IndexedDB database, including active and incoming data and the stable
installation-local jitter seed. It next deletes every owned
`four-visor-shell-*` Cache Storage entry after the app's registration attempt
settles, unregisters only the 4Visor root Service Worker registration, then
deletes and verifies owned caches again before reloading. A successful online
reload registers a new worker and
atomically precaches a fresh application shell, preserving later offline shell
reopens. Cancellation changes nothing.
The reset is local to this browser installation and performs no server reset.
It loses cached snapshot continuity and stable local jitter, but leaves unrelated
origin caches and the browser-managed HTTP cache untouched.

### Client snapshot synchronization

When a refresh is due, the client makes one `GET /api/snapshot` request and
downloads the complete response before replacing local data. It stores the
response under a fresh local generation key, rereads and validates the stored
schema-version-1 document, then atomically promotes it while deleting the
previous generation. The previous complete snapshot remains readable throughout
this work, including when the backend serves the same lineage ID or an older
observation time.

Network failure, an unavailable server, `410 Gone`, invalid JSON, an incompatible
schema version, insufficient quota, local storage failure, and cancellation
before activation commits all leave the previous active snapshot unchanged.
Once activation commits, the replacement wins and later cancellation does not
roll it back. Failed incoming data remains inactive and is replaced by a later
scheduled attempt or removed by **Reset local data**. No immediate retry or
individual board, catalog, thread, or post request is made. A fresh installation
remains empty, with a clear error, until its first complete synchronization
succeeds.

An incompatible-version error means the deployed frontend and backend must be
made compatible; version 1 has no migration or fallback parser. A quota error
requires freeing site storage or resetting local data, which also loses offline
snapshot continuity and the installation-local jitter seed. Snapshot responses
and records remain outside Service Worker Cache Storage.

### Client refresh schedule

On first activation, the client generates a private one-byte seed with the
browser cryptographic random-number generator and stores it under `jitter-seed`
in the existing IndexedDB settings store. Rejection sampling gives each
integer-second offset from 5 through 60 seconds equal probability. Reloads reuse
the stored seed; **Reset local data** deletes it with the rest of the database,
so the next activation generates a new offset. The seed is never added to a
snapshot request, log, or telemetry signal.

Let `A` be the monotonic time when successful startup has rendered its initial
local state, `J` the derived offset, and `I` one hour. The first due time is
`D0 = A + J`; later due times are `Dn = D0 + nI`. Jitter establishes the phase
once and is not added to each interval. After an attempt succeeds or fails, or
another tab holds the refresh lock, the client selects the first `Dn` strictly
after completion. Delayed and overlapping ticks are discarded rather than
replayed, queued, or retried immediately.

Each due cadence uses an origin-wide exclusive Web Lock in non-waiting
`ifAvailable` mode. One tab therefore performs the complete synchronization
while another skips that cadence. The existing local lineage remains readable
through timer waits and synchronization. Confirmed reset cancels the timer or
active request before deleting IndexedDB; each reload otherwise starts a new
monotonic schedule with the same persisted offset rather than persisting timer
history.

### Backend HTTP service

The backend serves internal `GET /health` and `GET /snapshot`; the edge maps
browser requests from `/api/health` and `/api/snapshot` to those routes.

`GET /health` returns `200 OK` when
Memcached answers a protocol query and `a.4cdn.org` resolves, otherwise it
returns `503 Service Unavailable` within the configured health timeout. Response
bodies are deliberately non-contractual and never contain dependency details.
Unsupported methods return `405`, undeclared routes return `404`, and there is
no readiness endpoint.

`GET /snapshot` pins the active Memcached lineage, reads its strict completion
metadata and every ordered block, then validates the reassembled schema-version-1
document before committing `200 OK`. The handler returns the stored serialized
bytes unchanged with `Content-Type: application/json`, an exact `Content-Length`,
and `Cache-Control: no-store`. A missing active pointer, completion entry, or
referenced block returns `410 Gone`; unavailable Memcached operations return
`503`, while present but invalid cache data returns `500`. Every matched snapshot
response is non-cacheable and generic failures disclose no cache keys, payloads,
addresses, or raw dependency errors.

The Go service does not implement Brotli, ranges, manifests, public block
transfer, or per-resource routes. VPS ingress alone owns normal Brotli HTTP
content encoding. Request cancellation stops further cache reads and validation;
one in-flight gomemcache operation remains bounded by its 500 ms socket timeout.

| Environment variable | Default | Purpose |
| --- | --- | --- |
| `FOURVISOR_SERVER_ADDRESS` | `:65102` | Backend HTTP listener. |
| `FOURVISOR_HEALTH_TIMEOUT` | `2s` | Total Memcached and DNS health deadline. |
| `FOURVISOR_MEMCACHED_ADDRESS` | required | Memcached host and project port. |
| `FOURVISOR_DNS_NAME` | `a.4cdn.org` | 4chan hostname resolved by health checks. |
| `FOURVISOR_OTLP_ENDPOINT` | `http://otelcol:65103` | OTLP/gRPC Collector URL; HTTPS and arbitrary remote ports are supported. |
| `FOURVISOR_ACQUISITION_RATE_INTERVAL` | `1s` | Minimum interval between all outbound request attempts; values below the official limit are rejected. |
| `FOURVISOR_ACQUISITION_MAX_CONCURRENCY` | `10` | Process-wide outbound concurrency, from 1 through 10. |
| `FOURVISOR_ACQUISITION_REQUEST_TIMEOUT` | `5s` | Timeout for one outbound request attempt and its response body. |
| `FOURVISOR_ACQUISITION_MAX_RETRIES` | `2` | Retries after the initial attempt, from 0 through 2. |
| `FOURVISOR_ACQUISITION_RETRY_BACKOFF` | `1s` | Base retry delay; retry one waits once this value and retry two waits twice it. |
| `FOURVISOR_SYNCHRONIZATION_INTERVAL` | `1h` | Fixed backend lineage cadence in whole seconds (minimum `1s`) and the basis for the twice-interval Memcached TTL. |
| `FOURVISOR_SYNCHRONIZATION_FAILED_RESOURCE_TOLERANCE` | `10` | Observability threshold; exceeding it never prevents activation. |
| `FOURVISOR_COMMIT_HASH` | required | Full lowercase 40-character deployed Git commit used in `User-Agent: 4Visor/<hash>`. |

Empty, malformed, or out-of-range project-local settings stop startup with a
diagnostic that names the setting but not its value. The backend emits JSON
error logs and exports logs, metrics, and traces through OTLP. Collector or
exporter unavailability can lose telemetry but never changes health processing.

### Upstream acquisition

The backend acquisition client observes `boards.json` and every returned board's
`catalog.json` through one shared rate and concurrency budget. It builds each
observation from scratch, preserves board and catalog array order, keeps every
catalog page and its metadata except `threads`, and retains only the first 250
thread summaries across those page boundaries. It does not retain conditional
request state or reuse a previous observation. This is a deliberate exception
to the upstream API's `If-Modified-Since` recommendation: a genuine `304`
revalidation needs a prior representation, which this fresh lineage forbids.
A fixed or token-valued header would not validate freshness or reduce upstream
work, so no fake conditional header is sent. The story's fresh/no-reuse
semantics take precedence. Scheduling and publication are added by later
stories.

Each retained catalog summary is expanded through the same shared client into
its current thread observation. Thread jobs use a fixed number of workers no
greater than the configured outbound concurrency, while every attempt remains
subject to the existing process-wide rate, concurrency, timeout, and retry
policy. Catalog, page, summary, and post order never depends on completion
order. Known terminal failures and lineage-deadline-expired jobs remain visible
as failed thread resources; external or shutdown cancellation aborts the whole
observation.

The official 4chan API exposes one complete-thread JSON endpoint and provides
no post limit or pagination. Consequently, 4Visor makes the ordinary single
thread request (plus policy retries when applicable), inspects its returned post
count, retains at most the first 250 opaque post objects, and marks 251 or more
as `oversize`. It never requests a remainder or any media binary. Original post
HTML and attachment references stay unchanged inside the retained opaque JSON;
discarded posts are neither retained nor exposed.

Network failures, request timeouts, and HTTP `429` responses may be retried.
Other HTTP failures and invalid upstream JSON are not retried. Retry delay is
the longer of the numbered configured backoff and a valid `Retry-After` delta or
HTTP date. Exhausted technical failures become exact failed resource wrappers.
A lineage deadline also degrades unfinished known resources to failed; external
or shutdown cancellation instead aborts the observation and returns no usable
partial result. Successful requests and retries are not logged. Each terminal
resource degradation emits one value-free error log, while outbound spans and
metrics omit raw URLs, response bodies, and board identifiers.

### Memcached lineage publication

A completed snapshot is validated and serialized before Memcached is mutated.
The backend stores its ordered 512 KiB blocks under immutable lineage-scoped
entries, verifies every block, then stores and verifies completion metadata.
Only after those checks does one atomic active-lineage pointer replacement make
the lineage visible. The backend is the single pointer-writer authority; no
distributed lock or compare-and-swap protocol is involved. All lineage entries
share a cleanup deadline calculated from twice the synchronization interval.
After activation, the prior completion entry and blocks are deleted immediately;
deletion failure leaves the new lineage active and its TTL remains the cleanup
fallback.

### Scheduled lineage synchronization

The backend derives one random integer-second startup offset from 5 through 60
seconds and keeps it for the process lifetime. The first synchronization starts
after that offset, and the configured fixed cadence is anchored there. A tick
delivered while construction is active is logged and discarded once: it is not
queued, replayed, used to cancel work, or followed by a catch-up attempt. The
next ordinary tick is the next opportunity. Shutdown cancels and waits for the
active synchronization.

Each attempt creates a new ULID and one UTC RFC 3339 `observedAt` from its start
instant. Acquisition runs from scratch under a fixed 30-minute child deadline.
Known board, catalog, and thread work unfinished at that cutoff becomes exact
failed-resource wrappers and may still publish under the live application
context. External or shutdown cancellation instead aborts publication and
preserves the previous active pointer.

Every validated, published lineage activates even when degraded. A positive
failed-resource count is reported as degraded; exceeding the configured
tolerance only raises the synchronization root span and completion log to error
severity. Publication receives the configured interval unchanged, and every
lineage key receives one shared conceptual deadline exactly twice that interval
after publication starts. Memcached encodes and evaluates that deadline at its
documented one-second resolution. Post-activation cleanup failure never rolls
activation back.

The active-lineage age gauge is intentionally instance-local and is absent until
the process activates a lineage. It may remain stale during undetected
Memcached loss. Cache loss still makes `GET /snapshot` return `410 Gone`; there
is no immediate repair, and the next ordinary scheduled attempt reconstructs
serving state from upstream.

Memcached is disposable serving state. Restart, loss, expiry after verification,
or a reader racing immediate eviction of a previously observed pointer can make
a lineage incomplete; the snapshot route added by US-006 reports that state as
`410 Gone`. There is no file, database, recovery copy, or request-triggered
rebuild. The next scheduled synchronization is the only server-side recovery.

For a loopback-only local run with Memcached already listening on a project
port:

```sh
cd backend
FOURVISOR_SERVER_ADDRESS=127.0.0.1:65102 \
FOURVISOR_MEMCACHED_ADDRESS=127.0.0.1:65100 \
FOURVISOR_COMMIT_HASH="$(git rev-parse HEAD)" \
go run ./cmd/app
```

## First-party images

The backend and frontend images support Linux amd64 only. Build them through a
rootless Docker daemon without `sudo`; both builds reject any other target:

```sh
docker build --platform=linux/amd64 -t four-visor-backend:local backend
docker build --platform=linux/amd64 -t four-visor-frontend:local frontend
```

Both final images use the numeric user `65532:65532`, have no shell or build
toolchain, and require a read-only root filesystem. They require no volume,
writable mount or tmpfs. The backend exposes its Go server directly on port
`65102`; the frontend Caddy serves only the built PWA shell and static assets on
port `65101`. Frontend Caddy does not proxy `/api`, compress responses, manage
TLS, expose an admin API or persist runtime configuration.

The backend requires `FOURVISOR_MEMCACHED_ADDRESS` and
`FOURVISOR_COMMIT_HASH`; the environment table above documents its remaining
optional settings. A network-reachable Memcached is required for health,
snapshot serving and synchronization, although the process does not connect to
it during startup. OpenTelemetry exports are asynchronous: Collector
unavailability may lose telemetry but does not stop or change backend
processing. DNS and 4chan availability affect health checks and scheduled
acquisition respectively, not process startup. For example, with the required
services already present on an operator-managed internal network:

```sh
docker run --rm --read-only \
  --network four-visor-internal \
  --publish 127.0.0.1:65102:65102 \
  --env FOURVISOR_MEMCACHED_ADDRESS=memcached:65100 \
  --env FOURVISOR_OTLP_ENDPOINT=http://otelcol:65103 \
  --env FOURVISOR_COMMIT_HASH="$(git rev-parse HEAD)" \
  four-visor-backend:local

docker run --rm --read-only \
  --publish 127.0.0.1:65101:65101 \
  four-visor-frontend:local
```

These loopback publications are local operator examples, not the production
topology. US-017 owns the private service network, edge routing and ingress;
ingress alone owns TLS and Brotli compression. Memcached, the OpenTelemetry
Collector, edge Caddy and other third-party images remain their upstream images
and are not rebuilt to impose first-party controls.

Run `mise run images:validate` to execute the backend and frontend gates, build
both images, inspect their metadata and contents, validate the Caddyfile, and
start each process briefly with `--network none --read-only`. This is artifact
integration validation: it publishes no port and makes no HTTP or health probe.
A cold run needs access to the configured container registries, Go module proxy
and npm registry and may take 15–30 minutes; cached runs usually take several
minutes. It installs no host or system packages.

## Compose deployment

The production Compose project contains exactly five Linux amd64 services:
edge Caddy, frontend Caddy, backend, Memcached and OpenTelemetry Collector.
Only edge Caddy is published, as plain HTTP on `127.0.0.1:65199`. The VPS
ingress must proxy to `http://127.0.0.1:65199` and remains the sole owner of TLS
termination and Brotli response compression. Do not add a public, IPv6 or
all-interface Docker publication and do not change the host firewall for this
deployment.

Edge, frontend, backend and Collector share the ordinary `app` network. Only
backend and Memcached share the separate `cache` network, which is marked
internal. Memcached listens on `65100`, frontend on `65101`, backend on `65102`,
Collector OTLP/gRPC on `65103`, and edge on `65199`. Frontend, backend,
Memcached and Collector have no host publication. Edge strips `/api` before
proxying `/api/*` to the backend, so `/api/snapshot` becomes `/snapshot` and
`/api/health` becomes `/health`; every other path goes to frontend Caddy.

Copy the environment template and fill both required values. The commit must be
the full lowercase hash of the checked-out deployment. The Collector endpoint
is required by its existing native configuration; do not put credentials in the
tracked example.

```sh
cp .env.example .env
git rev-parse HEAD
```

Optional `FOURVISOR_` entries in `.env.example` stay commented unless the
operator intends to override a backend default. When absent, Compose omits them
from the backend container environment and the Go defaults in the table above
remain authoritative. The Compose deployment does not pass through
`FOURVISOR_SERVER_ADDRESS`; its backend listener is fixed to `:65102` to match
edge routing and the healthcheck. Memcached, Caddy and Collector use their native
command, file and environment configuration; the Collector receives
`OTEL_EXPORTER_OTLP_ENDPOINT`, never a project-prefixed replacement.

Validate configuration, pull only the three upstream images, build the two
first-party images locally, then start the project without another build:

```sh
mise run compose:validate
docker compose pull edge memcached otelcol
docker compose build --pull backend frontend
docker compose up -d --no-build
```

Stop and resume the existing deployment with:

```sh
docker compose stop
docker compose up -d --no-build
```

For an upgrade, update the checkout, replace `FOURVISOR_COMMIT_HASH` in `.env`
with the new `git rev-parse HEAD` value, validate, and recreate from locally
built first-party images:

```sh
git pull --ff-only
mise run compose:validate
docker compose pull edge memcached otelcol
docker compose build --pull backend frontend
docker compose up -d --no-build --remove-orphans
```

Upstream service references use reviewed version tags plus pinned Linux amd64
manifest digests. Update each tag and digest together. Backend and frontend are
tagged with the deployed commit and are not pulled from a project registry.

This personal deployment accepts a temporary outage of any single service.
Edge loss makes the application unreachable; frontend loss prevents uncached
shell loads; backend loss prevents synchronization; Collector loss drops
telemetry without changing application operations. Memcached is disposable:
after loss, snapshot requests return `410 Gone` until the next ordinary
scheduled backend synchronization reconstructs and activates a lineage. Do not
add a persistent cache, manual or client-triggered acquisition, replicas,
failover, wait-for orchestration or firewall rules as a recovery mechanism.

## Verification

### Backend

> Run these Mise-en-Place tasks to verify backend

- `be:build`
- `be:lint`
- `be:test`

### Frontend

> Run these Mise-en-Place tasks to verify frontend

- `fe:lint`
- `fe:check`
- `fe:build`
- `fe:test`

### First-party images

- `images:validate`

### Compose deployment

- `compose:validate`
