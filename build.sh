#!/usr/bin/env bash
set -euo pipefail

cd "$(dirname "$0")"

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
