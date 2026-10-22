#!/usr/bin/env bash
# 在 mars-dns 树里按本仓库的 plugin.cfg 编译 CoreDNS，不改 hdns。
# 编译使用该仓库的 Go 1.20 与 vendor。产物写到 deploy/coredns/bin/coredns。
set -euo pipefail
ROOT="$(cd "$(dirname "$0")/.." && pwd)"
SRC="${MARS_DNS_SRC:-/Users/chenjiachi/go/src/universe_saturn/mars-dns}"
if [[ ! -f "$SRC/coredns.go" ]]; then
  echo "找不到 mars-dns：$SRC" >&2
  exit 1
fi
mkdir -p "$ROOT/deploy/coredns/bin"
workdir="$(mktemp -d)"
cleanup() { rm -rf "$workdir"; }
trap cleanup EXIT
# 只复制编译需要的顶层与 vendor，避免改动原仓库。
cp -a "$SRC/." "$workdir/"
cp "$ROOT/deploy/coredns/plugin.cfg" "$workdir/plugin.cfg"
cd "$workdir"
go generate coredns.go
export GOOS="${GOOS:-linux}"
export GOARCH="${GOARCH:-amd64}"
export CGO_ENABLED=0
go build -mod=vendor -ldflags="-s -w" -o "$ROOT/deploy/coredns/bin/coredns" .
echo "已生成 $ROOT/deploy/coredns/bin/coredns ($GOOS/$GOARCH)"
