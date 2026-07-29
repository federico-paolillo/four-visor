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
records never enter Cache Storage. Snapshot persistence is introduced by later
stories through IndexedDB. 4chan images, thumbnails, video, audio, and other
content media are never explicitly cached by 4Visor, although the browser may
retain them independently in its ordinary HTTP cache. Offline support therefore
promises the application shell only, not snapshot content or media.

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
