#!/usr/bin/env bash
# 本地后端快速启动：增量编译到 bin/，避免每次 go run 全量链接。
# 用法：./scripts/dev-backend.sh
# 强制重跑 migrate/seed：AUTO_DBA_FORCE_BOOTSTRAP=1 ./scripts/dev-backend.sh
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
mkdir -p bin
echo "[dev-backend] building…"
START=$(date +%s)
go build -o bin/auto-dba ./cmd/auto-dba
END=$(date +%s)
echo "[dev-backend] build $((END-START))s → bin/auto-dba"
exec ./bin/auto-dba -config "${CONFIG:-config/config.yaml}" "$@"
