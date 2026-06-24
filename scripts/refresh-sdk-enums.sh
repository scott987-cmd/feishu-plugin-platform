#!/usr/bin/env bash
# 从 basekit SDK 的 dist/index.d.ts 提取权威枚举,生成对账黄金文件
#   internal/shortcut/testdata/basekit_sdk_enums.json
#
# 这是「生成器可信」闭环的源头:生成器里所有 SDK 相关 allowlist(FieldType / NumberFormatter /
# AuthorizationType / FieldComponent 名,以及 addAction 授权联合)都必须 ⊆ 这里抽出的权威集合。
# 升级 SDK 后跑一次本脚本 → 若 golden 变了,sdk_reconcile_test.go 会把需要跟改的 Go allowlist 点出来。
#
# 用法:
#   scripts/refresh-sdk-enums.sh                 # 自动定位已安装的 SDK .d.ts
#   scripts/refresh-sdk-enums.sh --dts <path>    # 指定 dist/index.d.ts
#   scripts/refresh-sdk-enums.sh --pack          # 用 npm pack 拉取 SDK(版本见 SDK_VERSION,默认 latest)
set -euo pipefail

repo="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
out="$repo/internal/shortcut/testdata/basekit_sdk_enums.json"
pkg="@lark-opdev/block-basekit-server-api"
dts="" ; mode="auto"
while [ $# -gt 0 ]; do
  case "$1" in
    --dts) dts="$2"; shift 2;;
    --pack) mode="pack"; shift;;
    *) echo "未知参数 $1"; exit 1;;
  esac
done

# 1) 定位 .d.ts:--pack 强制重新拉取(CI 用);否则 --dts 优先,再自动探测本地安装
if [ "$mode" = "pack" ]; then
  ver="${SDK_VERSION:-latest}"
  case "$ver" in *[\^~x*]*) echo "⚠ SDK_VERSION='$ver' 非精确版本;对账应钉死与插件一致的精确版本(plugin-center/*/package.json)";; esac
  tmp="$(mktemp -d)"; echo "▶ npm pack $pkg@$ver → $tmp"
  if ! ( cd "$tmp" && npm pack "$pkg@$ver" >/dev/null ); then   # 不吞 stderr:npm 真错误要冒出来
    echo "✗ npm pack $pkg@$ver 失败(网络 / registry / 版本不存在?见上方 npm 报错)"; exit 1
  fi
  ( cd "$tmp" && tar -xzf ./*.tgz )
  dts="$tmp/package/dist/index.d.ts"
elif [ -z "$dts" ]; then
  for cand in \
    "$repo/plugin/block/node_modules/$pkg/dist/index.d.ts" \
    "$repo/frontend/node_modules/$pkg/dist/index.d.ts" \
    "$repo/node_modules/$pkg/dist/index.d.ts" \
    "/tmp/weather-shortcut/node_modules/$pkg/dist/index.d.ts"; do
    [ -f "$cand" ] && { dts="$cand"; break; }
  done
fi
[ -f "$dts" ] || { echo "✗ 找不到 $pkg 的 dist/index.d.ts。先 npm i,或用 --dts <path> / --pack"; exit 1; }

# 2) SDK 版本(尽力)
ver="unknown"
pj="$(dirname "$(dirname "$dts")")/package.json"
[ -f "$pj" ] && ver="$(python3 -c "import json;print(json.load(open('$pj')).get('version','unknown'))" 2>/dev/null || echo unknown)"

echo "▶ 解析 $dts (v$ver)"
# 3) 抽取:四个枚举的 KEY 名 + addAction 授权联合字面量
python3 - "$dts" "$ver" "$out" <<'PY'
import json, re, sys
dts, ver, out = sys.argv[1], sys.argv[2], sys.argv[3]
src = open(dts, encoding="utf-8").read()

def enum_keys(name):
    # Match `[export] declare [const] enum NAME {` and capture the body up to the first
    # close brace AT COLUMN 0. Top-level d.ts close braces are unindented, while any `}`
    # inside a member comment or string value is indented — so it cannot terminate the
    # body early. (The naive `\{(.*?)\}` stopped at the first `}` and could silently
    # truncate the golden, corrupting the very source of truth this gate relies on.)
    m = re.search(
        r"(?:export\s+)?declare\s+(?:const\s+)?enum\s+%s\b\s*\{(.*?)^\}" % re.escape(name),
        src, re.S | re.M)
    if not m:
        raise SystemExit(
            "✗ enum %s not found — its declaration form may have changed "
            "(export / const enum / renamed). Update scripts/refresh-sdk-enums.sh." % name)
    body = m.group(1)
    if "declare " in body and "enum " in body:  # over-capture guard
        raise SystemExit("✗ enum %s body over-captured (swallowed another declaration) — check the parser." % name)
    keys = re.findall(r"^\s*([A-Za-z_][A-Za-z0-9_]*)\s*=", body, re.M)
    if not keys:
        raise SystemExit("✗ enum %s parsed to 0 keys — parser/SDK mismatch." % name)
    return sorted(set(keys))

# addAction 的 authorization.type 是字符串联合,不是枚举:取 Action 接口里 authorization 块的 type 行字面量
def action_auth_union():
    m = re.search(r"interface Action\s*\{.*?authorization\?:\s*\{(.*?)\}", src, re.S)
    block = m.group(1) if m else ""
    tline = re.search(r"type:\s*([^;]+);", block)
    lits = re.findall(r"'([^']+)'", tline.group(1)) if tline else []
    if not lits:
        raise SystemExit("✗ 未解析到 addAction 授权联合(Action.authorization.type)")
    return sorted(set(lits))

data = {
    "_source": "%s dist/index.d.ts" % "@lark-opdev/block-basekit-server-api",
    "_sdkVersion": ver,
    "_generatedBy": "scripts/refresh-sdk-enums.sh — DO NOT EDIT BY HAND",
    "_note": "Authoritative SDK enum KEY names + addAction auth union literals. The Go shortcut allowlists must each be a subset of the matching set (see sdk_reconcile_test.go).",
    "FieldType": enum_keys("FieldType"),
    "NumberFormatter": enum_keys("NumberFormatter"),
    "AuthorizationType": enum_keys("AuthorizationType"),
    "FieldComponent": enum_keys("FieldComponent"),  # addField form item component
    "Component": enum_keys("Component"),            # addAction form item component (distinct enum!)
    "FieldCode": enum_keys("FieldCode"),            # field execute return code (Success/Error)
    "ActionAuthType": action_auth_union(),
}
with open(out, "w", encoding="utf-8") as f:
    json.dump(data, f, ensure_ascii=False, indent=2, sort_keys=False)
    f.write("\n")
print("✓ 写出 %s" % out)
for k in ("FieldType","NumberFormatter","AuthorizationType","FieldComponent","Component","FieldCode","ActionAuthType"):
    print("  %-18s %2d 项" % (k, len(data[k])))
PY