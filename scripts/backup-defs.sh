#!/usr/bin/env bash
# 备份平台的应用定义目录(GET /api/apps)到带时间戳的 JSON 文件。
# 这是 §灾备 里"脚本化记录级导出"那一层:轻量、可 cron;权威全量备份仍是飞书侧的
# Base 副本/快照(见 docs/PRODUCTION.md「灾备与备份」)。
#
# 用法:
#   scripts/backup-defs.sh [输出目录]      # 默认 ./backups
#   # cron 示例(每日 03:00):0 3 * * * cd /path/to/repo && scripts/backup-defs.sh /var/backups/fpp
#
# 配置: scripts/deploy.env(PLATFORM_API_BASE;token 留空则从服务器 .env 读)。
# 用 admin token(PLATFORM_API_TOKEN);GET /api/apps 是只读,不改任何数据。
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
[ -f "$here/deploy.env" ] || { echo "缺少 $here/deploy.env(复制 deploy.example.env 并填值)"; exit 1; }
# shellcheck disable=SC1091
source "$here/deploy.env"
: "${PLATFORM_API_BASE:?deploy.env 缺 PLATFORM_API_BASE}"

outdir="${1:-$here/../backups}"
mkdir -p "$outdir"

token="${PLATFORM_API_TOKEN:-}"
if [ -z "$token" ]; then
  : "${SERVER_HOST:?未配置 PLATFORM_API_TOKEN 且缺 SERVER_HOST}"; : "${SSH_KEY:?缺 SSH_KEY}"; : "${REMOTE_DIR:=~/fpp}"
  # shellcheck disable=SC2029
  token="$(ssh -i "$SSH_KEY" "$SERVER_HOST" "grep '^PLATFORM_API_TOKEN=' $REMOTE_DIR/deploy/compose/.env | cut -d= -f2-")"
fi
[ -n "$token" ] || { echo "拿不到 PLATFORM_API_TOKEN"; exit 1; }

# UTC 时间戳(可移植:GNU/BSD date 都支持 -u +FMT)。
ts="$(date -u +%Y%m%dT%H%M%SZ)"
out="$outdir/app-defs-$ts.json"

code="$(curl -sS -H "Authorization: Bearer $token" -o "$out" -w '%{http_code}' "$PLATFORM_API_BASE/api/apps")"
if [ "$code" != "200" ]; then
  echo "✗ 备份失败 HTTP $code"; head -c 300 "$out" 2>/dev/null; echo; rm -f "$out"; exit 1
fi

# 校验是 JSON 数组并数一下条数(python3 普遍可用;没有就跳过计数)。
n="?"
if command -v python3 >/dev/null 2>&1; then
  n="$(python3 -c 'import json,sys; d=json.load(open(sys.argv[1])); print(len(d) if isinstance(d,list) else len(d.get("apps",d.get("items",[]))))' "$out" 2>/dev/null || echo "?")"
fi
echo "✓ 已备份 $n 个应用定义 → $out"

# 轮转:仅保留最近 30 份(按文件名时间戳排序)。
ls -1t "$outdir"/app-defs-*.json 2>/dev/null | tail -n +31 | xargs -r rm -f
echo "  (保留最近 30 份;权威全量备份请同时做飞书 Base 副本/快照,见 docs/PRODUCTION.md「灾备与备份」)"
