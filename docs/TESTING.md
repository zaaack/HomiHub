# 测试计划

## TODO

- [ ] 使用 https://github.com/python-caldav/caldav-server-tester 测试 CalDAV 的 URL
- [ ] 使用 litmus 测试 WebDAV server
- [ ] 编写测试脚本测试 REST API

## 测试脚本

- `tests/run_caldav_tester.sh` — 运行 caldav-server-tester 对 CalDAV 端点做合规性检查
- `tests/run_litmus.sh` — 运行 litmus 对 WebDAV 端点做合规性检查
- `tests/run_rest_api.sh` — 对 REST API 做端到端冒烟测试
