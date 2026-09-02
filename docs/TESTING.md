# 测试计划

## 测试脚本

- `tests/run_tests.py [rest|caldav|webdav|all]` — 统一入口，逐套件拉起服务器并运行（`uv run --with caldav-server-tester python tests/run_tests.py all`）
- `tests/run_caldav_tester.sh` — 运行 caldav-server-tester 对 CalDAV 端点做合规性检查
- `tests/run_litmus.sh` — 运行 litmus 对 WebDAV 端点做合规性检查
- `tests/run_rest_api.sh` — 对 REST API 做端到端冒烟测试

## 测试结果

### WebDAV（litmus，`-k` 模式）

WebDAV 后端基于 `golang.org/x/net/webdav`（官方参考实现）。对照 `/dav/files/public/`，用户 `parent@test.local` / `pass1234`：

| 套件 | 结果 | 说明 |
|---|---|---|
| basic | 16/16 (100%) | |
| copymove | 13/13 (100%) | 集合递归 COPY/MOVE |
| props | 29/30 (96.7%) | 见已知失败 |
| locks | 32/34 (94.1%) | 见已知失败 |
| http | 4/4 (100%) | |

与 x/net 官方参考（MemFS）结果一致（MemFS 同样 29/30、32/34），属于 x/net 上游自身行为，非本项目缺陷：

- `props.propfind_invalid2` — 非法 namespace 的 PROPFIND 返回 207 而非 400（x/net 上游如此）
- `locks.fail_complex_cond_put` — 复杂条件 PUT 未按 412 拒绝
- `locks.lock_shared` — 共享锁（LOCK scope=shared）返回 501（x/net 未实现共享锁）

### CalDAV（caldav-server-tester）

PASS（具体用例见 `tests/test_caldav.py`）。

### REST API

PASS（冒烟测试见 `tests/test_rest_api.py`）。
