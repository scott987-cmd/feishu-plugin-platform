# 生产部署 — 单节点 docker compose + Caddy 自动 HTTPS

目标:拿到**固定、带合法 TLS 的公网 URL**,让飞书 webview 能访问后端,从而整条链路
(小白生成 DSL → 容器插件在真实多维表格里渲染)第一次端到端跑起来。

> 路线选择:单租户体量先用 compose;k8s 的收益(per-app 沙箱 pod 隔离)在 Phase4 才体现,
> 见 `deploy/k8s/`。本目录就是 roadmap 里"先 compose"那一步。

---

## 0. 前置(你要准备的)

| 项 | 说明 |
|---|---|
| 公网 Linux 服务器 | 1C2G 起步即可(Ubuntu/Debian 示例) |
| 域名 | 一个 A 记录指向服务器公网 IP,如 `fpp.example.com → 1.2.3.4`。**webview 要求合法 TLS,所以必须用域名,纯 IP 不行** |
| 防火墙/安全组 | 放行 `80`、`443`(80 用于 ACME 签证 + 跳转 HTTPS) |
| 密钥 | DeepSeek key、飞书 App ID/Secret、多维表格 `app_token`/`table_id`(你本地 `.env.local` 里已有) |

## 1. 装 Docker(服务器上)

```bash
curl -fsSL https://get.docker.com | sh      # 含 compose 插件
docker compose version                       # 验证
```

## 2. 部署

```bash
# 把仓库放到服务器(git clone 或本机 scp 上去)
cd feishu-plugin-platform/deploy/compose

cp .env.prod.example .env
# 编辑 .env,填:
#   DOMAIN=你的域名
#   PLATFORM_API_TOKEN=$(openssl rand -hex 32)   # 强随机 token
#   DEEPSEEK_API_KEY / FEISHU_APP_ID / FEISHU_APP_SECRET /
#   FEISHU_BITABLE_APP_TOKEN / FEISHU_BITABLE_TABLE_ID
#   ALLOWED_ORIGIN=*     # 首跑临时放开,第 6 步收敛
nano .env

docker compose -f docker-compose.prod.yml --env-file .env up -d --build
docker compose -f docker-compose.prod.yml logs -f      # 看 Caddy 签证 + api/generator 启动
```

启动期守卫:`PLATFORM_API_TOKEN` 含 `REPLACE_ME`、或 `STORE=bitable` 缺 FEISHU_* → 直接退出(不会"配置失败还裸跑")。

## 3. 验证后端

```bash
TOKEN=<.env 里的 PLATFORM_API_TOKEN>; D=https://<DOMAIN>

curl -s $D/healthz                                   # ok(存活)
curl -s $D/readyz                                    # ready(就绪,bitable 会 ping 飞书)
curl -s $D/api/apps                                  # 401(无 token 被拦 = 鉴权生效)
curl -s -H "Authorization: Bearer $TOKEN" $D/api/apps        # JSON 列表(含种子 sales_dashboard)
# 真实生成(DeepSeek):
curl -s -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"mode":"nl","prompt":"按部门统计员工数量的柱状图"}' $D/api/generate
```

## 4. 重打插件指向公网 + 上传

插件构建期把后端地址/Token 静态注入(webpack DefinePlugin → `PLATFORM_API_BASE`/`PLATFORM_API_TOKEN`)。

```bash
cd ../../plugin/block
PLATFORM_API_BASE=https://<DOMAIN> PLATFORM_API_TOKEN=<TOKEN> npm run build
opdev upload ./dist            # 交互要 version/description,用 expect 精确应答(见 publisher/README 或 plugin 备注)
```

到开发者后台 → 该应用「多维表格数据表视图」扩展 → 「小组件版本」选刚传的新版本。

## 5. 在真实多维表格里打开(第一次端到端)

用**测试企业 / 测试版本**打开该数据表视图插件(测试版几乎免审)。预期:插件从
`https://<DOMAIN>/api/apps` 拉到定义,经飞书 JS-SDK 读宿主表数据并渲染。

第一次大概率会暴露真实问题,按浏览器开发者工具排查:
- **CORS**:控制台若报跨域,看被拒的 `Origin`,记下来 → 第 6 步收敛。
- **SDK 数据读取**:`@lark-opdev/block-bitable-api` 的实际形状若与代码假设不符,在此暴露。
- **鉴权**:401 说明注入的 token 与后端 `PLATFORM_API_TOKEN` 不一致(重新 build)。

## 6. 收敛 CORS

```bash
# 把第 5 步拿到的 webview 真实来源填进 .env
ALLOWED_ORIGIN=https://<webview 实际 Origin>
docker compose -f docker-compose.prod.yml --env-file .env up -d   # 仅重启,证书不重签
```

## 7. 运维

```bash
# 更新代码后重建
git pull && docker compose -f docker-compose.prod.yml --env-file .env up -d --build
# 证书自动续期 —— caddy_data 卷持久化,别删,否则可能触发 Let's Encrypt 限流
# 日志 / 状态
docker compose -f docker-compose.prod.yml logs -f api
docker compose -f docker-compose.prod.yml ps
```

## 注意事项

- **前端内嵌 token 对终端用户可见** → 仅适合"企业内部自用、插件只发本企业";面向多用户/外部要升级到用户级鉴权(见 `PRODUCTION.md` §7)。
- `STORE=bitable` 把定义持久化到多维表格;多副本下的唯一约束/限流等生产边界见 `PRODUCTION.md` §8。
- 想升级到 k8s:`deploy/k8s/` 已备好(Deployment/HPA/Ingress/NetworkPolicy/PDB);届时把镜像推到 registry、用 cert-manager + Ingress 出 TLS 即可。
