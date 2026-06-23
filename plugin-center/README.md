# 私有化插件中心 · Private Plugin Center

A catalog of **AI-generated, real-machine-verified** Feishu Bitable plugins, ready to drop into a
privately-deployed Feishu environment. Every plugin here was produced from a one-sentence natural-language
prompt by this repo's generator (`cmd/shortcutgen -nl …`), then verified end-to-end against a **real external
API** via the basekit `testField` / `testAction` harness — not just compiled.

> 每个插件都由"一句话需求"经本平台生成,并经 `testField`/`testAction` **真实调用外部 API 验证通过**(非仅编译)。
> `dsl.json` 保留了生成它的自然语言与受约束 DSL(provenance,可审计)。

## Catalog

| Plugin | Type | One-sentence prompt (生成依据) | Verified result (真机) |
|---|---|---|---|
| [`exchange-rate`](exchange-rate/) | field · GET→JSON | 人民币金额按实时汇率换算成美元,输出美元金额+汇率 | `{usd_amount: 14.7, rate: 0.147}` (100 CNY) |
| [`translate-en-zh`](translate-en-zh/) | field · GET→JSON | 英文文本翻译成中文,输出译文(MyMemory 免费接口) | `"hello world"` → `你好世界` |
| [`ip-geo`](ip-geo/) | field · GET→JSON | IP 地址查地理位置,输出国家/城市/运营商(ip-api) | `8.8.8.8` → `United States / Ashburn / Google LLC` |
| [`text-to-qr`](text-to-qr/) | field · compute (URL-as-result, no fetch) | 文本转二维码图片 URL(api.qrserver.com) | `https://feishu.cn` → `…/create-qr-code/?size=300x300&data=https://feishu.cn` |
| [`fullname`](fullname/) | field · compute (pure local, no fetch) | 姓 + 名 拼接成全名 | concat verified |
| [`text-trim`](text-trim/) | field · compute · `trim()` | 去掉文本首尾空格 | `"  hello world  "` → `"hello world"` |
| [`idcard-birth`](idcard-birth/) | field · compute · `substr()` | 从身份证号截取出生日期(第7起8位) | `11010119900307123X` → `19900307` |
| [`idcard-gender`](idcard-gender/) | field · compute · **conditional** `if`/`eq`/`substr` | 按身份证第17位奇偶判别性别(奇=男/偶=女) | `…0011` (17th=1) → `男` · `…0028` (17th=2) → `女` (**testField-verified**) |
| [`to-upper`](to-upper/) | field · compute · `upper()` | 文本转全大写 | `"feishu"` → `"FEISHU"` |
| [`ai-polish`](ai-polish/) | field · **POST + nested JSON body + Bearer auth** (AI) | 调用 DeepSeek 大模型润色中文文本 | `"今天天气不错我们去公园玩吧"` → polished Chinese paragraph (**real LLM call verified**; fill your key in `config.json`) |
| [`fx-action`](fx-action/) | **automation action** · GET→JSON | 自动化:人民币金额按实时汇率换算,输出美元金额+汇率 | `{usd_amount: 14.7, exchange_rate: 0.147}` (100 CNY) · **also uploaded + published + run in a real Base automation** |
| [`feishu-record-writeback`](feishu-record-writeback/) | **connector** · action · **multi-step** (token → `batch_create`) | 写回:把文本作为新记录写入飞书多维表格(连接器) | 渲染出**精确的 Feishu `batch_create` 请求**;`tenant_access_token` 链路**真机验证**(`code:0`),tsc 编译通过;实际写入需填入你自己的 app 凭证+目标 Base |

Coverage of generator capabilities demonstrated here: **GET→JSON mapping**, **POST + JSON body**, **4 auth
types** (Bearer / QueryParamToken / CustomHeaderToken / Basic — see the generator), **compute-only / "the URL is
the result"** (no outbound request), **nested/structured JSON bodies** (AI chat-completions style: `messages`
array), **text-transformation functions** in expressions
(`trim` / `upper` / `lower` / `substr` / `slice` / `replace` / `concat` / `len` / `urlencode` / `round` plus math
`floor` / `ceil` / `abs` / `min` / `max`), **conditional logic in function form** — comparison
`eq` / `ne` / `gt` / `gte` / `lt` / `lte`, boolean `and` / `or` / `not`, and branching
`if(cond,a,b)` / `coalesce` / `default` (so the grammar never needs raw `< > = ? : & |` operators) — all
rendered as audited pure-JS helpers, **never `eval`**, the **automation (`addAction`) track**, the full HTTP
write-path (`PUT` / `PATCH` / `DELETE` + custom headers), **multi-step chaining** (a later request uses an
earlier one's response via `{stepId.json.path}`), and the **connector** pattern that writes records back into a
Feishu Base via its OpenAPI (`tenant_access_token` → `batch_create`).

## Deploy a plugin into your Feishu

Field shortcut (e.g. `exchange-rate/`):

```bash
cd exchange-rate
npm install
npm run build      # type-check against the real basekit SDK
npm run pack       # → output/*.zip
# Then in the (private) Developer Console: register a 字段捷径 capability,
# upload the zip, have an admin approve, install in a Base.
```

Automation action (e.g. `fx-action/`):

```bash
cd fx-action/proj   # app.json lives in the parent (fx-action/)
npm install
npx block-basekit-cli upload   # registers under the 自动化操作 capability
# Then select the version + 创建版本/发布 in the console; use it in a Base automation.
```

> These are standard, **human-auditable** basekit TypeScript projects — your security/compliance team can read
> `src/index.ts` (or `src/register.ts` for actions) line by line before anything is uploaded.

## Regenerate / add more

```bash
# field shortcut from natural language:
go run ./cmd/shortcutgen -nl "把英文文本翻译成中文…" -out ./plugin-center/<id> -dump
# automation action:
go run ./cmd/shortcutgen -action -nl "自动化:…" -out ./plugin-center/<id>/proj -dump
```

Set `DEEPSEEK_API_KEY` (or `LLM_PROVIDER=anthropic ANTHROPIC_API_KEY=…`) for the NL track.
