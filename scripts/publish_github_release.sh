#!/usr/bin/env bash
# 固定发版方式：用本机 git 已能 push GitHub 的凭据（keychain / credential helper）
# 注入 GH_TOKEN，再调 gh 创建 Release（与 v2.3.7 / v1.11.0 成功路径一致）。
#
# 不依赖交互式 `gh auth login`（避免浏览器 OAuth 超时）；也不把 token 写进仓库。
#
# 用法（仓库根目录）：
#   ./scripts/publish_github_release.sh v1.11.0
#   PACK=1 ./scripts/publish_github_release.sh v1.12.0          # 先打包再发版
#   PACK=1 PACK_MODE=all ./scripts/publish_github_release.sh v1.12.0
#   ASSETS='dist/a.tgz dist/b.tgz' ./scripts/publish_github_release.sh v1.12.0
#   NOTES=docs/changelog-v1.12.0.md ./scripts/publish_github_release.sh v1.12.0
#
# 可选：DELETE_RELEASES='v2.0.0 v2.3.7' 发版前删除这些 GitHub Release（不删受保护 tag）
set -euo pipefail

ROOT="$(cd "$(dirname "$0")/.." && pwd)"
cd "$ROOT"

VERSION="${1:-}"
if [[ -z "$VERSION" || "$VERSION" == "-h" || "$VERSION" == "--help" ]]; then
  cat <<'EOF'
用法: ./scripts/publish_github_release.sh <version>

  version     如 v1.11.0（须以 v 开头）

环境变量:
  PACK=1              发版前执行 pack_release.sh（默认 0）
  PACK_MODE=all|backend|frontend   打包模式（默认 all；仅 PACK=1 时生效）
  NOTES=path.md       Release 说明（默认 docs/changelog-<version>.md）
  ASSETS='a.tgz b.tgz'  手动指定上传文件；默认自动收集 dist/*-<version>.tgz
  SKIP_TAG=1          不创建/推送 git tag（默认会确保远程有该 tag）
  DELETE_RELEASES='vX vY'  发版前 gh release delete（忽略不存在）
  GOOS/GOARCH         传给 pack_release.sh（默认 linux/amd64）

示例:
  PACK=1 ./scripts/publish_github_release.sh v1.12.0
  ./scripts/publish_github_release.sh v1.11.0
EOF
  exit 0
fi

if [[ ! "$VERSION" =~ ^v[0-9] ]]; then
  echo "version 须形如 v1.11.0，收到: $VERSION" >&2
  exit 1
fi

need_cmd() {
  command -v "$1" >/dev/null 2>&1 || {
    echo "缺少命令: $1" >&2
    exit 1
  }
}

need_cmd git
need_cmd gh
need_cmd awk

PACK="${PACK:-0}"
PACK_MODE="${PACK_MODE:-all}"
NOTES="${NOTES:-docs/changelog-${VERSION}.md}"
SKIP_TAG="${SKIP_TAG:-0}"
GOOS="${GOOS:-linux}"
GOARCH="${GOARCH:-amd64}"

# ── 凭据：与 git push https://github.com 相同来源 ─────────
load_gh_token() {
  if [[ -n "${GH_TOKEN:-}" ]]; then
    echo "[publish] 使用环境变量 GH_TOKEN"
    return 0
  fi
  local token
  token="$(printf 'protocol=https\nhost=github.com\n\n' | git credential fill 2>/dev/null | awk -F= '/^password=/{print $2; exit}')"
  if [[ -z "$token" ]]; then
    echo "[publish] 无法从 git credential 取得 github.com 密码/token。" >&2
    echo "  请先成功执行一次: git push origin HEAD" >&2
    echo "  或导出 GH_TOKEN=... 后再跑本脚本。" >&2
    exit 1
  fi
  export GH_TOKEN="$token"
  echo "[publish] 已从 git credential 注入 GH_TOKEN（不落盘）"
}

cleanup_token() {
  unset GH_TOKEN
}

trap cleanup_token EXIT

load_gh_token
gh auth status 2>&1 | head -12 || true

# ── 可选：先打包 ─────────────────────────────────────────
if [[ "$PACK" == "1" ]]; then
  echo "[publish] PACK=1 → STAMP=${VERSION} ./scripts/pack_release.sh ${PACK_MODE}"
  STAMP="$VERSION" GOOS="$GOOS" GOARCH="$GOARCH" ./scripts/pack_release.sh "$PACK_MODE"
fi

# ── 收集产物 ────────────────────────────────────────────
ASSETS_ARR=()
if [[ -n "${ASSETS:-}" ]]; then
  # shellcheck disable=SC2206
  ASSETS_ARR=($ASSETS)
else
  while IFS= read -r f; do
    [[ -n "$f" ]] && ASSETS_ARR+=("$f")
  done < <(ls -1 dist/*-"${VERSION}".tgz 2>/dev/null || true)
fi

if [[ ${#ASSETS_ARR[@]} -eq 0 ]]; then
  echo "[publish] 未找到 dist/*-${VERSION}.tgz。请先打包或设 ASSETS=..." >&2
  echo "  例: STAMP=${VERSION} ./scripts/pack_release.sh all" >&2
  echo "  或: PACK=1 ./scripts/publish_github_release.sh ${VERSION}" >&2
  exit 1
fi

echo "[publish] 将上传:"
for a in "${ASSETS_ARR[@]}"; do
  ls -lh "$a"
done

if [[ ! -f "$NOTES" ]]; then
  echo "[publish] 缺少说明文件: $NOTES" >&2
  echo "  请写 changelog 或设 NOTES=..." >&2
  exit 1
fi

# ── 可选：删旧 Release ───────────────────────────────────
if [[ -n "${DELETE_RELEASES:-}" ]]; then
  # shellcheck disable=SC2206
  olds=($DELETE_RELEASES)
  for v in "${olds[@]}"; do
    echo "[publish] gh release delete $v"
    gh release delete "$v" -y 2>/dev/null || echo "  (跳过或不存在: $v)"
  done
fi

# ── tag ────────────────────────────────────────────────
if [[ "$SKIP_TAG" != "1" ]]; then
  if ! git rev-parse "$VERSION" >/dev/null 2>&1; then
    echo "[publish] 创建本地 tag $VERSION"
    git tag -a "$VERSION" -m "$VERSION"
  else
    echo "[publish] 本地已有 tag $VERSION"
  fi
  if git ls-remote --exit-code --tags origin "refs/tags/${VERSION}" >/dev/null 2>&1; then
    echo "[publish] 远程已有 tag $VERSION"
  else
    echo "[publish] 推送 tag $VERSION"
    git push origin "$VERSION"
  fi
fi

# ── 创建 Release（已存在则先删再建，保证资产最新）────────
if gh release view "$VERSION" >/dev/null 2>&1; then
  echo "[publish] 已存在 Release $VERSION，先删除再重建"
  gh release delete "$VERSION" -y
fi

echo "[publish] gh release create $VERSION"
gh release create "$VERSION" \
  "${ASSETS_ARR[@]}" \
  --title "$VERSION" \
  --notes-file "$NOTES"

echo "[publish] 完成: https://github.com/dbaspace/auto-dba/releases/tag/${VERSION}"
gh release view "$VERSION" --json tagName,url,isLatest,assets \
  --jq '{tag:.tagName,url:.url,latest:.isLatest,assets:[.assets[].name]}'
