# Open Questions

No open questions remain.

The seed's unspecified implementation policies were resolved for the personal-project scope:

- Global upstream rate: one request per second, following the [official 4chan API rule](https://github.com/4chan/4chan-API#api-rules).
- Transient retry default: two retries after the initial attempt, with one- then two-second backoff and longer `Retry-After` values honored.
- Excessive degradation default: more than 10 failed resources; the threshold affects telemetry only.
- Browser lineage storage: locally keyed fixed-size serialized records, preventing same-ID staging from aliasing the active lineage.
- Backend overlap: skip a tick while synchronization is active; do not queue, overlap, or cancel the current run.
