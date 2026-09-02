#!/usr/bin/env bash
# Shared helpers for HomiHub test suites.
set -euo pipefail

# Config
TEST_PORT="${TEST_PORT:-8091}"
TEST_HOST="http://127.0.0.1:${TEST_PORT}"
TEST_DATA="${TEST_DATA:-/tmp/opencode/homihub-test/data}"
TEST_BIN="${TEST_BIN:-/tmp/opencode/homihub-test-bin}"
TEST_EMAIL="${TEST_EMAIL:-parent@test.local}"
TEST_PASSWORD="${TEST_PASSWORD:-pass1234}"
TEST_TEAM="${TEST_TEAM:-Test Family}"
PROJECT_DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"

log() { printf '[%s] %s\n' "$(date +%H:%M:%S)" "$*"; }

# start_server builds (if needed) and launches a fresh HomiHub on TEST_PORT.
start_server() {
  if [ ! -x "${TEST_BIN}" ]; then
    log "building backend binary..."
    (cd "${PROJECT_DIR}/backend" && go build -o "${TEST_BIN}" .)
  fi
  rm -rf "${TEST_DATA}"
  mkdir -p "${TEST_DATA}"
  log "starting server on :${TEST_PORT}"
  HOMIHUB_NO_STATIC=1 \
  PORT="${TEST_PORT}" \
  HOMIHUB_DATA_DIR="${TEST_DATA}" \
  DB_DSN="${TEST_DATA}/homihub.db" \
  STORAGE_LOCAL_DIR="${TEST_DATA}/storage" \
  JWT_SECRET=test-secret \
  "${TEST_BIN}" >"${TEST_DATA}/server.log" 2>&1 &
  SERVER_PID=$!
  for _ in $(seq 1 50); do
    if curl -sf "${TEST_HOST}/health" >/dev/null 2>&1; then break; fi
    sleep 0.2
  done
  curl -sf "${TEST_HOST}/health" >/dev/null || { log "server failed to start"; cat "${TEST_DATA}/server.log"; exit 1; }
  log "server up (pid ${SERVER_PID})"
}

stop_server() {
  if [ -n "${SERVER_PID:-}" ]; then kill "${SERVER_PID}" 2>/dev/null || true; fi
}

# ensure_user registers the test user (idempotent) and sets TEST_TOKEN/TEST_CALTOKEN.
ensure_user() {
  log "registering user ${TEST_EMAIL}..."
  local reg
  reg="$(curl -sf -X POST "${TEST_HOST}/api/v1/auth/register" \
    -H 'Content-Type: application/json' \
    -d "{\"name\":\"Test Parent\",\"email\":\"${TEST_EMAIL}\",\"password\":\"${TEST_PASSWORD}\",\"teamName\":\"${TEST_TEAM}\"}")"
  TEST_TOKEN="$(printf '%s' "${reg}" | python3 -c "import sys,json;print(json.load(sys.stdin)['data']['token'])")"
  TEST_CALTOKEN="$(curl -sf "${TEST_HOST}/api/v1/team" -H "Authorization: Bearer ${TEST_TOKEN}" \
    | python3 -c "import sys,json;print(json.load(sys.stdin)['data']['calendarToken'])")"
  log "token=${TEST_TOKEN:0:8}... caltoken=${TEST_CALTOKEN}"
}

# run_suite cleans proxy env (python http libs pick up local proxy and break).
no_proxy_env() {
  env -u http_proxy -u https_proxy -u HTTP_PROXY -u HTTPS_PROXY \
      -u all_proxy -u ALL_PROXY -u socks_proxy -u SOCKS_PROXY "$@"
}
