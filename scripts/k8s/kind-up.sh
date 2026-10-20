#!/usr/bin/env bash
# 本地 kind 集群，供 KeePlus K8s 模块联调。
set -euo pipefail

CLUSTER_NAME="${KIND_CLUSTER_NAME:-keeplus-k8s}"
KUBECONFIG_OUT="${KUBECONFIG_OUT:-$(pwd)/scripts/k8s/kubeconfig.yaml}"

if ! command -v kind >/dev/null 2>&1; then
  echo "请先安装 kind: https://kind.sigs.k8s.io/docs/user/quick-start/" >&2
  exit 1
fi
if ! command -v kubectl >/dev/null 2>&1; then
  echo "请先安装 kubectl" >&2
  exit 1
fi

if kind get clusters 2>/dev/null | grep -qx "$CLUSTER_NAME"; then
  echo "cluster $CLUSTER_NAME already exists"
else
  kind create cluster --name "$CLUSTER_NAME"
fi

kind get kubeconfig --name "$CLUSTER_NAME" > "$KUBECONFIG_OUT"
chmod 600 "$KUBECONFIG_OUT"
echo "kubeconfig written to $KUBECONFIG_OUT"
echo "export KUBECONFIG=$KUBECONFIG_OUT"
kubectl --kubeconfig="$KUBECONFIG_OUT" get nodes
