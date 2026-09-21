#!/bin/sh
# 精简版节点 SSH：host user identity_file [port]
set -e
if [ "$#" -lt 3 ]; then
  echo "Usage: $0 <host> <user> <sshkey> [port]"
  exit 1
fi
HOST=$1
USER=$2
SSHKEY=$3
PORT=${4:-22}
exec ssh -tt -p "$PORT" -o StrictHostKeyChecking=no -o UserKnownHostsFile=/dev/null -i "$SSHKEY" "$USER@$HOST"
