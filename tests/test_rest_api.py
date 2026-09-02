#!/usr/bin/env python3
"""REST API smoke tests for HomiHub (pure Python)."""
import os
import sys

sys.path.insert(0, os.path.dirname(__file__))
from harness import (caltoken, log, request, multipart_upload, TEST_HOST,
                     TEST_EMAIL, TEST_TEAM, start_server, stop_server, register_user)

failures = 0


def check(name, ok):
    global failures
    print(f"PASS {name}" if ok else f"FAIL {name}: {ok}")
    if not ok:
        failures += 1


def run():
    # ------- Auth -------
    log("=== Auth ===")
    me = request("GET", "/api/v1/auth/me")
    check("auth/me", me["data"]["user"]["email"] == TEST_EMAIL)

    # ------- Calendar Events -------
    log("=== Calendar Events ===")
    ev = request("POST", "/api/v1/events", {
        "title": "TestEvent", "startsAt": "2026-09-10T09:00:00Z",
        "endsAt": "2026-09-10T10:00:00Z", "category": "family", "visibility": 3,
    })
    ev_id = ev["data"]["id"]
    check("event create", ev_id)

    ev_list = request("GET", "/api/v1/events?from=2026-09-01T00:00:00Z&to=2026-09-30T00:00:00Z")
    check("event list", len(ev_list["data"]) == 1)

    request("PUT", f"/api/v1/events/{ev_id}", {
        "title": "UpdatedEvent", "startsAt": "2026-09-10T11:00:00Z",
        "endsAt": "2026-09-10T12:00:00Z", "category": "family", "visibility": 3,
    })
    check("event update", True)

    request("DELETE", f"/api/v1/events/{ev_id}")
    check("event delete", True)

    # ------- Todos -------
    log("=== Todos ===")
    td = request("POST", "/api/v1/todos", {"title": "TestTodo", "priority": 1})
    td_id = td["data"]["id"]
    check("todo create", td_id)

    td_list = request("GET", "/api/v1/todos")
    check("todo list", len(td_list["data"]) >= 1)

    request("PUT", f"/api/v1/todos/{td_id}", {"title": "UpdatedTodo"})
    check("todo update", True)

    request("PATCH", f"/api/v1/todos/{td_id}/toggle")
    check("todo toggle", True)

    request("DELETE", f"/api/v1/todos/{td_id}")
    check("todo delete", True)

    # ------- Files -------
    log("=== Files ===")
    folder = request("POST", "/api/v1/files/folders", {"name": "TestFolder", "scope": "public"})
    folder_id = folder["data"]["id"]
    check("folder create", folder_id)

    ff = multipart_upload("/api/v1/files", "file", "fixture.txt", b"# test fixture\n",
                          extra={"folderId": folder_id, "scope": "public"})
    file_id = ff["data"]["id"]
    check("file upload", file_id)

    request("GET", f"/api/v1/files?folderId={folder_id}&scope=public")
    check("file list", True)

    content = request("GET", f"/api/v1/files/{file_id}/content", raw=True)
    check("file content", content == b"# test fixture\n")

    request("DELETE", f"/api/v1/files/{file_id}")
    check("file delete", True)

    request("DELETE", f"/api/v1/files/folders/{folder_id}")
    check("folder delete", True)

    # ------- Team -------
    log("=== Team ===")
    team = request("GET", "/api/v1/team")
    check("team get", team["data"]["name"] == TEST_TEAM)

    members = request("GET", "/api/v1/team/members")
    check("team members", len(members["data"]) >= 1)

    import urllib.request
    feed = urllib.request.urlopen(
        f"{TEST_HOST}/api/v1/calendar/feed.ics?token={caltoken()}").read().decode()
    check("ical feed", "VCALENDAR" in feed)

    # ------- Health -------
    check("health", request("GET", "/health")["status"] == "ok")

    print(f"=== Results: {failures} failures ===")
    return failures


def main():
    start_server()
    try:
        register_user()
        return run()
    finally:
        stop_server()


if __name__ == "__main__":
    sys.exit(main())