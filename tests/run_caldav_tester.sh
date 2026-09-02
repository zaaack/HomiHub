#!/usr/bin/env bash
# Run caldav-server-tester against the HomiHub CalDAV endpoint.
set -euo pipefail
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${DIR}/lib.sh"

start_server
trap stop_server EXIT
ensure_user

if ! command -v caldav-server-tester >/dev/null 2>&1; then
  log "installing caldav-server-tester..."
  uv tool install caldav-server-tester
fi

log "running caldav-server-tester against ${TEST_HOST}/dav ..."
no_proxy_env caldav-server-tester \
  --caldav-url "${TEST_HOST}/dav" \
  --caldav-username "${TEST_EMAIL}" \
  --caldav-password "${TEST_CALTOKEN}" \
  --caldav-calendar "我的" \
  --format text "$@"
log "caldav-server-tester finished."
