> 🌐 **中文** · [English](README.en.md)

# Feishu Bitable Plugin Generator · 飞书多维表格插件生成平台

> 一句话描述你的需求 → 得到一个**真实、可逐行审计**的飞书多维表格插件（TypeScript），可直接上传到飞书。

[![Go](https://img.shields.io/badge/Go-1.24-00ADD8?logo=go)](https://go.dev) <!-- badge placeholder -->
[![License: MIT](https://img.shields.io/badge/License-MIT-green.svg)](#license) <!-- badge placeholder -->
[![Tests](https://img.shields.io/badge/go%20test-passing-brightgreen)](#quick-start) <!-- badge placeholder -->
[![basekit](https://img.shields.io/badge/basekit-server--api%201.0.6-0F6E56)](#what-it-generates) <!-- badge placeholder -->

一个面向使用飞书的组织（**标准 SaaS 或私有化部署——两者都支持**）的自然语言 → basekit 插件生成器：它们拥有多维表格和插件能力，但**没有插件市场，也没有内部研发团队**。你只需输入一句话，平台便会产出一个标准、人类可读的 basekit TypeScript 工程，走飞书正常的上传 + 审核链路。

它同时交付企业最关心的**交付**侧能力：一个**自托管的 execute 运行时**（连接器 / 字段捷径通过你自己的服务器调用外部 API——无需外部函数托管），以及**一键发布 + 部署**（`scripts/release.sh`）。详见 **[`OPERATIONS.md`](docs/OPERATIONS.md)**。

---

## 演示 · Live demo

一段真实、未剪辑的屏幕录像——输入一句话，模型即**生成并校验**，你得到一个可审计的 `src/index.ts`，外加一键工程下载。该片段还展示了切换到 **自动化 (addAction)** 并生成一个自动化动作。

![一句话生成可审计的飞书 basekit 插件 · 实机录屏](docs/assets/demo.gif)

| ① 用一句话描述需求 | ② 生成结果概览（输入 / 出网白名单 / 鉴权 / 输出列） |
|---|---|
| ![输入自然语言需求](docs/assets/screenshots/02-input.png) | ![生成结果概览](docs/assets/screenshots/03-field-result.png) |
| **③ 可逐行审计的源码 + 一键下载工程** | **④ 切到「自动化」生成 addAction 动作** |
| ![可审计的 src/index.ts](docs/assets/screenshots/04-field-code.png) | ![自动化动作 register.ts](docs/assets/screenshots/05-action-result.png) |

> 完整图文走查见 **[`docs/index.html`](docs/index.html)**（含上面的录屏与逐步截图，可作为发给客户的使用指南；用 GitHub Pages 指向 `/docs` 即可在线访问）。

---

## 它解决的问题

私有化部署的飞书自带 basekit 插件能力，但出于设计原因**没有公开市场**——没有任何地方可以"安装"一个社区字段捷径或自动化。要自己构建，就意味着雇一个懂 basekit SDK 的人。而这些组织（信创 / 政企 / 国企）大多两样都没有。

本平台填补了这个空白。一个非工程师用通俗语言陈述需求，平台便产出一个**真实的 basekit 工程，其源码在上传之前就可由安全 / 合规团队逐行审阅**。生成的 TypeScript 就是交付物——不是一个不透明的二进制，也不是一个你不得不信任的托管运行时。对信创 / 政企客户而言，"自己审计，再提交审核"是卖点，而非缺陷说明。

在**标准（公有云）飞书**上，运行时是飞书自有的 basekit FaaS，因此生成器的工作只是产出一个正确、标准、可审计的工程。**私有化部署的飞书没有 FaaS**——因此连接器 / 字段捷径的执行运行在一个你自行部署的**自托管 execute 运行时**上（`cmd/execute-runner`：一个 DSL *解释器*，而非代码沙箱——无用户 JS，已做 SSRF 防护；详见 [`docs/EXECUTE_RUNTIME.md`](docs/EXECUTE_RUNTIME.md)）。无论哪种方式，安全 / 合规团队审阅的交付物都是**生成的 TypeScript**，而非一个不透明的二进制或一个你不得不信任的托管运行时。

---

## 它生成什么

两种生成器，均面向官方 SDK `@lark-opdev/block-basekit-server-api`（固定到 `1.0.6`，CLI `1.0.5`）：

### 1. Field shortcut · 字段捷径 (field shortcut) — `basekit.addField`

选择输入列 → 调用一个外部 API → 写回一个或多个输出列。

- **4 种鉴权类型**，由终端用户在配置时填写（绝不硬编码）：`HeaderBearerToken`、`QueryParamToken`、`CustomHeaderToken`、`Basic`
- `GET` / `POST` / `PUT` / `PATCH` / `DELETE`（读取或写入外部系统），支持扁平或**嵌套 JSON 请求体**（`bodyJson`），以及可选的**自定义请求头**
- **多步链式调用**（`steps`，≤3）：后一个请求可以使用前一个请求的响应——`{stepId.json.path}` 把某步的输出流转到下一步的 URL / 请求头 / 请求体（例如获取一个 token → 用它调用一个 API；地理编码 → 天气）
- **表达式映射**，作用于两个命名空间：`in.<inputKey>`（输入）和 `res.<dotted.json.path>`（响应），外加 `+ - * / % ( )`、数组下标（`res.list.0.x`）、`rand()`、字符串 / 数字函数（`concat`/`upper`/`trim`/`substr`/`round`/`floor`/…），以及**函数形式的条件逻辑**——`eq`/`gt`/`and`/`if(cond,a,b)`/`coalesce`——因此分支无需用裸的 `< > = ? :` 运算符
- 多属性 **Object 结果**（一次产出多个派生列），可选 `NumberFormatter`；列类型支持 `Text` / `Number` / `DateTime` / `Checkbox` / `SingleSelect` / `Phone` / `Email` / `Currency` / `Progress` / `Rating` / `Barcode` / `Url`（可点击链接，渲染为 `{text,link}` 单元格值）/ `MultiSelect`（一个 `string[]`——用 `split(field, ',')`）；主列按 SDK 要求为 `Text`/`Number`

**示例——自然语言 → 生成的字段捷径**

> "我有一个人民币金额字段，按实时汇率换算成美元，输出美元金额和当前汇率两列"
> *(I have an RMB-amount field; convert it to USD at the live rate and write back two columns: the USD amount and the current rate.)*

会产出这份 DSL（LLM 的结构化中间形态）……

```json
{
  "id": "exchange-rate",
  "title": { "zh_CN": "汇率换算", "en_US": "Exchange Rate" },
  "domains": ["api.exchangerate-api.com"],
  "formItems": [
    { "key": "account", "label": { "zh_CN": "人民币金额" },
      "component": "FieldSelect", "supportType": ["Number"], "required": true }
  ],
  "result": {
    "kind": "object",
    "properties": [
      { "key": "id",   "type": "Text",   "groupByKey": true, "hidden": true, "expr": "rand()" },
      { "key": "usd",  "type": "Number", "label": { "zh_CN": "美元金额" },
        "primary": true, "formatter": "DIGITAL_ROUNDED_2", "expr": "in.account * res.rates.USD" },
      { "key": "rate", "type": "Number", "label": { "zh_CN": "汇率" },
        "formatter": "DIGITAL_ROUNDED_4", "expr": "res.rates.USD" }
    ]
  },
  "execute": { "url": "https://api.exchangerate-api.com/v4/latest/CNY", "method": "GET" }
}
```

……它会编译成一个可审计的 `src/index.ts`，调用 `basekit.addDomainList([...])` + `basekit.addField({...})`，其中 `expr` 被降级（lower）为安全的可选链 JS（`in.account * res?.rates?.USD`）。

### 2. Automation action · 自动化 (automation action) — `basekit.addAction`

配置输入 → 调用一个外部 API → 返回一个**供下游自动化步骤消费**的结果对象。

- `APIKey` 鉴权（运行时注入），`GET`/`POST`/`PUT`/`PATCH`/`DELETE` + 扁平或嵌套（`bodyJson`）请求体 + 自定义请求头
- 同一套 `expr` 语法（`in.<inputKey>`、`res.<json.path>`、算术、函数、`if`/`eq`/`gt`… 条件、`rand()`）
- 结果是一个以你的输出键为键的普通对象，带有一个带类型的 `resultType`

**示例——自然语言 → 生成的动作**

> "自动化：当记录新增时，拿城市字段查实时天气，把温度和天气描述写进结果供后续步骤使用"
> *(Automation: on new record, take the city field, look up live weather, and return temperature + description for later steps.)*

会产出一份 Action DSL（inputs / result / execute），它编译成一个可审计的 `src/register.ts`，带有 `basekit.addAction({ formItems, execute, resultType })`。

---

## 架构

```
                 NL prompt
                     │
   DeepSeek (default) │  forced function call — the function's JSON-schema
   or Claude (opt-in) │  parameters ARE the DSL schema (same source as the
                     ▼  validator), so the model can only emit structured DSL
            ┌──────────────────┐      validate → if invalid, feed the error
            │  Constrained DSL │◀── back as a tool result and retry (≤ 2 rounds)
            │  (JSON, the IR)  │
            └────────┬─────────┘
                     │  Go renderer (standard library only)
                     ▼
        ┌──────────────────────────────┐
        │ auditable basekit TS project │  src/index.ts | src/register.ts,
        │  + provenance header         │  package.json, tsconfig, test/, README
        └────────┬─────────────────────┘
                 │  testField / testAction → real outbound API call
                 ▼
          npm install → build → pack → upload to Feishu
```

- **DSL 是一种中间表示，而非运行时。** 它存在的唯一目的，是让 LLM 有一个结构化、可校验的目标，并让 Go 渲染器有一个稳定的输入。`NL → DSL → TypeScript → testField → pack`。
- **NL → DSL** 默认使用 **DeepSeek**（OpenAI 兼容，**强制**函数调用；该工具的 `parameters` schema 由校验器所检查的同一套枚举构建而成，因此 schema 与校验器永不漂移）。当输出非法时，校验错误会作为工具结果回喂，模型重试——**自动修复，≤ 2 轮**。**Claude** 为可选项（`LLM_PROVIDER=anthropic`）。DeepSeek 是国内端点，所以它的客户端**绕过任何代理**。
- **三道编译期护栏**（这正是让输出可信赖的原因）：
  1. **出网域名白名单，静态预检** —— 每个 `execute.url` 的 host 都必须被 `domains`（`addDomainList`）覆盖。SDK 在运行时会硬性拒绝任何不在该列表内的 fetch；我们先在编译期就把它拦下。
  2. **表达式白名单 —— 绝不 `eval`，绝不执行任意代码。** `expr` 是一种极小的语法（`number | 'string' | rand() | in.<key> | res.<path>`，配合 `+ - * / % ( )` 以及一组白名单化的纯 JS 函数，包括比较 / 布尔 / 条件辅助函数 `eq`/`gt`/`and`/`if`/`coalesce`）。被禁的 token（`; = [ ] { } $ \` " \\ // ?: & | ! < >`）会被直接拒绝——所以即便是条件分支也走白名单函数，绝不走裸运算符。表达式是生成器*唯一*可能夹带 JS 的地方，因此它被白名单化，而非被解释执行。
  3. **URL 占位符校验** —— URL 或 POST 请求体中的每个 `{placeholder}` 都必须引用一个已声明的输入。
- **存储就是飞书多维表格本身 —— 零外部数据库。** 平台自身的数据（应用 / 插件定义 + 按用户的归属）存放在一个 多维表格 里，而非 Postgres/Redis。对一个私有化部署的 信创/政企 产品而言，这是特性而非妥协：少一个要部署、加固和备份的组件；持久性由飞书提供（且 Base 可被导出 / 快照以做留存）；管理员可以在熟悉的表格 UI 里检视 / 审计每一条存储的定义——平台 dogfood（自食其力地使用）了它自己所售卖的能力。读取走一个短 TTL 的缓存，搭配按表作用域的查询（`GET /api/apps?tableId=`），因此能扛住"读多写少"的现实（许多查看者，少数作者）。**规模边界，老实说：** 写是低频的管理动作（发布一个插件），受飞书单应用 QPS 限制；跨副本的读可能会有至多一个缓存 TTL 的陈旧。一个位于同一 `store.Store` 接口之后的 Postgres 后端，是为写多 / 严格容灾部署准备的**可选**逃生口——隔离、可插拔，且*不是*前置条件。

---

## 快速开始

前置条件：**Go 1.24+**。构建生成的插件需要：**Node.js + npm**。自然语言生成需要：一个 `DEEPSEEK_API_KEY`。

### 运行测试

```bash
go test ./...
```

覆盖 DSL 校验器（拒绝不符 schema 的输入）、表达式白名单、URL/域名预检，以及渲染器输出。固定的 basekit 版本与 `expr` 降级，已在一次真实的 basekit 上传中端到端验证。

### SDK 枚举对账 —— 信任闸门

生成器会向 basekit SDK 枚举发出引用（`FieldType.<KEY>`、`NumberFormatter.<KEY>`、`AuthorizationType.<KEY>`、`FieldComponent.<KEY>`，以及一个 `addAction` 鉴权字面量）。一个不在列表内的值会被编译成 `undefined`，并在运行时悄无声息地搞坏已发布的插件，**没有任何编译错误**——曾经就是这样混进了一个幻影 `PERCENT_ROUNDED_2` formatter。为了让这一类 bug 根本无法被发布：

- `scripts/refresh-sdk-enums.sh` 解析 SDK 的 `dist/index.d.ts`，把权威的枚举键写入 `internal/shortcut/testdata/basekit_sdk_enums.json`（黄金基准）。
- `internal/shortcut/sdk_reconcile_test.go`（在 `go test ./...` 下运行）断言**每一个**生成器白名单值都是对应 SDK 集合的子集，并顺带打印覆盖缺口（尚未支持的 SDK 值）。
- CI 双向兜底：若某个白名单偏离了黄金基准，测试就失败；`sdk-drift` 任务会从固定的 SDK 重新提取，若黄金基准自身陈旧则失败（`.github/workflows/ci.yml`）。

升级 SDK 之后：`make sdk-enums`，审阅 diff，测试会指出任何需要更新的白名单。

### CLI — `cmd/shortcutgen`

```bash
# JSON DSL → scaffolded basekit project (no LLM)
go run ./cmd/shortcutgen -out /tmp/exchange-rate \
  internal/shortcut/testdata/exchange_rate.json

# Natural language → field shortcut (needs DEEPSEEK_API_KEY)
DEEPSEEK_API_KEY=sk-... go run ./cmd/shortcutgen \
  -nl "把人民币金额按实时汇率换算成美元，输出美元金额和汇率" \
  -out /tmp/exchange-rate -dump

# Natural language → automation action
DEEPSEEK_API_KEY=sk-... go run ./cmd/shortcutgen -action \
  -nl "拿城市字段查实时天气，返回温度和天气描述供后续步骤使用" \
  -out /tmp/weather-action
```

标志：`-out`（必填）脚手架目录 · `-nl` 自然语言请求 · `-action` 将输入当作一个自动化 Action 处理 · `-dump` 同时把生成的 DSL JSON 打印到 stderr。

### Web 平台

两个服务：一个 **BFF 网关**（`cmd/api`）和 **NL→DSL 生成器**（`cmd/generator`，持有 LLM 密钥）。

```bash
# Terminal A — generator (holds the LLM key)
DEEPSEEK_API_KEY=sk-... PORT=8090 go run ./cmd/generator

# Terminal B — BFF + static web platform
PORT=8080 GENERATOR_URL=http://localhost:8090 WEB_DIR=./web go run ./cmd/api
```

打开 <http://localhost:8080/shortcut.html> —— 在 **字段捷径 / 自动化** 之间切换，输入一个请求，点击 一键生成，审阅**可审计的源码**，并下载工程 `.zip`（或只下载 DSL `.json`）。

### 用户登录与按用户归属（可选）

每个人都可以**用自己的飞书身份**登录，这样他们创建的插件就**归属于、并由他们所有**（源码 + `dsl.json` 中会渲染一行创建者信息，且每个用户在"我的插件"下只看到自己的插件）。在平台上配置飞书 OAuth 即可启用：

```bash
FEISHU_APP_ID=cli_xxx FEISHU_APP_SECRET=xxx \
  FEISHU_BASE_DOMAIN=feishu.cn \
  OAUTH_REDIRECT_URI=https://your-host/auth/callback \
  SESSION_SECRET="$(openssl rand -hex 32)" \
  PORT=8080 GENERATOR_URL=http://localhost:8090 WEB_DIR=./web go run ./cmd/api
```

把 `OAUTH_REDIRECT_URI` 注册到飞书应用的重定向 URL 白名单里。当未设置时，登录被禁用，平台保持匿名（行为不变）。路由：`GET /auth/login`、`GET /auth/callback`、`POST /auth/logout`、`GET /api/me`，以及需 cookie 鉴权的 `GET/POST /api/my/plugins` + `DELETE /api/my/plugins/{id}`。身份使用一个无状态、HMAC 签名的会话 cookie。

**归属持久化**：默认情况下按用户的插件存储在进程内（重启即丢）。要让**归属在重启后持久化**，把它指向一个飞书多维表格的表——加上 `FEISHU_BITABLE_APP_TOKEN`（平台的 Base）+ `FEISHU_PLUGINS_TABLE_ID`（一个带文本字段 `id`、`owner_open_id`、`owner_name`、`title`、`kind`、`dsl`、`created_at` 的表）。每个插件是一条记录；用户永远只看到自己的（按属主作用域读取）。

要用 Claude 代替 DeepSeek：

```bash
LLM_PROVIDER=anthropic ANTHROPIC_API_KEY=sk-ant-... MODEL=claude-opus-4-8 \
  PORT=8090 go run ./cmd/generator
```

### 构建并上传生成的工程

在任意脚手架 / 下载下来的工程内：

```bash
npm install
npm run build    # type-check against the real basekit SDK types
npm run pack     # block-basekit-cli pack:field → output/*.zip
```

然后在你的飞书开发者控制台（字段捷径能力）上传这个 zip，并由管理员审批通过。（自动化动作通过 `testAction` 验证，并用 `block-basekit-cli upload` 上传；该 CLI 没有 `pack:action`。）

---

## 安全模型

生成的源码是为了经受合规团队的敌意审阅而设计的。

- **无 `eval`，无任意代码。** 取值来自一套白名单化的表达式语法，被降级为安全 JS，并对（不可信的）响应使用可选链。LLM、DSL 或一个恶意提示，都没有任何路径能把可执行代码注入到渲染出的 `execute()` 里。
- **出网域名白名单。** 每个外部 host 都在 `domains` 中声明，并被发出为单个 `addDomainList([...])`；在渲染任何 TypeScript 之前，URL 的 host 就已被静态地对照它检查。basekit 运行时强制执行同一份列表——没有第二条、隐藏的出网通道。
- **凭据绝不硬编码。** API 密钥 / token 被声明为 `auth`，由**终端用户**在配置时输入、并由飞书运行时注入；它们绝不出现在生成的 URL 或源码里。
- **可审计 + 溯源。** 每个文件都带有一行 "Generated by feishu-plugin-platform … Human-auditable." 的头注。输出是朴素、可读的 TypeScript——diff 它，读它，然后提交审核。

---

## 仓库结构

```
.
├── cmd/
│   ├── api/             BFF / gateway: app CRUD, NL-generation proxy, /api/execute forward, auth
│   ├── generator/       NL → DSL service (holds the LLM key); /shortcut/* and /action/* endpoints
│   ├── execute-runner/  self-hosted execute runtime — the FaaS replacement for private deployments
│   ├── shortcutgen/     CLI: -nl (NL) / -action (automation) / -out (scaffold)
│   └── bitable-bootstrap/  one-shot helper to create the backing Bitable via app credentials
├── internal/
│   ├── shortcut/        field-shortcut + action DSL: validation, expr allowlist, render, scaffold, zip
│   ├── execrt/          DSL interpreter behind execute-runner (no user JS; SSRF-guarded)
│   ├── generator/       LLM integration: DeepSeek (default) + Claude (opt-in), forced tool call + auto-repair
│   ├── dsl/             AppDefinition DSL for the container renderer
│   ├── store/           definitions + per-user plugins (Bitable-backed, read-cached, table-scoped)
│   ├── auth/            Feishu OAuth + signed session
│   └── api/ · httpx/    BFF handlers + HTTP server helpers
├── plugin/block/        the in-Bitable container widget (opdev) — renders an AppDefinition / enrich DSL live
├── plugin-center/       catalog of example generated plugins (one directory each)
├── web/                 shortcut.html (NL authoring UI) + index.html (mock renderer; dev only)
├── publisher/           opdev / console publishing automation (RPA)
├── deploy/              docker compose (prod) + k8s manifests + Caddy
├── scripts/             release / deploy / publish-plugin / manage-plugins / refresh-sdk-enums
└── docs/                index.html (landing) + PRODUCTION · OPERATIONS · EXECUTE_RUNTIME · ROADMAP · design
```

> **当前两条并行的线，均已上线：**（1）**生成器**——NL → 可审计的 basekit 工程（`internal/shortcut` + `cmd/shortcutgen` + `web/shortcut.html`），走飞书正常的审核链路上传；以及（2）**容器渲染器**——`plugin/block`（opdev SDK）渲染一份从平台 API 拉取的 `AppDefinition`/`enrich` DSL，*直接在一个多维表格内部*渲染，因此一个小团队作者发布的是一份**定义**（数据），而非每次都新过一遍审核的插件。`internal/dsl` + `internal/store` + 该容器属于这条线。一个更早的独立 `@lark-base-open/js-sdk` 渲染器（`frontend/`）已被 `plugin/block` 取代并移除；`web/index.html` 仅作为开发用的 mock 渲染器保留。

---

## 状态与已验证项

- ✅ `go test ./...` 全绿：DSL 校验器、表达式白名单、URL/域名护栏、渲染器输出。
- ✅ 生成的工程对照**真实**的 basekit SDK 类型完成类型检查（`block-basekit-server-api 1.0.6`）。
- ✅ `testField` / `testAction` 发起**真实**的出网调用（汇率、天气、httpbin）并正确写回。
- ✅ 已通过 Web 平台在浏览器中端到端验证。
- ✅ **全链路已证实：** 一个生成的自动化动作被上传（`block-basekit-cli upload`）、发布，随后**在一个真实的飞书 Base 自动化中被安装、配置并启用**。

---

## 路线图

- **字段捷径：** 更多 `FieldComponent` 类型与单一 Object 结果之外的结果种类；更广的 `NumberFormatter` 覆盖。
- **自动化动作：** `APIKey` 之外的鉴权（动作的授权形态与字段不同且文档不足——推迟到一次经验证的后续跟进）。
- **表达式语法：** 在保持严格白名单不变量的前提下，谨慎地拓宽原子 / 运算符。
- **平台：** 生成定义的持久化；从生成 → 审核 → 上传的一键路径。

（参见 `docs/ROADMAP.md`，了解对飞书插件生态更广的能力盘点——注意它在很大程度上勾勒的是更早的容器 / DSL 视图扩展方向。）

---

## License

MIT.
