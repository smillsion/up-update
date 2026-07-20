#!/usr/bin/env bash

set -Eeuo pipefail

SCRIPT_DIR="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
ENV_FILE="${UP_UPDATE_ENV_FILE:-${SCRIPT_DIR}/.env}"
IMAGE_NAME="${UP_UPDATE_IMAGE:-up-update:local}"
CONTAINER_NAME="${UP_UPDATE_CONTAINER:-up-update}"
VOLUME_NAME="${UP_UPDATE_VOLUME:-up-update-data}"
BIND_ADDRESS="${UP_UPDATE_BIND_ADDRESS:-127.0.0.1}"
GO_MODULE_PROXY="${GOPROXY:-https://goproxy.cn,direct}"

log() {
    printf '[up-update] %s\n' "$*"
}

die() {
    printf '[up-update] ERROR: %s\n' "$*" >&2
    exit 1
}

read_env() {
    local key="$1"

    awk -v key="$key" '
        index($0, key "=") == 1 {
            value = substr($0, length(key) + 2)
            sub(/\r$/, "", value)
            found = 1
        }
        END {
            if (found) print value
        }
    ' "$ENV_FILE"
}

command -v docker >/dev/null 2>&1 || die "docker command not found"
command -v awk >/dev/null 2>&1 || die "awk command not found"
docker info >/dev/null 2>&1 || die "Docker daemon is unavailable; start Docker first"
[[ -f "$ENV_FILE" ]] || die "missing environment file: $ENV_FILE"

HOST_PORT="$(read_env UP_UPDATE_HTTP_PORT)"
ADMIN_USERNAME="$(read_env UP_UPDATE_ADMIN_USERNAME)"
ADMIN_PASSWORD="$(read_env UP_UPDATE_ADMIN_PASSWORD)"
ENCRYPTION_KEY="$(read_env UP_UPDATE_ENCRYPTION_KEY)"
SECURE_COOKIES="$(read_env UP_UPDATE_SECURE_COOKIES)"

[[ "$HOST_PORT" =~ ^[0-9]+$ ]] || die "UP_UPDATE_HTTP_PORT must be a number"
(( HOST_PORT >= 1 && HOST_PORT <= 65535 )) || die "UP_UPDATE_HTTP_PORT must be between 1 and 65535"
[[ -n "$ADMIN_USERNAME" ]] || die "UP_UPDATE_ADMIN_USERNAME cannot be empty"
(( ${#ADMIN_PASSWORD} >= 10 )) || die "UP_UPDATE_ADMIN_PASSWORD must contain at least 10 characters"
[[ "$ENCRYPTION_KEY" =~ ^[0-9A-Fa-f]{64}$ ]] || die "UP_UPDATE_ENCRYPTION_KEY must be exactly 64 hexadecimal characters"

case "$SECURE_COOKIES" in
    true|TRUE|True) ;;
    *) log "WARNING: set UP_UPDATE_SECURE_COOKIES=true when using HTTPS" ;;
esac

cd "$SCRIPT_DIR"

log "building image: $IMAGE_NAME"
docker build --pull \
    --build-arg "GOPROXY=${GO_MODULE_PROXY}" \
    --tag "$IMAGE_NAME" \
    .

if ! docker volume inspect "$VOLUME_NAME" >/dev/null 2>&1; then
    log "creating data volume: $VOLUME_NAME"
    docker volume create "$VOLUME_NAME" >/dev/null
fi

if docker container inspect "$CONTAINER_NAME" >/dev/null 2>&1; then
    log "removing existing container: $CONTAINER_NAME"
    docker rm --force "$CONTAINER_NAME" >/dev/null
fi

log "starting container on ${BIND_ADDRESS}:${HOST_PORT}"
docker run --detach \
    --name "$CONTAINER_NAME" \
    --restart unless-stopped \
    --env-file "$ENV_FILE" \
    --publish "${BIND_ADDRESS}:${HOST_PORT}:8080" \
    --volume "${VOLUME_NAME}:/data" \
    --read-only \
    --tmpfs /tmp \
    --cap-drop ALL \
    --security-opt no-new-privileges:true \
    "$IMAGE_NAME" >/dev/null

log "waiting for the health endpoint"
for attempt in $(seq 1 45); do
    if docker exec "$CONTAINER_NAME" wget -qO- http://127.0.0.1:8080/healthz >/dev/null 2>&1; then
        log "deployment completed: http://${BIND_ADDRESS}:${HOST_PORT}"
        log "public address: https://upup.tlllu.com"
        exit 0
    fi

    if [[ "$(docker inspect --format '{{.State.Running}}' "$CONTAINER_NAME" 2>/dev/null || true)" != "true" ]]; then
        break
    fi

    sleep 2
done

log "container did not become healthy; recent logs follow"
docker logs --tail 100 "$CONTAINER_NAME" || true
exit 1
