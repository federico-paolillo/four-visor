#!/usr/bin/env bash
# This module validates the two first-party runtime images without probing a deployment.

set -Eeuo pipefail
umask 077

readonly backend_image="four-visor-backend:us016-validation"
readonly frontend_image="four-visor-frontend:us016-validation"
readonly backend_name="four-visor-us016-backend-validation"
readonly frontend_name="four-visor-us016-frontend-validation"
readonly caddy_name="four-visor-us016-caddy-config-validation"
readonly zero_commit="0000000000000000000000000000000000000000"

validation_directory="$(mktemp -d "${TMPDIR:-/tmp}/four-visor-us016-images.XXXXXX")"
readonly validation_directory
readonly backend_cidfile="$validation_directory/backend.cid"
readonly frontend_cidfile="$validation_directory/frontend.cid"
readonly caddy_cidfile="$validation_directory/caddy.cid"
readonly -a cidfiles=("$backend_cidfile" "$frontend_cidfile" "$caddy_cidfile")

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
				timeout 15s docker container stop --time 10 "$id" >/dev/null 2>&1 || true
				timeout 10s docker container rm --force "$id" >/dev/null 2>&1 || true
			fi
		elif [[ -e "$cidfile" ]]; then
			printf 'refusing cleanup for invalid container ID file: %s\n' "$cidfile" >&2
		fi

		rm -f -- "$cidfile"
	done

	rmdir -- "$validation_directory" 2>/dev/null || true
}

trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

fail() {
	printf 'image validation failed: %s\n' "$*" >&2
	exit 1
}

assert_equal() {
	local label="$1"
	local expected="$2"
	local actual="$3"

	[[ "$actual" == "$expected" ]] || fail "$label: got '$actual', want '$expected'"
}

assert_image_value() {
	local image="$1"
	local template="$2"
	local expected="$3"
	local label="$4"

	assert_equal "$label" "$expected" "$(docker image inspect --format "$template" "$image")"
}

assert_container_runtime() {
	local id="$1"
	local label="$2"

	assert_equal "$label read-only root" "true" \
		"$(docker container inspect --format '{{.HostConfig.ReadonlyRootfs}}' "$id")"
	assert_equal "$label network" "none" \
		"$(docker container inspect --format '{{.HostConfig.NetworkMode}}' "$id")"
	assert_equal "$label mounts" "0" \
		"$(docker container inspect --format '{{len .Mounts}}' "$id")"
	assert_equal "$label tmpfs" "0" \
		"$(docker container inspect --format '{{len .HostConfig.Tmpfs}}' "$id")"
}

assert_running() {
	local id="$1"
	local label="$2"

	if [[ "$(docker container inspect --format '{{.State.Running}}' "$id")" != "true" ]]; then
		docker container logs "$id" >&2 || true
		fail "$label process exited during startup"
	fi
}

stop_container() {
	local id="$1"
	local label="$2"

	timeout 15s docker container stop --time 10 "$id" >/dev/null || fail "$label did not stop within 15 seconds"
}

assert_required_path() {
	local contents="$1"
	local path="$2"

	grep -Fxq "$path" <<<"$contents" || fail "missing runtime path $path"
}

assert_clean_contents() {
	local contents="$1"
	local label="$2"
	local forbidden

	for forbidden in \
		bin/ash bin/bash bin/busybox bin/sh \
		usr/bin/ash usr/bin/bash usr/bin/busybox usr/bin/node usr/bin/npm usr/bin/sh \
		usr/local/bin/node usr/local/bin/npm usr/local/go/bin/go; do
		if grep -Fxq "$forbidden" <<<"$contents"; then
			fail "$label contains forbidden runtime path $forbidden"
		fi
	done

	if grep -Eq '(^|/)(Dockerfile|go\.mod|go\.sum|package\.json|package-lock\.json|[^/]+\.(go|ts|tsx))$' <<<"$contents"; then
		fail "$label contains source or dependency locks"
	fi
}

for name in "$backend_name" "$frontend_name" "$caddy_name"; do
	if docker container inspect "$name" >/dev/null 2>&1; then
		fail "validation container name already exists: $name"
	fi
done

printf 'Building Linux amd64 images...\n'
docker build --platform=linux/amd64 --tag "$backend_image" backend
docker build --platform=linux/amd64 --tag "$frontend_image" frontend

printf 'Inspecting image metadata...\n'
assert_image_value "$backend_image" '{{.Os}}/{{.Architecture}}' 'linux/amd64' 'backend platform'
assert_image_value "$backend_image" '{{.Config.User}}' '65532:65532' 'backend user'
assert_image_value "$backend_image" '{{json .Config.Entrypoint}}' '["/usr/bin/four-visor"]' 'backend entrypoint'
assert_image_value "$backend_image" '{{json .Config.Cmd}}' 'null' 'backend command'
assert_image_value "$backend_image" '{{json .Config.ExposedPorts}}' '{"65102/tcp":{}}' 'backend ports'
assert_image_value "$backend_image" '{{.Config.WorkingDir}}' '/' 'backend working directory'
assert_image_value "$backend_image" '{{len .Config.Volumes}}' '0' 'backend volumes'

assert_image_value "$frontend_image" '{{.Os}}/{{.Architecture}}' 'linux/amd64' 'frontend platform'
assert_image_value "$frontend_image" '{{.Config.User}}' '65532:65532' 'frontend user'
assert_image_value "$frontend_image" '{{json .Config.Entrypoint}}' '["/usr/bin/caddy"]' 'frontend entrypoint'
assert_image_value "$frontend_image" '{{json .Config.Cmd}}' '["run","--config","/etc/caddy/Caddyfile","--adapter","caddyfile"]' 'frontend command'
assert_image_value "$frontend_image" '{{json .Config.ExposedPorts}}' '{"65101/tcp":{}}' 'frontend ports'
assert_image_value "$frontend_image" '{{.Config.WorkingDir}}' '/srv' 'frontend working directory'
assert_image_value "$frontend_image" '{{len .Config.Volumes}}' '0' 'frontend volumes'

for image in "$backend_image" "$frontend_image"; do
	if docker image inspect --format '{{range .Config.Env}}{{println .}}{{end}}' "$image" \
		| grep -Eq '^(FOURVISOR_|HOME=|XDG_(CONFIG|DATA)_HOME=)'; then
		fail "$image declares application or writable-storage environment"
	fi
done

printf 'Validating the final Caddy configuration...\n'
docker container create \
	--cidfile "$caddy_cidfile" \
	--name "$caddy_name" \
	--network none \
	--read-only \
	"$frontend_image" \
	validate --config /etc/caddy/Caddyfile --adapter caddyfile >/dev/null
caddy_id="$(container_id_from_file "$caddy_cidfile")" || fail 'Caddy validation container ID was not recorded safely'
docker container start --attach "$caddy_id"
assert_equal 'Caddy validation exit code' '0' \
	"$(docker container inspect --format '{{.State.ExitCode}}' "$caddy_id")"
assert_container_runtime "$caddy_id" 'Caddy validation'

printf 'Starting the backend before its minimum synchronization jitter...\n'
docker container create \
	--cidfile "$backend_cidfile" \
	--name "$backend_name" \
	--network none \
	--read-only \
	--env FOURVISOR_SERVER_ADDRESS=:65102 \
	--env FOURVISOR_MEMCACHED_ADDRESS=127.0.0.1:65100 \
	--env FOURVISOR_OTLP_ENDPOINT=http://127.0.0.1:65103 \
	--env FOURVISOR_COMMIT_HASH="$zero_commit" \
	"$backend_image" >/dev/null
backend_id="$(container_id_from_file "$backend_cidfile")" || fail 'backend container ID was not recorded safely'
docker container start "$backend_id" >/dev/null
sleep 1
assert_running "$backend_id" 'backend'
assert_container_runtime "$backend_id" 'backend'
stop_container "$backend_id" 'backend'

printf 'Starting the frontend...\n'
docker container create \
	--cidfile "$frontend_cidfile" \
	--name "$frontend_name" \
	--network none \
	--read-only \
	"$frontend_image" >/dev/null
frontend_id="$(container_id_from_file "$frontend_cidfile")" || fail 'frontend container ID was not recorded safely'
docker container start "$frontend_id" >/dev/null
sleep 1
assert_running "$frontend_id" 'frontend'
assert_container_runtime "$frontend_id" 'frontend'
stop_container "$frontend_id" 'frontend'

printf 'Inspecting final filesystem contents...\n'
backend_contents="$(docker container export "$backend_id" | tar -tf -)"
assert_required_path "$backend_contents" 'usr/bin/four-visor'
assert_clean_contents "$backend_contents" 'backend image'

frontend_contents="$(docker container export "$frontend_id" | tar -tf -)"
for path in \
	usr/bin/caddy \
	etc/caddy/Caddyfile \
	srv/index.html \
	srv/service-worker.js \
	srv/manifest.webmanifest \
	srv/icons/icon-192.png \
	srv/icons/icon-512.png; do
	assert_required_path "$frontend_contents" "$path"
done
grep -Eq '^srv/assets/[^/]+\.js$' <<<"$frontend_contents" || fail 'frontend image has no built JavaScript asset'
grep -Eq '^srv/assets/[^/]+\.css$' <<<"$frontend_contents" || fail 'frontend image has no built CSS asset'
assert_clean_contents "$frontend_contents" 'frontend image'

caddy_config="$(docker container cp "$frontend_id":/etc/caddy/Caddyfile - | tar -xOf -)"
for setting in 'admin off' 'auto_https off' 'persist_config off' 'output stdout' 'http://:65101' 'try_files {path} /index.html' 'file_server'; do
	grep -Fq "$setting" <<<"$caddy_config" || fail "Caddyfile is missing: $setting"
done
if grep -Eq '(^|[[:space:]])(encode|reverse_proxy|tls)([[:space:]]|$)|/api|output[[:space:]]+file' <<<"$caddy_config"; then
	fail 'Caddyfile owns proxying, compression, TLS, API routing, or filesystem logging'
fi

printf 'First-party image artifact validation passed.\n'
