#!/usr/bin/env bash
# Run litmus against the HomiHub WebDAV endpoint.
set -euo pipefail
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${DIR}/lib.sh"

start_server
trap stop_server EXIT
ensure_user

if ! command -v litmus >/dev/null 2>&1; then
  log "ERROR: litmus not installed (apt install litmus)"
  exit 1
fi

WEBDAV_ROOT="${WEBDAV_ROOT:-/dav/files/public/}"
log "running litmus against ${TEST_HOST}${WEBDAV_ROOT} ..."
litmus -k "${TEST_HOST}${WEBDAV_ROOT}" "${TEST_EMAIL}" "${TEST_CALTOKEN}"
log "litmus finished."
