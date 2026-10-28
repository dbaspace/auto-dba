#!/usr/bin/env bash
# 多种发布打包方式（构建机执行，勿在 CentOS 7 上 pnpm build）。
#
# 用法（仓库根目录）：
#   ./scripts/pack_release.sh              # 默认 all：后端 + 前端（serve_static 一体）
#   ./scripts/pack_release.sh all
#   ./scripts/pack_release.sh backend      # 仅 linux 二进制（API）
#   ./scripts/pack_release.sh frontend     # 仅静态资源（给 Nginx）
#   ./scripts/pack_release.sh docker       # 构建 Docker 镜像（含前端）
#
# 环境变量：GOOS GOARCH STAMP IMAGE_TAG SKIP_PNPM_INSTALL=1
#   STAMP 默认时间戳；发版可设 STAMP=v1.0.0
# all / backend 会交叉编译并打入 Node/DB/Platform Agent（frontend 不含）
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

MODE="${1:-all}"
GOOS="${GOOS:-linux}"
GOARCH="${GOARCH:-amd64}"
STAMP="${STAMP:-$(date +%Y%m%d%H%M%S)}"
IMAGE_TAG="${IMAGE_TAG:-auto-dba:${STAMP}}"

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "缺少命令: $1" >&2
    exit 1
  }
}

usage() {
  cat <<'EOF'
多种发布打包方式（构建机执行，勿在 CentOS 7 上 pnpm build）。

用法（仓库根目录）：
  ./scripts/pack_release.sh              # 默认 all：后端 + 前端（serve_static 一体）
  ./scripts/pack_release.sh all
  ./scripts/pack_release.sh backend      # 仅 linux 二进制（API）
  ./scripts/pack_release.sh frontend     # 仅静态资源（给 Nginx）
  ./scripts/pack_release.sh docker       # 构建 Docker 镜像（含前端）

环境变量：GOOS GOARCH STAMP IMAGE_TAG SKIP_PNPM_INSTALL=1
  STAMP 默认时间戳；发版示例：STAMP=v1.0.0 ./scripts/pack_release.sh all

all / backend 含 Node Agent(:8091)、DB Agent(:8092)、Platform Agent(:8090)；
frontend 仅静态资源，不含 Agent。
EOF
  exit "${1:-0}"
}

case "$MODE" in
  -h|--help|help) usage 0 ;;
  all|backend|frontend|docker) ;;
  *)
    echo "未知模式: $MODE" >&2
    usage 1
    ;;
esac

build_frontend() {
  need_cmd pnpm
  if [[ "${SKIP_PNPM_INSTALL:-0}" != "1" ]]; then
    echo "[pack] frontend: pnpm install"
    (cd frontend && pnpm install)
  fi
  echo "[pack] frontend: pnpm build"
  (cd frontend && pnpm build)
  if [[ ! -f frontend/dist/index.html ]]; then
    echo "frontend/dist/index.html 不存在" >&2
    exit 1
  fi
}

build_backend() {
  need_cmd go
  echo "[pack] go build GOOS=${GOOS} GOARCH=${GOARCH}"
  mkdir -p bin
  CGO_ENABLED=0 GOOS="$GOOS" GOARCH="$GOARCH" go build -o "bin/auto-dba" ./cmd/auto-dba
}

# SSH 下发用 Agent（相对包根路径与 Resolve*Artifact 一致）
build_agents() {
  need_cmd go
  echo "[pack] agents: GOOS=${GOOS} GOARCH=${GOARCH}"
  (
    export GOOS GOARCH CGO_ENABLED=0
    ./deploy/node-agent/build.sh
    ./deploy/db-agent/build.sh
    ./deploy/backup-agent/build.sh
    ./deploy/sql-agent/build.sh
  )
  local need=(
    deploy/node-agent/bin/node-agent
    deploy/db-agent/bin/db-agent
    deploy/backup-agent/bin/platform-agent
    deploy/sql-agent/bin/sql-agent
  )
  local p
  for p in "${need[@]}"; do
    if [[ ! -f "$p" ]]; then
      echo "Agent 制品缺失: $p" >&2
      exit 1
    fi
  done
}

copy_agents() {
  local stage="$1"
  mkdir -p \
    "$stage/deploy/node-agent/bin" \
    "$stage/deploy/db-agent/bin" \
    "$stage/deploy/backup-agent/bin" \
    "$stage/deploy/sql-agent/bin"
  cp -a deploy/node-agent/bin/node-agent "$stage/deploy/node-agent/bin/"
  cp -a deploy/db-agent/bin/db-agent "$stage/deploy/db-agent/bin/"
  cp -a deploy/backup-agent/bin/platform-agent "$stage/deploy/backup-agent/bin/"
  cp -a deploy/sql-agent/bin/sql-agent "$stage/deploy/sql-agent/bin/"
  # 兼容旧路径名
  if [[ -f deploy/backup-agent/bin/backup-agent ]]; then
    cp -a deploy/backup-agent/bin/backup-agent "$stage/deploy/backup-agent/bin/"
  fi
  chmod a+x \
    "$stage/deploy/node-agent/bin/node-agent" \
    "$stage/deploy/db-agent/bin/db-agent" \
    "$stage/deploy/sql-agent/bin/sql-agent" \
    "$stage/deploy/backup-agent/bin/"* 2>/dev/null || true
  # DNS 集群部署从包根读取 deploy/coredns/bin/coredns
  if [[ -f deploy/coredns/bin/coredns ]]; then
    mkdir -p "$stage/deploy/coredns/bin"
    cp -a deploy/coredns/bin/coredns "$stage/deploy/coredns/bin/"
    chmod a+x "$stage/deploy/coredns/bin/coredns"
  fi
}

copy_config_examples() {
  local dest="$1"
  mkdir -p "$dest"
  [[ -f config/config.example.yaml ]] && cp -a config/config.example.yaml "$dest/"
  [[ -f config/bootstrap.example.yaml ]] && cp -a config/bootstrap.example.yaml "$dest/"
}

# 主机 Web 终端运行期依赖（相对包根：scripts/terminal、assets/terminal）
copy_terminal_runtime() {
  local stage="$1"
  if [[ ! -f scripts/terminal/node-terminal.sh ]]; then
    echo "缺少 scripts/terminal/node-terminal.sh" >&2
    exit 1
  fi
  if [[ ! -d assets/terminal ]]; then
    echo "缺少 assets/terminal/" >&2
    exit 1
  fi
  mkdir -p "$stage/scripts" "$stage/assets"
  cp -a scripts/terminal "$stage/scripts/"
  chmod a+x "$stage/scripts/terminal/"*.sh 2>/dev/null || true
  cp -a assets/terminal "$stage/assets/"
}

# Nacos 管理页本地静态资源（相对包根：assets/nacos-console）；缺失时仍可全量回源，但不打进包。
copy_nacos_console_assets() {
  local stage="$1"
  if [[ ! -d assets/nacos-console/next ]]; then
    echo "[pack] 跳过 assets/nacos-console（无 next/，可用 scripts/sync-nacos-console-assets.sh 同步）"
    return 0
  fi
  mkdir -p "$stage/assets"
  cp -a assets/nacos-console "$stage/assets/"
}

pack_tgz() {
  local name="$1"
  local stage="${ROOT}/dist/${name}"
  local tgz="${ROOT}/dist/${name}.tgz"
  echo "[pack] tar → ${tgz}"
  tar -C "${ROOT}/dist" -czf "$tgz" "$name"
  echo "[pack] 完成: $tgz"
  echo "[pack] 目录: $stage"
}

# ── docker ─────────────────────────────────────────────
if [[ "$MODE" == "docker" ]]; then
  need_cmd docker
  echo "[pack] docker build -f deploy/Dockerfile -t ${IMAGE_TAG}（含 Agent 与 SQL 执行器）"
  docker build -f deploy/Dockerfile -t "$IMAGE_TAG" .
  echo "[pack] 镜像: $IMAGE_TAG"
  echo "[pack] 运行示例: docker run --rm -p 8800:8800 -v \$PWD/config:/app/config $IMAGE_TAG"
  echo "        需在挂载的 config 中设 http.serve_static: true（镜像内已含 frontend/dist 与 deploy/*/bin Agent）"
  exit 0
fi

mkdir -p "${ROOT}/dist"

# ── frontend only ──────────────────────────────────────
if [[ "$MODE" == "frontend" ]]; then
  OUT_NAME="auto-dba-frontend-${STAMP}"
  STAGE="${ROOT}/dist/${OUT_NAME}"
  rm -rf "$STAGE"
  mkdir -p "$STAGE"
  build_frontend
  cp -a frontend/dist "$STAGE/dist"
  if [[ -f deploy/nginx.conf.example ]]; then
    cp -a deploy/nginx.conf.example "$STAGE/"
  fi
  cat >"$STAGE/README-RELEASE.txt" <<EOF
KeePlus 前端静态包（给 Nginx / CDN）
====================================
目录 dist/ 为 Vite 构建产物。

Nginx 示例见 nginx.conf.example：
  - root 指向本包 dist/
  - /api/ 反代到后端 :8800
  - 后端 http.serve_static: false

说明：docs/deploy.md
EOF
  pack_tgz "$OUT_NAME"
  exit 0
fi

# ── backend only ───────────────────────────────────────
if [[ "$MODE" == "backend" ]]; then
  OUT_NAME="auto-dba-backend-${GOOS}-${GOARCH}-${STAMP}"
  STAGE="${ROOT}/dist/${OUT_NAME}"
  rm -rf "$STAGE"
  mkdir -p "$STAGE/bin"
  build_backend
  build_agents
  cp -a bin/auto-dba "$STAGE/bin/auto-dba"
  copy_config_examples "$STAGE/config"
  copy_terminal_runtime "$STAGE"
  copy_nacos_console_assets "$STAGE"
  copy_agents "$STAGE"
  cat >"$STAGE/README-RELEASE.txt" <<EOF
KeePlus 后端 API 包
==================
含二进制、主机终端运行文件（scripts/terminal、assets/terminal）、
Nacos 控制台静态资源（assets/nacos-console，若有），以及
Node/DB/Platform Agent 与 SQL 执行器 sql-agent（deploy/*/bin），无前端。

1. 配置 Nacos / config/config.yaml
2. http.serve_static: false（页面由 Vite:5174 或 Nginx 提供）
3. cd 本目录后：./bin/auto-dba -config config/config.yaml
4. API：http://服务器:8800/api/v1/...
5. 主机终端需本包根下 scripts/terminal/node-terminal.sh，并配置 terminal.ssh_identity_file
6. 集群/库部署 SSH 下发 Agent，以及 SQL 执行器下发：工作目录须为本包根（已含 deploy/node-agent、deploy/sql-agent 等制品）

说明：docs/deploy.md、docs/terminal.md、docs/db-cluster.md
EOF
  pack_tgz "$OUT_NAME"
  exit 0
fi

# ── all（一体：后端 + 前端）────────────────────────────
OUT_NAME="auto-dba-release-${GOOS}-${GOARCH}-${STAMP}"
STAGE="${ROOT}/dist/${OUT_NAME}"
rm -rf "$STAGE"
mkdir -p "$STAGE/bin" "$STAGE/frontend" "$STAGE/config"

build_frontend
build_backend
build_agents
cp -a frontend/dist "$STAGE/frontend/dist"
cp -a bin/auto-dba "$STAGE/bin/auto-dba"
copy_config_examples "$STAGE/config"
copy_terminal_runtime "$STAGE"
copy_nacos_console_assets "$STAGE"
copy_agents "$STAGE"
if [[ -f deploy/nginx.conf.example ]]; then
  cp -a deploy/nginx.conf.example "$STAGE/config/"
fi

cat >"$STAGE/README-RELEASE.txt" <<EOF
KeePlus 一体发布包（推荐 CentOS 手工部署）
========================================
工作目录必须为本包根（含 frontend/dist、scripts/terminal、assets/terminal、
assets/nacos-console（若有）、deploy/*/bin Agent 制品）。

1. 配置 Nacos 或 config/config.yaml
2. 开启静态托管（页面与 API 同端口，无需 5174）：
     http:
       serve_static: true
       port: 8800
3. 启动：./bin/auto-dba
4. 浏览器：http://服务器:8800/
5. 主机 Web 终端依赖 scripts/terminal/node-terminal.sh（已打入本包）与 terminal.ssh_identity_file
6. Node/DB/Platform Agent 与 SQL 执行器 sql-agent 已打入 deploy/*/bin，SSH 下发无需再本机构建

运行机无需 Node/pnpm。说明：docs/deploy.md、docs/terminal.md、docs/db-cluster.md
EOF

pack_tgz "$OUT_NAME"
