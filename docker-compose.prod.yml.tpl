# Production Compose template copied into release archives; .env supplies the image tag.
name: four-visor

services:
  edge:
    image: caddy:2.11.4-alpine@sha256:98eb57d882ccd5213d1688764db10c1ca2c58a1ca3a6717a3411ad798f7a423a
    platform: linux/amd64
    restart: unless-stopped
    ports:
      - host_ip: 127.0.0.1
        published: "65199"
        target: 65199
        protocol: tcp
    volumes:
      - type: bind
        source: ./Caddyfile
        target: /etc/caddy/Caddyfile
        read_only: true
    networks:
      - app

  frontend:
    image: ghcr.io/federico-paolillo/four-visor/frontend:${FOURVISOR_IMAGE_TAG:?FOURVISOR_IMAGE_TAG is required}
    platform: linux/amd64
    user: "65532:65532"
    read_only: true
    restart: unless-stopped
    networks:
      - app

  backend:
    image: ghcr.io/federico-paolillo/four-visor/backend:${FOURVISOR_IMAGE_TAG:?FOURVISOR_IMAGE_TAG is required}
    platform: linux/amd64
    user: "65532:65532"
    read_only: true
    restart: unless-stopped
    environment:
      - FOURVISOR_MEMCACHED_ADDRESS=memcached:65100
      - FOURVISOR_COMMIT_HASH=${FOURVISOR_COMMIT_HASH:?FOURVISOR_COMMIT_HASH is required}
      - FOURVISOR_HEALTH_TIMEOUT
      - FOURVISOR_DNS_NAME
      - FOURVISOR_OTLP_ENDPOINT
      - FOURVISOR_ACQUISITION_RATE_INTERVAL
      - FOURVISOR_ACQUISITION_MAX_CONCURRENCY
      - FOURVISOR_ACQUISITION_REQUEST_TIMEOUT
      - FOURVISOR_ACQUISITION_MAX_RETRIES
      - FOURVISOR_ACQUISITION_RETRY_BACKOFF
      - FOURVISOR_ACQUISITION_DEADLINE
      - FOURVISOR_SYNCHRONIZATION_INTERVAL
      - FOURVISOR_SYNCHRONIZATION_FAILED_RESOURCE_TOLERANCE
    healthcheck:
      test: ["CMD", "/usr/bin/four-visor", "healthcheck", "http://127.0.0.1:65102/health"]
      interval: 30s
      timeout: 5s
      retries: 3
      start_period: 5s
    networks:
      - app
      - cache

  memcached:
    image: memcached:1.6.45-alpine@sha256:fb019eacc7baefab28dd9424a093181f9be578785ff820acfc223cca7d196eb3
    platform: linux/amd64
    restart: unless-stopped
    command:
      [
        "memcached",
        "--listen=0.0.0.0",
        "--port=65100",
        "--memory-limit=1024",
        "--max-item-size=1m",
        "--disable-evictions",
      ]
    networks:
      - cache

  otelcol:
    image: ghcr.io/open-telemetry/opentelemetry-collector-releases/opentelemetry-collector-contrib:0.157.0@sha256:4eb842091c796156d4d3c994eb22ba793590f5723719dbf6b8436cb4dfc17f48
    platform: linux/amd64
    restart: unless-stopped
    command: ["--config=/etc/otelcol-contrib/config.yaml"]
    environment:
      - GRAFANA_CLOUD_OTLP_ENDPOINT=${GRAFANA_CLOUD_OTLP_ENDPOINT:?GRAFANA_CLOUD_OTLP_ENDPOINT is required}
      - GRAFANA_CLOUD_INSTANCE_ID=${GRAFANA_CLOUD_INSTANCE_ID:?GRAFANA_CLOUD_INSTANCE_ID is required}
      - GRAFANA_CLOUD_API_KEY=${GRAFANA_CLOUD_API_KEY:?GRAFANA_CLOUD_API_KEY is required}
    volumes:
      - type: bind
        source: ./otel-collector.yaml
        target: /etc/otelcol-contrib/config.yaml
        read_only: true
    networks:
      - app

networks:
  app:
  cache:
    internal: true
