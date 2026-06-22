# 飞书容器插件 (container plugin) — frontend

这是平台设计里 README 第 115 行所说的「真飞书容器插件」：用官方 **React + TypeScript + Webpack** 栈写的飞书**多维表格数据表视图插件 (view_extension)**。

它复用平台同一套**应用定义 DSL**：

1. 经平台 API (`GET /api/apps`、`GET /api/apps/{id}`) 拉取一份 `AppDefinition`(DSL，等同 `internal/dsl/dsl.go`)。
2. 用 `@lark-base-open/js-sdk` 从**宿主多维表格**读取真实记录。
3. 用与 `web/index.html`（mock 渲染器）相同的组件语义把 DSL 解释成 UI —— 区别在于**数值是宿主表格真实聚合值，而不是 `mockVal()` 造的占位数**。

这正是平台的核心思路：小白产出的是 DSL（数据），容器插件只发布**一次**，之后实时渲染任意 DSL，绕过逐个发布审核。

```
平台 API (/api/apps) ──▶ AppDefinition(DSL)
                                 │
飞书宿主多维表格 ──(js-sdk)──▶ 真实记录 ──▶ 容器插件渲染器 ──▶ 视图
```

## 目录结构

```
frontend/
├── package.json          react / react-dom / @lark-base-open/js-sdk + webpack 工具链
├── tsconfig.json         strict / jsx react-jsx / moduleResolution bundler / ES2020
├── webpack.config.js     entry src/index.tsx, ts-loader, html-webpack-plugin, devServer
└── src/
    ├── index.html        挂载根 #root
    ├── index.tsx         启动流程: 取 app id → 拉 DSL → 读宿主数据 → 渲染 (含 loading/error)
    ├── dsl.ts            TS 类型, 与 internal/dsl/dsl.go 严格镜像 (JSON 字段名一致)
    ├── api.ts            平台 API 客户端: listApps() / getApp(id), 基址默认 :8080
    ├── data.ts           js-sdk 读表 + 聚合: aggregate() / groupAggregate()
    ├── styles.ts         样式 (移植自 mock 渲染器配色与卡片/柱状图)
    └── render/
        ├── Renderer.tsx  遍历 ui.components[] 并分派
        ├── Stat.tsx      stat 卡片 (真实聚合值)
        ├── Chart.tsx     chart 柱状图 (真实分组聚合)
        └── Text.tsx      text 文本卡片
```

## 前置条件

- Node.js 18+ 与 npm。
- 平台后端已在运行（默认 `http://localhost:8080`，见仓库根 README 的「本地运行」）。
- 真机预览还需**飞书官方开发者工具 / opdev CLI** 并完成 `opdev login`（见下）。

## 安装与运行

```bash
cd frontend
npm install                 # 安装依赖 (本仓库未预跑, 无网络环境下请自行执行)

npm start                   # webpack dev server, 默认 :3000
npm run build               # 产出 dist/ (生产构建)
npm run typecheck           # tsc --noEmit, 仅类型检查不产物
```

> **配置平台 API 基址**：`src/api.ts` 的 `API_BASE` 默认 `http://localhost:8080`，
> 可通过构建期注入 `PLATFORM_API_BASE` 覆盖（需在 webpack 用 `DefinePlugin` 注入 `process.env`），
> 线上须指向真实网关，并在后端把 `ALLOWED_ORIGIN` 设为具体 feishu.cn 来源。

> **指定要渲染的应用**：URL 加 `?app=<id>` 渲染指定定义；不带则取列表第一个。

## 真机预览（在飞书里看效果）

数据表视图插件**只能在飞书多维表格宿主里**真正运行（`js-sdk` 才能取到宿主表格）。
纯浏览器打开会停在 error 态，因为 `bitable.base.getActiveTable()` 没有宿主可连。

官方流程（以飞书开放平台/opdev 文档为准）：

```bash
opdev login                 # 登录开发者账号
# 在飞书多维表格中以「开发者本地预览」加载本插件的 dev server (npm start 跑着)
```

## 发布流程（容器插件只发一次）

按官方步骤，本「容器插件」只需发布一次，之后实时渲染任意 DSL：

1. `npm run build` 产出 `dist/`。
2. `opdev upload` 上传构建产物。
3. 飞书开放平台**后台配置**（权限、入口、能力范围）。
4. **创建版本**。
5. **申请线上发布**。
6. **管理员审核**通过后上线。

## 与平台的关系 / 注意事项

- DSL 是硬通货：`src/dsl.ts` 必须与 `internal/dsl/dsl.go` 同步改动（字段名/枚举值一致）。
- 渲染语义对齐 `web/index.html`：`stat` / `chart` / `text` 三类已实现，`table` 留占位（mock 同样未实现真实多列表格）。
- **`@lark-base-open/js-sdk` 调用需对照官方文档核验**（见 `src/data.ts` 顶部注释）：
  本插件用的是文档常见 API —— `bitable.base.getActiveTable()` / `getTableById()`、
  `table.getFieldMetaList()`、`table.getRecords({ pageSize, pageToken })`、`record.fields[fieldId]`。
  不同 SDK 版本的**分页返回结构**（`records` / `pageToken` / `hasMore`）与**单元格值形状**
  （富文本/单选/人员等）可能略有差异，集成时请按
  <https://lark-base-team.github.io/js-sdk-docs/> 复核。
- `filter` 字段只透传展示，**不在前端执行**（与 DSL 注释一致：filter 是交给 Bitable 查询/导出引擎的公式，必须在那层解析/白名单化）。
