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

## Goal

This repository source code and consequent software is not the goal. The goal of
this repository is to try out and tune an autonomous AI-driven process (driven
mainly by `/goal`) to decompose and implement a software from start to finish
using a [design document](docs/SEED.md).

## Configuration

> Provide these values as a `.env` file (refer to .env.example)

| Environment variable                                  | Default                | Purpose                                                                                       | Required |
| ----------------------------------------------------- | ---------------------- | --------------------------------------------------------------------------------------------- | -------- |
| `FOURVISOR_SERVER_ADDRESS`                            | `:65102`               | Backend HTTP listener.                                                                        | ❌       |
| `FOURVISOR_MEMCACHED_ADDRESS`                         | required               | Memcached host and project port.                                                              | ❌       |
| `FOURVISOR_OTLP_ENDPOINT`                             | `http://otelcol:65103` | OTLP/gRPC Collector URL.                                                                      | ❌       |
| `FOURVISOR_ACQUISITION_RATE_INTERVAL`                 | `1s`                   | Minimum interval between outbound attempts; must be at least `1s`.                            | ❌       |
| `FOURVISOR_ACQUISITION_MAX_CONCURRENCY`               | `10`                   | Process-wide outbound concurrency, from 1 through 10.                                         | ❌       |
| `FOURVISOR_ACQUISITION_REQUEST_TIMEOUT`               | `5s`                   | Timeout for one outbound request attempt and response body.                                   | ❌       |
| `FOURVISOR_ACQUISITION_MAX_RETRIES`                   | `2`                    | Retries after the initial attempt, from 0 through 2.                                          | ❌       |
| `FOURVISOR_ACQUISITION_RETRY_BACKOFF`                 | `1s`                   | Base retry delay.                                                                             | ❌       |
| `FOURVISOR_ACQUISITION_DEADLINE`                      | `4h`                   | Evidence-backed total acquisition deadline; operator overrides may use any positive duration. | ❌       |
| `FOURVISOR_SYNCHRONIZATION_INTERVAL`                  | `4h`                   | Synchronization cadence in whole seconds, minimum `1s`.                                       | ❌       |
| `FOURVISOR_SYNCHRONIZATION_FAILED_RESOURCE_TOLERANCE` | `10`                   | Failed-resource observability threshold.                                                      | ❌       |
| `FOURVISOR_COMMIT_HASH`                               | n/a                    | Full lowercase 40-character deployed Git commit.                                              | ✔️       |
| `GRAFANA_CLOUD_OTLP_ENDPOINT`                         | n/a                    | Grafana Cloud OTLP/HTTP base URL.                                                             | ✔️       |
| `GRAFANA_CLOUD_INSTANCE_ID`                           | n/a                    | Grafana Cloud Basic Auth instance ID.                                                         | ✔️       |
| `GRAFANA_CLOUD_API_KEY`                               | n/a                    | Grafana Cloud Basic Auth API key.                                                             | ✔️       |
