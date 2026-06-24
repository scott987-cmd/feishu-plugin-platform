> 🌐 [中文](PRODUCTION.md) · **English**

# Production Deployment Guide

Take the platform to production grade. The backend (`api` + `generator`) is already production-quality and automatically verifiable; **only "publishing the container plugin on Feishu" is a manual gate** (`opdev` QR-code login + admin review). The code and artifacts are all prepared — the final step requires you to execute it.

## 1. Production Topology

```
Feishu container plugin (published, in user's browser) ──HTTPS(Bearer)──▶ Ingress(TLS)
                                                          │
                                              ┌───────────┴───────────┐
                                              ▼                        ▼
                                         api (Deployment×2)      generator (Deployment+HPA)
                                              │  ▲                     │
                              STORE=bitable ──┘  └── /api/generate ────┘
                                              │                        │
                                       Feishu Bitable (stores defs)   DeepSeek API
```

## 2. Pre-Launch Prerequisites

1. **DeepSeek key**: `DEEPSEEK_API_KEY` (direct connection within China; the client already bypasses the proxy).
2. **Feishu custom app**: enable `bitable:app` (table creation + record read/write); obtain `FEISHU_APP_ID/SECRET`.
3. **Bitable definition table**: `go run ./cmd/bitable-bootstrap` creates it one-time, yielding `FEISHU_BITABLE_APP_TOKEN/TABLE_ID` (fields id/name/version/definition).
4. **API token**: `openssl rand -hex 32` generates `PLATFORM_API_TOKEN` (shared between the container plugin and the backend).
5. **CORS Origin**: your Feishu domain (e.g. `https://<enterprise>.feishu.cn`).

## 3. Production Configuration (Environment Variables)

| Variable | Service | Production Value |
|---|---|---|
| `STORE` | api | `bitable` |
| `FEISHU_APP_ID/SECRET` | api | real credentials (Secret) |
| `FEISHU_BITABLE_APP_TOKEN/TABLE_ID` | api | bootstrap output |
| `PLATFORM_API_TOKEN` | api | 32-byte random string (Secret) |
| `ALLOWED_ORIGIN` | api | your Feishu origin (not `*`) |
| `GENERATE_RPM` | api | e.g. `60` (rate limit protects LLM budget) |
| `DEEPSEEK_API_KEY` | generator | real key (Secret) |
| `LLM_PROVIDER` / `MODEL` | generator | `deepseek` / `deepseek-chat` |

## 4. Security Checklist

- [x] **API auth**: once `PLATFORM_API_TOKEN` is set, `/api/*` enforces `Authorization: Bearer` (constant-time comparison); a startup warning fires if it is not set.
- [x] **CORS** narrowed to the specified origin (`ALLOWED_ORIGIN`; default `*` is dev-only and warns).
- [x] **Rate limiting**: `/api/generate` has a fixed-window RPM cap → 429.
- [x] **TLS**: Ingress `tls` + `ssl-redirect` (cert-manager).
- [x] **Container hardening**: read-only root FS, drop ALL caps, seccomp, nonroot uid; namespace PSA `restricted`.
- [x] **Network isolation**: NetworkPolicy default-deny, allowing only api→generator and ingress→api (requires Calico/Cilium).
- [x] **Secrets**: `Secret` (for production, sealed-secrets / external-secrets is recommended); `.env.local` is already ignored by `.gitignore`.
- [ ] **User-level auth (upgrade item)**: currently a shared token (sufficient for internal enterprise use). For per-Feishu-user auth, use a JSAPI ticket on the plugin side and validate user identity on the backend — see §7.

## 5. Deployment (k8s)

```bash
# 1. Edit the ConfigMap/Secret placeholder values in deploy/k8s/00-namespace-config.yaml
# 2. Build and push images
make images push REGISTRY=<your-registry> VERSION=0.1.0
#    and change the image in deploy/k8s/{10,20}-*.yaml to <your-registry>/...
# 3. Apply
make k8s-apply         # = kubectl apply -f deploy/k8s/
kubectl -n feishu-plugin-platform rollout status deploy/api deploy/generator
```

Once ready: `/healthz` (liveness), `/readyz` (readiness: api verifies the store is reachable).

## 6. Publishing the Container Plugin (Manual Gate)

The real plugin project is in `plugin/` (opdev layout, official SDK, already type-checked + builds successfully). See [plugin/README.md](plugin/README.md).

```bash
# 0. In the admin console, register a "Data Table View Plugin" extension for app cli_xxxxxxxxxxxxxxxx,
#    obtain the blockTypeID, and fill it into blockTypeID in plugin/block/block.json (replace REPLACE_WITH_YOUR_BLOCK_TYPE_ID)
cd plugin/block && npm install
# 1. Inject the backend gateway address + Bearer token at build time (DefinePlugin static substitution):
PLATFORM_API_BASE=https://<your-backend-gateway> \
PLATFORM_API_TOKEN=<same as backend PLATFORM_API_TOKEN> \
  npm run build                  # → plugin/block/dist (contains project.config.json/index.json)
# 2. Publish (opdev already logged in; token in ~/ config)
opdev upload ./dist
# Developer console: configure metadata → create version → request production release → admin review (container plugin only once)
```

> ⚠️ Two prerequisites: ① the `blockTypeID` in `block.json` must be filled with a real value (obtained by registering the extension in the admin console), otherwise you upload a placeholder; ② `npm run build` must be passed `PLATFORM_API_BASE`/`PLATFORM_API_TOKEN`, otherwise the bundle falls back to `localhost`, carries no Authorization, and cannot connect / gets 401 in production.
> `opdev` login is complete (QR-code scanned in this session), token stored in user config; the rendering of `@lark-opdev/block-bitable-api` has passed type checking, but **real-device data reads must be validated by live debugging inside the Feishu host**.

## 7. Operations

- **Scaling**: generator has an HPA (CPU 70%, 1–5 replicas); api starts at 2 replicas.
- **Graceful shutdown**: both services listen for SIGTERM and drain for 10s (friendly to k8s rolling updates).
- **Observability**: request logs (method/path/status/latency) go to stdout, pipe into your logging stack; LLM failures / balance exhaustion are logged separately (echoing "when scores drop, check the LLM balance first").
- **LLM budget**: `GENERATE_RPM` is the first gate; when the balance runs out, generation automatically falls back to keyword routing (without interrupting service).
- **Storage = Bitable (a deliberate design, not a compromise)**: the platform's own definition/ownership data is stored in Feishu Bitable, **introducing no external database**. This is a highlight for enterprise self-hosting / domestic-stack (Xinchuang) — one fewer component to deploy, harden, and back up; durability is managed by Feishu (Base can be exported/snapshotted for retention); admins can audit every definition directly in the table; the platform dogfoods the very capability it sells. The read path has a TTL cache + per-table querying (`GET /api/apps?tableId=`), suited for read-heavy (many viewers, few publishers) large-scale usage.
  - **Applicability boundaries (stated honestly)**: writes are low-frequency management actions (publishing a plugin), constrained by Feishu's per-app QPS; under multiple replicas, reads may have a staleness window not exceeding the cache TTL.
  - **Optional escape hatch**: for extreme write-heavy / strong-DR-compliance scenarios, a Postgres backend can be implemented behind the same `store.Store` interface (already isolated, drop-in) — this is an **option**, not a prerequisite.

## 8. Known Boundaries

Manual gate:
- Publishing the container plugin and admin review are manual steps (Feishu's security model; cannot be fully automated).

Auth / security:
- API auth is a **shared bearer token**, embedded in the frontend bundle (end users can extract it), and the single token carries full read/write/delete permissions. Only suitable for "internal enterprise use, plugin published only to this enterprise." Before multi-user / external use, upgrade to user-level auth (JSAPI ticket), or split a read-only token for the plugin and use a separate ops token for write/delete generation.
- The generator itself has **no auth**, relying on the `api→generator` NetworkPolicy for isolation; **CNIs such as flannel do not enforce NetworkPolicy**, so for production use Calico/Cilium or add an internal token to `/generate`.

Scaling / rate limiting:
- The `/api/generate` rate limit is a **per-replica** token bucket; under N replicas the global cap ≈ N×`GENERATE_RPM`, so prorate the budget by replica count; a cross-replica hard cap requires a shared rate limiter (e.g. Redis).
- The generator HPA scales on CPU, but the load is I/O-bound (waiting on the LLM), so CPU may be insensitive; for high concurrency, switch to a custom concurrency/request-rate metric.

Observability / degradation:
- The generator's readiness probe is equivalent to its liveness probe (template generation needs no key and is always available); when the AI key is missing, NL **silently degrades to keyword routing**, warning only in the startup log — monitoring should watch this warning and the LLM balance.
- Real online DeepSeek/Bitable calls have each been tested and passed (see README); the exact API shape of the frontend `@lark-base-open/js-sdk` must be verified by live debugging inside a real Feishu host (already isolated behind the interface).

Rendering / data:
- The frontend **does not yet execute `filter` on the client**: stats/charts with a filter display the full value and show a "⚠ filter not executed" notice on the card; filter parsing supporting subsets is a follow-up item.

Deployment:
- Images use a mutable tag + `IfNotPresent`: re-pushing the same tag name will not re-pull. For production, pin by digest (`@sha256:...`) or bump the version each time; the `REGISTRY` of `make images` must be manually aligned with the image prefix in `deploy/k8s/{10,20}-*.yaml` (no kustomize templating).
- `docker compose` is for local dev only (no healthcheck gating; defaults to STORE=memory, CORS=*); production runs on k8s.
- The frontend build requires network access (npm) and is not covered by `go test ./...`; CI should add `npm ci && npm run typecheck`.

## 9. AI Data Egress (Compliance)

The platform calls an LLM only during **natural-language generation**. The egress surface is small, and it can be turned off, redirected, or self-hosted:

- **What egresses** — only the **natural-language prompt** you type + the platform's built-in static few-shot examples + the tool schema. It does **NOT** send Bitable row data, user credentials, or API keys (those go through the self-hosted `execute-runner` and stay inside your cluster). What a prompt contains is up to the author — **do not paste secrets / personal sensitive data into prompts** (the prompt is the only egress channel).
- **Where it egresses** — by default DeepSeek's public `api.deepseek.com` (`LLM_PROVIDER=deepseek`). Use **`DEEPSEEK_BASE_URL`** to pin the endpoint to a **self-hosted / in-region OpenAI-compatible model**, so prompts never leave your boundary / region; `MODEL` picks the specific model.
  > Public DeepSeek has no zero-retention DPA by default; regulated / data-residency tenants should redirect to a self-hosted model or choose a provider with a DPA / zero-retention commitment.
- **How to fully disable** — **`AI_ENABLED=false`**: no NL ever calls the LLM (templates + the deterministic keyword router only); prompts **never egress**.
- **Boot-time transparency** — the generator's startup log explicitly prints whether it is "AI ON → egress to \<endpoint\>", "DISABLED", or "no key → keyword-router fallback" — compliance audits can read this one line.
- **Defaults** — `AI_ENABLED=true`; when `DEEPSEEK_API_KEY` is unset, NL automatically degrades to the keyword router (equivalent to no egress).

## 10. Disaster Recovery & Backup

The platform's system-of-record (app / plugin definitions + ownership) lives in Feishu Bitable (`STORE=bitable`), with no external database. Feishu manages durability, but you still need a backup + restore plan for **accidental deletion / human mis-edits / the app losing access**.

**What to back up** — two tables, both inside the same Base:
- the definitions table `FEISHU_BITABLE_TABLE_ID` (fields id/name/version/definition)
- the plugin-ownership table `FEISHU_PLUGINS_TABLE_ID` (if persistent per-user ownership is enabled)

**Three backup layers (authoritative → convenient)**
1. **Feishu Base copy/snapshot (authoritative, full, recommended)** — in Bitable, "⋯ → Make a copy" for a whole-Base snapshot, or copy the Base via the Drive API (`drive +copy`). One copy covers both tables + the schema. Do it on a fixed cadence (e.g. daily/weekly) with a dated name. **RPO** = the interval between copies; **RTO** = the time to switch to / re-import a copy.
2. **Scriptable record-level export (convenient, cron-able)** — `scripts/backup-defs.sh [outdir]` dumps the full app-definition catalog from `GET /api/apps` to a timestamped JSON (read-only; auto-retains the latest 30). Drop it in cron (`0 3 * * * …`). This is a lightweight copy of the "definition catalog," handy for diffing / quick re-import.
3. **(Optional) record-level API export** — to also script-export the ownership table, use Feishu `bitable records list` to pull that table in full → JSON.

**Restore**
- From a Base copy: point the platform at the copy as the new data Base (update `FEISHU_BITABLE_APP_TOKEN`/`TABLE_ID`), or re-import the copy's records into the original table.
- From the JSON: re-apply definitions one by one via `POST /api/apps` (admin token) — the renderer picks them up immediately, no release needed.

**Hardening (prevent mis-edits)** — restrict human edit access to the data Base (admins only / read-only sharing) so nobody hand-edits a definition into a broken state; the only trusted write path for definitions is the platform API.

> Boundary: the exact entry points and quotas for Base copies / record export depend on your Feishu console; before relying on it, run the full "copy → switch → restore" drill once in the target environment and confirm RPO/RTO meet your requirements.
