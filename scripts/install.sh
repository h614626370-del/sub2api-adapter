#!/usr/bin/env bash
set -Eeuo pipefail

IMAGE="614626370/sub2api-adapter"
VERSION="latest"
LISTEN_ADDR="127.0.0.1:18080"
INSTALL_DIR="$(pwd)"
PROXY_URL=""

usage() {
  cat <<'EOF'
Usage: bash install-sub2api-adapter.sh [options]

Options:
  --dir PATH        Install directory. Default: current directory
  --listen ADDRESS  Adapter listen address. Default: 127.0.0.1:18080
  --proxy URL       Online updater proxy, for example http://192.168.1.2:7897
  --image IMAGE     Docker image repository. Default: 614626370/sub2api-adapter
  --version TAG     Docker image tag. Default: latest
  -h, --help        Show this help

Examples:
  bash install-sub2api-adapter.sh
  bash install-sub2api-adapter.sh --listen 0.0.0.0:18080
  bash install-sub2api-adapter.sh --proxy http://192.168.1.2:7897

The --proxy option configures only the online updater. Model requests remain
direct. It cannot configure the Docker daemon used for the initial image pull.
EOF
}

require_value() {
  if [[ $# -lt 2 || -z "${2-}" ]]; then
    echo "Missing value for ${1}." >&2
    exit 2
  fi
}

while [[ $# -gt 0 ]]; do
  case "$1" in
    --dir) require_value "$@"; INSTALL_DIR="$2"; shift 2 ;;
    --listen) require_value "$@"; LISTEN_ADDR="$2"; shift 2 ;;
    --proxy) require_value "$@"; PROXY_URL="$2"; shift 2 ;;
    --image) require_value "$@"; IMAGE="$2"; shift 2 ;;
    --version) require_value "$@"; VERSION="$2"; shift 2 ;;
    -h|--help) usage; exit 0 ;;
    *) echo "Unknown option: $1" >&2; usage >&2; exit 2 ;;
  esac
done

for value in "$IMAGE" "$VERSION" "$LISTEN_ADDR" "$INSTALL_DIR" "$PROXY_URL"; do
  if [[ "$value" == *$'\n'* || "$value" == *$'\r'* ]]; then
    echo "Options must not contain line breaks." >&2
    exit 2
  fi
done

LISTEN_PORT="${LISTEN_ADDR##*:}"
if [[ "$LISTEN_ADDR" != *:* || ! "$LISTEN_PORT" =~ ^[0-9]+$ || "$LISTEN_PORT" -lt 1 || "$LISTEN_PORT" -gt 65535 ]]; then
  echo "Invalid --listen value: ${LISTEN_ADDR}. Expected HOST:PORT." >&2
  exit 2
fi

if ! command -v docker >/dev/null 2>&1; then
  echo "Docker Engine is required. Install Docker before running this script." >&2
  exit 1
fi
if docker compose version >/dev/null 2>&1; then
  COMPOSE=(docker compose)
elif command -v docker-compose >/dev/null 2>&1; then
  COMPOSE=(docker-compose)
else
  echo "Docker Compose is required (docker compose or docker-compose)." >&2
  exit 1
fi
if ! docker info >/dev/null 2>&1; then
  echo "Cannot connect to Docker. Start Docker or grant this user Docker access." >&2
  exit 1
fi

mkdir -p "${INSTALL_DIR}/configs"

UPDATE_TOKEN=""
ADMIN_PASSWORD=""
if [[ -f "${INSTALL_DIR}/.env" ]]; then
  UPDATE_TOKEN="$(sed -n 's/^ADAPTER_UPDATE_TOKEN=//p' "${INSTALL_DIR}/.env" | tail -n 1 | tr -d '\r')"
  ADMIN_PASSWORD="$(sed -n 's/^ADAPTER_ADMIN_PASSWORD=//p' "${INSTALL_DIR}/.env" | tail -n 1 | tr -d '\r')"
fi
if [[ -z "$UPDATE_TOKEN" ]]; then
  if command -v openssl >/dev/null 2>&1; then
    UPDATE_TOKEN="$(openssl rand -hex 32)"
  else
    UPDATE_TOKEN="$(od -An -N32 -tx1 /dev/urandom | tr -d ' \n')"
  fi
fi
if [[ -z "$ADMIN_PASSWORD" ]]; then
  if command -v openssl >/dev/null 2>&1; then
    ADMIN_PASSWORD="$(openssl rand -hex 16)"
  else
    ADMIN_PASSWORD="$(od -An -N16 -tx1 /dev/urandom | tr -d ' \n')"
  fi
fi

NO_PROXY_VALUE="localhost,127.0.0.1,::1"
cat > "${INSTALL_DIR}/.env" <<EOF
ADAPTER_LISTEN_ADDR=${LISTEN_ADDR}
ADAPTER_IMAGE=${IMAGE}
ADAPTER_VERSION=${VERSION}
ADAPTER_UPDATE_CHANNEL=${VERSION}
ADAPTER_UPDATE_TOKEN=${UPDATE_TOKEN}
ADAPTER_ADMIN_USERNAME=admin
ADAPTER_ADMIN_PASSWORD=${ADMIN_PASSWORD}
ADAPTER_CONFIG_DIR=./configs
ADAPTER_MEMORY_LIMIT=512m
UPDATER_MEMORY_LIMIT=256m
HTTP_PROXY=${PROXY_URL}
HTTPS_PROXY=${PROXY_URL}
NO_PROXY=${NO_PROXY_VALUE}
EOF
chmod 600 "${INSTALL_DIR}/.env"

cat > "${INSTALL_DIR}/docker-compose.yml" <<'COMPOSE'
services:
  moderation-adapter:
    image: ${ADAPTER_IMAGE:-614626370/sub2api-adapter}:${ADAPTER_VERSION:-latest}
    container_name: sub2api-moderation-adapter
    restart: unless-stopped
    network_mode: host
    environment:
      ADAPTER_CONFIG: /app/configs/config.json
      ADAPTER_LISTEN_ADDR: ${ADAPTER_LISTEN_ADDR:-127.0.0.1:18080}
      ADAPTER_DB: /app/data/adapter.db
      ADAPTER_UPDATE_URL: http://127.0.0.1:18081/v1/update
      ADAPTER_UPDATE_TOKEN: ${ADAPTER_UPDATE_TOKEN:?ADAPTER_UPDATE_TOKEN is required}
      ADAPTER_IMAGE: ${ADAPTER_IMAGE:-614626370/sub2api-adapter}
      ADAPTER_UPDATE_CHANNEL: ${ADAPTER_UPDATE_CHANNEL:-latest}
      ADAPTER_ADMIN_USERNAME: ${ADAPTER_ADMIN_USERNAME:-admin}
      ADAPTER_ADMIN_PASSWORD: ${ADAPTER_ADMIN_PASSWORD:?ADAPTER_ADMIN_PASSWORD is required}
    volumes:
      - ${ADAPTER_CONFIG_DIR:-./configs}:/app/configs:ro
      - adapter-data:/app/data
    labels:
      com.centurylinklabs.watchtower.enable: "true"
    read_only: true
    tmpfs:
      - /tmp:size=32m,mode=1777
    cap_drop:
      - ALL
    security_opt:
      - no-new-privileges:true
    pids_limit: 256
    mem_limit: ${ADAPTER_MEMORY_LIMIT:-512m}
    stop_grace_period: 15s
    healthcheck:
      test: ["CMD-SHELL", "wget -q -O /dev/null http://127.0.0.1:$${ADAPTER_LISTEN_ADDR##*:}/healthz"]
      interval: 30s
      timeout: 5s
      retries: 3
      start_period: 10s

  adapter-updater:
    image: nickfedor/watchtower:nightly@sha256:011cbd0246d247f8827a2624dd6202d8b0d1a3d8b9c9fc7937b427e37aa5f2c9
    container_name: sub2api-adapter-updater
    restart: unless-stopped
    ports:
      - "127.0.0.1:18081:8080"
    environment:
      WATCHTOWER_HTTP_API_UPDATE: "true"
      WATCHTOWER_HTTP_API_TOKEN: ${ADAPTER_UPDATE_TOKEN:?ADAPTER_UPDATE_TOKEN is required}
      WATCHTOWER_HTTP_API_PERIODIC_POLLS: "false"
      WATCHTOWER_LABEL_ENABLE: "true"
      WATCHTOWER_CLEANUP: "true"
      HTTP_PROXY: ${HTTP_PROXY:-}
      HTTPS_PROXY: ${HTTPS_PROXY:-}
      NO_PROXY: ${NO_PROXY:-localhost,127.0.0.1,::1}
      http_proxy: ${HTTP_PROXY:-}
      https_proxy: ${HTTPS_PROXY:-}
      no_proxy: ${NO_PROXY:-localhost,127.0.0.1,::1}
    volumes:
      - /var/run/docker.sock:/var/run/docker.sock
    read_only: true
    tmpfs:
      - /tmp:size=16m,mode=1777
    cap_drop:
      - ALL
    security_opt:
      - no-new-privileges:true
    pids_limit: 128
    mem_limit: ${UPDATER_MEMORY_LIMIT:-256m}

volumes:
  adapter-data:
COMPOSE

cd "${INSTALL_DIR}"
echo "Pulling Docker images..."
if ! "${COMPOSE[@]}" pull; then
  echo "Image pull failed. If this server needs a proxy, configure the Docker daemon proxy first." >&2
  exit 1
fi

echo "Starting sub2api Adapter..."
"${COMPOSE[@]}" up -d --remove-orphans

HEALTH_URL="http://127.0.0.1:${LISTEN_PORT}/healthz"
healthy=false
for _ in $(seq 1 30); do
  if command -v curl >/dev/null 2>&1; then
    if curl -fsS --max-time 3 "$HEALTH_URL" >/dev/null 2>&1; then healthy=true; break; fi
  elif command -v wget >/dev/null 2>&1; then
    if wget -q -T 3 -O /dev/null "$HEALTH_URL"; then healthy=true; break; fi
  else
    echo "Neither curl nor wget is installed; skipping the HTTP health check."
    healthy=true
    break
  fi
  sleep 1
done

"${COMPOSE[@]}" ps
if [[ "$healthy" != true ]]; then
  echo "Containers started, but ${HEALTH_URL} was not healthy within 30 seconds." >&2
  echo "Inspect logs: cd ${INSTALL_DIR} && ${COMPOSE[*]} logs --tail 100" >&2
  exit 1
fi

echo
echo "sub2api Adapter is ready."
echo "Install directory: ${INSTALL_DIR}"
echo "Image: ${IMAGE}:${VERSION}"
echo "Admin URL: http://${LISTEN_ADDR}/admin"
echo "Admin username: admin"
echo "Admin password: ${ADMIN_PASSWORD}"
echo "Online updates are available on the System Maintenance page."
