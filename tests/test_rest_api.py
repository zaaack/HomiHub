#!/usr/bin/env python3
"""REST API smoke tests for HomiHub (pure Python)."""
import os
import sys

sys.path.insert(0, os.path.dirname(__file__))
from harness import (caltoken, log, request, multipart_upload, TEST_HOST,
                     TEST_EMAIL, TEST_TEAM, start_server, stop_server, register_user, _open)

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

    # Recycle bin: the deleted file appears in trash and can be restored.
    trash = request("GET", "/api/v1/files/trash?scope=public")
    check("trash lists deleted file", any(x["id"] == file_id for x in trash["data"]))
    request("POST", f"/api/v1/files/trash/{file_id}/restore", {})
    trash2 = request("GET", "/api/v1/files/trash?scope=public")
    check("trash restore removes from trash", all(x["id"] != file_id for x in trash2["data"]))
    restored = request("GET", f"/api/v1/files?folderId={folder_id}&scope=public")
    check("trash restore brings file back", any(x["id"] == file_id for x in restored["data"]))

    # Trash retention setting (default 90, 0 = keep forever).
    st = request("GET", "/api/v1/settings/trash")
    check("trash settings default", st["data"]["days"] == 90)
    request("PUT", "/api/v1/settings/trash", {"days": 7})
    st = request("GET", "/api/v1/settings/trash")
    check("trash settings update", st["data"]["days"] == 7)

    # Permanent delete removes the file from trash and storage.
    ff2 = multipart_upload("/api/v1/files", "file", "purge.txt", b"purge me\n",
                           extra={"scope": "public"})
    file2 = ff2["data"]["id"]
    request("DELETE", f"/api/v1/files/{file2}")
    request("DELETE", f"/api/v1/files/trash/{file2}")
    trash3 = request("GET", "/api/v1/files/trash?scope=public")
    check("trash permanent delete removes file", all(x["id"] != file2 for x in trash3["data"]))
    try:
        request("GET", f"/api/v1/files/{file2}/content", raw=True)
        check("trash permanent delete removes content", False)
    except Exception:
        check("trash permanent delete removes content", True)

    # ------- Attachments -------
    log("=== Attachments ===")
    ev2 = request("POST", "/api/v1/events", {
        "title": "AttachEvent", "startsAt": "2026-09-11T09:00:00Z",
        "endsAt": "2026-09-11T10:00:00Z", "category": "family", "visibility": 3,
    })
    ev2_id = ev2["data"]["id"]
    check("attachment target event", ev2_id)

    att = multipart_upload("/api/v1/attachments", "file", "note.txt", b"attachment body\n",
                           extra={"kind": "event", "itemId": ev2_id})
    att_id = att["data"]["id"]
    check("attachment upload", att_id and att["data"]["fileId"])

    atts = request("GET", f"/api/v1/attachments?kind=event&itemId={ev2_id}")
    check("attachment list", len(atts["data"]) == 1 and atts["data"][0]["name"] == "note.txt")

    att_content = request("GET", f"/api/v1/files/{att['data']['fileId']}/content", raw=True)
    check("attachment content", att_content == b"attachment body\n")

    # Attachment management list carries reverse-lookup item info.
    manage = request("GET", "/api/v1/attachments/manage")
    check("attachment manage list",
          any(x["item"] and x["item"]["id"] == ev2_id and x["item"]["title"] == "AttachEvent"
              for x in manage["data"]))
    manage_ev = request("GET", "/api/v1/attachments/manage?kind=event")
    check("attachment manage kind filter", all(x["kind"] == "event" for x in manage_ev["data"]))

    # Attachment files are hidden from the Files page listing.
    fl = request("GET", "/api/v1/files?scope=public")
    check("attachment hidden from files", all(x["id"] != att["data"]["fileId"] for x in fl["data"]))

    # Deleting the owning item cascades the attachment: the item is gone, so
    # listing its attachments now 404s.
    request("DELETE", f"/api/v1/events/{ev2_id}")
    try:
        request("GET", f"/api/v1/attachments?kind=event&itemId={ev2_id}")
        check("attachment cascade delete", False)
    except Exception:
        check("attachment cascade delete", True)

    # ------- Team -------
    log("=== Team ===")
    team = request("GET", "/api/v1/team")
    check("team get", team["data"]["name"] == TEST_TEAM)

    members = request("GET", "/api/v1/team/members")
    check("team members", len(members["data"]) >= 1)

    import urllib.request
    feed_team = _open(urllib.request.Request(
        f"{TEST_HOST}/api/v1/calendar/team.ics?token={caltoken()}")).read().decode()
    check("ical team feed", "VCALENDAR" in feed_team)

    # Personal (self) feed is keyed by an App Password in the URL — no account.
    ap = request("POST", "/api/v1/auth/app-passwords", {"name": "ical test"})
    feed_self = _open(urllib.request.Request(
        f"{TEST_HOST}/api/v1/calendar/feed.ics?token={ap['data']['token']}")).read().decode()
    check("ical self feed", "VCALENDAR" in feed_self)

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