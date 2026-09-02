#!/usr/bin/env python3
"""Run caldav-server-tester against HomiHub and validate observed deviations.

Exit 0 when every observed deviation is in the known allowlist below, 1 otherwise.
"""
import json
import os
import shutil
import subprocess
import sys

sys.path.insert(0, os.path.dirname(__file__))
from harness import stop_server, start_server, clean_env, register_user, TEST_HOST, TEST_EMAIL, caltoken, log

# Known deviations (observed support level != standard default). Each entry is
# (feature, reason). Features not listed here will fail the run.
# These are all library/protocol limits or deliberate design decisions:
#  - go-webdav does not implement free-busy/principal-search/scheduling
#  - is-not-defined optional-ish per RFC4791
#  - recurrences.expanded or alarm time-range matching not needed by our clients
#  - MKCALENDAR is only RECOMMENDED (create-calendar.auto observed full = we aut-create)
#  - sync-token is implemented via a custom wrapper (sync.go); removed from allowlist
#  - case-insensitive text-match (i;ascii-casemap) implemented via a local go-webdav fork (vendor/go-webdav)
KNOWN_DEVIATIONS = {
    "create-calendar": "MKCALENDAR is only RECOMMENDED by RFC4791; we expose a pre-created per-user calendar",
    # observed 'full' while spec default is 'unsupported': harmless, we do auto-create
    "create-calendar.auto": "creating a calendar on demand is supported (bonus)",
    "save-load.journal": "VJOURNAL rejected by design (409 precondition)",
    "search.recurrences.expanded": "expand attribute not implemented (python-caldav expands client-side anyway)",
    "search.recurrences.expanded.event": "expand attribute not implemented (python-caldav expands client-side anyway)",
    "search.recurrences.expanded.todo": "expand attribute not implemented (python-caldav expands client-side anyway)",
    "search.recurrences.expanded.exception": "expand attribute not implemented (python-caldav expands client-side anyway)",
    "search.comp-type.optional": "comp-type omitted search yields unexpected result set; the tester itself marks this inconclusive (TODO in its source)",
    "freebusy-query": "go-webdav library does not implement free-busy-query REPORT",
    "principal-search": "go-webdav library does not implement principal-property/search REPORTs",
    "principal-search.by-name.self": "go-webdav library does not implement principal-property/search REPORTs",
    "principal-search.list-all": "go-webdav library does not implement principal-property/search REPORTs",
    "scheduling": "CalDAV scheduling (RFC6638) not implemented by go-webdav library",
    "scheduling.mailbox": "CalDAV scheduling (RFC6638) not implemented by go-webdav library",
    "scheduling.mailbox.inbox-delivery": "CalDAV scheduling (RFC6638) not implemented by go-webdav library",
    "scheduling.calendar-user-address-set": "CalDAV scheduling (RFC6638) not implemented by go-webdav library",
    "scheduling.schedule-tag": "CalDAV scheduling (RFC6638) not implemented by go-webdav library",
    "scheduling.auto-schedule": "CalDAV scheduling (RFC6638) not implemented by go-webdav library",
    "scheduling.freebusy-query": "CalDAV scheduling (RFC6638) not implemented by go-webdav library",
}


def _runner():
    """Return a command list that runs the tool-installed caldav-server-tester.

    Prefer the installed tool entry point (~/.local/bin/caldav-server-tester →
    the tool venv which includes `vobject`). Do NOT use `uv tool run`: it
    resolves to a cache archive venv WITHOUT vobject, which makes
    vobject_instance checks misreport (e.g. save-load.event.timezone appears
    'broken').
    """
    candidates = (
        os.path.expanduser("~/.local/bin/caldav-server-tester"),
        shutil.which("caldav-server-tester"),
    )
    for c in candidates:
        if c and os.path.isfile(c):
            return [c]
    raise SystemExit("caldav-server-tester not found; install with: uv tool install caldav-server-tester")


def main():
    cmd = _runner()
    if cmd is None:
        return 1
    start_server()
    try:
        register_user()
        log(f"running caldav-server-tester against {TEST_HOST}/dav ...")
        proc = subprocess.run(
            cmd + ["--caldav-url", f"{TEST_HOST}/dav", "--caldav-username", TEST_EMAIL,
                   "--caldav-password", caltoken(), "--caldav-calendar", "我的", "--format", "json"],
            capture_output=True, text=True, env=clean_env(),
        )
        try:
            report = json.loads(proc.stdout)
        except json.JSONDecodeError:
            print(proc.stdout)
            print(proc.stderr)
            log(f"ERR caldav-server-tester produced no JSON report (exit {proc.returncode})")
            return 1

        deviations = report.get("features", {})
        # 'unknown' support means the tester could not determine/run the check
        # (e.g. delete-calendar needs create-calendar first). Not a failure.
        unknown_ok = {f: info for f, info in deviations.items() if info.get("support") == "unknown"}
        real = {f: info for f, info in deviations.items() if info.get("support") != "unknown"}
        unexpected = {f: info for f, info in real.items() if f not in KNOWN_DEVIATIONS}
        if unexpected:
            log(f"FAIL: {len(unexpected)} unexpected deviation(s):")
            for feature, info in sorted(unexpected.items()):
                print(f"  {feature}: {info}")
            return 1
        if real:
            log(f"OK: all {len(real)} observed deviations are known/accepted")
            for feature in sorted(real):
                print(f"  ~ {feature}: {KNOWN_DEVIATIONS[feature]}")
        else:
            log("OK: no deviations from CalDAV spec defaults")
        if unknown_ok:
            log(f"note: {len(unknown_ok)} feature(s) could not be tested (support unknown)")
        return 0
    finally:
        stop_server()


if __name__ == "__main__":
    sys.exit(main())