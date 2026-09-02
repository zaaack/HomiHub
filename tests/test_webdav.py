#!/usr/bin/env python3
"""Run litmus against the HomiHub WebDAV endpoint (pure Python)."""
import os
import shutil
import subprocess
import sys

sys.path.insert(0, os.path.dirname(__file__))
from harness import stop_server, start_server, clean_env, register_user, TEST_HOST, TEST_EMAIL, caltoken, log


def main():
    cmd = shutil.which("litmus")
    if cmd is None:
        log("ERR litmus not installed (apt install litmus)")
        return 1
    webdav_root = os.environ.get("WEBDAV_ROOT", "/dav/files/public/")
    start_server()
    try:
        register_user()
        log(f"running litmus against {TEST_HOST}{webdav_root} ...")
        proc = subprocess.run(
            [cmd, "-k", f"{TEST_HOST}{webdav_root}", TEST_EMAIL, caltoken()],
            env=clean_env(),
        )
        log(f"litmus finished (exit {proc.returncode})")
        return proc.returncode
    finally:
        stop_server()


if __name__ == "__main__":
    sys.exit(main())