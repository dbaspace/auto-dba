#!/usr/bin/env bash
# 交叉编译 linux Node Agent（StarRocks 节点，默认 :8091）
set -euo pipefail
ROOT="$(cd "$(dirname "$0")" && pwd)"
OUT_DIR="${ROOT}/bin"
OUT="${OUT_DIR}/node-agent"
mkdir -p "${OUT_DIR}"
export CGO_ENABLED=0
export GOOS="${GOOS:-linux}"
export GOARCH="${GOARCH:-amd64}"
echo "building ${GOOS}/${GOARCH} -> ${OUT}"
(cd "${ROOT}" && go build -trimpath -ldflags="-s -w" -o "${OUT}" .)
ls -la "${OUT}"
echo "ok: ${OUT}"
