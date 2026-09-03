#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")"

# Vendor forks — gitignored, auto-clone if missing
VENDOR_DIR="vendor"
FORK_GO_WEBDAV="https://github.com/zaaack/go-webdav.git"
FORK_GO_WEBDAVP="https://github.com/zaaack/go-webdavp.git"

ensure_vendor() {
  local dir="$1" url="$2"
  if [ ! -d "$VENDOR_DIR/$dir" ]; then
    echo "==> 克隆缺失的 vendor fork: $dir"
    git clone --depth 1 "$url" "$VENDOR_DIR/$dir"
  fi
}

ensure_vendor go-webdav  "$FORK_GO_WEBDAV"
ensure_vendor go-webdavp "$FORK_GO_WEBDAVP"

echo "==> 构建前端"
(
  cd frontend
  pnpm install --frozen-lockfile
  pnpm build
)

echo "==> 同步产物到 backend/static"
rm -rf backend/static/*
cp -r frontend/dist/* backend/static/

echo "==> 构建后端单二进制"
(
  cd backend
  go build -o ../homihub .
)

echo "==> 完成: ./homihub"
