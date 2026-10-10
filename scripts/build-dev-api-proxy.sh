#!/usr/bin/env bash
# 预编译 Vite 用的 print_dev_api_proxy，避免每次 pnpm dev 都 go run。
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"
mkdir -p bin
go build -o bin/print_dev_api_proxy ./scripts/print_dev_api_proxy
echo "[build-dev-api-proxy] → bin/print_dev_api_proxy"
