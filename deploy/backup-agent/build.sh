#!/usr/bin/env bash
# 交叉编译 linux 平台 Agent 制品（供平台容器 SSH/SFTP 下发）
set -euo pipefail
ROOT="$(cd "$(dirname "$0")" && pwd)"
OUT_DIR="${ROOT}/bin"
OUT_PRIMARY="${OUT_DIR}/platform-agent"
OUT_COMPAT="${OUT_DIR}/backup-agent"
mkdir -p "${OUT_DIR}"
export CGO_ENABLED=0
export GOOS="${GOOS:-linux}"
export GOARCH="${GOARCH:-amd64}"
echo "building ${GOOS}/${GOARCH} -> ${OUT_PRIMARY}"
(cd "${ROOT}" && go build -trimpath -ldflags="-s -w" -o "${OUT_PRIMARY}" .)
cp -f "${OUT_PRIMARY}" "${OUT_COMPAT}"
ls -la "${OUT_PRIMARY}" "${OUT_COMPAT}"
echo "ok: ${OUT_PRIMARY} (compat: ${OUT_COMPAT})"
