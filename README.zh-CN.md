# HomiHub

一站式个人中心 —— 日历、待办、文件、团队、设置，全部打包在一个单二进制中。

## 功能

- **日历** — 兼容 CalDAV（RFC 4791），支持 RFC 6578 sync-token 增量同步、重复事件例外（Master + Exception 共用一个 .ics 资源）、大小写不敏感搜索。可与 Apple 日历、Google 日历、Nextcloud、Outlook 配合使用。
- **文件** — 兼容 WebDAV（RFC 4918）的个人文件存储。后端基于 `go-webdavp`（`golang.org/x/net/webdav` 的并发 fork），对目录树 COPY/MOVE/PROPFIND 等批量操作做了并行化改造。
- **待办** — 简单的个人待办列表。
- **团队** — 多人家庭团队，支持邀请/加入流程。
- **设置** — 用户级配置。
- **国际化** — 简体中文与英文界面。

## 技术栈

| 层     | 技术                                                              |
|--------|-----------------------------------------------------------------|
| 后端   | Go 1.26, Gin, GORM, SQLite                                     |
| 前端   | React 19, Vite, TailwindCSS v4, TypeScript, Zustand, react-i18next |
| 输出   | 单静态二进制，`./build.sh` → `./homihub`                        |

## 快速开始

依赖：Go ≥ 1.26、Node.js + pnpm。

```bash
./build.sh          # 构建前端 + 后端，输出 ./homihub
./homihub           # 运行，默认端口 :8080
```

打开 http://localhost:8080 注册第一个用户即可。

> `vendor/` 目录内的 fork 模块被 gitignore，`build.sh` 在缺失时会自动 clone。

## 开发

```bash
# 前端开发服务器（Vite 代理 /api、/dav、/.well-known → :8099）
cd frontend && pnpm install && pnpm dev

# 后端单独运行
cd backend && go run .
```

## 配置

全部通过环境变量配置：

| 变量                | 默认值                              | 说明                              |
|---------------------|--------------------------------------|-----------------------------------|
| `PORT`              | `8080`                               | HTTP 监听端口                     |
| `HOMIHUB_DATA_DIR`  | `./data`                             | 数据目录                          |
| `DB_DRIVER`         | `sqlite`                             | 数据库驱动                        |
| `DB_DSN`            | `{DATA_DIR}/db/homihub.db`           | SQLite 数据库文件路径             |
| `STORAGE_LOCAL_DIR` | `{DATA_DIR}/storage`                 | 文件存储目录                      |
| `JWT_SECRET`        | `dev-secret-change-me`               | JWT 签名密钥（生产环境务必修改！）|
| `CORS_ORIGINS`      | (空)                                 | 允许的 CORS 域名                  |
| `CALENDAR_TZ`       | `UTC`                                | 日历时区                          |
| `HOMIHUB_NO_STATIC` | (未设置)                             | 设置后不提供前端 UI              |

## 目录结构

```
backend/   Go 后端（模块：auth、calendar、files、settings、team、todos）
frontend/  React SPA（src/pages、src/i18n/locales/{en,zh}.json）
tests/     REST / CalDAV / WebDAV 测试脚本
docs/      CALDAV.md、TESTING.md
build.sh   构建单二进制
```

## 测试

详见 `docs/TESTING.md`。关键结果：

- **CalDAV** — 通过 `caldav-server-tester`（sync-token、递归例外、大小写不敏感搜索）。
- **WebDAV** — 通过 litmus：basic/copymove/http 100%、props 96.7%、locks 94.1%。剩余失败项是上游 `x/net/webdav` 的行为（`propfind_invalid2`、`fail_complex_cond_put`、`lock_shared`），非本项目缺陷。
- **REST** — 端到端冒烟测试通过。

## 许可证

开源版本使用 AGPL-3.0。商业许可请联系 [zaaack@qq.com](mailto:zaaack@qq.com)。