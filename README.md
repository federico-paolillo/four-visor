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

### Backend health service

The backend currently serves only `GET /health`. It returns `200 OK` when
Memcached answers a protocol query and `a.4cdn.org` resolves, otherwise it
returns `503 Service Unavailable` within the configured health timeout. Response
bodies are deliberately non-contractual and never contain dependency details.
Unsupported methods return `405`, undeclared routes return `404`, and there is
no readiness endpoint.

| Environment variable | Default | Purpose |
| --- | --- | --- |
| `FOURVISOR_SERVER_ADDRESS` | `:65102` | Backend HTTP listener. |
| `FOURVISOR_HEALTH_TIMEOUT` | `2s` | Total Memcached and DNS health deadline. |
| `FOURVISOR_MEMCACHED_ADDRESS` | required | Memcached host and project port. |
| `FOURVISOR_DNS_NAME` | `a.4cdn.org` | 4chan hostname resolved by health checks. |
| `FOURVISOR_OTLP_ENDPOINT` | `http://otelcol:65103` | OTLP/gRPC Collector URL; HTTPS and arbitrary remote ports are supported. |

Empty, malformed, or out-of-range project-local settings stop startup with a
diagnostic that names the setting but not its value. The backend emits JSON
error logs and exports logs, metrics, and traces through OTLP. Collector or
exporter unavailability can lose telemetry but never changes health processing.

For a loopback-only local run with Memcached already listening on a project
port:

```sh
cd backend
FOURVISOR_SERVER_ADDRESS=127.0.0.1:65102 \
FOURVISOR_MEMCACHED_ADDRESS=127.0.0.1:65100 \
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
- `fe:typecheck`
- `fe:build`
- `fe:test`
