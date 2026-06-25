> 🌐 **中文** · [English](PRODUCTION.en.md)

# 生产部署手册

把平台跑到生产级。后端(`api` + `generator`)我已做到生产级质量并可自动验证;**只有"容器插件在飞书上架"是人工门禁**(`opdev` 扫码 + 管理员审核),代码/产物都已备好,最后一步需你执行。

## 1. 生产拓扑

**生产实况(已端到端验证,2026-06-25)**:单节点 `docker compose` + **Caddy 自动 TLS** 跑在一台 AWS EC2 主机上(Caddy 经 `<IP>.sslip.io` 魔法 DNS 主机名向 Let's Encrypt 签发证书,免买域名);`STORE=bitable`。线上总览页:<https://scott987-cmd.github.io/feishu-plugin-platform/>。k8s(`deploy/k8s/`)是**可选的未来 / 扩容路径**(Phase4),不是主路径。

```
飞书容器插件(已上架, 用户浏览器内) ──HTTPS(Bearer)──▶ Caddy(:443 自动 TLS, EC2)
                                                          │  反代 api:8080
                                              ┌───────────┴────────────────────┐
                                              ▼                ▼               ▼
                                         api (:8080)   generator (:8090)  execute-runner (:8095)
                                              │  ▲ /api/generate    ▲ /api/execute
                              STORE=bitable ──┘  └── DeepSeek API ───┘  (仅容器网络可见)
                                              │
                                       飞书多维表格(存定义 + 审计 / 出网账本)
```

> generator(持 LLM key)与 execute-runner(出网收口)**仅容器网络可见、不对外暴露**,只由 api 经内网调用。prod compose 文件:`deploy/compose/docker-compose.prod.yml`;仓库根的 `docker-compose.yml` 仅本地 dev(`STORE=memory`、`CORS=*`)。

## 2. 上线前置

1. **DeepSeek key**:`DEEPSEEK_API_KEY`(国内直连,客户端已绕代理)。
2. **飞书自建应用**:开通 `bitable:app`(建表 + 记录读写);拿 `FEISHU_APP_ID/SECRET`。
3. **Bitable 定义表**:`go run ./cmd/bitable-bootstrap` 一次性创建,得 `FEISHU_BITABLE_APP_TOKEN/TABLE_ID`(字段 id/name/version/definition)。
4. **API token**:`openssl rand -hex 32` 生成 `PLATFORM_API_TOKEN`(容器插件与后端共享)。
5. **CORS Origin**:你的飞书域名(如 `https://<企业>.feishu.cn`)。

## 3. 生产配置(环境变量)

| 变量 | 服务 | 生产值 |
|---|---|---|
| `STORE` | api | `bitable` |
| `SERVE_WEB` | api | `false`(生产关掉 mock 渲染器) |
| `FEISHU_APP_ID/SECRET` | api | 真实凭证(Secret) |
| `FEISHU_BITABLE_APP_TOKEN/TABLE_ID` | api | bootstrap 输出 |
| `FEISHU_AUDIT_TABLE_ID` | api / execute-runner | bootstrap 输出(留空=仅 stdout 审计) |
| `PLATFORM_API_TOKEN` | api | 32 字节随机串、admin/写、仅服务端(Secret)。**必填**(compose `${VAR:?}`) |
| `PLATFORM_READ_TOKEN` | api | 32 字节随机串、只读、内嵌进 widget bundle;**须与 admin 不同**。**必填** |
| `GENERATOR_TOKEN` | api + generator | api↔generator 共享强随机 token。**必填** |
| `EXECUTE_RUNNER_URL` | api | `http://execute-runner:8095`(内网 runner) |
| `EXECUTE_RUNNER_TOKEN` | api + execute-runner | api↔runner 共享强随机 token。**必填** |
| `ALLOWED_ORIGIN` | api | 你的飞书 origin(**非 `*`**,见 §4) |
| `GENERATE_RPM` | api | 如 `60`(限流护 LLM 预算) |
| `AI_ENABLED` | generator | `true`(`false`=彻底关 NL→LLM,提示词永不出域) |
| `DEEPSEEK_API_KEY` | generator | 真实 key(Secret) |
| `DEEPSEEK_BASE_URL` | generator | 留空=DeepSeek 公网;钉到自托管/境内 OpenAI 兼容端点则提示词不出域 |
| `LLM_PROVIDER` / `MODEL` | generator | `deepseek` / `deepseek-chat` |

> `GENERATOR_TOKEN` / `EXECUTE_RUNNER_TOKEN` / `PLATFORM_READ_TOKEN` 在 prod compose 里是 `${VAR:?}` **必填**——旧的 `.env` 缺这几项会让 `docker compose` 插值直接报 `required variable … is missing a value` 而起不来(见 §4「升级既有部署」)。

## 4. 安全清单

- [x] **API 鉴权(能力分离)**:客户端 bundle 只内嵌**只读 token `PLATFORM_READ_TOKEN`**(仅 `GET /api/apps*` + `POST /api/execute`);**写 / 删 / 生成**(`POST`、`DELETE /api/apps`、`/api/generate`)需服务端持有的 **admin token `PLATFORM_API_TOKEN`** 或登录会话;`/api/me`、`/api/my/*` 仅会话;`GET /api/audit` 仅 admin。常数时间比较;`path.Clean` 守 `//api` 绕过。即便客户端 token 泄露也只能读(IDOR 已消除)。
- [x] **CORS** 收敛到指定 origin(`ALLOWED_ORIGIN`)。**开了鉴权时 `ALLOWED_ORIGIN=*` 会被拒绝启动**(`cmd/api/main.go` `log.Fatal`,凭证 + 通配 CORS 危险)——必须设具体 origin,或设 `ALLOWED_ORIGIN_INSECURE=true` 显式放开(仅 dev/首跑)。
- [x] **限流**:`/api/generate` 固定窗口 RPM 上限 → 429。execute-runner 另有 `EXECUTE_MAX_CONCURRENCY`(默认 64)并发闸,过载吐 429 + Retry-After。
- [x] **TLS(生产实况)**:Caddy 自动 TLS——经 `<IP>.sslip.io` 主机名向 Let's Encrypt 签发 / 续期,`80→443` 跳转。*(k8s 路径走 Ingress `tls` + `ssl-redirect` + cert-manager。)*
- [x] **容器加固**(k8s 路径):只读根 FS、drop ALL caps、seccomp、nonroot uid;命名空间 PSA `restricted`。
- [x] **网络隔离**:compose 生产里 generator / execute-runner 仅 `expose`(容器网络可见、不发布端口),只由 api 内网调用。*(k8s 路径用 NetworkPolicy 默认拒绝,仅放行 api→generator/runner、ingress→api,需 Calico/Cilium。)*
- [x] **密钥**:`Secret`(k8s 生产建议外接 sealed-secrets / external-secrets);compose 路径走主机 `deploy/compose/.env`;`.env*` 已被 `.gitignore` 忽略。
- [ ] **用户级鉴权(升级项)**:容器 widget 在 Bitable webview 内无我方会话、拿的是降权只读 token,尚非真·per-user 身份。真 per-user 需飞书 webview-OAuth——见 §8。

**升级既有部署(真实坑)** —— 把一台旧 EC2 上跑的 compose 升到当前版本时,逐项核对:

- (a) **必填 token**:`GENERATOR_TOKEN`、`EXECUTE_RUNNER_TOKEN`、`PLATFORM_READ_TOKEN` 现在是 compose `${VAR:?}` 必填;旧 `.env` 缺它们会让 `docker compose` 插值直接失败(`required variable … is missing a value`)。先在 `deploy/compose/.env` 补齐(`openssl rand -hex 32`),且 `PLATFORM_READ_TOKEN` 必须与 `PLATFORM_API_TOKEN` 不同。
- (b) **CORS 致命门**:开了鉴权又把 `ALLOWED_ORIGIN` 留成 `*`,api **拒绝启动**(不是告警)。设具体 origin,或临时 `ALLOWED_ORIGIN_INSECURE=true`。
- (c) **发版脚本的 admin token**:`scripts/deploy.env` 的 `PLATFORM_API_TOKEN` 刻意留空——`publish-plugin.sh` / `backup-defs.sh` 会经 SSH 从服务器 `deploy/compose/.env` 读 admin token,别在本地填它。
- (d) **免买域名**:`sslip.io` 魔法 DNS(`<IP>.sslip.io`)让 Caddy 不买域名也能签真 TLS——飞书 webview 要求合法 TLS 域名,这是最省事的路子。

## 5. 部署(k8s,可选扩容路径)

> 生产实况是 §1 的单节点 compose + Caddy(`deploy/compose/docker-compose.prod.yml`,用法见 `deploy/compose/DEPLOY.md`)。下面的 k8s 是**可选的扩容 / 未来路径**(Phase4)。

```bash
# 1. 改 deploy/k8s/00-namespace-config.yaml 的 ConfigMap/Secret 占位值
# 2. 构建并推镜像
make images push REGISTRY=<your-registry> VERSION=0.1.0
#    并把 deploy/k8s/{10,20}-*.yaml 的 image 改成 <your-registry>/...
# 3. 应用
make k8s-apply         # = kubectl apply -f deploy/k8s/
kubectl -n feishu-plugin-platform rollout status deploy/api deploy/generator
```

就绪后:`/healthz`(存活)、`/readyz`(就绪:api 校验 store 可达)。

## 6. 容器插件上架(人工门禁)

真插件工程在 `plugin/`(opdev 布局,官方 SDK,已类型检查 + 构建通过)。详见 [plugin/README.md](plugin/README.md)。

```bash
# 0. 后台为应用 cli_xxxxxxxxxxxxxxxx 注册「数据表视图插件」扩展,拿 blockTypeID,
#    填进 plugin/block/block.json 的 blockTypeID(替换 REPLACE_WITH_YOUR_BLOCK_TYPE_ID)
cd plugin/block && npm install
# 1. 构建期注入后端网关地址 + Bearer token(DefinePlugin 静态替换):
PLATFORM_API_BASE=https://<你的后端网关> \
PLATFORM_API_TOKEN=<同后端 PLATFORM_API_TOKEN> \
  npm run build                  # → plugin/block/dist(含 project.config.json/index.json)
# 2. 上架(opdev 已登录;token 见 ~/ 配置)
opdev upload ./dist
# 开发者后台:配置元数据 → 创建版本 → 申请线上发布 → 管理员审核(容器插件仅此一次)
```

> ⚠️ 两个前置:① `block.json` 的 `blockTypeID` 必须填真值(后台注册扩展获得),否则上传的是占位;② `npm run build` 必须传 `PLATFORM_API_BASE`/`PLATFORM_API_TOKEN`,否则 bundle 回退 `localhost`、不带 Authorization,线上连不上/被 401。
> `opdev` 登录已完成(本会话扫码),token 存用户配置;`@lark-opdev/block-bitable-api` 的渲染已类型检查通过,但**真机数据读取需在飞书宿主内联调验证**。

## 7. 运维

- **伸缩**(k8s 路径):generator 有 HPA(CPU 70%,1–5 副本);api 2 副本起。单节点 compose 为单副本。
- **优雅停机**:两服务监听 SIGTERM,排空 10s(k8s 滚动更新友好)。
- **可观测**:请求日志(method/path/status/耗时)输出到 stdout,接你的日志栈;LLM 失败/余额耗尽会单独 log(呼应"分数降先查 LLM 余额")。
- **LLM 预算**:`GENERATE_RPM` 是第一道闸;余额耗尽时生成自动回退关键词路由(不中断服务)。
- **存储=多维表格(刻意设计,非妥协)**:平台自身的定义/归属数据存进飞书多维表格,**不引入任何外部数据库**。对企业自托管/信创是亮点——少一个要部署、加固、备份的组件;durability 由飞书托管(Base 可导出/快照做留存);管理员能在表里直接审计每条定义;平台 dogfood 它所售卖的能力。读路径有 TTL 缓存 + 按表查询(`GET /api/apps?tableId=`),适配读密集(看的人多、发布的人少)的大规模使用。
  - **适用边界(诚实陈述)**:写是低频管理动作(发布插件),受飞书单 app QPS 约束;多副本下读可能有不超过缓存 TTL 的陈旧窗口。
  - **可选逃生舱**:极端写密集 / 强 DR 合规场景,可在同一 `store.Store` 接口后实现 Postgres 后端(已隔离、可 drop-in)——这是**可选项**,不是前置要求。

## 8. 已知边界

人工门禁:
- 容器插件上架与管理员审核是人工步骤(飞书安全模型,无法全自动)。

鉴权 / 安全:
- API 鉴权是**能力分离的双 token + 会话**:客户端 bundle 只内嵌**只读 token `PLATFORM_READ_TOKEN`**(仅 `GET /api/apps*` 与 `/api/execute`);**写 / 删 / 生成**(`POST`、`DELETE /api/apps`、`/api/generate`)需服务端持有的 **admin token `PLATFORM_API_TOKEN`** 或登录会话;`/api/my/*` 仅会话。即便客户端 token 泄露也只能读,删库 / 烧预算的 IDOR 已消除;`put`/`delete` 写审计日志。**遗留边界**:容器 widget 在 Bitable webview 内无我方会话、拿的是降权只读 token,尚非真·per-user 身份(真 per-user 需飞书 webview-OAuth,后续)。
- generator 自身**无鉴权**,依赖 `api→generator` 的 NetworkPolicy 隔离;**flannel 等 CNI 不强制 NetworkPolicy**,生产请用 Calico/Cilium 或为 `/generate` 加内部 token。

伸缩 / 限流:
- `/api/generate` 限流是**每副本**令牌桶,N 副本下全局上限 ≈ N×`GENERATE_RPM`,按副本数折算预算;跨副本硬上限需共享限流器(如 Redis)。
- generator HPA 按 CPU,但负载是 I/O 型(等 LLM),CPU 可能不灵敏;高并发建议换并发/请求率自定义指标。

可观测 / 退化:
- generator 就绪探针等价存活探针(模板生成不需 key 恒可用);AI key 缺失时 NL **静默退化为关键词路由**,仅启动日志告警——监控请关注该告警与 LLM 余额。
- 线上 DeepSeek/Bitable 真调用已分别实测通过(见 README);容器渲染器(plugin/block)读宿主数据用的是 opdev 的 `@lark-opdev/block-bitable-api`,其具体 API 形状需在真飞书宿主内联调核实(已隔离在接口后)。

渲染 / 数据:
- 前端**暂不在客户端执行 `filter`**:带 filter 的 stat/chart 显示全量值,并在卡片上打"⚠ 过滤未执行"提示;支持子集的 filter 解析为后续项。

部署:
- 镜像用可变 tag + `IfNotPresent`:重推同名 tag 不会重新拉取。生产请按 digest 固定(`@sha256:...`)或每次 bump 版本;`make images` 的 `REGISTRY` 需与 `deploy/k8s/{10,20}-*.yaml` 的 image 前缀手动对齐(无 kustomize 模板)。
- **生产 = 单节点 `docker compose` + Caddy 自动 TLS 跑在 EC2(已端到端验证)**;prod compose 是 `deploy/compose/docker-compose.prod.yml`。**仓库根的 `docker-compose.yml` 仅本地 dev**(默认 `STORE=memory`、`CORS=*`、无 healthcheck 门禁)。k8s(`deploy/k8s/`)是可选的扩容/未来路径。
- 前端构建需联网(npm),不在 `go test ./...` 覆盖内;建议 CI 加 `npm ci && npm run typecheck`。

## 9. AI 数据出域(合规)

平台只在**自然语言生成**时调用 LLM,出域面很小,且可关、可改向、可自托管:

- **出域什么** —— 仅你输入的**自然语言提示词** + 平台内置的静态 few-shot 示例 + 工具 schema。**不发送**多维表格行数据、用户凭证、API key(这些走自托管 `execute-runner`,留在你的集群内)。提示词内容由作者输入决定——**请勿在提示词里粘贴密钥 / 个人敏感信息**(它是唯一的出域通道)。
- **出域到哪** —— 默认 DeepSeek 公网 `api.deepseek.com`(`LLM_PROVIDER=deepseek`)。用 **`DEEPSEEK_BASE_URL`** 把端点钉到**自托管 / 境内的 OpenAI 兼容模型**,提示词即不出你的边界 / 不出境;`MODEL` 选具体模型。
  > DeepSeek 公网默认无零留存 DPA;受监管 / 不出境租户请改向自托管模型,或选有 DPA / 零留存承诺的提供方。
- **怎么彻底关** —— **`AI_ENABLED=false`**:任何 NL 都不再调用 LLM(只用模板 + 确定性关键词路由),提示词**永不出域**。
- **启动透明** —— generator 启动日志会明确打印当前是「AI ON → 出域到 \<endpoint\>」「DISABLED」还是「无 key 退化关键词路由」,合规审计直接看这一行。
- **默认** —— `AI_ENABLED=true`;未配 `DEEPSEEK_API_KEY` 时 NL 自动退化为关键词路由(等效不出域)。

## 10. 灾备与备份

平台的"系统级数据"(应用 / 插件定义 + 归属)存在飞书多维表格里(`STORE=bitable`),无外部数据库。durability 由飞书托管,但**误删 / 人为误改 / 应用失权**仍需有备份与恢复预案。

**备份什么** —— 两张表,都在同一个 Base 内:
- 定义表 `FEISHU_BITABLE_TABLE_ID`(字段 id/name/version/definition)
- 插件归属表 `FEISHU_PLUGINS_TABLE_ID`(若启用了持久化 per-user 归属)

**三层备份(从权威到便捷)**
1. **飞书 Base 副本/快照(权威全量,推荐)** —— 在多维表格里「⋯ → 创建副本」做整库快照,或用云空间 API 复制该 Base(`drive +copy`)。一份副本即覆盖上面两张表 + 结构。建议固定节奏(如每日/每周)+ 命名带日期。**RPO** = 两次副本的间隔;**RTO** = 切到副本/重导的时间。
2. **脚本化记录级导出(便捷、可 cron)** —— `scripts/backup-defs.sh [输出目录]` 把 `GET /api/apps` 全量应用定义 dump 成带时间戳 JSON(只读、自动保留最近 30 份),适合放进 cron(`0 3 * * * …`)。这是"定义目录"的轻量副本,便于 diff / 快速回灌。
3. **(可选)记录级 API 导出** —— 如需把插件归属表也脚本化导出,用飞书 `bitable records list` 全量拉取该表 → JSON。

**恢复**
- 从 Base 副本恢复:把副本设为新的数据 Base(更新 `FEISHU_BITABLE_APP_TOKEN`/`TABLE_ID` 指向它),或把副本里的记录回灌到原表。
- 从 JSON 回灌定义:逐条 `POST /api/apps`(admin token)写回(渲染器即时生效,零发版)。

**加固(防误改)** —— 把数据 Base 的人工编辑权限收紧(仅管理员 / 只读共享),避免有人在表里手改定义导致渲染异常;定义的唯一可信写入口是平台 API。

> 边界:Base 副本/记录导出的具体入口与配额以你的飞书控制台为准;首次请在目标环境验证一次"副本 → 切换 → 恢复"全流程,确认 RPO/RTO 满足要求。

## 11. 审计账本(持久化)

平台对目录的每次**写 / 删**都产一条审计事件(谁 `actor` / 何时 `time` / 动作 `action` / 目标 `target` / 版本 `version` / 来源 IP)。事件**始终**进 stdout 日志;**另配一张 Bitable 表即持久化**,重启不丢、可被合规人查询。

- **启用** —— 设 `FEISHU_AUDIT_TABLE_ID`(指向 `audit_log` 表;`bitable-bootstrap` 会一并创建并打印该表 id)。留空 = 仅 stdout。
- **表结构** —— `time / actor / action / target / version / ip / detail`(`version` 为数字,其余文本)。
- **追加式** —— 平台只 `create` 不 update/delete,应用层即防篡改(配合「§10 防误改」把数据 Base 人工写权限收紧)。
- **查询** —— `GET /api/audit?limit=N`(newest-first,默认 200、上限 1000),**仅 admin token**(只读 token、登录会话均 401)——组织级审计轨只给运维/合规持有的 admin token。管理员也可直接在 Base UI 原生筛选。
- **写入失败不阻断请求** —— 审计 append 失败只告警(事件仍在 stdout),不让一次发布因审计表故障而失败。
- **出网账本(`execute.egress`)** —— execute-runner 对**每次外呼**(多步逐跳)产一条 `action=execute.egress` 事件(`actor=plugin:<id>`、`target=<外部域名>`、detail 含 method/outcome/step):谁把数据发给了哪个外部域名、放行还是拦截(SSRF / 重定向拦截记为 `error`),落**同一张审计表**。归属用平台 pluginId(api 经 `/api/execute` 透传;缺省回退捷径自身 id)。**热路径友好**:execute 是高频路径,所以记录走 stdout(始终)+ **异步缓冲单 worker** 写 Bitable(缓冲满则丢弃并记丢弃数,**绝不拖慢 execute、不打爆飞书 QPS**)。**进程关闭时优雅 flush**:收到 SIGTERM 后,HTTP 优雅排空 → worker 把缓冲里剩余事件全部写完再退出(10s 上限),所以滚动重启/重部署**不丢已缓冲的出网记录**;stdout 始终是兜底。runner 需配 `FEISHU_APP_ID/SECRET/BITABLE_APP_TOKEN` + `FEISHU_AUDIT_TABLE_ID` 才持久化,否则仅 stdout。

> 这条 + 出网账本一起闭合了 ROADMAP「必补缺口:发布审计流水账」与「企业增强路线 #1/#2」。剩余增量:bundle↔受审源码哈希(attestation)。
