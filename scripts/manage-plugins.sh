#!/usr/bin/env bash
# 列出 / 删除平台上已上架的插件(应用定义)。
# 渲染器「按当前表渲染绑定它的全部插件」,所以同一张表绑了哪些插件、要不要清理旧的,用这个看与删。
#
# 用法:
#   scripts/manage-plugins.sh list [tableId]   # 列全部;给 tableId 只列绑定该表的
#   scripts/manage-plugins.sh rm   <pluginId>  # 删除一个插件(立即生效,零发版)
#
# 配置: scripts/deploy.env(同 publish-plugin.sh)。
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
[ -f "$here/deploy.env" ] || { echo "缺少 $here/deploy.env"; exit 1; }
# shellcheck disable=SC1091
source "$here/deploy.env"
: "${PLATFORM_API_BASE:?deploy.env 缺 PLATFORM_API_BASE}"

token="${PLATFORM_API_TOKEN:-}"
if [ -z "$token" ]; then
  : "${SERVER_HOST:?未配置 PLATFORM_API_TOKEN 且缺 SERVER_HOST}"; : "${SSH_KEY:?缺 SSH_KEY}"; : "${REMOTE_DIR:=~/fpp}"
  # shellcheck disable=SC2029
  token="$(ssh -i "$SSH_KEY" "$SERVER_HOST" "grep '^PLATFORM_API_TOKEN=' $REMOTE_DIR/deploy/compose/.env | cut -d= -f2-")"
fi
[ -n "$token" ] || { echo "拿不到 PLATFORM_API_TOKEN"; exit 1; }
api() { curl -sS -H "Authorization: Bearer $token" "$@"; }

cmd="${1:?list|rm}"; shift || true
case "$cmd" in
  list)
    filter="${1:-}"
    api "$PLATFORM_API_BASE/api/apps" | python3 - "$filter" <<'PY'
import json, sys
filt = sys.argv[1] if len(sys.argv) > 1 else ""
d = json.load(sys.stdin)
apps = d if isinstance(d, list) else d.get("apps", d.get("items", []))
rows = []
for a in apps:
    tid = (a.get("bind") or {}).get("tableId", "")
    if filt and tid != filt:
        continue
    comps = ",".join(c.get("type", "") for c in (a.get("ui", {}) or {}).get("components", []))
    rows.append((a.get("id", ""), a.get("type", ""), tid, comps, a.get("name", "")))
if not rows:
    print("(没有匹配的插件)"); raise SystemExit
print("%-14s %-15s %-20s %-10s %s" % ("ID", "TYPE", "BIND.TABLE", "COMPS", "NAME"))
for r in rows:
    print("%-14s %-15s %-20s %-10s %s" % r)
print("\n共 %d 个%s" % (len(rows), ("(绑表 %s)" % filt) if filt else ""))
PY
    ;;
  rm)
    id="${1:?pluginId(先用 list 查)}"
    code="$(api -o /tmp/mp-resp -w '%{http_code}' -X DELETE "$PLATFORM_API_BASE/api/apps/$id")"
    if [ "$code" = "200" ] || [ "$code" = "204" ]; then
      echo "✓ 已删除 $id(立即生效)"
    else
      echo "✗ 删除失败 HTTP $code:"; head -c 300 /tmp/mp-resp; echo; exit 1
    fi
    ;;
  *) echo "未知命令 $cmd(list|rm)"; exit 1 ;;
esac
