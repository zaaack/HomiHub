#!/usr/bin/env python3
"""Run all HomiHub test suites (pure Python, requires uv-installed tools).

Usage:
  uv run --with caldav-server-tester python tests/run_tests.py [rest|caldav|webdav|all]

Server lifecycle is managed per-suite by each test module.
caldav-server-tester must be installed (uv tool install caldav-server-tester).
litmus must be installed (apt install litmus).
"""
import importlib
import sys

sys.path.insert(0, os.path.dirname(__file__))

MODULES = {
    "rest": "test_rest_api",
    "caldav": "test_caldav",
    "webdav": "test_webdav",
}


def main():
    suites = sys.argv[1:] or ["all"]
    if "all" in suites:
        suites = ["rest", "caldav", "webdav"]
    unknown = [s for s in suites if s not in MODULES]
    if unknown:
        print(f"unknown suite(s): {unknown}; expected one of {list(MODULES)}")
        return 2

    results = {}
    for suite in suites:
        print(f"\n{'='*60}\nRUN {suite}\n{'='*60}", flush=True)
        module = importlib.import_module(MODULES[suite])
        rc = module.main()
        results[suite] = rc

    print("\n" + "=" * 60)
    for suite, rc in results.items():
        print(f"{'PASS' if rc == 0 else 'FAIL'} {suite} (exit {rc})")
    return 1 if any(rc != 0 for rc in results.values()) else 0


if __name__ == "__main__":
    sys.exit(main())