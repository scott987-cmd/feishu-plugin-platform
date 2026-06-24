> 🌐 [中文](DEPLOY.md) · **English**

# Production deployment — single-node docker compose + Caddy automatic HTTPS

Goal: obtain a **fixed public URL with a valid TLS certificate** so that the Feishu webview can reach the backend, getting the entire chain
(novice-generated DSL → container plugin rendering inside a real Bitable) running end-to-end for the first time.

> Route choice: for single-tenant scale, start with compose; the benefits of k8s (per-app sandbox pod isolation) only materialize in Phase4,
> see `deploy/k8s/`. This directory is exactly the "compose first" step on the roadmap.

---

## 0. Prerequisites (what you need to prepare)

| Item | Notes |
|---|---|
| Public Linux server | 1C2G is enough to start (Ubuntu/Debian examples) |
| Domain name | An A record pointing to the server's public IP, e.g. `fpp.example.com → 1.2.3.4`. **The webview requires valid TLS, so you must use a domain; a bare IP won't work** |
| Firewall / security group | Open `80` and `443` (80 is used for ACME certificate issuance + HTTPS redirect) |
| Secrets | DeepSeek key, Feishu App ID/Secret, Bitable `app_token`/`table_id` (you already have these in your local `.env.local`) |

## 1. Install Docker (on the server)

```bash
curl -fsSL https://get.docker.com | sh      # includes the compose plugin
docker compose version                       # verify
```

## 2. Deploy

```bash
# put the repo on the server (git clone, or scp it up from your local machine)
cd feishu-plugin-platform/deploy/compose

cp .env.prod.example .env
# edit .env and fill in:
#   DOMAIN=your domain
#   PLATFORM_API_TOKEN / PLATFORM_READ_TOKEN / EXECUTE_RUNNER_TOKEN / GENERATOR_TOKEN
#                        # each $(openssl rand -hex 32), all four distinct
#                        # API=admin/write (server-side only) READ=read-only (goes into the client bundle)
#   DEEPSEEK_API_KEY / FEISHU_APP_ID / FEISHU_APP_SECRET /
#   FEISHU_BITABLE_APP_TOKEN / FEISHU_BITABLE_TABLE_ID
#   ALLOWED_ORIGIN=https://<webview origin>  # do not use * (it will refuse to start); if you really need to open it up on the first run, use ALLOWED_ORIGIN_INSECURE=true
nano .env

docker compose -f docker-compose.prod.yml --env-file .env up -d --build
docker compose -f docker-compose.prod.yml logs -f      # watch Caddy certificate issuance + api/generator startup
```

Startup guard: if `PLATFORM_API_TOKEN` contains `REPLACE_ME`, or `STORE=bitable` is set but FEISHU_* is missing → it exits immediately (it will not "fail configuration yet keep running bare").

## 3. Verify the backend

```bash
TOKEN=<PLATFORM_API_TOKEN from .env>; D=https://<DOMAIN>

curl -s $D/healthz                                   # ok (alive)
curl -s $D/readyz                                    # ready (ready; bitable will ping Feishu)
curl -s $D/api/apps                                  # 401 (blocked without token = auth in effect)
curl -s -H "Authorization: Bearer $TOKEN" $D/api/apps        # JSON list (includes the seed sales_dashboard)
# real generation (DeepSeek):
curl -s -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' \
  -d '{"mode":"nl","prompt":"按部门统计员工数量的柱状图"}' $D/api/generate
```

### 3.1 Verify the execute runtime (self-hosted FaaS stand-in, call-chain B)

`execute-runner` is an internal service (not exposed through Caddy); the api forwards to it via `/api/execute`. You must set
`EXECUTE_RUNNER_TOKEN=$(openssl rand -hex 32)` in `.env` (a bearer shared between api↔runner). Verify (via the public api):

```bash
# city → weather field shortcut DSL (key-free, multi-step Open-Meteo); inline dsl form
curl -s -H "Authorization: Bearer $TOKEN" -H 'Content-Type: application/json' -d '{
  "inputs": {"city": "Beijing"},
  "dsl": {
    "id":"city-weather","title":{"zh_CN":"城市天气"},
    "domains":["geocoding-api.open-meteo.com","api.open-meteo.com"],
    "formItems":[{"key":"city","label":{"zh_CN":"城市"},"component":"FieldSelect","supportType":["Text"],"required":true}],
    "steps":[
      {"id":"geo","method":"GET","url":"https://geocoding-api.open-meteo.com/v1/search?name={city}&count=1"},
      {"id":"weather","method":"GET","url":"https://api.open-meteo.com/v1/forecast?latitude={geo.results.0.latitude}&longitude={geo.results.0.longitude}&current=temperature_2m,wind_speed_10m"}
    ],
    "result":{"kind":"object","properties":[
      {"key":"temperature","type":"Number","label":{"zh_CN":"温度"},"primary":true,"expr":"res.current.temperature_2m"},
      {"key":"wind_speed","type":"Number","label":{"zh_CN":"风速"},"expr":"res.current.wind_speed_10m"}
    ]}
  }
}' $D/api/execute
# expected: {"ok":true,"data":{"temperature":<real temperature>,"wind_speed":<real wind speed>,...}}
```

Note: `execute-runner` uses ~64Mi at runtime; low-memory machines (e.g. 419MB) can run it via swap, but building the 3 Go images puts memory under pressure,
so a slow `compose up --build` is normal — don't interrupt it midway. The SSRF guard is ON by default (`EXECUTE_ALLOW_PRIVATE=false`),
and outbound traffic is only allowed for each plugin's `domains` allowlist.

## 4. Rebuild the plugin to point at the public URL + upload

At plugin build time the backend address / **read-only** token are statically injected (webpack DefinePlugin → `PLATFORM_API_BASE`/`PLATFORM_READ_TOKEN`). Be sure to inject the **read-only** token, never the admin `PLATFORM_API_TOKEN`.

```bash
cd ../../plugin/block
PLATFORM_API_BASE=https://<DOMAIN> PLATFORM_READ_TOKEN=<READ_TOKEN> npm run build
opdev upload ./dist            # interactively asks for version/description; answer precisely with expect (see publisher/README or the plugin notes)
# or just use scripts/release-widget.sh (auto-injects the read-only token + uploads)
```

In the developer console → that app's "Bitable data-table view" extension → "Widget version", select the version you just uploaded.

## 5. Open it inside a real Bitable (the first end-to-end run)

Open the data-table view plugin with a **test enterprise / test version** (test versions require almost no review). Expected: the plugin pulls its definition from
`https://<DOMAIN>/api/apps`, reads the host table's data via the Feishu JS-SDK, and renders.

The first run will very likely surface real issues; troubleshoot using the browser developer tools:
- **CORS**: if the console reports a cross-origin error, look at the rejected `Origin`, note it down → converge in step 6.
- **SDK data read**: if the actual shape of `@lark-opdev/block-bitable-api` differs from what the code assumes, it will surface here.
- **Auth**: a 401 means the injected read-only token doesn't match the backend `PLATFORM_READ_TOKEN` (rebuild).

## 6. Converge CORS

```bash
# put the real webview origin obtained in step 5 into .env
ALLOWED_ORIGIN=https://<actual webview Origin>
docker compose -f docker-compose.prod.yml --env-file .env up -d   # restart only; the certificate is not re-issued
```

## 7. Operations

```bash
# rebuild after updating code
git pull && docker compose -f docker-compose.prod.yml --env-file .env up -d --build
# certificates auto-renew — the caddy_data volume is persistent; don't delete it, or you may trigger Let's Encrypt rate limits
# logs / status
docker compose -f docker-compose.prod.yml logs -f api
docker compose -f docker-compose.prod.yml ps
```

## Caveats

- **The client embeds a read-only token (`PLATFORM_READ_TOKEN`)** → even if extracted it can only read (`GET /api/apps*` + `/api/execute`); write/delete/generate require the server-held admin token or a session. For true per-user identity inside the Bitable webview, upgrade to Feishu webview-OAuth (see `docs/PRODUCTION.en.md` §7).
- `STORE=bitable` persists definitions into a Bitable; for production boundaries such as uniqueness constraints / rate limiting under multiple replicas, see `docs/PRODUCTION.en.md` §8.
- To upgrade to k8s: `deploy/k8s/` is ready (Deployment/HPA/Ingress/NetworkPolicy/PDB); at that point, push the images to a registry and use cert-manager + Ingress to terminate TLS.
