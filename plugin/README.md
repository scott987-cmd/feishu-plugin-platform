# 飞书容器插件(opdev 工程)

真正可上架的飞书**多维表格 · 数据表视图插件**。基于官方 opdev 模板(`bitable-extensions / table-view`),用官方 SDK `@lark-opdev/block-bitable-api`,复用平台同一套 DSL:从平台 API 拉应用定义 → 读宿主表真实数据 → 按 DSL 渲染 stat/chart。

## 结构(opdev 工程布局)

```
plugin/
├── app.json            # 应用绑定(appId=cli_xxxxxxxxxxxxxxxx);BitableAppWebpackPlugin 读 ../app.json
└── block/              # 插件本体(构建/上传在此目录执行)
    ├── block.json      # manifestVersion / blockTypeID / projectName
    ├── package.json    # 官方栈:@lark-opdev/block-bitable-api + webpack-utils + react
    ├── config/webpack.config.js   # 含 DefinePlugin 注入 PLATFORM_API_BASE/TOKEN
    ├── tsconfig.json
    ├── public/index.html
    └── src/
        ├── index.tsx       # 挂载
        ├── App.tsx         # 渲染:拉定义 → 读宿主数据 → 渲染组件 + 「过滤未执行」提示
        ├── dsl.ts          # 与后端 internal/dsl 一致的 DSL 类型(硬通货)
        ├── api.ts          # 平台 API 客户端(带 Bearer;base/token 构建期注入)
        └── bitableData.ts  # @lark-opdev/block-bitable-api 读表 + agg/分组(复刻后端语义)
```

## 构建 / 上传

```bash
cd plugin/block
npm install

# 类型检查(esbuild 构建不查类型,单独跑 tsc)
npm run typecheck   # 或 ./node_modules/.bin/tsc --noEmit

# 生产构建:必须注入后端网关地址 + 同后端的 bearer token
PLATFORM_API_BASE=https://<你的后端网关> \
PLATFORM_API_TOKEN=<同后端 PLATFORM_API_TOKEN> \
  npm run build      # → plugin/block/dist(含 project.config.json/index.json,可被 opdev upload)

# 上架(需先登录 opdev,且 block.json 的 blockTypeID 填真值)
opdev upload ./dist
# 然后开发者后台:配置元数据 → 创建版本 → 申请线上发布 → 管理员审核
```

## ⚠️ 上传前必须做的两件事

1. **填 `blockTypeID`**:在[开发者后台](https://open.feishu.cn/app/cli_xxxxxxxxxxxxxxxx) 给应用 `cli_xxxxxxxxxxxxxxxx` 注册一个「数据表视图插件」扩展,拿到 `blockTypeID`,替换 `block/block.json` 里的 `REPLACE_WITH_YOUR_BLOCK_TYPE_ID`,重新 build。
2. **后端先部署**:`PLATFORM_API_BASE` 必须指向已上线的后端网关(见根目录 PRODUCTION.md),否则插件连不上。

## 说明

- 工程由官方模板 + 手工移植代码而成(opdev create 的交互流需要 console 注册扩展,此处直接搭好工程,blockTypeID 留占位)。
- `typecheck` 脚本若不存在,直接 `./node_modules/.bin/tsc --noEmit`。
- 仓库另有 `frontend/`:是早期手搓的 `@lark-base-open/js-sdk` 原型 + 本地 mock 渲染器参照,**已被本工程取代**,可删。
