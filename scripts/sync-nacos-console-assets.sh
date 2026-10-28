#!/usr/bin/env bash
# 包装 scripts/sync_nacos_console_assets.py
# 用法: CONSOLE_URL=http://host:8080/next ./scripts/sync-nacos-console-assets.sh
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
exec python3 "$ROOT/scripts/sync_nacos_console_assets.py" "$@"
