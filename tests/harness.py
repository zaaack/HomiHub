#!/usr/bin/env python3
"""Shared helpers for the HomiHub test suites (pure Python)."""
import json
import os
import shutil
import subprocess
import time
import urllib.error
import urllib.request

TEST_PORT = int(os.environ.get("TEST_PORT", "8091"))
TEST_HOST = f"http://127.0.0.1:{TEST_PORT}"
TEST_DATA = os.environ.get("TEST_DATA", "/tmp/opencode/homihub-test/data")
TEST_BIN = os.environ.get("TEST_BIN", "/tmp/opencode/homihub-test-bin")
TEST_EMAIL = os.environ.get("TEST_EMAIL", "parent@test.local")
TEST_PASSWORD = os.environ.get("TEST_PASSWORD", "pass1234")
TEST_TEAM = os.environ.get("TEST_TEAM", "Test Family")
PROJECT_DIR = os.path.realpath(os.path.join(os.path.dirname(__file__), ".."))

_token = None
_caltoken = None
_server = None


def log(msg):
    print(f"[{time.strftime('%H:%M:%S')}] {msg}", flush=True)


def clean_env():
    """Return env with proxy vars removed (local proxy breaks python http libs)."""
    env = os.environ.copy()
    for k in ("http_proxy", "https_proxy", "HTTP_PROXY", "HTTPS_PROXY",
              "all_proxy", "ALL_PROXY", "socks_proxy", "SOCKS_PROXY"):
        env.pop(k, None)
    return env


def build_binary():
    if os.path.isfile(TEST_BIN) and os.access(TEST_BIN, os.X_OK):
        return
    log("building backend binary...")
    subprocess.run(["go", "build", "-o", TEST_BIN, "."], cwd=os.path.join(PROJECT_DIR, "backend"), check=True)


def start_server():
    global _server
    build_binary()
    shutil.rmtree(TEST_DATA, ignore_errors=True)
    os.makedirs(TEST_DATA, exist_ok=True)
    log(f"starting server on :{TEST_PORT}")
    env = clean_env()
    env.update({
        "HOMIHUB_NO_STATIC": "1",
        "PORT": str(TEST_PORT),
        "HOMIHUB_DATA_DIR": TEST_DATA,
        "DB_DSN": os.path.join(TEST_DATA, "homihub.db"),
        "STORAGE_LOCAL_DIR": os.path.join(TEST_DATA, "storage"),
        "JWT_SECRET": "test-secret",
    })
    with open(os.path.join(TEST_DATA, "server.log"), "wb") as fh:
        _server = subprocess.Popen([TEST_BIN], stdout=fh, stderr=subprocess.STDOUT, env=env)
    for _ in range(80):
        try:
            with urllib.request.urlopen(f"{TEST_HOST}/health", timeout=2) as r:
                if r.status == 200:
                    log(f"server up (pid {_server.pid})")
                    return
        except Exception:
            pass
        time.sleep(0.25)
    log("server failed to start; server.log:")
    with open(os.path.join(TEST_DATA, "server.log")) as fh:
        print(fh.read())
    raise SystemExit(1)


def stop_server():
    global _server
    if _server is not None and _server.poll() is None:
        _server.terminate()
        try:
            _server.wait(timeout=10)
        except subprocess.TimeoutExpired:
            _server.kill()
    _server = None


def register_user():
    """Register the test user (idempotent) and cache token + calendar token."""
    global _token, _caltoken
    log(f"registering user {TEST_EMAIL}...")
    reg = request("POST", "/api/v1/auth/register",
                  body={"name": "Test Parent", "email": TEST_EMAIL,
                        "password": TEST_PASSWORD, "teamName": TEST_TEAM})
    _token = reg["data"]["token"]
    team = request("GET", "/api/v1/team")
    _caltoken = team["data"]["calendarToken"]
    log(f"token={_token[:8]}... caltoken={_caltoken}")


def request(method, path, body=None, token=None, raw=False):
    """HTTP helper returning parsed JSON (raw=True returns bytes)."""
    if token is None and path.startswith("/api/"):
        token = _token
    req = urllib.request.Request(f"{TEST_HOST}{path}", method=method)
    if token:
        req.add_header("Authorization", f"Bearer {token}")
    if body is not None:
        req.add_header("Content-Type", "application/json")
        req.data = json.dumps(body).encode()
    with urllib.request.urlopen(req, timeout=15) as resp:
        data = resp.read()
        return data if raw else json.loads(data)


def multipart_upload(path, filefield, filename, filedata, token=None, extra=None):
    """POST multipart/form-data to an endpoint, returning parsed JSON."""
    boundary = "----homihubtestboundary"
    parts = []
    parts.append(f"--{boundary}\r\n"
                 f'Content-Disposition: form-data; name="{filefield}"; filename="{filename}"\r\n'
                 f"Content-Type: text/plain\r\n\r\n".encode() + filedata + b"\r\n")
    for name, value in (extra or {}).items():
        parts.append(f"--{boundary}\r\n"
                     f'Content-Disposition: form-data; name="{name}"\r\n\r\n'
                     f"{value}\r\n".encode())
    parts.append(f"--{boundary}--\r\n".encode())
    req = urllib.request.Request(f"{TEST_HOST}{path}", method="POST", data=b"".join(parts))
    req.add_header("Content-Type", f"multipart/form-data; boundary={boundary}")
    if token or (_token and path.startswith("/api/")):
        req.add_header("Authorization", f"Bearer {token or _token}")
    with urllib.request.urlopen(req, timeout=30) as resp:
        return json.loads(resp.read())


def token():
    return _token


def caltoken():
    return _caltoken