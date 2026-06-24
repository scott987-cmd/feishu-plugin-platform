#!/usr/bin/env bash
# 一键部署后端到服务器(api + generator + execute-runner + caddy)。
# 同步当前代码 → 远端 docker compose 构建并滚动重启。标准飞书与私有化飞书通用:
# 唯一要求是这台服务器能被飞书 webview 经 HTTPS 访问(域名 + TLS 由 Caddy 自动签发)。
#
# 用法:  scripts/deploy-backend.sh [服务名...]
#   不带参数 = 构建并重启全部服务;带参数 = 只重建指定服务(如 `api execute-runner`)。
# 配置:  scripts/deploy.env(见 deploy.example.env)
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo="$(cd "$here/.." && pwd)"
[ -f "$here/deploy.env" ] || { echo "缺少 $here/deploy.env(复制 deploy.example.env 并填值)"; exit 1; }
# shellcheck disable=SC1091
source "$here/deploy.env"

: "${SERVER_HOST:?deploy.env 缺 SERVER_HOST}"
: "${SSH_KEY:?deploy.env 缺 SSH_KEY}"
: "${REMOTE_DIR:=~/fpp}"
: "${COMPOSE_FILE:=docker-compose.prod.yml}"
services="$*"

echo "▶ 1/3 打包代码(排除 .git/node_modules/构建产物/密钥)…"
tarball="$(mktemp -t fpp-deploy-XXXX).tgz"
trap 'rm -f "$tarball"' EXIT
tar --exclude='./.git' --exclude='node_modules' --exclude='output' --exclude='dist' \
    --exclude='*.env' --exclude='.env' --exclude='state.json' \
    -czf "$tarball" -C "$repo" . 2>/dev/null

echo "▶ 2/3 同步到 $SERVER_HOST:$REMOTE_DIR(保留远端 .env / 证书卷)…"
scp -i "$SSH_KEY" "$tarball" "$SERVER_HOST:/tmp/fpp-deploy.tgz"
# shellcheck disable=SC2029
ssh -i "$SSH_KEY" "$SERVER_HOST" "mkdir -p $REMOTE_DIR && tar -xzf /tmp/fpp-deploy.tgz -C $REMOTE_DIR && rm -f /tmp/fpp-deploy.tgz"

echo "▶ 3/3 远端 compose 构建并重启${services:+($services)}…"
# shellcheck disable=SC2029
ssh -i "$SSH_KEY" "$SERVER_HOST" \
  "cd $REMOTE_DIR/deploy/compose && sudo docker compose -f $COMPOSE_FILE --env-file .env up -d --build $services"

echo "▶ 健康检查…"
# shellcheck disable=SC2016
ssh -i "$SSH_KEY" "$SERVER_HOST" 'cd '"$REMOTE_DIR"'/deploy/compose && set -a && . ./.env && set +a && \
  printf "  healthz=%s readyz=%s\n" "$(curl -s -o /dev/null -w "%{http_code}" https://$DOMAIN/healthz)" "$(curl -s -o /dev/null -w "%{http_code}" https://$DOMAIN/readyz)"'
echo "✅ 后端部署完成。"
