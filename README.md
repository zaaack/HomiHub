# HomiHub

A self-contained personal hub — calendar, todos, files, team, and settings in a
single binary.

## Features

- **Calendar** — CalDAV-compliant (RFC 4791) with RFC 6578 sync-token support,
  recurring-event exceptions (master + exception in one `.ics`), and
  case-insensitive search. Works with Apple Calendar, Google Calendar,
  Nextcloud, and Outlook.
- **Files** — WebDAV-compliant (RFC 4918) personal file storage. Backed by
  `go-webdavp`, a parallel fork of `golang.org/x/net/webdav` with concurrent
  batch operations (COPY/MOVE/PROPFIND on large trees).
- **Todos** — simple personal task list.
- **Team** — multi-member personal team with an invite/join flow.
- **Settings** — per-user configuration.
- **i18n** — English and Chinese (Simplified) UI.

## Tech Stack

| Layer    | Stack                                                        |
|----------|--------------------------------------------------------------|
| Backend  | Go 1.26, Gin, GORM, SQLite                                   |
| Frontend | React 19, Vite, TailwindCSS v4, TypeScript, Zustand, react-i18next |
| Output   | single static binary via `build.sh` → `./homihub`            |

## Quick Start

Requirements: Go ≥ 1.26, Node.js + pnpm.

```bash
./build.sh          # builds frontend + backend into ./homihub
./homihub           # run, defaults to :8080
```

Open http://localhost:8080 and register your first user.

> Vendored forks in `vendor/` are gitignored. `build.sh` auto-clones them if
> missing.

## Development

```bash
# Frontend dev server (Vite proxies /api, /dav, /.well-known → :8099)
cd frontend && pnpm install && pnpm dev

# Backend only
cd backend && go run .
```

## Configuration

All settings are environment variables:

| Variable            | Default                              | Description                          |
|---------------------|--------------------------------------|--------------------------------------|
| `PORT`              | `8080`                               | HTTP listen port                     |
| `HOMIHUB_DATA_DIR`  | `./data`                             | Data directory                       |
| `DB_DRIVER`         | `sqlite`                             | Database driver                      |
| `DB_DSN`            | `{DATA_DIR}/db/homihub.db`           | SQLite database file                 |
| `STORAGE_LOCAL_DIR` | `{DATA_DIR}/storage`                 | File storage directory               |
| `JWT_SECRET`        | `dev-secret-change-me`               | JWT signing secret (change in prod!) |
| `CORS_ORIGINS`      | (empty)                              | Allowed CORS origins                 |
| `CALENDAR_TZ`       | `UTC`                                | Calendar timezone                    |
| `HOMIHUB_NO_STATIC` | (unset)                              | Set to skip serving the embedded UI  |

## Project Layout

```
backend/   Go backend (modules: auth, calendar, files, settings, team, todos)
frontend/  React SPA (src/pages, src/i18n/locales/{en,zh}.json)
tests/     REST / CalDAV / WebDAV test harnesses
docs/      CALDAV.md, TESTING.md
build.sh   Builds the single binary
```

## Testing

See `docs/TESTING.md`. Highlights:

- **CalDAV** — passes `caldav-server-tester` (sync-token, recurrence
  exceptions, case-insensitive search).
- **WebDAV** — passes litmus: basic/copymove/http 100%, props 96.7%, locks
  94.1%. The remaining failures are upstream `x/net/webdav` behavior
  (`propfind_invalid2`, `fail_complex_cond_put`, `lock_shared`), not HomiHub
  defects.
- **REST** — end-to-end smoke tests.

## License

AGPL-3.0 for open-source use. Commercial licenses available — contact [zaaack@qq.com](mailto:zaaack@qq.com).
