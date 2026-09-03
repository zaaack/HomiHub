#!/usr/bin/env bash
# REST API smoke tests for HomiHub.
set -euo pipefail
DIR="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
source "${DIR}/lib.sh"

start_server
trap stop_server EXIT
ensure_user

failures=0
pass() { log "PASS $1"; }
fail() { log "FAIL $1: $2"; failures=$((failures+1)); }

# Helpers
api() { curl -sf -H "Authorization: Bearer ${TEST_TOKEN}" "$@"; }
api_noauth() { curl -sf "$@"; }

# ------- Auth -------
log "=== Auth ==="
# Can GET /auth/me
ME=$(api "${TEST_HOST}/api/v1/auth/me")
if printf '%s' "$ME" | python3 -c "import sys,json;d=json.load(sys.stdin)['data'];assert d['user']['email']=='${TEST_EMAIL}'" 2>/dev/null; then
  pass "auth/me"
else
  fail "auth/me" "unexpected response"
fi

# ------- Calendar Events -------
log "=== Calendar Events ==="
# Create event
EV=$(api -X POST "${TEST_HOST}/api/v1/events" \
  -H 'Content-Type: application/json' \
  -d '{"title":"TestEvent","startsAt":"2026-09-10T09:00:00Z","endsAt":"2026-09-10T10:00:00Z","category":"family","visibility":3}')
EV_ID=$(printf '%s' "$EV" | python3 -c "import sys,json;print(json.load(sys.stdin)['data']['id'])" 2>/dev/null)
if [ -n "$EV_ID" ]; then pass "event create"; else fail "event create" "no id"; fi

# List events
EV_LIST=$(api "${TEST_HOST}/api/v1/events?from=2026-09-01T00:00:00Z&to=2026-09-30T00:00:00Z")
if printf '%s' "$EV_LIST" | python3 -c "import sys,json;d=json.load(sys.stdin)['data'];assert len(d)==1" 2>/dev/null; then
  pass "event list"
else
  fail "event list" "unexpected count"
fi

# Update event
api -X PUT "${TEST_HOST}/api/v1/events/${EV_ID}" \
  -H 'Content-Type: application/json' \
  -d '{"title":"UpdatedEvent","startsAt":"2026-09-10T11:00:00Z","endsAt":"2026-09-10T12:00:00Z","category":"family","visibility":3}' >/dev/null 2>&1 && pass "event update" || fail "event update" ""

# Delete event
api -X DELETE "${TEST_HOST}/api/v1/events/${EV_ID}" >/dev/null 2>&1 && pass "event delete" || fail "event delete" ""

# ------- Todos -------
log "=== Todos ==="
# Create todo
TD=$(api -X POST "${TEST_HOST}/api/v1/todos" \
  -H 'Content-Type: application/json' \
  -d '{"title":"TestTodo","priority":1}')
TD_ID=$(printf '%s' "$TD" | python3 -c "import sys,json;print(json.load(sys.stdin)['data']['id'])" 2>/dev/null)
if [ -n "$TD_ID" ]; then pass "todo create"; else fail "todo create" "no id"; fi

# List todos
TD_LIST=$(api "${TEST_HOST}/api/v1/todos")
if printf '%s' "$TD_LIST" | python3 -c "import sys,json;d=json.load(sys.stdin)['data'];assert len(d)>=1" 2>/dev/null; then
  pass "todo list"
else
  fail "todo list" "unexpected"
fi

# Update
api -X PUT "${TEST_HOST}/api/v1/todos/${TD_ID}" \
  -H 'Content-Type: application/json' \
  -d '{"title":"UpdatedTodo"}' >/dev/null 2>&1 && pass "todo update" || fail "todo update" ""

# Toggle
api -X PATCH "${TEST_HOST}/api/v1/todos/${TD_ID}/toggle" >/dev/null 2>&1 && pass "todo toggle" || fail "todo toggle" ""

# Delete
api -X DELETE "${TEST_HOST}/api/v1/todos/${TD_ID}" >/dev/null 2>&1 && pass "todo delete" || fail "todo delete" ""

# ------- Files -------
log "=== Files ==="
# Create folder
FOLDER=$(api -X POST "${TEST_HOST}/api/v1/files/folders" \
  -H 'Content-Type: application/json' \
  -d '{"name":"TestFolder","scope":"public"}')
FOLDER_ID=$(printf '%s' "$FOLDER" | python3 -c "import sys,json;print(json.load(sys.stdin)['data']['id'])" 2>/dev/null)
if [ -n "$FOLDER_ID" ]; then pass "folder create"; else fail "folder create" "no id"; fi

# Upload file
FF=$(api -X POST "${TEST_HOST}/api/v1/files" \
  -H 'Content-Type: multipart/form-data' \
  -F "file=@${DIR}/lib.sh" -F "folderId=${FOLDER_ID}" -F "scope=public")
FILE_ID=$(printf '%s' "$FF" | python3 -c "import sys,json;print(json.load(sys.stdin)['data']['id'])" 2>/dev/null)
if [ -n "$FILE_ID" ]; then pass "file upload"; else fail "file upload" "no id"; fi

# List files
api "${TEST_HOST}/api/v1/files?folderId=${FOLDER_ID}&scope=public" >/dev/null 2>&1 && pass "file list" || fail "file list" ""

# Get file content
api "${TEST_HOST}/api/v1/files/${FILE_ID}/content" -o /dev/null >/dev/null 2>&1 && pass "file content" || fail "file content" ""

# Delete file
api -X DELETE "${TEST_HOST}/api/v1/files/${FILE_ID}" >/dev/null 2>&1 && pass "file delete" || fail "file delete" ""

# Delete folder
api -X DELETE "${TEST_HOST}/api/v1/files/folders/${FOLDER_ID}" >/dev/null 2>&1 && pass "folder delete" || fail "folder delete" ""

# ------- Team -------
log "=== Team ==="
TEAM=$(api "${TEST_HOST}/api/v1/team")
if printf '%s' "$TEAM" | python3 -c "import sys,json;d=json.load(sys.stdin)['data'];assert d['name']=='${TEST_TEAM}'" 2>/dev/null; then
  pass "team get"
else
  fail "team get" "unexpected"
fi

# Members
MEMBERS=$(api "${TEST_HOST}/api/v1/team/members")
if printf '%s' "$MEMBERS" | python3 -c "import sys,json;d=json.load(sys.stdin)['data'];assert len(d)>=1" 2>/dev/null; then
  pass "team members"
else
  fail "team members" "unexpected"
fi

# iCal team feed (calendar token)
FEED=$(api_noauth "${TEST_HOST}/api/v1/calendar/team.ics?token=${TEST_CALTOKEN}")
if printf '%s' "$FEED" | grep -q "VCALENDAR"; then
  pass "ical team feed"
else
  fail "ical team feed" "no VCALENDAR"
fi

# Personal (self) feed uses an App Password as the URL token — no account
AP=$(api -X POST "${TEST_HOST}/api/v1/auth/app-passwords" -H 'Content-Type: application/json' -d '{"name":"ical test"}')
AP_TOKEN=$(printf '%s' "$AP" | python3 -c "import sys,json;print(json.load(sys.stdin)['data']['token'])" 2>/dev/null)
FEED_SELF=$(api_noauth "${TEST_HOST}/api/v1/calendar/feed.ics?token=${AP_TOKEN}")
if printf '%s' "$FEED_SELF" | grep -q "VCALENDAR"; then
  pass "ical self feed"
else
  fail "ical self feed" "no VCALENDAR"
fi

# ------- Health -------
api_noauth "${TEST_HOST}/health" | grep -q "ok" && pass "health" || fail "health" ""

# Summary
log "=== Results: ${failures} failures ==="
exit $failures