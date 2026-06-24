# publisher — 模拟操作上架器(RPA)

飞书开发者后台有几步**没有 OpenAPI**(建自建应用、登记数据表视图扩展取 `blockTypeID`、创建版本+申请线上发布)。本模块用 **Playwright + 持久化登录态** 模拟人点这些步骤,把"导出独立插件并上架"这条路径自动化。

> 定位:这是 **可选的「导出上架」路径**。小白一键生成的主线建议用**容器模式**(一个扩展只登记一次、生成物全是 DSL 数据),根本不碰后台、也就不需要本模块。RPA 脆弱、需维护,只在你确实要给每个插件造后台产物时才用。

## 能跑哪些步骤
| 命令 | 动作(后台无 API) |
|---|---|
| `login` | 一次性人工登录,保存 `state.json` 登录态 |
| `create-app` | 创建企业自建应用 → 打印 `APP_ID` |
| `register-ext` | 登记「数据表视图」扩展 → 打印 `BLOCK_TYPE_ID`(填进 `plugin/block/block.json`) |
| `publish` | 创建版本 + 申请线上发布 |

> 审核(通过/拒绝)**有 API**,不用 RPA:`PATCH /open-apis/application/v6/applications/{app_id}/app_versions/{version_id}`(status=1 通过,需管理员 operator_id + tenant_access_token);或后台开一次「自建应用免审」。

## 全自动发布 Quickstart(从零到「申请线上发布」)

> **成熟度先说清**:`login` / `register-ext` 已对真实后台实测校准;`create-app` / `publish` 的部分表单步骤仍标 `[CALIBRATE]`(后台 DOM/文案随版本变),**首跑务必 `HEADED=1` 观察并对照 `screenshots/` 校准**,详见下方「必读边界」。测试租户(免审、即时生效)配合得最顺。

### 0. 安装(每台机器一次)
```bash
cd publisher
npm install
npx playwright install chromium        # 下载浏览器内核(~150MB,需联网)
```

### 1. 一次性人工登录 → 保存登录态
```bash
npm run login                          # 弹有头浏览器,扫码/账号登录;进入应用列表后自动存 state.json
```
> `state.json` = 账号凭证等价物:已 gitignore,请当密钥严管;会随 2FA/设备绑定过期,需定期重登。

### 2. 建应用 → 拿 appId
```bash
HEADED=1 node src/cli.mjs create-app --name "插件平台" --desc "示例插件中心"
# → APP_ID=cli_xxxxxxxxxxxxxxxx
```

### 3. 登记「数据表视图」扩展 → 拿 blockTypeID(幂等)
```bash
node src/cli.mjs register-ext --app cli_xxxxxxxxxxxxxxxx --name 渲染器
# → BLOCK_TYPE_ID=blk_xxxxxxxxxxxxxxxx     (已登记则直接读出)
```
把拿到的 `blockTypeID` 写进要发布的扩展工程的 `block.json`(容器扩展:[`../plugin/block/block.json`](../plugin/block/block.json))。

### 4. 上传插件代码包 —— 用官方 CLI(不是 RPA)
真正把 TypeScript bundle 推上去、生成待发布版本,用飞书 SDK 自带的 `block-basekit-cli`:
```bash
cd ../plugin                           # 或某个 plugin-center/<id>
npm install
# upload 是交互式;非交互喂「版本号 / 更新说明」绕开 inquirer 管道乱码(示例值,按需改):
npx block-basekit-cli upload < <(printf '1.0.0\n'; sleep 3; printf '示例插件中心首发\n'; sleep 3)
```
> 这一步官方稳定可靠;publisher 只负责它前后那些「后台无 OpenAPI」的点击步骤。

### 5. 创建应用版本 + 申请线上发布
```bash
cd ../publisher
node src/cli.mjs publish --app cli_xxxxxxxxxxxxxxxx --version 1.0.0 --notes "首发"
# → SUBMITTED {"appId":"cli_...","version":"1.0.0","submitted":true}
```

### 6.(可选)审核放行 —— 有 API,无需 RPA
测试租户多为免审/即时生效;正式「通过」走 OpenAPI:
`PATCH /open-apis/application/v6/applications/{app_id}/app_versions/{version_id}`(`status=1`,需管理员 `operator_id` + `tenant_access_token`),或后台开一次「自建应用免审」。

### 环境变量
| 变量 | 默认 | 作用 |
|---|---|---|
| `OPDEV_ENV` | `feishu` | `feishu`(open.feishu.cn)/ `lark`(open.larksuite.com) |
| `HEADED` | `0` | `1`=有头浏览器(首跑/校准必开) |
| `SLOWMO` | `0` | 每步放慢的毫秒数,便于观察 |
| `PUBLISHER_STATE` | `publisher/state.json` | 自定义登录态路径 |
| `NAV_TIMEOUT` | `30000` | 单步导航/等待超时(ms) |
| `LOGIN_TIMEOUT` | `240000` | `login` 等待人工完成的上限(ms) |

## ⚠️ 必读边界
- **选择器需校准**:后台 DOM/文案因版本而异,代码里 `[CALIBRATE]` 处按当前后台尽力写,首跑用 `HEADED=1` 对照 `screenshots/` 逐步修正。这是 RPA 的固有维护成本。
- **登录态 = 凭证**:`state.json` 等同账号凭证,已 gitignore;请当密钥严管;会过期(受 2FA/设备绑定影响),需定期重新 `login`。
- **验证码/风控不绕过**:遇到安全验证会截图并停下报错——请人工完成该步(设计如此,不破解验证码)。
- **条款与账号风险**:用存储 session 自动操作后台,在你自己的租户/账号上是你自主的内部自动化,但仍可能触风控或涉平台条款,需你自行判断。
- **测试租户更顺**:测试版应用/自建测试企业下变更直接生效、几乎全免审,RPA 配合得最好。

## 与平台集成
把 `register-ext` 输出的 `BLOCK_TYPE_ID` 写进 `../plugin/block/block.json`,`opdev upload` 出版本后,再 `publish` 提交。整条可由平台的"发布"编排串起来(见根目录 ROADMAP/PRODUCTION)。
