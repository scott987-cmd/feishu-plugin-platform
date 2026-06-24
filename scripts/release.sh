#!/usr/bin/env bash
# 一键「发版 + 部署」编排:先部署后端(api/generator/execute-runner/caddy),
# 再构建并上传容器插件 widget。最后的「控制台选版本 + 发 app 版本」需 1 次人工点击
# (免审租户即时;全自动化路径见 docs/OPERATIONS.md)。
#
# 用法:  scripts/release.sh ["更新说明"]
set -euo pipefail
here="$(cd "$(dirname "${BASH_SOURCE[0]}")" && pwd)"
desc="${1:-release}"

echo "═══ ① 部署后端 ═══"
"$here/deploy-backend.sh"
echo ""
echo "═══ ② 发版 widget ═══"
"$here/release-widget.sh" "$desc"
echo ""
echo "═══ 完成。后端已上线;widget 待控制台确认发布(见上)。 ═══"
