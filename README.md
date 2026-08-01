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

## Operator guide

The Compose deployment supports Linux amd64 and a rootless Docker daemon. Run
Docker and Docker Compose as the deployment user without `sudo`.

### Backend environment

| Environment variable | Default | Purpose |
| --- | --- | --- |
| `FOURVISOR_SERVER_ADDRESS` | `:65102` | Backend HTTP listener. |
| `FOURVISOR_HEALTH_TIMEOUT` | `2s` | Total Memcached and DNS health deadline. |
| `FOURVISOR_MEMCACHED_ADDRESS` | required | Memcached host and project port. |
| `FOURVISOR_DNS_NAME` | `a.4cdn.org` | Hostname resolved by health checks. |
| `FOURVISOR_OTLP_ENDPOINT` | `http://otelcol:65103` | OTLP/gRPC Collector URL. |
| `FOURVISOR_ACQUISITION_RATE_INTERVAL` | `1s` | Minimum interval between outbound attempts; must be at least `1s`. |
| `FOURVISOR_ACQUISITION_MAX_CONCURRENCY` | `10` | Process-wide outbound concurrency, from 1 through 10. |
| `FOURVISOR_ACQUISITION_REQUEST_TIMEOUT` | `5s` | Timeout for one outbound request attempt and response body. |
| `FOURVISOR_ACQUISITION_MAX_RETRIES` | `2` | Retries after the initial attempt, from 0 through 2. |
| `FOURVISOR_ACQUISITION_RETRY_BACKOFF` | `1s` | Base retry delay. |
| `FOURVISOR_SYNCHRONIZATION_INTERVAL` | `1h` | Synchronization cadence in whole seconds, minimum `1s`. |
| `FOURVISOR_SYNCHRONIZATION_FAILED_RESOURCE_TOLERANCE` | `10` | Failed-resource observability threshold. |
| `FOURVISOR_COMMIT_HASH` | required | Full lowercase 40-character deployed Git commit. |

The backend image supplies every listed optional default. Required values have
empty image placeholders, and empty optional durations or integers are invalid.
Compose supplies the Memcached address and commit hash.

### Compose setup

Copy the environment template, determine the checked-out commit, and fill all
four required values in `.env`. Do not put credentials in the tracked example.

```sh
cp .env.example .env
git rev-parse HEAD
```

| Required variable | Value |
| --- | --- |
| `FOURVISOR_COMMIT_HASH` | Full lowercase hash printed by `git rev-parse HEAD`. |
| `GRAFANA_CLOUD_OTLP_ENDPOINT` | Grafana Cloud OTLP/HTTP base URL. |
| `GRAFANA_CLOUD_INSTANCE_ID` | Grafana Cloud Basic Auth instance ID. |
| `GRAFANA_CLOUD_API_KEY` | Grafana Cloud Basic Auth API key. |

Optional backend overrides in `.env.example` remain commented unless needed.

### Start and stop

Render the configuration, pull the upstream images, build the first-party
images, and start the deployment:

```sh
docker compose config
docker compose pull edge memcached otelcol
docker compose build --pull backend frontend
docker compose up -d --no-build
```

Stop and resume the existing deployment with:

```sh
docker compose stop
docker compose up -d --no-build
```

### Upgrade

Fast-forward the checkout, replace the commit hash in `.env` with the new
`git rev-parse HEAD` value, then rebuild and recreate the deployment:

```sh
git pull --ff-only
git rev-parse HEAD
docker compose config
docker compose pull edge memcached otelcol
docker compose build --pull backend frontend
docker compose up -d --no-build --remove-orphans
```

### Ingress

Only edge Caddy is published, as plain HTTP on `127.0.0.1:65199`. Configure the
VPS ingress to proxy to `http://127.0.0.1:65199`; the ingress remains the sole
owner of TLS termination and Brotli compression. Edge routes `/api/*` to the
backend and all other paths to the frontend. Do not add public, IPv6, or
all-interface Docker publications, and do not change the host firewall.

### Troubleshooting

Check the rendered configuration, service state, and relevant logs first:

```sh
docker compose config
docker compose ps
docker compose logs edge frontend backend memcached otelcol
```

Memcached is disposable. After its loss, snapshot requests return `410 Gone`
until the next scheduled synchronization rebuilds serving state. If only
telemetry is missing, inspect the backend and Collector logs and verify the
Grafana Cloud endpoint and credentials; telemetry loss does not change normal
application processing.
