> 🌐 **中文** · [English](EXECUTE_RUNTIME.en.md)

# 自托管 Execute 运行时设计 · 容器渲染轨的连接器 / 字段捷径执行

> 状态：设计稿（2026-06-23）。对应 `design.md` **Phase 4**、`ROADMAP.md` 🔴 红区（自动化 execute / 字段公式连接器扩展）。
> 设计动机：容器渲染轨的 execute 类能力（字段捷径 / 连接器，要调外部 API 算出一列）需要一个自托管、可审计的执行运行时——把出网 / 凭证 / 审计 / 限流收口到一个可审计的咽喉点；这也是它跑在 k8s 上的原因之一。

---

## 0. 一句话

绿区/黄区的**只读数据视图**靠容器渲染引擎在飞书 webview 客户端跑、不需要任何后端运行时；但红区的 **execute 类插件（字段捷径 / 连接器，要调外部 API 算出一列）需要一个能跑 `execute` 的运行时**。这类能力如果让每个插件各自散调外部 API，出网、凭证、审计、限流就无处收口；公有云第三方字段 FaaS 又被网关锁死。因此 execute 类能力唯一现实的归宿，是**我们自己在 k8s 上托管的一个执行运行时**，把对外调用集中到一个可审计的出网点——本文设计它。

---

## 1. 背景：为什么必须自托管

| 部署形态 | execute 运行时现状 | 结论 |
|---|---|---|
| 飞书公有云 | 第三方**字段** execute FaaS 被网关锁死（仅官方/火山）；官方扩展只开放 记录视图/数据表视图/自动化 三类；`block-basekit-cli` 无字段上传命令；字段捷径官方发布走表单+人工审核 | 不可自助 |
| **容器渲染轨 / 自托管轨** | 出网、凭证、审计、限流需要**集中到一个可审计的出网点**，而非每个插件各自散调外部 API | 需自托管 execute 运行时 |

> 📌 **已修正的过时假设**：`internal/shortcut/dsl.go` 头部曾写「runtime is ALWAYS Feishu's basekit FaaS — there is no self-hostable runtime」。
> 这条假设对**容器渲染轨的 execute 类插件不成立**;注释已更新,本设计把「self-hostable runtime」落为现实。
> 注意保留双路径：上传到飞书的插件仍走「生成标准 basekit 工程 → opdev 上传 → 跑在飞书 basekit FaaS」路径；容器渲染轨的连接器 / 字段捷径执行走「自托管 execute 运行时」路径。同一份 execute DSL，两个宿主。

这也解释了整套架构的分水岭：
- **只读数据视图**（stat/chart/table/gauge/pivot/…，已实现 12 渲染器 9 算子）→ 容器渲染引擎读 DSL，用 opdev 的 `@lark-opdev/block-bitable-api` 在 webview 里直接读宿主数据渲染。**无 execute、无新出网、无写入**，所以无需任何后端运行时。
- **execute 类**（字段捷径/连接器）→ 要发起对外 HTTP（如 Open-Meteo）并把响应映射成输出列。这一步需要把出网/凭证/审计/限流收口到一处 → 必须落到我们的 k8s。

---

## 1.5 官方参照模式与校准（2026-06-23，据官方 Vibe Coding / FaaS 开发指南）

官方文档证实了我们要复刻的模式，并给了关键事实（来源：飞书官方「AI 编程实践」+「字段捷径插件（FaaS 版）- 开发指南」+ `Lark-Base-Team/field-demo` + `@lark-opdev/block-basekit-server-api` 类型定义）：

1. **FaaS 捷径本质 = 部署在飞书服务器的 nodejs 函数**（linux-x64 / Node 14.21.0 / 1核1G / 超时 15min / 2–4 并发 / 队列 1w / 排队 1h）。**这是上传到飞书的插件所走的宿主；容器渲染轨的 execute 则需要我们自托管的等价物。**
2. **官方推荐的「重活外包」模式**：捷径 `execute` 通过 `addDomainList` 白名单 `fetch` 一个**外部后端服务**（官方示例把 Markitdown 部署在 replit + API key 鉴权；示例捷径「AI 文件转文本 / 附件转文本 / 短链生成器」都是这个形态）。**这正是 execute-runner 的角色** —— 区别只是宿主从 replit(公有云) 换成我们自托管的 k8s。
3. **流量验签机制 `baseSignature` + `packID`**：捷径请求外部后端时携带签名头，后端用开发者公钥验签，确认请求确实来自飞书 Base。→ 我们的 runner 在被 webview/api 调用时也要有等价的**请求来源校验**（见 §6 验证模型）。

**校准结论**：execute-runner 定位为**「所有 execute/连接器插件收口的单一自托管后端」**——不是每个插件各自散调外部 API，而是统一经 runner 出网。好处：出网白名单、凭证处理、审计、限流**集中一处**，自托管客户只需审计这一个出网点。这比官方「每个捷径各连各的 replit」更可控，是我们的差异化。

### 1.5.1 从官方权威枚举对账出的生成器修正
- 🐞 **已修**：`shortcut.ValidFormatters` 原含 `PERCENT_ROUNDED_2`（SDK 枚举里**不存在**），LLM 选中会生成无效 `NumberFormatter.PERCENT_ROUNDED_2`。已据 `dist/index.d.ts` 修正为权威集合（`INTEGER / DIGITAL_ROUNDED_1..4 / DIGITAL_THOUSANDS / DIGITAL_THOUSANDS_DECIMALS / PERCENTAGE_ROUNDED / PERCENTAGE`）。
- 📋 **缺口（待评估增量）**：组件缺 `Radio / MultipleSelect / TableSelect`（现有 FieldSelect/Input/SingleSelect）；鉴权缺 `OAuth2 / MultiHeaderToken / MultiQueryParamToken / Custom`（现有 4 种）；输入/结果字段类型缺 `User / Attachment`。这些是 render+validate+test 的增量，按需排期。
- ✅ **已确认对齐**：`Lark-Base-Team/field-demo` 的官方 addField 结构（i18n / formItems FieldSelect+supportType / resultType Object 带 id+isGroupByKey / execute 带 debugLog+fetch 封装 / FieldCode）与我们 `render.go` 产出一致。

## 2. 设计目标 / 非目标

**目标**
1. 在 k8s 上提供一个无状态服务 `execute-runner`，按 execute DSL（`internal/shortcut` 的 `FieldShortcut.Execute`/`Steps` + `Expr`/`Template` 映射）发起受控对外请求并返回映射后的输出。
2. **硬落地三条红线**（见 §4）：不执行用户 JS、不引入新出网域名、不写入。
3. 与现有 `deploy/k8s/`（api/generator/ingress/netpol/pdb、PSS restricted、distroless nonroot）同套编排无缝接入。
4. 自托管客户在自己的 k8s/k3s 上一键起，飞书 webview 容器插件指向客户自己的 runner URL。

**非目标**
- 不跑任意用户 JS。我们生成的 execute **本就是声明式**（URL 模板 + 白名单表达式），runtime 是**解释器不是代码沙箱**（见 §3）。任意 AI 代码片段沙箱是可选的 Phase 4b，默认不做。
- 不做多租户 SaaS / 计费（单租户企业内部自用，沿用既定决策）。
- 不替代只读渲染器；只读视图继续在 webview 客户端跑。
- runtime **不碰写入**——它只 fetch + map + return，不持有 Bitable 凭证。

---

## 3. 执行模型：解释声明式 DSL，而不是跑代码

我们生成器产出的 execute 已经是受约束的声明式计划，不是自由代码：

```
FieldShortcut {
  Domains  []string          // 出网白名单（硬约束）
  Auth     *Auth             // 用户在配置时填的凭证（从不硬编码）
  FormItems[]FormItem        // 输入（城市名…）
  Steps    []Step            // 有序多步：step.url 含 {city} / {geo.results.0.latitude} 占位
  Execute  Execute           // 或单步
  Result.Properties[].Expr   // res.<json路径> / in.<输入> + - * / ( ) rand()，编译期白名单校验，绝不 eval
  Result.Properties[].Template // {key} 占位的纯字符串拼接
}
```

> 这正是「城市→天气」那个已验证逻辑的形状：step `geo` 取经纬度 → step `weather` 用 `{geo.results.0.latitude}` 取温度 → `Expr = res.current.temperature_2m` 映射成「当前温度」列。我已在本机实测它返回真实数据（北京 29℃、上海 22.2℃…）。

**关键设计决策：runtime 是一个 DSL 解释器。**（已实现，见 `internal/execrt`）
- `expr.go` 原本只**校验 + 翻译成 JS**（编译期），没有运行期求值器。所以 `internal/execrt/eval.go` **新写了一个 Go 求值器**，实现与 `exprHelperDefs` 完全一致的函数语义；并**复用** `shortcut.ValidateExpr` 做求值前的白名单校验。一个 parity 单测（`TestExprFuncParity`）断言解释器实现的函数集合 == `shortcut.ExprFuncNames()`，保证解释器永不与编译器漂移。
- 对每个 Step：把 `{占位符}` 用「输入 + 前序步骤响应」插值 → `shortcut.CheckURLHost` 校验 URL host ∈ `Domains` → 发请求 → 存为 `<stepId>` 命名空间。
- 对每个 Result 属性：用 `Expr`/`Template` 在 `in.*` 和 `res.*` 命名空间上求值（白名单算子）。
- **全程没有 `eval`、没有跑用户 JS** → 红线①「不执行用户 JS」由构造保证。

> Phase 4b（可选、默认不做）：若未来要支持模板覆盖不到的任意 AI 代码片段，再引入 `quickjs-emscripten`（WASM 沙箱，无宿主访问）或每应用一 k8s pod。届时本服务从「解释器」升级为「解释器 + WASM 沙箱」，但默认路径永远是声明式解释。

---

## 4. 安全模型：三条红线如何硬落地

| 红线 | 落地手段 |
|---|---|
| **不执行用户 JS** | runtime 解释声明式 DSL（URL 模板 + 白名单 Expr），从不 eval。Expr 复用 `internal/shortcut/expr.go` 的白名单求值（仅 `in.*`/`res.*` 取值 + `+-*/()` + `rand()`）。任意代码片段→默认拒绝，Phase 4b 才进 WASM 沙箱。 |
| **不引入新出网域名** | **双层强制**：①运行时层——每个出站 URL 解析 host，∉ 该插件 `Domains` 即拒绝（`shortcut.CheckURLHost`）；②k8s 层——`execute-runner` 的 **egress NetworkPolicy / 出网转发代理白名单**，即使解释器有 bug，pod 也只能到声明过的 host。自托管客户审计「这个插件能连哪些外网」= 看 `Domains`。**SSRF 守卫**额外拒绝 dial 到私网/环回/链路本地 IP；当配了出网代理（`HTTP_PROXY`，即生产 egress 控制面）时放行对**该代理地址**的连接（代理是出网控制点，host 白名单仍生效），但不放行其它私网目标。 |
| **不写入** | runtime **只 fetch + map + return**，不持有任何 Bitable / tenant 凭证，没有任何写宿主数据的代码路径。写入（若将来要）只能由 webview 里的 SDK 在用户权限下做，且是另一条显式门禁路径——不在本 runtime 内。 |

**纵深防御（沿用现有 `deploy/k8s` 基线）**
- PSS `restricted` namespace；distroless nonroot（uid 65532）、`readOnlyRootFilesystem`、`drop ALL caps`、`seccomp RuntimeDefault`、`allowPrivilegeEscalation:false`——直接抄 `20-api.yaml` 的 securityContext。
- 资源 `requests/limits`（CPU/内存）+ 每请求**超时**（防慢响应/SSRF 拖死）+ 响应体大小上限（已生成代码里有 `text.slice(0,4000)` 雏形）。
- 入站 NetworkPolicy：只允许 `api`（或渲染器经 ingress）打到 `execute-runner`，default-deny 其余。
- **用户 Auth 凭证**（如某 API 的 key）：随请求传入、用完即弃，**不在 runtime 落盘/缓存**；自托管下凭证不出客户集群。
- SSRF 防护：URL host 必须命中 `Domains` 白名单（已是红线②）；额外禁止解析到内网/链路本地地址（拒 `169.254/10./172.16/192.168/127.`）。

---

## 5. 架构定位与调用链

```
飞书 webview（不在 k8s）                 客户自有 k8s / k3s 集群
┌─────────────────────────┐            ┌──────────────────────────────────────┐
│ 容器插件 + 渲染引擎       │            │  Ingress(TLS)                          │
│  · 只读视图：本地渲染     │            │    ├── api (BFF)        Deployment×2   │
│  · execute 类：HTTPS 调 → │──────────▶ │    │     · 定义 CRUD / 生成代理        │
│    runner                │  (需在飞书   │    │     · 代理转发 → execute-runner   │
│                          │  安全设置白  │    ├── generator         (内网)        │
│                          │  名单 + TLS) │    ├── execute-runner ★  Deployment+HPA│  ← 本文新增
└─────────────────────────┘            │    │     · 解释 execute DSL            │
                                        │    │     · 出网仅限 plugin.Domains     │
                                        │    └── (egress 白名单代理 / netpol)    │
                                        └──────────────────────────────────────┘
```

- **execute-runner** = 新增 k8s `Deployment` + `Service`（+ `HPA`，突发型负载）。
- 调用方两选一（见 §8 开放问题）：
  - **A. webview 直连 runner**（经 ingress，需 TLS 域名 + 飞书「安全设置→服务器域名白名单」加该域名，和现有后端域名同样的约束）；
  - **B. webview → api → runner**（api 做鉴权/限流/审计的统一入口，runner 仅集群内可达）。**推荐 B**：复用 api 已有的 Bearer 鉴权 + 限流 + 请求日志，runner 不暴露公网、入站只收 api。

---

## 6. API 契约（execute-runner）

```
POST /execute
Authorization: Bearer <EXECUTE_RUNNER_TOKEN>    # 若走方案B，由 api 注入；runner 侧用 PLATFORM_API_TOKEN 读取并校验
Content-Type: application/json

{
  "pluginId": "city-weather-query",     // 或 inline "dsl": {FieldShortcut...}
  "inputs":  { "city": "Beijing" },     // FormItems 的取值（渲染器从宿主单元格取）
  "auth":    { "weatherApiKey": "..." } // 可选，用户在配置时填的凭证；用完即弃
}

200 OK
{
  "ok": true,
  "data": { "temperature": 29.0, "wind_speed": 12.8 }   // Result.Properties 映射结果
}

4xx/5xx
{ "ok": false, "error": "domain_not_allowed: example.com ∉ [api.open-meteo.com,...]" }
```

- 无状态、幂等（同输入同输出，除 `rand()` 生成的 `_id`）。
- 12-factor：配置走 env / ConfigMap / Secret；可被现有编排平滑接入。
- 渲染器拿到 `data` 后，按 Result 定义渲染成单元格/列（沿用现有渲染器层）。
- **已实现**：`/execute` 收 inline `dsl`；M2 验证用的就是这个形态。
- **令牌契约（已落地，B1 能力分离）**：
  - **client → api**（`POST /api/execute`）= **只读** `PLATFORM_READ_TOKEN`（与渲染所需的 `GET /api/apps*` 同一只读令牌，可安全嵌入客户端 bundle；泄露也只能读/算，不能改目录、不耗 LLM 预算）。
  - **api → runner** = `EXECUTE_RUNNER_TOKEN`（仅服务端持有，`cmd/api/main.go` 注入到转发请求的 `Authorization: Bearer`）；runner 侧用 `PLATFORM_API_TOKEN` 读取并校验同一个值（`cmd/execute-runner/main.go`）。两条边各用各的令牌，client 永远拿不到 runner 令牌。
- **并发上限（已落地）**：runner 用 `EXECUTE_MAX_CONCURRENCY`(默认 64) 限制在飞请求数；过载即 **HTTP 429 + Retry-After** 卸载，而不是把 pod 拖死（`cmd/execute-runner/main.go`）。

### 6.1 收口模型：pluginId 优先（校准自官方模式）

官方模式里每个捷径各连各的后端；我们**收口到单一 runner**，所以请求优先用 `pluginId`：
- **`pluginId`（推荐，收口）**：api（call-chain B）按 id 从定义存储（多维表格 Store）取出已注册的 DSL，转发给 runner。webview 只发 `{pluginId, inputs}`，**不把 DSL 暴露到客户端**，且出网域名集合由后台注册的定义集中决定/审计。
- **inline `dsl`（已实现，调试/无存储场景）**：直接把 DSL 塞进请求。runner 仍 `Validate()` 防御。
- pluginId 查存储的转发逻辑 = **M3**（api → runner），runner 侧 `/execute` 两种入参都支持。

### 6.2 请求验证模型（对应官方 baseSignature + packID）

官方：捷径请求外部后端带 `baseSignature`（飞书签名）+ `packID`，后端用开发者公钥验签确认来源。我们的等价物：
- **call-chain B（推荐）**：webview→api 用只读 Bearer（`PLATFORM_READ_TOKEN`）+ CORS 收敛到飞书 webview Origin；api→runner 集群内 + Bearer（`EXECUTE_RUNNER_TOKEN`，runner 侧读作 `PLATFORM_API_TOKEN`）；runner 不暴露公网。验证集中在 api，runner 只信任 api。
- **call-chain A（webview 直连 runner）**：才需要把官方 `baseSignature` 验签搬到 runner（公钥验签 + `packID` 校验），防止他人直接打 runner。默认走 B 即可回避。
- 自托管下两端都在客户域内，信任边界更短；但 Bearer + CORS + TLS + 域名白名单仍是底线。

### 6.3 出网账本 / Egress Ledger（已落地）

「这个插件到底连了哪些外网、连成没连成」从一句设计目标变成了**逐跳落库的审计事实**。每一次出站尝试（多步链里的每一跳）在 `execrt.fetch` 咽喉点经 `EgressRecorder` 接口发出**一条** `action=execute.egress` 审计事件，写进与平台审计同一张 `audit_log` 表，可经管理员 `GET /api/audit` 回看：

- **字段**：`actor=plugin:<id>`（归属 = api `/api/execute` 透传的平台 pluginId，缺失时回退 `fs.ID`）、`target=<host>`、`detail=method/outcome/step`。
- **拦截即审计**：SSRF / 重定向 / 域名白名单等拦截以 `outcome=error` 落库——「被拦的出网」和「成功的出网」一样留痕。
- **热路径安全**：stdout **永远**先打一行；落库走**单 worker 异步缓冲**（1024 缓冲，满则丢审计写、绝不阻塞 execute，丢弃计数另行记账）。
- **优雅 drain**：`SIGTERM → HTTP 先停 → worker 把缓冲冲完再退`（10s 上限），所以 pod 重启不丢已缓冲记录。
- **落库前提**：runner 需配 `FEISHU_APP_ID/SECRET/BITABLE_APP_TOKEN` + `FEISHU_AUDIT_TABLE_ID` 才持久化；任一缺失 = 仅 stdout（`audit_log` 表由 `bitable-bootstrap` 创建）。
- 实现：`internal/execrt/engine.go`(`EgressRecorder` 接口 + `fetch` 咽喉点)、`cmd/execute-runner/main.go`(异步缓冲 + drain)、`internal/api/execute.go`(透传 pluginId 归属)。测试：`internal/execrt/egress_test.go`。

---

## 7. 与现有 deploy/k8s 集成（落地清单）

| 文件 | 改动 |
|---|---|
| `deploy/k8s/15-execute-runner.yaml`（新增） | `Deployment`（镜像 `feishu-plugin-platform/execute-runner`，securityContext 抄 `20-api.yaml`）+ `Service` + `HPA` |
| `deploy/k8s/40-netpol.yaml` | 加：①入站—只允许 `app: api` 打 `app: execute-runner`;②出站—`execute-runner` egress 仅放行 DNS + 白名单 host（需 Calico/Cilium 才真正生效，flannel/kindnet 是 no-op，文档已注明） |
| `deploy/k8s/00-namespace-config.yaml` | ConfigMap 加 `EXECUTE_RUNNER_URL`（api 转发用）、超时/体积上限等参数 |
| `cmd/execute-runner/`（新增 Go 服务） | 复用 `internal/shortcut`（DSL 类型 + `expr.go` 求值 + Domains 校验）；纯标准库 HTTP；与 api/generator 同构 |
| `internal/shortcut/dsl.go` | 修正头部「runtime is ALWAYS Feishu FaaS / no self-hostable runtime」过时注释为「双路径：上传飞书=basekit FaaS；容器渲染轨=自托管 execute-runner」 |

> **出网白名单代理可选实现**：若集群 CNI 不支持 egress netpol，用一个 forward proxy（如带 allowlist 的 squid/envoy）做 `execute-runner` 的唯一出网口，runner 的 `HTTP_PROXY` 指向它，代理按全集群插件 `Domains` 并集放行。这样红线②不依赖 CNI。

---

## 8. 决策（用户 2026-06-23 拍板「按建议」）+ 剩余开放项

**已定：**
1. **调用链 = B**（webview→api→runner，runner 不公网暴露，验证/限流/审计集中在 api）。
2. **runtime 只读** —— 严格 fetch + map + return，不持 Bitable 凭证、无写路径（红线③）。写回宿主列（若将来要）走 webview SDK 在用户权限下做，另案。
3. **用户 Auth 凭证** —— 自托管下只存客户集群（Secret / 定义表），绝不出集群；运行时随请求带入、用完即弃、不落盘。
4. **执行模型 = 解释声明式 DSL**（红线①由构造保证）；Phase 4b 的 quickjs 任意代码沙箱**默认不做**，仅当模板覆盖不到再上。

**已落地（原开放项）：**
- **出网逐跳审计** ✅ —— 出网/拦截已逐跳落 `execute.egress` 审计账本（见 §6.3），「这个插件连了哪些外网、连成没连成」可经 `GET /api/audit` 回看；不再是开放项。

**剩余开放项：**
5. **出网白名单强制层**：CNI egress netpol（需 Calico/Cilium）还是 forward proxy？取决于客户集群 CNI（M3/M4 按客户环境定）。
6. **可观测（指标层）**：execute 调用的成功率/延迟还可补一层 metrics 打点（审计账本已覆盖逐跳事实，呼应「余额/故障第一时间发现」既有纪律）。
7. **凭证复用**：同一插件多次执行是否在客户集群内加密存一份凭证复用，还是每次由 webview 配置带入？（影响 §6.2 与凭证 UX）

---

## 9. 里程碑

- **M1 设计**（本文）✅。
- **M2 解释器服务** ✅ **已完成并真实验证**：`cmd/execute-runner` + `/execute` API；`internal/execrt`（`eval.go` Go 求值器 + `engine.go` 多步/单步 fetch + 映射 + SSRF 守卫）；复用 `internal/shortcut` 的校验/Domains。单测覆盖：算术/函数/条件/路径/rand、func parity、多步链、Domains 拒绝、SSRF 拒绝、单步 QueryParam 鉴权、非法 DSL 拒绝、**出网账本（`egress_test.go`：逐跳事件 + 拦截=error + 异步缓冲/drain）**。`go build/vet/test ./...` 全绿。**真实端到端冒烟**：服务实跑「城市→天气」DSL，对真实 Open-Meteo 返回 Beijing 26.3℃ / Tokyo 19.6℃ / London 30.6℃ / Paris 33.9℃（多步链：地理编码→天气，纯自托管解释，无飞书 FaaS）。
- **M3 上线** ✅ **已完成并端到端真实验证（2026-06-25）**——**生产形态 = AWS EC2 上单节点 docker compose + Caddy 自动 TLS（Let's Encrypt 经 `<ip>.sslip.io` magic-DNS 主机签发），`STORE=bitable`**；`deploy/k8s/` 是**可选的未来横向扩展路径，不是当前主线**：
  - ✅ `deploy/compose/docker-compose.prod.yml` 把 execute-runner 与 api/generator 一起跑在 EC2；k8s 物料（`deploy/k8s/15-execute-runner.yaml` Deployment×2 + Service + HPA，securityContext 抄 api，SSRF 守卫 ON；`Dockerfile.execute-runner` distroless nonroot；netpol `allow-api-to-execute-runner` + `execute-runner-egress`）保留为可选扩展路径。
  - ✅ ConfigMap/compose 注入 `EXECUTE_RUNNER_URL` + `EXECUTE_RUNNER_TOKEN`（api 侧）/ `PLATFORM_API_TOKEN`（runner 侧读取同值校验）+ `EXECUTE_MAX_CONCURRENCY`。
  - ✅ **调用链 B 已实现并真实验证**：`POST /api/execute`（`internal/api/execute.go`，client 用只读 `PLATFORM_READ_TOKEN`）转发到 runner；支持 inline `dsl` 与 `pluginId`(会话+插件存储取 DSL,收口模型)；单测覆盖（503未配置/inline转发+Bearer/缺参/pluginId需登录）。
  - ✅ **真实生产端到端（EC2，`STORE=bitable`）**：真 client → 真 api(`/api/execute`) → 真 runner → 真 Open-Meteo，并把每跳 `execute.egress` 落进飞书 Base 的 `audit_log` 表（经 `GET /api/audit` 可回看）。
- **M4 加固**：资源配额、出网代理、（按需）Phase 4b 沙箱 per-app pod 隔离。

---

## 附：与 ROADMAP 红线的关系

`ROADMAP.md` 把 execute/连接器列入 🔴 红区，三条不可越红线 = 不执行用户 JS · 不引入新出网域名 · 不写入。本设计**不是突破红线，而是给红区一个受控落地容器**：解释器（非 JS 执行）+ Domains 双层白名单（不引入新出网）+ 只读 fetch（不写入）。绿/黄区继续在 webview 客户端跑，红区在自托管 k8s runner 跑，分层清晰。
