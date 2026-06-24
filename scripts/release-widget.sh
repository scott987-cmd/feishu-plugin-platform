#!/usr/bin/env bash
# 构建并上传飞书容器插件(渲染器 widget)的新版本。
#   1) 把真实 appId / blockTypeID 注入 app.json / block.json(仓库内存的是占位符)
#   2) webpack 构建,把后端地址 + Bearer 注入 bundle(DefinePlugin)
#   3) opdev upload 上传一个新 widget 版本(非交互:-v patch -d ...)
# 上传成功后,最后一步「在控制台选版本 + 发 app 版本」见 docs/OPERATIONS.md(免审租户即时生效)。
#
# 用法:  scripts/release-widget.sh ["更新说明"]
# 配置:  scripts/deploy.env(见 deploy.example.env)
set -euo pipefail

here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
repo="$(cd "$here/.." && pwd)"
[ -f "$here/deploy.env" ] || { echo "缺少 $here/deploy.env(复制 deploy.example.env 并填值)"; exit 1; }
# shellcheck disable=SC1091
source "$here/deploy.env"

: "${PLATFORM_API_BASE:?deploy.env 缺 PLATFORM_API_BASE}"
: "${OPDEV_BIN:=opdev}"
: "${PLUGIN_DIR:=plugin}"
: "${WIDGET_DIST:=./block/dist}"
: "${FEISHU_APP_ID:?deploy.env 缺 FEISHU_APP_ID}"
: "${WIDGET_BLOCK_TYPE_ID:?deploy.env 缺 WIDGET_BLOCK_TYPE_ID}"
desc="${1:-release via script}"
plugin="$repo/$PLUGIN_DIR"

# 只读 Bearer:widget 只读 /api/apps + 调 execute,故内嵌 PLATFORM_READ_TOKEN(只读)。
# 即便从 bundle 提取也无法增删改插件 / 驱动 generate(后端 withAuth 按能力区分)。
# 绝不把 admin 的 PLATFORM_API_TOKEN 注入客户端。优先用配置;否则从服务器 .env 读取。
token="${PLATFORM_READ_TOKEN:-}"
if [ -z "$token" ]; then
  : "${SERVER_HOST:?未配置 PLATFORM_READ_TOKEN 且缺 SERVER_HOST(无法从服务器取 token)}"
  : "${SSH_KEY:?缺 SSH_KEY}" ; : "${REMOTE_DIR:=~/fpp}"
  echo "▶ 从服务器读取 PLATFORM_READ_TOKEN(不落本地)…"
  # shellcheck disable=SC2029
  token="$(ssh -i "$SSH_KEY" "$SERVER_HOST" "grep '^PLATFORM_READ_TOKEN=' $REMOTE_DIR/deploy/compose/.env | cut -d= -f2-")"
  [ -n "$token" ] || { echo "服务器 .env 未取到 PLATFORM_READ_TOKEN(请在 .env 设置只读 token)"; exit 1; }
fi

echo "▶ 1/3 注入真实 appId / blockTypeID…"
python3 - "$plugin/app.json" "$FEISHU_APP_ID" <<'PY'
import json,sys
p,appid=sys.argv[1],sys.argv[2]; d=json.load(open(p)); d['appId']=appid; json.dump(d,open(p,'w'),indent=2,ensure_ascii=False)
PY
python3 - "$plugin/block/block.json" "$WIDGET_BLOCK_TYPE_ID" <<'PY'
import json,sys
p,btid=sys.argv[1],sys.argv[2]; d=json.load(open(p)); d['blockTypeID']=btid; json.dump(d,open(p,'w'),indent=2,ensure_ascii=False)
PY

echo "▶ 2/3 构建 widget(注入 $PLATFORM_API_BASE + 只读 Bearer)…"
( cd "$plugin/block" && PLATFORM_API_BASE="$PLATFORM_API_BASE" PLATFORM_READ_TOKEN="$token" npm run build >/dev/null )
grep -rq "$(echo "$PLATFORM_API_BASE" | sed 's#https\?://##')" "$plugin/block/dist" \
  && echo "  ✓ 后端地址已注入 dist" || { echo "  ✗ dist 未注入后端地址,终止"; exit 1; }

echo "▶ 3/3 opdev upload(patch 版本)…"
( cd "$plugin" && "$OPDEV_BIN" upload "$WIDGET_DIST" -t block -v patch -d "$desc" )

cat <<EOF
✅ widget 已上传(新 patch 版本)。最后一步在控制台完成(免审租户即时):
   1) 打开  https://open.feishu.cn/app/$FEISHU_APP_ID/blocks/$WIDGET_BLOCK_TYPE_ID
   2) 标题旁「编辑」铅笔 → 小组件版本下拉选「X.Y.Z(待更新)」→ 保存
   3)「版本管理与发布」→ 创建版本 → 确认发布
   详见 docs/OPERATIONS.md(含 opdev headless / 应用版本 API 的全自动化路径)。
EOF
