#!/usr/bin/env bash
# This module validates deployment configuration without starting or probing the Compose application.

set -Eeuo pipefail
umask 077

readonly project_root="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")/.." && pwd)"
readonly compose_file="$project_root/docker-compose.yml"
readonly edge_image='caddy:2.11.4-alpine@sha256:98eb57d882ccd5213d1688764db10c1ca2c58a1ca3a6717a3411ad798f7a423a'
readonly collector_image='ghcr.io/open-telemetry/opentelemetry-collector-releases/opentelemetry-collector-contrib:0.157.0@sha256:4eb842091c796156d4d3c994eb22ba793590f5723719dbf6b8436cb4dfc17f48'
readonly edge_name='four-visor-us017-edge-config-validation'
readonly collector_name='four-visor-us017-collector-config-validation'
readonly capture_name='four-visor-us018-collector-capture'
readonly policy_name='four-visor-us018-collector-policy'
readonly policy_network='four-visor-us018-collector-validation'

validation_directory="$(mktemp -d "${TMPDIR:-/tmp}/four-visor-us017-compose.XXXXXX")"
readonly validation_directory
readonly baseline_env="$validation_directory/baseline.env"
readonly override_env="$validation_directory/override.env"
readonly baseline_json="$validation_directory/baseline.json"
readonly override_json="$validation_directory/override.json"
readonly caddy_json="$validation_directory/caddy.json"
readonly capture_config="$validation_directory/capture.yaml"
readonly capture_output="$validation_directory/output"
readonly captured_traces="$capture_output/traces.json"
readonly captured_metrics="$capture_output/metrics.json"
readonly captured_logs="$capture_output/logs.json"
readonly edge_cidfile="$validation_directory/edge.cid"
readonly collector_cidfile="$validation_directory/collector.cid"
readonly capture_cidfile="$validation_directory/capture.cid"
readonly policy_cidfile="$validation_directory/policy.cid"
readonly -a cidfiles=("$edge_cidfile" "$collector_cidfile" "$capture_cidfile" "$policy_cidfile")
readonly -a compose_environment=(
	FOURVISOR_COMMIT_HASH
	OTEL_EXPORTER_OTLP_TRACES_ENDPOINT
	OTEL_EXPORTER_OTLP_METRICS_ENDPOINT
	OTEL_EXPORTER_OTLP_LOGS_ENDPOINT
	FOURVISOR_HEALTH_TIMEOUT
	FOURVISOR_DNS_NAME
	FOURVISOR_OTLP_ENDPOINT
	FOURVISOR_ACQUISITION_RATE_INTERVAL
	FOURVISOR_ACQUISITION_MAX_CONCURRENCY
	FOURVISOR_ACQUISITION_REQUEST_TIMEOUT
	FOURVISOR_ACQUISITION_MAX_RETRIES
	FOURVISOR_ACQUISITION_RETRY_BACKOFF
	FOURVISOR_SYNCHRONIZATION_INTERVAL
	FOURVISOR_SYNCHRONIZATION_FAILED_RESOURCE_TOLERANCE
)

fail() {
	printf 'compose validation failed: %s\n' "$*" >&2
	exit 1
}

container_id_from_file() {
	local cidfile="$1"
	local -a ids=()

	[[ -f "$cidfile" ]] || return 1
	mapfile -t ids <"$cidfile"
	[[ "${#ids[@]}" -eq 1 && "${ids[0]}" =~ ^[0-9a-f]{64}$ ]] || return 1
	printf '%s' "${ids[0]}"
}

cleanup() {
	local cidfile
	local id

	for cidfile in "${cidfiles[@]}"; do
		if id="$(container_id_from_file "$cidfile")"; then
			if docker container inspect "$id" >/dev/null 2>&1; then
				timeout 10s docker container rm --force "$id" >/dev/null 2>&1 || true
			fi
		elif [[ -e "$cidfile" ]]; then
			printf 'refusing cleanup for invalid container ID file: %s\n' "$cidfile" >&2
		fi
		rm -f -- "$cidfile"
	done
	if docker network inspect "$policy_network" >/dev/null 2>&1; then
		docker network rm "$policy_network" >/dev/null 2>&1 || true
	fi

	rm -f -- \
		"$baseline_env" "$override_env" "$baseline_json" "$override_json" "$caddy_json" \
		"$capture_config" "$captured_traces" "$captured_metrics" "$captured_logs"
	rmdir -- "$capture_output" 2>/dev/null || true
	rmdir -- "$validation_directory" 2>/dev/null || true
}

trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

render_compose() {
	local env_file="$1"
	local output="$2"
	local name

	(
		for name in "${compose_environment[@]}"; do
			unset "$name"
		done
		docker compose --env-file "$env_file" --file "$compose_file" config --format json >"$output"
	)
}

wait_for_loopback_port() {
	local port="$1"
	node - "$port" <<'NODE'
const net = require("node:net");

const port = Number(process.argv[2]);
const deadline = Date.now() + 10_000;

function attempt() {
  const socket = net.createConnection({ host: "127.0.0.1", port });
  socket.setTimeout(250);
  socket.once("connect", () => {
    socket.destroy();
    process.exit(0);
  });
  const retry = () => {
    socket.destroy();
    if (Date.now() >= deadline) process.exit(1);
    setTimeout(attempt, 100);
  };
  socket.once("error", retry);
  socket.once("timeout", retry);
}

attempt();
NODE
}

cat >"$baseline_env" <<'EOF'
FOURVISOR_COMMIT_HASH=0000000000000000000000000000000000000000
OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=traces.invalid:65103
OTEL_EXPORTER_OTLP_METRICS_ENDPOINT=metrics.invalid:65103
OTEL_EXPORTER_OTLP_LOGS_ENDPOINT=logs.invalid:65103
EOF

cat >"$override_env" <<'EOF'
FOURVISOR_COMMIT_HASH=0000000000000000000000000000000000000000
OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=traces.invalid:65103
OTEL_EXPORTER_OTLP_METRICS_ENDPOINT=metrics.invalid:65103
OTEL_EXPORTER_OTLP_LOGS_ENDPOINT=logs.invalid:65103
FOURVISOR_HEALTH_TIMEOUT=750ms
FOURVISOR_DNS_NAME=boards.4chan.org
FOURVISOR_OTLP_ENDPOINT=http://otelcol:65103
FOURVISOR_ACQUISITION_RATE_INTERVAL=2s
FOURVISOR_ACQUISITION_MAX_CONCURRENCY=4
FOURVISOR_ACQUISITION_REQUEST_TIMEOUT=3s
FOURVISOR_ACQUISITION_MAX_RETRIES=1
FOURVISOR_ACQUISITION_RETRY_BACKOFF=500ms
FOURVISOR_SYNCHRONIZATION_INTERVAL=2h
FOURVISOR_SYNCHRONIZATION_FAILED_RESOURCE_TOLERANCE=4
EOF

render_compose "$baseline_env" "$baseline_json"
render_compose "$override_env" "$override_json"

node - "$baseline_json" "$override_json" "$project_root" <<'NODE'
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");

const baseline = JSON.parse(fs.readFileSync(process.argv[2], "utf8"));
const override = JSON.parse(fs.readFileSync(process.argv[3], "utf8"));
const projectRoot = process.argv[4];
const services = baseline.services;
const optional = [
  "FOURVISOR_HEALTH_TIMEOUT",
  "FOURVISOR_DNS_NAME",
  "FOURVISOR_OTLP_ENDPOINT",
  "FOURVISOR_ACQUISITION_RATE_INTERVAL",
  "FOURVISOR_ACQUISITION_MAX_CONCURRENCY",
  "FOURVISOR_ACQUISITION_REQUEST_TIMEOUT",
  "FOURVISOR_ACQUISITION_MAX_RETRIES",
  "FOURVISOR_ACQUISITION_RETRY_BACKOFF",
  "FOURVISOR_SYNCHRONIZATION_INTERVAL",
  "FOURVISOR_SYNCHRONIZATION_FAILED_RESOURCE_TOLERANCE",
];
const expectedOverrides = {
  FOURVISOR_HEALTH_TIMEOUT: "750ms",
  FOURVISOR_DNS_NAME: "boards.4chan.org",
  FOURVISOR_OTLP_ENDPOINT: "http://otelcol:65103",
  FOURVISOR_ACQUISITION_RATE_INTERVAL: "2s",
  FOURVISOR_ACQUISITION_MAX_CONCURRENCY: "4",
  FOURVISOR_ACQUISITION_REQUEST_TIMEOUT: "3s",
  FOURVISOR_ACQUISITION_MAX_RETRIES: "1",
  FOURVISOR_ACQUISITION_RETRY_BACKOFF: "500ms",
  FOURVISOR_SYNCHRONIZATION_INTERVAL: "2h",
  FOURVISOR_SYNCHRONIZATION_FAILED_RESOURCE_TOLERANCE: "4",
};

assert.deepEqual(Object.keys(services).sort(), ["backend", "edge", "frontend", "memcached", "otelcol"]);
assert.deepEqual(Object.keys(baseline.networks).sort(), ["app", "cache"]);
assert.equal(Boolean(baseline.networks.app.internal), false);
assert.equal(baseline.networks.cache.internal, true);

const expectedNetworks = {
  backend: ["app", "cache"],
  edge: ["app"],
  frontend: ["app"],
  memcached: ["cache"],
  otelcol: ["app"],
};
for (const [name, service] of Object.entries(services)) {
  assert.equal(service.platform, "linux/amd64", `${name} platform`);
  assert.equal(service.restart, "unless-stopped", `${name} restart`);
  assert.deepEqual(Object.keys(service.networks).sort(), expectedNetworks[name], `${name} networks`);
  assert.notEqual(service.network_mode, "host", `${name} host network`);
  assert.equal(service.depends_on, undefined, `${name} depends_on`);
  assert.equal(service.expose, undefined, `${name} expose`);
  assert.equal(service.deploy, undefined, `${name} deploy`);
  if (name !== "backend") {
    assert.equal(service.healthcheck, undefined, `${name} healthcheck`);
  }
}

const published = Object.entries(services).flatMap(([name, service]) =>
  (service.ports ?? []).map((port) => ({ name, ...port })),
);
assert.deepEqual(published, [{
  name: "edge",
  mode: "ingress",
  host_ip: "127.0.0.1",
  target: 65199,
  published: "65199",
  protocol: "tcp",
}]);

assert.equal(services.edge.image, "caddy:2.11.4-alpine@sha256:98eb57d882ccd5213d1688764db10c1ca2c58a1ca3a6717a3411ad798f7a423a");
assert.equal(services.memcached.image, "memcached:1.6.45-alpine@sha256:fb019eacc7baefab28dd9424a093181f9be578785ff820acfc223cca7d196eb3");
assert.equal(services.otelcol.image, "ghcr.io/open-telemetry/opentelemetry-collector-releases/opentelemetry-collector-contrib:0.157.0@sha256:4eb842091c796156d4d3c994eb22ba793590f5723719dbf6b8436cb4dfc17f48");

for (const name of ["backend", "frontend"]) {
  assert.equal(services[name].pull_policy, "never", `${name} pull policy`);
  assert.equal(services[name].user, "65532:65532", `${name} user`);
  assert.equal(services[name].read_only, true, `${name} read-only`);
  assert.equal(services[name].volumes, undefined, `${name} mounts`);
  assert.equal(services[name].build.context, path.join(projectRoot, name), `${name} context`);
  assert.equal(services[name].build.dockerfile, "Dockerfile", `${name} Dockerfile`);
  assert.equal(services[name].image, `four-visor-${name}:0000000000000000000000000000000000000000`);
}
for (const name of ["edge", "memcached", "otelcol"]) {
  assert.equal(services[name].read_only, undefined, `${name} third-party read-only override`);
  assert.equal(services[name].user, undefined, `${name} third-party user override`);
  const environment = services[name].environment ?? {};
  assert.equal(Object.keys(environment).some((key) => key.startsWith("FOURVISOR_")), false, `${name} FOURVISOR_ environment`);
}

assert.deepEqual(services.edge.volumes, [{
  type: "bind",
  source: path.join(projectRoot, "Caddyfile"),
  target: "/etc/caddy/Caddyfile",
  read_only: true,
}]);
assert.deepEqual(services.otelcol.volumes, [{
  type: "bind",
  source: path.join(projectRoot, "otel-collector.yaml"),
  target: "/etc/otelcol-contrib/config.yaml",
  read_only: true,
}]);
assert.deepEqual(services.memcached.command, [
  "memcached",
  "--listen=0.0.0.0",
  "--port=65100",
  "--memory-limit=64",
  "--max-item-size=1m",
  "--disable-evictions",
]);
assert.deepEqual(services.otelcol.command, ["--config=/etc/otelcol-contrib/config.yaml"]);
assert.deepEqual(services.otelcol.environment, {
  OTEL_EXPORTER_OTLP_LOGS_ENDPOINT: "logs.invalid:65103",
  OTEL_EXPORTER_OTLP_METRICS_ENDPOINT: "metrics.invalid:65103",
  OTEL_EXPORTER_OTLP_TRACES_ENDPOINT: "traces.invalid:65103",
});

assert.deepEqual(services.backend.healthcheck, {
  test: ["CMD", "/usr/bin/four-visor", "healthcheck", "http://127.0.0.1:65102/health"],
  timeout: "5s",
  interval: "30s",
  retries: 3,
  start_period: "5s",
});
assert.equal(services.backend.environment.FOURVISOR_MEMCACHED_ADDRESS, "memcached:65100");
assert.equal(services.backend.environment.FOURVISOR_COMMIT_HASH, "0000000000000000000000000000000000000000");
assert.equal("FOURVISOR_SERVER_ADDRESS" in services.backend.environment, false, "fixed backend listener");
assert.equal("FOURVISOR_SERVER_ADDRESS" in override.services.backend.environment, false, "fixed backend listener override");
for (const key of optional) {
  assert.equal(services.backend.environment[key], null, `${key} baseline must be absent/null`);
  assert.notEqual(services.backend.environment[key], "", `${key} baseline must not be blank`);
  assert.equal(override.services.backend.environment[key], expectedOverrides[key], `${key} override`);
}
assert.equal(Object.values(services.backend.environment).some((value) => value === ""), false, "blank backend environment");

const composeSource = fs.readFileSync(path.join(projectRoot, "docker-compose.yml"), "utf8");
assert.equal((composeSource.match(/^\s+ports:/gm) ?? []).length, 1, "one long-form ports block");
for (const field of ["host_ip: 127.0.0.1", 'published: "65199"', "target: 65199", "protocol: tcp"]) {
  assert.ok(composeSource.includes(field), `missing long-form port field ${field}`);
}
assert.equal(/^\s+expose:/m.test(composeSource), false, "source expose");
assert.equal(/^\s+depends_on:/m.test(composeSource), false, "source depends_on");
assert.equal(composeSource.includes("OTEL_EXPORTER_OTLP_ENDPOINT"), false, "ambiguous Collector endpoint");
assert.equal(composeSource.includes("FOURVISOR_OTLP_EXPORT_ENDPOINT"), false, "project-prefixed Collector endpoint");
assert.equal(composeSource.includes("FOURVISOR_SERVER_ADDRESS"), false, "backend listener override");
NODE

collector_source="$(<"$project_root/otel-collector.yaml")"
grep -Fq 'endpoint: 0.0.0.0:65103' <<<"$collector_source" || fail 'Collector OTLP/gRPC listener is missing'
for signal in TRACES METRICS LOGS; do
	grep -Fq "endpoint: \${env:OTEL_EXPORTER_OTLP_${signal}_ENDPOINT}" <<<"$collector_source" || \
		fail "Collector native ${signal,,} exporter endpoint is missing"
done
for field in \
	'sampling_strategy: trace-complete' \
	'decision_wait: 40m' \
	'decision_wait_after_root_received: 10s' \
	'num_traces: 5000' \
	'expected_new_traces_per_sec: 2' \
	'status_codes: [ERROR]' \
	'sampling_percentage: 10' \
	'processors: [filter/metrics, transform/allowlisted_attributes]' \
	'processors: [filter/logs, transform/allowlisted_attributes]'; do
	grep -Fq "$field" <<<"$collector_source" || fail "Collector policy field is missing: $field"
done
for metric in \
	http.server.request.count http.server.request.duration \
	http.client.request.count http.client.request.duration \
	cache.operation.count cache.operation.duration \
	lineage.synchronization.duration lineage.synchronization.activated \
	lineage.failed_resource.count lineage.active.age; do
	grep -Fq "\"$metric\"" <<<"$collector_source" || fail "Collector metric allowlist is missing $metric"
done
for message in \
	'synchronization started' 'outbound acquisition completed' 'lineage activated' \
	'previous lineage evicted' 'synchronization completed' 'oversized thread detected' \
	'synchronization tick skipped'; do
	grep -Fq "log.body != \"$message\"" <<<"$collector_source" || fail "Collector log allowlist is missing $message"
done
[[ "$(grep -Fc 'enabled: false' <<<"$collector_source")" -eq 6 ]] || fail 'Collector retries and queues are not all disabled'
if grep -Eq '65104|OTEL_EXPORTER_OTLP_ENDPOINT|FOURVISOR_OTLP_EXPORT_ENDPOINT|^[[:space:]]+http:|file_storage|filelog|^[[:space:]]+file/' <<<"$collector_source"; then
	fail 'Collector config contains the removed HTTP listener or project-prefixed endpoint'
fi

port_inventory="$(
	grep -Eh ':[0-9]{2,5}([/[:space:]\"]|$)|--port=[0-9]{2,5}|EXPOSE[[:space:]]+[0-9]{2,5}' \
		"$compose_file" \
		"$project_root/Caddyfile" \
		"$project_root/frontend/Caddyfile" \
		"$project_root/otel-collector.yaml" \
		"$project_root/backend/Dockerfile" \
		"$project_root/frontend/Dockerfile" \
		"$project_root/.env.example" \
		| grep -Ev 'user:|^USER[[:space:]]' \
		| grep -Eo ':[0-9]{2,5}([/[:space:]\"]|$)|--port=[0-9]{2,5}|EXPOSE[[:space:]]+[0-9]{2,5}' \
		| grep -Eo '[0-9]{2,5}' \
		| sort -nu
)"
[[ "$port_inventory" == $'65100\n65101\n65102\n65103\n65199' ]] || fail "deployment port inventory is: ${port_inventory//$'\n'/, }"

for name in "$edge_name" "$collector_name" "$capture_name" "$policy_name"; do
	if docker container inspect "$name" >/dev/null 2>&1; then
		fail "validation container name already exists: $name"
	fi
done
if docker network inspect "$policy_network" >/dev/null 2>&1; then
	fail "validation network name already exists: $policy_network"
fi

docker container create \
	--cidfile "$edge_cidfile" \
	--name "$edge_name" \
	--network none \
	--mount "type=bind,src=$project_root/Caddyfile,dst=/etc/caddy/Caddyfile,readonly" \
	"$edge_image" \
	caddy adapt --config /etc/caddy/Caddyfile --adapter caddyfile --pretty >/dev/null
edge_id="$(container_id_from_file "$edge_cidfile")" || fail 'edge validation container ID was not recorded safely'
docker container start --attach "$edge_id" >"$caddy_json"

node - "$caddy_json" <<'NODE'
const assert = require("node:assert/strict");
const fs = require("node:fs");

const config = JSON.parse(fs.readFileSync(process.argv[2], "utf8"));
assert.equal(config.admin.disabled, true);
assert.equal(config.admin.config.persist, false);
assert.equal(config.apps.tls, undefined, "TLS app");

const servers = Object.values(config.apps.http.servers);
assert.equal(servers.length, 1);
const server = servers[0];
assert.deepEqual(server.listen, [":65199"]);
assert.equal(server.automatic_https.disable, true);
assert.equal(server.routes.length, 2);

const [api, fallback] = server.routes;
assert.deepEqual(api.match, [{ path: ["/api/*"] }]);
assert.ok(api.group && api.group === fallback.group, "exclusive ordered handle group");
assert.equal(fallback.match, undefined);

const apiJSON = JSON.stringify(api);
const fallbackJSON = JSON.stringify(fallback);
assert.ok(apiJSON.includes('"strip_path_prefix":"/api"'), "API prefix strip");
assert.ok(apiJSON.includes('"dial":"backend:65102"'), "backend upstream");
assert.equal(apiJSON.includes('"dial":"frontend:65101"'), false);
assert.ok(fallbackJSON.includes('"dial":"frontend:65101"'), "frontend fallback");
assert.equal(fallbackJSON.includes('"dial":"backend:65102"'), false);

const proxies = [];
function collectProxies(value) {
  if (Array.isArray(value)) {
    value.forEach(collectProxies);
  } else if (value && typeof value === "object") {
    if (value.handler === "reverse_proxy") proxies.push(value);
    Object.values(value).forEach(collectProxies);
  }
}
collectProxies(server.routes);
assert.equal(proxies.length, 2, "reverse proxy count");
for (const proxy of proxies) {
  assert.deepEqual(proxy.headers?.request?.set?.["Accept-Encoding"], ["identity"], "upstream Accept-Encoding");
}

const all = JSON.stringify(config);
for (const forbidden of ['"handler":"encode"', '"handler":"static_response"', '"handler":"headers"']) {
  assert.equal(all.includes(forbidden), false, forbidden);
}
for (const external of ["/api/snapshot", "/api/health"]) {
  assert.ok(external.startsWith("/api/"));
  assert.ok(["/snapshot", "/health"].includes(external.slice(4)));
}
NODE

docker container create \
	--cidfile "$collector_cidfile" \
	--name "$collector_name" \
	--network none \
	--env OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=traces.invalid:65103 \
	--env OTEL_EXPORTER_OTLP_METRICS_ENDPOINT=metrics.invalid:65103 \
	--env OTEL_EXPORTER_OTLP_LOGS_ENDPOINT=logs.invalid:65103 \
	--mount "type=bind,src=$project_root/otel-collector.yaml,dst=/etc/otelcol-contrib/config.yaml,readonly" \
	"$collector_image" \
	validate --config=/etc/otelcol-contrib/config.yaml >/dev/null
collector_id="$(container_id_from_file "$collector_cidfile")" || fail 'Collector validation container ID was not recorded safely'
docker container start --attach "$collector_id"

mkdir --mode=0777 "$capture_output"
chmod 0777 "$capture_output"
cat >"$capture_config" <<'EOF'
receivers:
  otlp:
    protocols:
      grpc:
        endpoint: 0.0.0.0:65103

exporters:
  file/traces:
    path: /output/traces.json
    format: json
    flush_interval: 100ms
  file/metrics:
    path: /output/metrics.json
    format: json
    flush_interval: 100ms
  file/logs:
    path: /output/logs.json
    format: json
    flush_interval: 100ms

service:
  telemetry:
    metrics:
      level: none
      readers: []
  pipelines:
    traces:
      receivers: [otlp]
      exporters: [file/traces]
    metrics:
      receivers: [otlp]
      exporters: [file/metrics]
    logs:
      receivers: [otlp]
      exporters: [file/logs]
EOF
chmod 0644 "$capture_config"

docker network create "$policy_network" >/dev/null
docker container create \
	--cidfile "$capture_cidfile" \
	--name "$capture_name" \
	--network "$policy_network" \
	--network-alias capture \
	--publish 127.0.0.1:65195:65103 \
	--mount "type=bind,src=$capture_config,dst=/etc/otelcol-contrib/config.yaml,readonly" \
	--mount "type=bind,src=$capture_output,dst=/output" \
	"$collector_image" \
	--config=/etc/otelcol-contrib/config.yaml >/dev/null
capture_id="$(container_id_from_file "$capture_cidfile")" || fail 'capture Collector container ID was not recorded safely'

docker container create \
	--cidfile "$policy_cidfile" \
	--name "$policy_name" \
	--network "$policy_network" \
	--publish 127.0.0.1:65196:65103 \
	--env OTEL_EXPORTER_OTLP_TRACES_ENDPOINT=capture:65103 \
	--env OTEL_EXPORTER_OTLP_METRICS_ENDPOINT=capture:65103 \
	--env OTEL_EXPORTER_OTLP_LOGS_ENDPOINT=capture:65103 \
	--mount "type=bind,src=$project_root/otel-collector.yaml,dst=/etc/otelcol-contrib/config.yaml,readonly" \
	"$collector_image" \
	--config=/etc/otelcol-contrib/config.yaml >/dev/null
policy_id="$(container_id_from_file "$policy_cidfile")" || fail 'policy Collector container ID was not recorded safely'

docker container start "$capture_id" >/dev/null
if ! wait_for_loopback_port 65195; then
	docker container logs "$capture_id" >&2 || true
	fail 'capture Collector did not become ready'
fi
docker container start "$policy_id" >/dev/null
if ! wait_for_loopback_port 65196; then
	docker container logs "$policy_id" >&2 || true
	fail 'policy Collector did not become ready'
fi

(
	cd "$project_root/backend"
	FOURVISOR_TEST_COLLECTOR_ENDPOINT=http://127.0.0.1:65196 \
		FOURVISOR_TEST_COLLECTOR_TRACES_OUTPUT="$captured_traces" \
		FOURVISOR_TEST_COLLECTOR_METRICS_OUTPUT="$captured_metrics" \
		FOURVISOR_TEST_COLLECTOR_LOGS_OUTPUT="$captured_logs" \
		go test -v -count=1 -run '^TestCollectorSignalPolicies$' ./telemetry
)

printf 'Compose deployment configuration validation passed.\n'
