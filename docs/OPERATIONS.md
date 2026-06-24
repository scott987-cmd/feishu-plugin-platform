> 🌐 **中文** · [English](OPERATIONS.en.md)

# 发版与部署手册(Operations)

把「改完代码 → 上线到真实飞书多维表格」这条链做成**可重复、可脚本化**的流程。
适用于**企业飞书**——唯一要求是后端服务器能被飞书 webview 经 HTTPS 访问。

> 一键编排:`scripts/release.sh "更新说明"`(先部署后端,再发版 widget)。
> 配置一次:`cp scripts/deploy.example.env scripts/deploy.env` 并填值(已 gitignore)。

---

## 0. 部署拓扑(两部分)

| 部分 | 跑在哪 | 怎么上线 |
|---|---|---|
| **后端服务** api / generator / **execute-runner** / caddy | 你的服务器(docker compose;或 k8s,见 `deploy/k8s/`) | `scripts/deploy-backend.sh`(全自动) |
| **容器插件 / 渲染器 widget** | 飞书 webview(飞书托管,不在你服务器) | `scripts/release-widget.sh` 构建+上传 → 控制台确认发布(1 次点击) |

- **execute-runner** = 自托管的「执行运行时」:容器插件里的「连接器 / 字段捷径」要调外部 API 时,经 `api → execute-runner` 执行(域名白名单 + SSRF 防护 + 只读)。执行**集中、自托管、可审计**,不依赖任何外部托管函数服务。
- 后端是无状态 12-factor 服务,配置走环境变量;compose ↔ k8s 平滑切换。

---

## 1. 一次性准备

```bash
cp scripts/deploy.example.env scripts/deploy.env
# 编辑 scripts/deploy.env:
#   SERVER_HOST / SSH_KEY / REMOTE_DIR     —— 后端服务器
#   PLATFORM_API_BASE                      —— 后端对外 HTTPS 地址(免买域名可用 <IP>.sslip.io)
#   FEISHU_APP_ID / WIDGET_BLOCK_TYPE_ID   —— 飞书应用 id + 数据表视图扩展的 blockTypeID
#   OPDEV_BIN                              —— opdev 可执行路径
```

服务器侧 `deploy/compose/.env`(首次按 `deploy/compose/.env.prod.example` 建,含密钥,留在服务器、勿提交):
`DOMAIN / PLATFORM_API_TOKEN / EXECUTE_RUNNER_TOKEN / DEEPSEEK_API_KEY / STORE / FEISHU_*`。

opdev 登录一次(浏览器扫码,token 存全局):`opdev login -e feishu`。

---

## 2. 发版(日常)

### 一键
```bash
scripts/release.sh "本次更新说明"
```

### 分步
```bash
# 仅改了后端(Go):
scripts/deploy-backend.sh                 # 全部服务
scripts/deploy-backend.sh api             # 只重建某服务

# 仅改了插件前端(渲染器):
scripts/release-widget.sh "更新说明"       # 构建+注入后端地址/Bearer + opdev upload
```

### 最后一步:控制台确认发布(免审租户即时生效)
`release-widget.sh` 上传后会打印链接。在控制台:
1. 打开 `https://open.feishu.cn/app/<APP_ID>/blocks/<BLOCK_TYPE_ID>`
2. 标题旁「编辑」铅笔 → **小组件版本**下拉选「X.Y.Z(待更新)」→ 保存
3. **版本管理与发布** → 创建版本 → 确认发布

> 这一步当前是控制台操作;**全自动化路径**见 §4。

---

## 3. 验证

```bash
# 后端(deploy-backend.sh 末尾已自动跑健康检查):
curl -s https://<DOMAIN>/healthz        # ok
curl -s -H "Authorization: Bearer <TOKEN>" https://<DOMAIN>/api/apps   # JSON 列表

# 自托管执行运行时(城市→天气,真实数据,经 api→execute-runner→外部 API):
#   见 deploy/compose/DEPLOY.md §3.1 的 /api/execute 示例。
```
插件侧:在多维表格里打开扩展视图插件,确认渲染/取数正常(新版本传播约 30–60s,期间稍等别狂刷)。

---

## 4. 走向全自动发布(进阶)

控制台那一步可进一步 API 化,做到「提交即上线」:
- **opdev headless**:用 `opdev login` 铸一个 `OPDEV_TOKEN`,CI 中 `opdev upload ... -v patch -d ...` 非交互上传(脚本已是非交互)。
- **应用版本发布 API**:`PATCH /open-apis/application/v6/applications/{app_id}/app_versions/{version_id}`(`status=1`,需管理员 `operator_id` + tenant token);测试租户/企业内部应用多为免审,提交即生效。
- **小组件版本绑定**:把上传返回的版本号写进应用版本草稿再发布。
> 现状:后端部署 + widget 构建上传**已全自动**;应用版本「创建+发布」在免审租户可经上述 API 收口为一条命令(企业内部自用场景最实用)。

---

## 5. 排错(实战踩坑)

| 现象 | 原因 / 解法 |
|---|---|
| `opdev upload` 报 `Invalid APPID` | `app.json`/`block.json` 还是占位符 —— 必须在 **build 之前**注入真值(webpack 把 appId 烤进 `dist/project.config.json`);`release-widget.sh` 已自动注入。 |
| 插件里数据来自旧版本 | widget 版本没切/没发布,或新版本未传播完(等 30–60s,硬刷)。 |
| 插件渲染的不是你刚 POST 的应用 | 渲染器取 `listApps()[0]`=**最早**的应用(后端按插入顺序返回);删掉更早的应用或用 `?app=<id>`。 |
| 服务器构建慢/被 OOM kill | 小内存机(如 <1G)给足 swap;AL2023 上 compose build 需单独装 buildx 插件。 |
| 本机 ssh 报 `Operation not permitted` 读不到 key | macOS TCC:Terminal 无 `~/Downloads` 权限。把 key 放 `~/.ssh/` 或给 Terminal 开「完全磁盘访问」。 |
| 飞书 webview `Failed to fetch` | 「安全设置 → 服务器域名白名单」要加后端域名;加后需发新版本生效。 |
| `execute` 调外部 API 失败 | 该域名不在插件 `domains` 白名单(运行时硬拒);或服务器出网被限。 |

---

## 6. 相关文档
- `deploy/compose/DEPLOY.md` —— compose 单机部署详解 + `/api/execute` 验证
- `deploy/k8s/` —— Kubernetes 清单(含 `15-execute-runner.yaml` + netpol)
- `EXECUTE_RUNTIME.md` —— 自托管执行运行时设计
- `PRODUCTION.md` —— 生产加固清单与已知边界
