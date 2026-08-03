# Snapshot acquisition statistics

These native Linux/AMD64 runs measure complete snapshot acquisition through the
production CLI and its configured global limiter. Each row is one raw attempt;
the snapshots and logs remain untracked outside the repository.

| Run (UTC / lineage) | Host / emulation | Commit / image | Policy (`deadline`; `rate`, `concurrency`, `request timeout`, `retries`, `backoff`) | Elapsed (s) | Exit / outcome | Snapshot (bytes) | Boards (`state` / total) | Catalogs (present / failed) | Threads (total / present / oversize / failed) | Failures (deadline / non-deadline) |
| --- | --- | --- | --- | ---: | --- | ---: | --- | --- | --- | --- |
| 2026-08-03T12:44:10Z / `01KZ3TEE86JAQT969AXG2JM76Z` | `x86_64` / none | `18e5260` / `sha256:911ac1a04c26` | `1h`; `1s`, `10`, `5s`, `2`, `1s` | 3646.22 | 1 / acquisition completed; output bind permission failure | n/a | `present` / n/a | n/a / 1 | 11423 / 3194 / 271 / 7958 | 7951 / 8 |
| 2026-08-03T13:49:18.732312028Z / `01KZ3Y5MGC8TCG3CQ7ZGH3KHZJ` | `x86_64` / none | `18e5260` / `sha256:911ac1a04c26` | `1h`; `1s`, `10`, `5s`, `2`, `1s` | 3641.31 | 0 / deadline-degraded snapshot written | 133332099 | `present` / 77 | 76 / 1 | 11424 / 3188 / 277 / 7959 | 7952 / 8 |
| 2026-08-03T14:51:18.234370098Z / `01KZ41Q4TTHCRKFW0R3GVG9GMA` | `x86_64` / none | `18e5260` / `sha256:911ac1a04c26` | `4h`; `1s`, `10`, `5s`, `2`, `1s` | 11782.12 | 0 / completed without deadline failures | 416249620 | `present` / 77 | 76 / 1 | 11434 / 10091 / 1296 / 47 | 0 / 48 |
| 2026-08-03T18:09:19.768389706Z / `01KZ4D1QWRGN424SE8RWZTYHQW` | `x86_64` / none | `18e5260` / `sha256:911ac1a04c26` | `4h`; `1s`, `10`, `5s`, `2`, `1s` | 11803.24 | 0 / completed without deadline failures | 419502271 | `present` / 77 | 76 / 1 | 11447 / 10098 / 1315 / 34 | 0 / 35 |

## Definitions and limitations

- Durations are wall-clock seconds measured around `docker run`; bytes are the
  serialized JSON file size. `present`, `oversize`, and `failed` are snapshot
  contract states.
- Deadline failures are terminal summaries with `error.type=lineage_deadline`.
  Non-deadline failures are the other terminal resource counts; every one in
  these runs was an HTTP 404 caused by live upstream churn.
- The image was built from the stated commit plus the uncommitted deadline
  implementation. Docker ran it natively as `linux/amd64`; these measurements
  do not support ARM or emulation performance claims.
- Board/thread populations and upstream responses change during and between
  runs. `/usr/bin/time` measured the Docker client and container memory was not
  sampled consistently, so memory use is omitted.
- The first attempt remains recorded because it contacted the upstream and ran
  to its deadline. Rootless UID remapping prevented its host bind output, so its
  serialized size and board/catalog totals are unavailable. Its thread total,
  oversize logs, and failure summaries allow the other state counts to be
  reconstructed. Later attempts used a task-scoped Docker volume and exported
  completed files afterward.

Two consecutive `4h` runs completed in 3h16m22s and 3h16m43s without deadline
failures, leaving 43m38s and 43m17s of headroom (about 22% of elapsed time).
This supports a static `4h` acquisition deadline. The synchronization interval
also defaults to `4h` so ordinary defaults do not overlap and Memcached expiry
remains twice the cadence. Dynamic deadline adjustment is not warranted.
