> 🌐 **中文** · [English](ROADMAP.en.md)

# 能力路线图:飞书插件生态 × 我们的容器/DSL 平台覆盖度

> 产出方式:6 路并行 Web 调研飞书插件生态 → 按本平台「预审容器 + 只读数据 DSL」模型分级 → 对抗式复核定稿(2026-06-22)。
> 去重后约 **30 类**:🟢 5 / 🟡 15 / 🔴 12。

## 0. 核心原理(边界从哪来)

「一键生成 + 零审核」成立的**唯一前提**:产物是 **DSL(数据)**,被一个**只审一次的容器**解释渲染。
能一键生成的范围 ≡ **容器 DSL 能表达的范围**。瓶颈不是自然语言生成(DeepSeek 早就够),是容器解释器的能力。

**三条不可越的红线**(越过任一条 = 击穿零审核前提 → 必须降级到「导出独立插件」):
1. **绝不执行**用户产物里的任意 JS。
2. **绝不引入**新的出网域名(受飞书「服务器域名白名单」硬约束——真机已证)。
3. **绝不写入**宿主数据 / 不改已审权限 scope。**写入是分级的真正分水岭,不是「只读 vs 可视化」。**
   - ⚠️ 暗门:渲染宿主单元格里的 Markdown/富文本若不强消毒 = 不写一行用户 JS 也能注入 JS(数据通道 XSS)。绑定字段一律默认纯文本转义。

---

## 🟢 绿区:当前 DSL 直接覆盖 / 微扩即可(真·一键 + 零审核)

| 类型 | 现状 |
|---|---|
| 基础图表(柱/条/折线/面积/饼/环) | 已实现 `chart`(x=分组 / y=聚合)+ `bind.tableId`;**真机订单看板已用真实数据验证渲染正确** |
| 指标卡 / KPI | 已实现 `stat`(sum/avg/count/max/min);真机「订单总额/订单数/总销量」已正确 |
| 指标趋势(按时间分组折线) | = `chart(line, x=日期字段)`,无新积木 |
| 排行榜 / TopN | 现有聚合管线加 `sort desc + limit N` 修饰符,微扩、只读 |
| 文本/标题块 | 已有 `text`;**绑定字段必须强制纯文本转义,不得当 HTML/Markdown 解析** |

---

## 🟡 黄区:容器可做,但需新积木 / 第二容器(成本 S→L 递增)

| 类型 | 要加的 DSL 积木 | 量 | 备注 |
|---|---|---|---|
| **容器解释器/渲染层(地基)** | 「聚合算子 + 渲染器」两层(register 模式);跨表只读数据访问层 | M | **真正的资产**。加图表=注册渲染器,加统计=注册算子,只审解释器本体一次。stat/chart/text 已真机跑通,缺显式抽象 |
| **明细表(选列+筛选)** | table 渲染器;**真正实现 filter 算子**(当前是「过滤未执行」占位 stub);行级只读(注意批量读 ≤200) | M | **阻塞多项的前置:先把 filter 做成真算子**,否则一切「筛选后展示」是空头支票 |
| 进度图 / 目标达成(gauge) | gauge/progress 渲染器(current + target) | S | 逻辑简单,仅因需新渲染器列黄 |
| 单元格 Markdown/长文本预览 | markdown 渲染器 + **DOMPurify 级强消毒**(禁 script/iframe/on*/javascript:) | M | **安全红线格**:消毒层有洞 = 零审核前提失效 |
| 透视表 / 透视图 | pivot 积木(两分组字段 + 一聚合)+ 二维分组 | M | 纯聚合,无新原子算子 |
| 高级图表族(漏斗/雷达/散点/桑基/瀑布/直方/箱线/词云/热力) | chart.type 枚举扩展 + 新算子(cumsum/quantile/分桶/流向/词频) | L | 先上纯聚合的(漏斗/雷达/散点),需新算子的放后 |
| 时间轴 / 时间线 | timeline 渲染器(日期+事件,只读铺排) | S | 接近绿区 |
| 层级 / 树形 | tree 渲染器(parent/双向关联→构树) | M | 只读展示,受控折叠 OK |
| 只读原生视图镜像(看板/日历/画册) | 只读 kanban / calendar / gallery 渲染器 | L | **明确「只读呈现」,绝不承诺拖拽写回**(写回即跌红区) |
| 记录视图扩展(单记录排版/卡片/打印) | record-layout DSL + 只读 active record + 浏览器原生 print | M | **需第二个已审容器**(记录视图位 blockTypeID);建议 v2 |
| 倒计时 | countdown 渲染器 + 解释器内置受控 tick | S | 动态由解释器提供,非用户 JS |
| **地图视图** | map 渲染器 + 地图商域名固定进已审白名单 + key 后端持有 + 地理编码后端预处理 | L | **黄区最危险一格**:唯一需出网三方。允许用户自填地图源/key 即跌红区 |
| **模板化轻自动化**(定时/阈值→发卡片/写一条记录) | automation-lite DSL + 平台后端调度器 + 平台机器人身份 | L | **含写入,越过纯只读边界**:写入只能后端用平台凭证、动作集白名单可枚举;否则转红区。与官方「应用模式」正面交锋 |

---

## 🔴 红区:零审核做不到 → 走「导出独立插件 + 送审」逃生舱

需「任意代码 / 三方 API·OAuth / 服务端逻辑 / 写回工作流 / 新原生能力」,容器能力域外:

- 自动化扩展(execute 服务端逻辑,如 OCR 写回)
- AI/公式字段捷径(基于 Coze/企业接口批量写回)
- 字段扩展(自定义字段类型)、公式扩展(自定义函数)
- 数据连接器(如 Jira 双向同步)
- 应用机器人 / 流程机器人 / 链接预览 / 消息卡片回调
- 网页应用(H5)/ 小程序(Gadget)
- 工作台小组件 / 云文档小组件(可写回 + 新宿主位)
- 浏览器扩展(网页剪藏→写 Base)
- aily 智能体 / aPaaS 低代码 / AnyCross / **官方「应用模式」(正面竞品)**
- CRM/项目/HRM/财务/BI/OA/工单 等业务 SaaS 大类

> 逃生舱定位:把这些做成「**一键生成脚手架代码 + 引导送审**」的**付费增值产品线**,而非零审核承诺。

---

## 战略要点

1. **护城河 = 只审一次的容器 + 纯数据 DSL**;靠死守上面三条红线维持。任何让用户自填 API/key/代码的功能(哪怕一处)都击穿前提——宁可放红区导出。
2. **第一版死守「只读数据看板」**:绿区 + 低成本黄区(filter/table/进度/时间轴/透视)能吃掉飞书「仪表盘 + 只读可视化」几乎全部需求。
3. **最高优先工程项 = 把 filter 从「未执行徽标」做成真算子**——它阻塞明细表/筛选看板等多项。
4. **把容器抽象成两层(聚合算子 + 渲染器)**:让黄区高级图表从「线性逐个加」变成「加积木、只审一次」。这是「绕过逐个审核」洞察的工程落地。
5. **两条产品线划清**:零审核容器产物(数据 DSL) vs 需审独立插件(代码),避免对用户过度承诺。
6. **竞品 = 官方「应用模式 + AI 工作流」(2025-11 上线)**:「做完整业务系统」维度打不过。差异化 =(a)跨租户模板一键复用;(b)更轻、嵌任意 Base 视图位即用的只读看板;(c)把字段捷径/Coze 当出海口而非对手。

---

## 一句话

**我们一键生成的是「声明式数据视图」,不是「任意插件」。** 把这句话当产品边界,绿区 + 低成本黄区就是清晰、可赢、且零审核护城河成立的战场。

---
---

# 生成器主线路线图(字段捷径 / 自动化 + 企业上架)

> 上面是**容器/视图组件**模型的覆盖度;本节针对已成主线的**字段捷径(addField)/ 自动化(addAction)NL 生成器 + 导出模式**。
> 产出方式:多智能体工作流——并行审计真实代码(`internal/shortcut/*`、`internal/generator/*`、`publisher/`)+ Web 调研飞书企业上架 + 事实核查 + 综合(2026-06-23)。

## A. 能力建设路线图

核心杠杆 = **「测试转生成」闭环**:平台已渲染 `test/index.ts`(跑 testField/testAction),但生成器**从不执行它**,自动修复循环只吃静态 `Validate()` 报错(`shortcut_llm.go:215`)→ 插件能"编译过却返回错数据"。接通它才能从"能编译"升级到"真返回对值",也是可信自动发布的判分信号。

按性价比(value/effort)排序:

| 优先 | 能力 | 价值 | 量 | 关键点 |
|---|---|---|---|---|
| ✅1 | **表达式条件逻辑**(比较+三元)— **已完成 2026-06-23** | 高 | M | 已加 `eq/ne/gt/gte/lt/lte`+`and/or/not`+`if/coalesce/default`+`floor/ceil/abs/min/max` 为**白名单纯JS函数**(`expr.go`),解析器与 `< > = ? : & \|` 禁令不变、仍不 eval。已真机验证:`plugin-center/idcard-gender` 按身份证 17 位奇偶判性别,build:field 编译通过 + testField 真跑(`…0011`→男/`…0028`→女);NL→DeepSeek 也正确产出 `if(eq(substr(...) % 2,1),'男','女')`。修了 `string % 2` 的 TS 严格算术报错(helper 返回类型标 `any`) |
| ✅2 | **验证过模板库 / few-shot**(已完成 2026-06-23) | 高 | S | `internal/generator/exemplars/{field,action}.json` 内嵌 6+1 个验证过的「NL→DSL」范例(go:embed),`exemplars.go` 按中文 bigram+英文词重叠检索最相关 2-3 个注入 system prompt。单测保证范例恒 `Validate()`(不漂移)。真机:NL「英译中」→ 模型复现范例里的 `res.responseData.translatedText` 嵌套路径并编译通过 |
| ✅3 | **测试→生成闭环**(已完成 2026-06-23) | 高 | L | `verify.go` 的 `Verifier`:`Validate()` 通过后用 `block-basekit-cli build:field` **对真 SDK 编译**,把编译错回喂同一修复循环(原本只吃静态 Validate 错)。opt-in(`VERIFY_BUILD=1`+`BASEKIT_NODE_MODULES`),无工具链则优雅跳过(`errVerifyUnavailable`)。真机证明捕获类:`substr(in.text)`(缺参)**过 Validate 但编译报 TS2554** → 被 build 捕获;单测 fakeVerifier 证明编译失败驱动修复轮 |
| ✅4 | **写路径栈**全部完成(2026-06-23):✅action bodyJson + ✅`PUT/PATCH/DELETE`+✅自定义header + ✅多步链式 | 高/中 | M→S→L | `ValidMethods` 加 PUT/PATCH/DELETE;`Execute.Headers`;body/bodyJson 适用 POST/PUT/PATCH;统一 `renderFetchInit`。**多步链式**(`steps.go`):`Steps []Step`(≤3),每步响应绑 `s_<id>`、末步别名 `res`;占位符 `{input}` 与 `{priorStepId.json.path}`(支持数组下标)在 url/headers/body 解析;前向引用/与 execute 并存/与 auth 并存均被拒。真机:httpbin 两步链(field·testField + action·testAction:step2 用 step1 的 .method,回显正确);**live NL「城市→经纬度→天气」生成出 `${s_geo?.[0]?.lat}` 跨步数组引用并通过校验**。**连接器的技术前置(结构化体+REST写+链式)已全部就绪。** |
| ✅5 | 结果列类型扩展**全部完成**(标量+Url+MultiSelect,2026-06-23) | 中 | M | 已加 **Phone/Email/Currency/Progress/Rating/Barcode**(标量)+ **Url**(`{text,link}` cell)+ **MultiSelect**(`string[]`,新增 `split(text, ',')` expr 函数产数组);按 SDK 修了 **primary 列必须 Text\|Number**。全部 `build:field` 对真 SDK 编译过 + testField 形状正确;**Url 的 `{text,link}` 与 MultiSelect 的 `string[]` 都对真 Base 字段写读确认**(write code:0,读回正确)。plugin-center 加 `github-homepage`(Url)、`tags-split`(MultiSelect)。**结果列类型已无显著缺口。** |
| 6 | 分页/数组迭代;附件二进制 | 中/低 | L | 现仅取定长 `res.list.0.x`;附件 SDK 不确定性高→放最后 |
| ✅🎯 | **连接器 / 写回 Base 记录**(方案A 完成 + 真机写入确认 2026-06-23) | 高 | L | 选 A=action 调飞书 OpenAPI。**关键发现:多步链式已直接解锁它,零新生成器代码**——2 步管线(step1 取 `tenant_access_token` → step2 `bitable/v1 .../records/batch_create`,头 `Authorization: Bearer {token.tenant_access_token}`,体 `{records:[{fields:{列名:{input}}}]}`)。**端到端真机验证(用户授权)**:testAction → 真往测试 Base 写入 1 条记录 `code:0 record_id=recvnkoBDe0ohs`,读回字段 `{标题,正文}` 完全正确,临时表已清理。另:DSL 校验+scaffold、tsc 对真 SDK 编译过、token 链路 `code:0`、live NL 也能生成。生成器加了 action prompt 连接器配方 + few-shot + plugin-center `feishu-record-writeback`(凭证/Base 为运行时输入,绝不烤入)。**连接器(写回 Base 记录)成立。** client-side 视图轨(方案B)仍未开,按需再说 |
| 旁支 | 仪表盘/自定义视图、Basic/OAuth2 补全 | 低/中 | L/M | dashboard 代码在休眠 `internal/dsl` 轨但零真机验证;另一套 SDK + 另一审核点,按需再说 |

**建议节奏**:⭐1+⭐2(天级、立刻提质)→ ⭐3 闭环 → 写路径栈 → 连接器战略冲刺。

## B. 符合企业上架的发布流程

**结论(信创/政企):走「导出模式 + 关闭免审 + 管理员人工审」。**

| # | 阶段 | 谁 | 合规闸 |
|---|---|---|---|
| 0 | 需求受理:一句话→DSL,`dsl.json` 存 NL+DSL | 业务方/平台 | 审计留痕起点(provenance) |
| 1 | 编译期硬门禁:expr 白名单不eval、`url host ⊆ domains`、result≤20、最小scope | 平台 | 源码机器初筛+出网白名单+权限最小化 |
| 2 | **源码安全人审**:逐行读 `index.ts`/`register.ts` + SCA/SBOM | 安全合规(企业自办) | **核心人审**——飞书自建应用不过官方源码审,这道闸政企必办 |
| 3 | 建应用+登记扩展拿 APP_ID/blockTypeID(publisher RPA) | 平台/管理员 | 载体就绪 + 开通最小 scope |
| 4 | 官方 `block-basekit-cli upload` 推 bundle | 平台/管理员 | 运行期出网:runtime 强制 `addDomainList`+`context.fetch` |
| 5 | 建版本+申请发布:**可用范围先收敛到小范围/测试人群** | 平台/管理员 | 版本与可用范围 + 任一变更强制重审(可证伪钩子) |
| 6 | **管理员人工审批放行**(或 `PATCH app_versions status=1`) | 管理员+合规 | 唯一强制审批闸——**显式关掉免审**,不让版本静默上线 |
| 7 | 上线+真机联调+成员行为审计验收,再逐步放大可用范围 | 管理员/合规 | 留痕收口 + 数据安全验收 |

**容器 vs 导出**(审一次 vs 每个审):
- **导出模式**(主线):每插件独立可审计 TS,逐个 upload+逐个审,每个出网域名/scope/版本强制走审 = 政企要的可证伪钩子。
- **容器模式**:容器只审一次,生成物是 DSL 数据。后端鉴权已做**能力分离**(客户端只内嵌只读 `PLATFORM_READ_TOKEN` / admin `PLATFORM_API_TOKEN` 管写删 / 会话),即便客户端 token 泄露也只能读。**遗留**:widget 在 Bitable webview 内尚非真·per-user 身份(需 webview-OAuth),强多租户/对外敏感场景仍建议走导出模式或补 per-user。

**必补缺口**(否则不算企业级合规):
1. **发布审计流水账**:publisher 现仅截图做校准,无"谁/何时/哪租户/哪版本/源码哈希"不可篡改日志,也没做**上传 bundle 与受审源码一致性比对(attestation)**。→ 即下「企业增强路线」#1/#2(持久化审计账本 + 出网账本)。
2. ~~**共享 token 升级**~~ → **已做**:能力分离鉴权落地(客户端只读 / admin 写删 / 会话),见 [PRODUCTION](PRODUCTION.md) §7「鉴权 / 安全」。

> ⚠️ **须在目标控制台亲核**(事实核查低置信):① 飞书官方介绍页当前确认 GA 的扩展类型为**记录视图 / 数据表视图 / 自动化操作 + 字段捷径(server 能力)**;"连接器/仪表盘"作为官方插件类型未在官方页确认(或属路线图/旧版)。② 免审默认人数、私有化部署 OAuth2 行为网上常错,以私有化控制台为准。

---

# 企业增强路线(让 10 万人"用得爽")

> 产出方式:多智能体 workflow——5 个企业视角(治理 / 作者体验 / 采用分发 / 集成连接 / 运维可信)并行挖掘 + 去重排优先级 + 4 维代码审计对抗验证(2026-06-24)。
> 排序 = 企业增益 / 单位工作量(高→低)。工作量:S≈1 天 · M≈2–4 天 · L≈1 周+。**全部建立在已落地的能力分离鉴权 + Bitable 存储 + 自托管 execute-runner 之上,多为薄复用而非重写。**

## 第一梯队(最高 ROI)

| # | 能力 | 量 | 它解决什么 | 怎么建(复用现成) |
|---|------|----|-----------|------------------|
| 1 | **持久化审计账本 + 只读查看器** | M | 把现在的 `AUDIT` stdout 行(重启即丢)变成可筛选、抗篡改的 who/when/what/which-version 痕迹——正是上文「必补缺口 #1」 | 新增 AuditStore,**完全复刻 BitablePluginStore 模式**把记录 append 进专用 Bitable 表;`server.go` 两处 `log.Printf("AUDIT")` 改为 store append;`GET /api/audit`(admin)。管理员在 Base UI 原生查询,零新控制台 |
| 2 | **execute-runtime 逐次出网账本** | M | 记录「哪个插件把哪行数据发给了哪个外部域名、为谁、放行/拦截」——信创安全团队要的 DLP 出网证据,坐实 README 已宣称的"出网集中审计" | runner 已在 `execrt.Engine.Run` 收口每次出网且已知 host + PluginID;每次 fetch 落一条结构化记录进 #1 同一张审计表。全平台合规敏感度最高的一点 |
| 3 | **UI 内"试运行"(dry-run)** | M | 小白在走上传+审核链路前,先看插件真调 API、产出真值——杀死"生成黑盒→盲传→等审→发现错了"的循环 | execute-runner 已能跑 DSL+inputs 返回映射结果;`web/shortcut.html` 把刚生成的 DSL+样例输入 POST 到现成的 `/api/execute` 内联路径。后端几乎零改动,作者信任增益最高 |
| 4 | **人话化失败解释** | S | 小白撞上 TS 编译栈直接放弃;友好、可行动的提示留住人、降工单 | Verifier 已把 `build:field` 编译错回喂修复循环;最终失败时给这些错加一层翻译(TS2554「缺参」→「这个捷径需要你没提到的一个输入」)。最低成本广覆盖 |

## 第二梯队

| # | 能力 | 量 | 它解决什么 | 怎么建 |
|---|------|----|-----------|--------|
| 5 | **per-user / 租户配额** | M | 防单用户(或泄露的只读 token)烧爆共享 LLM 预算 / 打爆外部 API——任何企业评审都问的基本公平用量;AI 杀死开关的自然补充 | 已有 GenerateRPM 限流器 + 鉴权身份;把 `/api/generate` 限流键改为已认证 OpenID,`/api/execute` 按 plugin/actor。复用现成中间件 |
| 6 | **审批发布工作流(草稿→管理员核准→上线目录)** | M | 业务用户写的插件,未经合规人核准不进同事的 Bitable——落实政企唯一强制审批点 | 给记录加 `status`(draft/approved);渲染器读的 `GET /api/apps` 只放 approved,作者仍见自己草稿;核准是 admin-only PATCH,落 #1 审计账本 |
| 7 | **我的插件库:修复静默重复 + 版本历史/回滚** | M | 作者放心迭代可回退;管理员得每插件变更轨;消除让库"像坏了"的重复堆积 | `SaveForUser` 本就按 ID insert-or-replace——重复是前端老发空 ID,补回 ID 即修;版本历史是加列存旧版,回滚=重存旧版 |
| 8 | **服务端凭证保险库** | L | 企业不会让每个分析师把 Jira/内部 API token 粘进单元格;中心化、管理员管、可审计的保险库是 demo 与「IT 批 10 万席」之间的分界 | 现在 `executeRequest.Auth` 每次用户传、用完不存;改为 admin 管的加密保险库,runner 在 execute 时服务端解析 auth 引用,密钥永不到客户端,每次解析落 #2 出网账本。解锁整条企业连接器线 |
| 9 | **OAuth2 客户端凭证 + token 缓存** | M | 多数企业/SaaS API(含飞书 tenant_access_token)是 OAuth2 客户端凭证;不缓存则 200 行重算打 200 次取 token 触发上游限流 | runner 已支持多步链(步1取 token、步2用);在 `execrt.Engine` 加按(凭证,token-url)的 TTL token 缓存,批内复用。最好与 #8 一起做 |
| 10 | **目录清单 + 可搜索模板库** | M | 10 万人组织一次性发现并复用那 ~14 个已验证已审的积木——压缩重复生成、重复 LLM 花销、重复安全审 | 积木已存在于 `plugin-center/*`;由这些目录生成清单,`GET /api/templates`(只读 token)带 title/kind/description/domains 供搜索。是"套用到我这张表/表内发现"等采用功能的前置 |
| 11 | **定时 / 阈值自动化** | L | 把连接器从"人改一行才触发"推向"每早刷新汇率列"/"状态翻 Overdue 即写升级表"——最常被要的自动化形态,在轻量端正面对 official「应用模式」 | 黄区已界定为 automation-lite(只用平台凭证写、动作白名单);加后端调度器经 execute-runner 跑存储的 DSL,写回走已验证的 batch_create 链。最重,依赖 #1/#2/#8 |

## 已显式降级 / 与已做项重叠
- **只读管理控制台(L)**:价值高但主要是 #1/#2/#5/#6 的"打包成一块玻璃";先把底层账本/配额/审批建好(各自可独立用 + Base UI 原生看),政企验收要"单屏"时再组装。
- **产物溯源清单 + 内容哈希(M)**:对上文 attestation 缺口有意义,但已被"可审计源码 + Created-by 溯源头"部分覆盖;高价值增量(bundle↔源码哈希对比)作为 #1 账本的一列搭车。

> 落地建议:**先做第一梯队(#1+#2+#3+#4)**——审计/出网账本坐实合规与"出网集中"卖点,dry-run + 人话化解释直接提作者体验,且全是薄复用、零新依赖。第二梯队按客户实际诉求拉起。
