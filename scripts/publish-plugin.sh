#!/usr/bin/env bash
# 一键「生成 + 上架」一个新插件 —— 零发版、即时可用。
# 新插件 = 后端存一条应用定义(DSL),已发布的容器渲染器即时渲染;不需要 opdev / 控制台。
# 渲染器「按当前数据表匹配插件」,所以每个插件绑一张表,打开该表即用。
#
# 用法:
#   scripts/publish-plugin.sh enrich <tableId> <inputField> <formKey> "<一句话需求>"
#       连接器/字段捷径型:对该表每行的 <inputField> 列调外部 API,渲染结果。
#       例: scripts/publish-plugin.sh enrich tblXXyy 城市 city "输入城市名,查实时天气,输出温度和风速,免key用Open-Meteo"
#   scripts/publish-plugin.sh view <tableId> "<一句话需求>"
#       只读数据视图型(stat/chart/table…)。
#       例: scripts/publish-plugin.sh view tblXXyy "按部门统计员工数量的柱状图"
#
# 配置: scripts/deploy.env(PLATFORM_API_BASE;token 留空则从服务器 .env 读)。
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
[ -f "$here/deploy.env" ] || { echo "缺少 $here/deploy.env(复制 deploy.example.env 并填值)"; exit 1; }
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

api() { curl -sS -H "Authorization: Bearer $token" -H "Content-Type: application/json" "$@"; }
type="${1:?type: enrich|view}"; shift
rnd="$(openssl rand -hex 3)"

case "$type" in
  enrich)
    table="${1:?tableId}"; inputField="${2:?inputField}"; formKey="${3:?formKey}"; prompt="${4:?一句话需求}"
    echo "▶ 生成字段捷径 DSL(NL→DSL)…"
    gen="$(api -X POST -d "$(python3 -c 'import json,sys; print(json.dumps({"prompt":sys.argv[1]}))' "$prompt")" "$PLATFORM_API_BASE/api/shortcut/generate")"
    echo "▶ 包成 enrich 应用定义并上架(绑表 $table)…"
    body="$(python3 - "$gen" "$table" "$inputField" "$formKey" "$rnd" <<'PY'
import json,sys
gen=json.loads(sys.argv[1]); table,inputField,formKey,rnd=sys.argv[2],sys.argv[3],sys.argv[4],sys.argv[5]
dsl=gen["dsl"]
outs=[p["key"] for p in dsl.get("result",{}).get("properties",[]) if p.get("key") and not p.get("hidden") and p.get("key")!="_id"]
app={"id":"enrich-%s"%rnd,"name":dsl.get("title",{}).get("zh_CN","连接器")+" · "+inputField,
     "type":"view_extension","bind":{"baseId":"","tableId":table},
     "ui":{"layout":"dashboard","components":[{"type":"enrich","title":dsl.get("title",{}).get("zh_CN","连接器"),
            "inputField":inputField,"formKey":formKey,"executeDsl":dsl,"outputKeys":outs}]}}
print(json.dumps(app,ensure_ascii=False))
PY
)"
    ;;
  view)
    table="${1:?tableId}"; prompt="${2:?一句话需求}"
    echo "▶ 生成数据视图 DSL(NL→DSL)…"
    gen="$(api -X POST -d "$(python3 -c 'import json,sys; print(json.dumps({"mode":"nl","prompt":sys.argv[1]}))' "$prompt")" "$PLATFORM_API_BASE/api/generate")"
    echo "▶ 注入绑表并上架(绑表 $table)…"
    body="$(python3 - "$gen" "$table" "$rnd" <<'PY'
import json,sys
app=json.loads(sys.argv[1]); table,rnd=sys.argv[2],sys.argv[3]
app["id"]="view-%s"%rnd; app.setdefault("bind",{}); app["bind"]["tableId"]=table; app["bind"].setdefault("baseId","")
print(json.dumps(app,ensure_ascii=False))
PY
)"
    ;;
  *) echo "未知类型 $type(enrich|view)"; exit 1 ;;
esac

code="$(api -o /tmp/pp-resp.json -w '%{http_code}' -X POST -d "$body" "$PLATFORM_API_BASE/api/apps")"
if [ "$code" = "200" ]; then
  pid="$(python3 -c 'import json;print(json.load(open("/tmp/pp-resp.json")).get("id",""))')"
  echo "✅ 已上架:$pid(绑表 $table)。在该表「新建视图 → 扩展视图插件 → 多为表格渲染」即可直接使用,零发版。"
else
  echo "✗ 上架失败 HTTP $code:"; head -c 400 /tmp/pp-resp.json; echo; exit 1
fi
