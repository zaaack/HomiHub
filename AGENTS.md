# AGENTS.md

## Project

HomiHub — self-contained personal hub (calendar, todos, files, team, settings).

- **Backend**: Go (Gin + GORM + SQLite) at `backend/`
- **Frontend**: React 19 + Vite + TailwindCSS v4 + TypeScript at `frontend/`
- Single binary output: `build.sh` → `./homihub`

## Key Commands

### Full build (produces `./homihub`)
```bash
./build.sh
```

### Frontend only
```bash
cd frontend && pnpm install && pnpm build
# or dev server:
cd frontend && pnpm dev
```
Vite proxies `/api`, `/dav`, `/.well-known` → `http://localhost:8099` (the Go backend).

### Backend only
```bash
cd backend && go build -o ../homihub .
# or run directly:
cd backend && go run .
```

### Tests
No automated tests exist yet. See `docs/TESTING.md` for planned test suites (CalDAV compliance via caldav-server-tester, WebDAV via litmus, REST smoke tests).

## Architecture

- Backend embeds `frontend/dist/` into the Go binary via `go:embed static` (`backend/embed.go`). Set `HOMIHUB_NO_STATIC=1` to skip serving static files.
- Backend modules: `auth`, `calendar`, `files`, `settings`, `team`, `todos` — all under `backend/internal/modules/`.
- Frontend uses Zustand for state, react-i18next for i18n (locales in `frontend/src/i18n/locales/`).
- Default port: backend `8080`, frontend dev `5173` (Vite default).

## Gotchas

- 禁止 `find /`（find 根目录）之类扫描整个文件系统的操作；只在项目目录内搜索。
- `build.sh` does `rm -rf backend/static/*` before copying frontend dist — don't put anything you need in `backend/static/`.
- `pnpm-lock.yaml` is the lockfile; use `pnpm install --frozen-lockfile` in CI/scripts.
- No `README` exists. This file is the primary orientation doc.
