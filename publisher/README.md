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

## 用法
```bash
cd publisher
npm install
npx playwright install chromium      # 下载浏览器内核(~150MB,需联网)

# 1) 一次性人工登录(有头浏览器,扫码/账号),保存登录态
npm run login

# 2) 之后无头重放(首跑强烈建议 HEADED=1 观察 + 看 screenshots/ 校准选择器)
HEADED=1 node src/cli.mjs create-app --name "插件平台"
node src/cli.mjs register-ext --app cli_xxx --name 渲染器      # → BLOCK_TYPE_ID=blk_...
node src/cli.mjs publish --app cli_xxx --version 0.1.1
```
环境变量:`OPDEV_ENV=feishu|lark`、`HEADED=1`(有头)、`SLOWMO=300`(放慢)、`PUBLISHER_STATE=...`(自定义登录态路径)。

## ⚠️ 必读边界
- **选择器需校准**:后台 DOM/文案因版本而异,代码里 `[CALIBRATE]` 处按当前后台尽力写,首跑用 `HEADED=1` 对照 `screenshots/` 逐步修正。这是 RPA 的固有维护成本。
- **登录态 = 凭证**:`state.json` 等同账号凭证,已 gitignore;请当密钥严管;会过期(受 2FA/设备绑定影响),需定期重新 `login`。
- **验证码/风控不绕过**:遇到安全验证会截图并停下报错——请人工完成该步(设计如此,不破解验证码)。
- **条款与账号风险**:用存储 session 自动操作后台,在你自己的租户/账号上是你自主的内部自动化,但仍可能触风控或涉平台条款,需你自行判断。
- **测试租户更顺**:测试版应用/自建测试企业下变更直接生效、几乎全免审,RPA 配合得最好。

## 与平台集成
把 `register-ext` 输出的 `BLOCK_TYPE_ID` 写进 `../plugin/block/block.json`,`opdev upload` 出版本后,再 `publish` 提交。整条可由平台的"发布"编排串起来(见根目录 ROADMAP/PRODUCTION)。
