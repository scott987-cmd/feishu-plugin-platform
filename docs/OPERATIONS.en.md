> 🌐 [中文](OPERATIONS.md) · **English**

# Release & Deployment Handbook (Operations)

Turn the "code changed → live on a real Feishu Bitable" chain into a **repeatable, scriptable** process.
Applies to **enterprise Feishu** — the only requirement is that the backend server can be reached by the Feishu webview over HTTPS.

> One-shot orchestration: `scripts/release.sh "release notes"` (deploys the backend first, then ships the widget).
> Configure once: `cp scripts/deploy.example.env scripts/deploy.env` and fill in the values (already gitignored).

---

## 0. Deployment Topology (two parts)

| Part | Where it runs | How to ship it |
|---|---|---|
| **Backend services** api / generator / **execute-runner** / caddy | Your server (docker compose; or k8s, see `deploy/k8s/`) | `scripts/deploy-backend.sh` (fully automated) |
| **Container plugin / renderer widget** | Feishu webview (hosted by Feishu, not on your server) | `scripts/release-widget.sh` build + upload → confirm publish in console (1 click) |

- **execute-runner** = the self-hosted "execution runtime": when a "connector / field shortcut" inside a container plugin needs to call an external API, it goes through `api → execute-runner` (domain allowlist + SSRF protection + read-only). Execution is **centralized, self-hosted, and auditable**, with no dependency on any externally hosted function service.
- The backend is a stateless 12-factor service, configured via environment variables; compose ↔ k8s switch smoothly.

---

## 1. One-time Setup

```bash
cp scripts/deploy.example.env scripts/deploy.env
# Edit scripts/deploy.env:
#   SERVER_HOST / SSH_KEY / REMOTE_DIR     —— backend server
#   PLATFORM_API_BASE                      —— backend public HTTPS address (no need to buy a domain; you can use <IP>.sslip.io)
#   FEISHU_APP_ID / WIDGET_BLOCK_TYPE_ID   —— Feishu app id + blockTypeID of the data-table view extension
#   OPDEV_BIN                              —— path to the opdev executable
```

On the server side, `deploy/compose/.env` (create it from `deploy/compose/.env.prod.example` on first run; it contains secrets, so keep it on the server and never commit it):
`DOMAIN / PLATFORM_API_TOKEN / PLATFORM_READ_TOKEN / GENERATOR_TOKEN / EXECUTE_RUNNER_TOKEN / DEEPSEEK_API_KEY / STORE / ALLOWED_ORIGIN / FEISHU_*`.

> **Required tokens (the prod compose enforces them via `${VAR:?}` — missing any fails startup):**
> - `PLATFORM_API_TOKEN` — admin/write bearer, server-only (POST/DELETE /api/apps + /api/generate)
> - `PLATFORM_READ_TOKEN` — read-only token, baked into the client bundle, **must differ from the admin token** (GET /api/apps* + POST /api/execute)
> - `GENERATOR_TOKEN` — shared between api ↔ generator
> - `EXECUTE_RUNNER_TOKEN` — shared between api ↔ execute-runner
>
> Generate a separate strong random value for each (**all distinct**): `openssl rand -hex 32`.
>
> **`ALLOWED_ORIGIN`**: with auth enabled it **cannot** be `*`, or the api refuses to start (`cmd/api/main.go` log.Fatal). Set a specific origin (e.g. `https://<IP>.sslip.io`); only for local dev may you set `ALLOWED_ORIGIN_INSECURE=true` to override.

Log into opdev once (scan the QR code in the browser; the token is stored globally): `opdev login -e feishu`.

---

## 2. Release (day-to-day)

### One shot
```bash
scripts/release.sh "release notes for this update"
```

### Step by step
```bash
# Backend only (Go) changed:
scripts/deploy-backend.sh                 # all services
scripts/deploy-backend.sh api             # rebuild just one service

# Plugin frontend only (renderer) changed:
scripts/release-widget.sh "release notes"  # build + inject backend address/Bearer + opdev upload
```

### Final step: confirm publish in the console (takes effect instantly for review-exempt tenants)
After uploading, `release-widget.sh` prints a link. In the console:
1. Open `https://open.feishu.cn/app/<APP_ID>/blocks/<BLOCK_TYPE_ID>`
2. Click the "Edit" pencil next to the title → in the **Widget version** dropdown choose "X.Y.Z (pending update)" → Save
3. **Version management & release** → Create version → Confirm release

> This step is currently a console operation; the **fully automated path** is in §4.

---

## 3. Verification

```bash
# Backend (deploy-backend.sh already runs a health check at the end):
curl -s https://<DOMAIN>/healthz        # ok
# GET /api/apps only needs the read-only token (PLATFORM_READ_TOKEN):
curl -s -H "Authorization: Bearer <PLATFORM_READ_TOKEN>" https://<DOMAIN>/api/apps   # JSON list

# Self-hosted execution runtime (city → weather, real data, via api→execute-runner→external API):
#   see the /api/execute example in deploy/compose/DEPLOY.en.md §3.1.
```
On the plugin side: open the extension-view plugin in the Bitable and confirm rendering / data fetching works (a new version takes about 30–60s to propagate; wait it out, don't keep hammering refresh).

---

## 3.1 Audit / Egress Ledger

With persisted auditing enabled, the platform appends both **admin operations** (action=…) and **every outbound call** (action=execute.egress, actor=plugin:<id>, with method/outcome/step; SSRF/redirect blocks = outcome=error) into the same `audit_log` table.

- The table is created by `cmd/bitable-bootstrap`; after running it prints `FEISHU_AUDIT_TABLE_ID=…` — put that into the server `.env`.
- `FEISHU_AUDIT_TABLE_ID` **empty = stdout-only** (nothing persisted); set it to persist (append failures never fail the request — best-effort).
- Verify (`/api/audit` is **admin**-token-only, newest-first, `?limit≤1000`):

```bash
curl -s -H "Authorization: Bearer <PLATFORM_API_TOKEN>" \
  "https://<DOMAIN>/api/audit?limit=50"   # most recent 50 audit/egress records
# When unconfigured, returns 503: audit ledger not configured (set FEISHU_AUDIT_TABLE_ID)
```

---

## 4. Toward Fully Automated Publishing (advanced)

The console step can be further API-ified, achieving "commit equals live":
- **opdev headless**: use `opdev login` to mint an `OPDEV_TOKEN`, and in CI run `opdev upload ... -v patch -d ...` for non-interactive upload (the script is already non-interactive).
- **App version release API**: `PATCH /open-apis/application/v6/applications/{app_id}/app_versions/{version_id}` (`status=1`, requires admin `operator_id` + tenant token); test tenants / internal enterprise apps are mostly review-exempt, so submitting takes effect immediately.
- **Widget version binding**: write the version number returned by the upload into the app version draft, then release it.
> Status quo: backend deployment + widget build/upload are **fully automated**; app version "create + release" can, on review-exempt tenants, be wrapped into a single command via the APIs above (most useful for internal enterprise self-use scenarios).

---

## 5. Troubleshooting (real-world pitfalls)

| Symptom | Cause / Fix |
|---|---|
| `opdev upload` reports `Invalid APPID` | `app.json`/`block.json` is still a placeholder — the real value must be injected **before build** (webpack bakes appId into `dist/project.config.json`); `release-widget.sh` already injects it automatically. |
| Plugin shows data from an old version | The widget version wasn't switched/published, or the new version hasn't finished propagating (wait 30–60s, hard refresh). |
| The plugin renders an app you didn't just POST | The renderer takes `listApps()[0]` = the **earliest** app (the backend returns by insertion order); delete the earlier app or use `?app=<id>`. |
| Server build is slow / gets OOM killed | On low-memory machines (e.g. <1G), give it plenty of swap; on AL2023, compose build needs the buildx plugin installed separately. |
| Local ssh reports `Operation not permitted` and can't read the key | macOS TCC: Terminal lacks `~/Downloads` permission. Put the key in `~/.ssh/`, or grant Terminal "Full Disk Access". |
| Feishu webview `Failed to fetch` | Add the backend domain under "Security settings → Server domain allowlist"; after adding, release a new version for it to take effect. |
| `execute` fails calling an external API | The domain isn't in the plugin's `domains` allowlist (the runtime hard-rejects it); or the server's outbound network is restricted. |
| compose reports `required variable … is missing a value` | An old `.env` is missing a required token — the prod compose enforces them via `${VAR:?}`; add `PLATFORM_READ_TOKEN / GENERATOR_TOKEN / EXECUTE_RUNNER_TOKEN` (each `openssl rand -hex 32`, all distinct). |
| api restart-loops / log `refusing to start: ALLOWED_ORIGIN=*` | Auth is enabled but CORS is open — set `ALLOWED_ORIGIN` to a specific origin, or set `ALLOWED_ORIGIN_INSECURE=true` for dev only. |

---

## 6. Related Docs
- `deploy/compose/DEPLOY.en.md` —— detailed single-machine compose deployment + `/api/execute` verification
- `deploy/k8s/` —— Kubernetes manifests (including `15-execute-runner.yaml` + netpol)
- `EXECUTE_RUNTIME.en.md` —— self-hosted execution runtime design
- `PRODUCTION.en.md` —— production hardening checklist and known boundaries
- `scripts/backup-defs.sh` —— back up the app/plugin definitions in the Feishu Base (DR/backup)
