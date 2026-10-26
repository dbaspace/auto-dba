#!/usr/bin/env bash
# 交叉编译 linux SQL 执行器（默认 :8093）
set -euo pipefail
ROOT="$(cd "$(dirname "$0")" && pwd)"
OUT_DIR="${ROOT}/bin"
OUT="${OUT_DIR}/sql-agent"
mkdir -p "${OUT_DIR}"
export CGO_ENABLED=0
export GOOS="${GOOS:-linux}"
export GOARCH="${GOARCH:-amd64}"
echo "building ${GOOS}/${GOARCH} -> ${OUT}"
(cd "${ROOT}" && go build -trimpath -ldflags="-s -w" -o "${OUT}" .)
ls -la "${OUT}"
echo "ok: ${OUT}"
