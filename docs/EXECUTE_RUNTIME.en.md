> 🌐 [中文](EXECUTE_RUNTIME.md) · **English**

# Self-Hosted Execute Runtime Design · connector / field-shortcut execution for the container-renderer track

> Status: design draft (2026-06-23). Corresponds to **Phase 4** of `design.en.md` and the 🔴 red zone of `ROADMAP.en.md` (automated execute / field-formula connector extensions).
> Design motivation: centralize execute-class execution into a single self-hosted, auditable runtime, rather than letting each plugin call external functions on its own — this is also why it runs on k8s.

---

## 0. In One Sentence

The **read-only data views** of the green/yellow zones rely on the container rendering engine running in the Feishu webview client, requiring no backend runtime at all; but the **execute-class plugins of the red zone (field shortcuts / connectors, which need to call external APIs to compute a column) require a runtime capable of running `execute`**. For the container-renderer track, the realistic home for these execute-class capabilities is **an execution runtime that we host ourselves on k8s** — so that egress, credentials, audit, and rate-limiting all converge into one auditable chokepoint. This document designs it.

---

## 1. Background: Why a Self-Hosted Execute Runtime

There are two distinct execution hosts, depending on how a plugin ships:

| Shipping Path | execute Runtime Host | Conclusion |
|---|---|---|
| Plugin uploaded to Feishu | Runs on Feishu's **basekit FaaS**; field-shortcut publishing goes through the official form + manual review; the public-cloud third-party **field** execute FaaS is gated (official/Volcano only), and official extensions only open up the three categories of record view / data table view / automation; `block-basekit-cli` has no field upload command | Use Feishu's FaaS |
| Container-renderer / self-hosted track | Runs on the **self-hosted execute-runner** — one auditable runtime where all execute/connector traffic converges | Self-host the runner |

> 📌 **Outdated assumption, now corrected**: the header comment of `internal/shortcut/dsl.go` once read *"The runtime is ALWAYS Feishu's basekit FaaS — there is no self-hostable runtime"*.
> That assumption **does not hold for the container-renderer / self-hosted track**; the comment has been updated, and this design makes the "self-hostable runtime" real.
> Note that we preserve the dual goal: plugins uploaded to Feishu can still take the "generate a standard basekit project → opdev upload" path and run on basekit FaaS; the container-renderer / self-hosted track takes the "self-hosted execute runtime" path. One and the same execute DSL, two hosts.

This also explains the watershed of the entire architecture:
- **Read-only data views** (stat/chart/table/gauge/pivot/…, 12 renderers and 9 operators already implemented) → the container rendering engine reads the DSL and uses opdev's `@lark-opdev/block-bitable-api` to directly read the host data and render in the webview. **No execute, no new outbound networking, no writes**, so no backend runtime is needed.
- **execute-class** (field shortcuts / connectors) → must initiate outbound HTTP (e.g., Open-Meteo) and map the response into an output column. For the container-renderer track this step must land on our k8s, so that all outbound calls go through one auditable runtime.

---

## 1.5 Official Reference Pattern and Calibration (2026-06-23, per official Vibe Coding / FaaS development guide)

The official documentation confirms the pattern we want to replicate and provides key facts (sources: Feishu's official "AI Coding Practices" + "Field Shortcut Plugin (FaaS Edition) - Development Guide" + `Lark-Base-Team/field-demo` + `@lark-opdev/block-basekit-server-api` type definitions):

1. **A FaaS shortcut is essentially = a nodejs function deployed on Feishu's servers** (linux-x64 / Node 14.21.0 / 1 core 1G / 15min timeout / 2–4 concurrency / queue 10k / 1h queue wait). **This is the basekit FaaS that plugins uploaded to Feishu run on; for the container-renderer track we self-host an equivalent execute runtime instead.**
2. **The official recommended "outsource the heavy lifting" pattern**: the shortcut's `execute` uses `addDomainList` to whitelist a `fetch` to an **external backend service** (the official example deploys Markitdown on replit + API key authentication; the example shortcuts "AI file-to-text / attachment-to-text / short-link generator" are all of this form). **This is exactly the role of execute-runner** — the only difference is that the host switches from replit (public cloud) to the customer's own k8s.
3. **Traffic signature verification mechanism `baseSignature` + `packID`**: when the shortcut requests the external backend, it carries a signature header, and the backend verifies the signature with the developer's public key to confirm the request truly comes from Feishu Base. → When our runner is called by the webview/api, it must also have an equivalent **request-origin verification** (see §6 verification model).

**Calibration conclusion**: execute-runner is positioned as **"the single self-hosted backend where all execute/connector plugins converge"** — instead of each plugin calling external APIs separately, all outbound traffic uniformly goes through the runner. Benefits: outbound whitelist, credential handling, auditing, and rate limiting are **centralized in one place**, so self-hosting customers only need to audit this single outbound point. This is more controllable than the official "each shortcut connects to its own replit," and is our differentiation.

### 1.5.1 Generator Corrections Reconciled from the Official Authoritative Enumeration
- 🐞 **Fixed**: `shortcut.ValidFormatters` originally included `PERCENT_ROUNDED_2` (which **does not exist** in the SDK enumeration); if the LLM selected it, it would generate an invalid `NumberFormatter.PERCENT_ROUNDED_2`. It has been corrected to the authoritative set per `dist/index.d.ts` (`INTEGER / DIGITAL_ROUNDED_1..4 / DIGITAL_THOUSANDS / DIGITAL_THOUSANDS_DECIMALS / PERCENTAGE_ROUNDED / PERCENTAGE`).
- 📋 **Gaps (incremental work to be assessed)**: components missing `Radio / MultipleSelect / TableSelect` (currently FieldSelect/Input/SingleSelect); authentication missing `OAuth2 / MultiHeaderToken / MultiQueryParamToken / Custom` (currently 4 kinds); input/result field types missing `User / Attachment`. These are incremental additions to render+validate+test, to be scheduled as needed.
- ✅ **Confirmed aligned**: the official addField structure of `Lark-Base-Team/field-demo` (i18n / formItems FieldSelect+supportType / resultType Object with id+isGroupByKey / execute with debugLog+fetch wrapper / FieldCode) is consistent with what our `render.go` produces.

## 2. Design Goals / Non-Goals

**Goals**
1. Provide a stateless service `execute-runner` on k8s that, per the execute DSL (`internal/shortcut`'s `FieldShortcut.Execute`/`Steps` + `Expr`/`Template` mapping), initiates controlled outbound requests and returns the mapped output.
2. **Hard-enforce the three red lines** (see §4): do not execute user JS, do not introduce new outbound domains, do not write.
3. Seamlessly integrate into the same orchestration as the existing `deploy/k8s/` (api/generator/ingress/netpol/pdb, PSS restricted, distroless nonroot).
4. Self-hosting customers spin it up with one click on their own k8s/k3s; the Feishu webview container plugin points to the customer's own runner URL.

**Non-Goals**
- Do not run arbitrary user JS. The execute we generate is **inherently declarative** (URL templates + whitelisted expressions); the runtime is **an interpreter, not a code sandbox** (see §3). An arbitrary-AI-code-snippet sandbox is an optional Phase 4b, not done by default.
- No multi-tenant SaaS / billing (single-tenant, internal enterprise use, following the established decision).
- Does not replace the read-only renderers; read-only views continue to run in the webview client.
- The runtime **does not touch writes** — it only fetches + maps + returns, and does not hold Bitable credentials.

---

## 3. Execution Model: Interpret a Declarative DSL, Not Run Code

The execute our generator produces is already a constrained declarative plan, not free-form code:

```
FieldShortcut {
  Domains  []string          // outbound whitelist (hard constraint)
  Auth     *Auth             // credentials the user fills in at configuration time (never hardcoded)
  FormItems[]FormItem        // inputs (city name…)
  Steps    []Step            // ordered multi-step: step.url contains {city} / {geo.results.0.latitude} placeholders
  Execute  Execute           // or single step
  Result.Properties[].Expr   // res.<json path> / in.<input> + - * / ( ) rand(), compile-time whitelist validation, never eval
  Result.Properties[].Template // pure string concatenation with {key} placeholders
}
```

> This is exactly the shape of the already-verified "city → weather" logic: step `geo` fetches latitude/longitude → step `weather` uses `{geo.results.0.latitude}` to fetch the temperature → `Expr = res.current.temperature_2m` maps to the "current temperature" column. I have already tested locally that it returns real data (Beijing 29℃, Shanghai 22.2℃…).

**Key design decision: the runtime is a DSL interpreter.** (Already implemented, see `internal/execrt`)
- `expr.go` originally only **validated + translated to JS** (compile-time), with no runtime evaluator. So `internal/execrt/eval.go` **newly wrote a Go evaluator** that implements function semantics exactly identical to `exprHelperDefs`; and it **reuses** `shortcut.ValidateExpr` to do the whitelist validation before evaluation. A parity unit test (`TestExprFuncParity`) asserts that the function set implemented by the interpreter == `shortcut.ExprFuncNames()`, guaranteeing the interpreter never drifts from the compiler.
- For each Step: interpolate `{placeholder}` using "inputs + prior step responses" → `shortcut.CheckURLHost` validates the URL host ∈ `Domains` → send the request → store under the `<stepId>` namespace.
- For each Result property: evaluate (with whitelisted operators) using `Expr`/`Template` over the `in.*` and `res.*` namespaces.
- **There is no `eval` anywhere, no running of user JS** → red line ① "do not execute user JS" is guaranteed by construction.

> Phase 4b (optional, not done by default): if in the future we need to support arbitrary AI code snippets that templates cannot cover, then introduce `quickjs-emscripten` (WASM sandbox, no host access) or one k8s pod per app. At that point this service is upgraded from "interpreter" to "interpreter + WASM sandbox," but the default path is always declarative interpretation.

---

## 4. Security Model: How the Three Red Lines Are Hard-Enforced

| Red Line | Enforcement Means |
|---|---|
| **Do not execute user JS** | The runtime interprets the declarative DSL (URL templates + whitelisted Expr) and never evals. Expr reuses the whitelisted evaluation of `internal/shortcut/expr.go` (only `in.*`/`res.*` value access + `+-*/()` + `rand()`). Arbitrary code snippets → denied by default; only enter the WASM sandbox in Phase 4b. |
| **Do not introduce new outbound domains** | **Double-layer enforcement**: ① runtime layer — each outbound URL's host is resolved, and if ∉ that plugin's `Domains` it is denied (`shortcut.CheckURLHost`); ② k8s layer — the **egress NetworkPolicy / outbound forward-proxy whitelist** for `execute-runner`, so that even if the interpreter has a bug, the pod can only reach declared hosts. For a self-hosting customer, auditing "which external networks can this plugin reach" = looking at `Domains`. The **SSRF guard** additionally refuses to dial private/loopback/link-local IPs; when an outbound proxy is configured (`HTTP_PROXY`, i.e., the production egress control plane), connections to **that proxy address** are allowed (the proxy is the outbound control point, and the host whitelist still applies), but other private-network targets are not allowed. |
| **Do not write** | The runtime **only fetches + maps + returns**, holds no Bitable / tenant credentials, and has no code path to write host data. Writing (if needed in the future) can only be done by the SDK in the webview under the user's permissions, and it is another explicit gated path — not within this runtime. |

**Defense in depth (following the existing `deploy/k8s` baseline)**
- PSS `restricted` namespace; distroless nonroot (uid 65532), `readOnlyRootFilesystem`, `drop ALL caps`, `seccomp RuntimeDefault`, `allowPrivilegeEscalation:false` — copy the securityContext of `20-api.yaml` directly.
- Resource `requests/limits` (CPU/memory) + per-request **timeout** (prevents slow responses / SSRF from hanging) + response-body size cap (the generated code already has a `text.slice(0,4000)` prototype).
- Inbound NetworkPolicy: only allow `api` (or the renderer via ingress) to reach `execute-runner`, default-deny the rest.
- **User Auth credentials** (e.g., the key of some API): passed in with the request, discarded after use, **not persisted/cached in the runtime**; when self-hosted, credentials never leave the customer's cluster.
- SSRF protection: the URL host must hit the `Domains` whitelist (already red line ②); additionally forbid resolving to intranet/link-local addresses (deny `169.254/10./172.16/192.168/127.`).

---

## 5. Architectural Positioning and Call Chain

```
Feishu webview (not in k8s)              Customer's own k8s / k3s cluster
┌─────────────────────────┐            ┌──────────────────────────────────────┐
│ Container plugin + render │            │  Ingress(TLS)                          │
│  · read-only: local render│            │    ├── api (BFF)        Deployment×2   │
│  · execute-class: HTTPS → │──────────▶ │    │     · definition CRUD / gen proxy │
│    runner                │  (needs to  │    │     · proxy forward → execute-runner │
│                          │  be in the   │    ├── generator         (intranet)    │
│                          │  Feishu      │    ├── execute-runner ★  Deployment+HPA│  ← new in this doc
│                          │  security    │    │     · interpret execute DSL       │
│                          │  whitelist + │    │     · outbound limited to plugin.Domains │
│                          │  TLS)        │    └── (egress whitelist proxy / netpol) │
└─────────────────────────┘            └──────────────────────────────────────┘
```

- **execute-runner** = a new k8s `Deployment` + `Service` (+ `HPA`, for bursty workloads).
- The caller picks one of two (see §8 open questions):
  - **A. webview connects to runner directly** (via ingress, requires a TLS domain + adding that domain to Feishu's "Security Settings → Server Domain Whitelist," the same constraint as existing backend domains);
  - **B. webview → api → runner** (api is the unified entry point doing auth/rate-limiting/auditing, runner is only reachable within the cluster). **B recommended**: reuse api's existing Bearer auth + rate limiting + request logging; the runner is not exposed to the public network, and inbound only accepts api.

---

## 6. API Contract (execute-runner)

```
POST /execute
Authorization: Bearer <EXECUTE_RUNNER_TOKEN>    # if using option B, injected by api; the runner reads/validates it as PLATFORM_API_TOKEN
Content-Type: application/json

{
  "pluginId": "city-weather-query",     // or inline "dsl": {FieldShortcut...}
  "inputs":  { "city": "Beijing" },     // values of FormItems (the renderer reads them from host cells)
  "auth":    { "weatherApiKey": "..." } // optional, credentials the user filled in at config time; discarded after use
}

200 OK
{
  "ok": true,
  "data": { "temperature": 29.0, "wind_speed": 12.8 }   // Result.Properties mapping result
}

4xx/5xx
{ "ok": false, "error": "domain_not_allowed: example.com ∉ [api.open-meteo.com,...]" }
```

- Stateless, idempotent (same input same output, except the `_id` generated by `rand()`).
- 12-factor: config goes through env / ConfigMap / Secret; can be smoothly integrated into the existing orchestration.
- After the renderer obtains `data`, it renders it into a cell/column per the Result definition (following the existing renderer layer).
- **Already implemented**: `/execute` accepts an inline `dsl`; this is exactly the form used for M2 validation.
- **Token contract (shipped, B1 capability split)**:
  - **client → api** (`POST /api/execute`) = the **read-only** `PLATFORM_READ_TOKEN` (the same read-only token rendering already needs for `GET /api/apps*`; safe to embed in the client bundle — leaking it can only read/compute, cannot mutate the catalog or spend the LLM budget).
  - **api → runner** = `EXECUTE_RUNNER_TOKEN` (server-only; `cmd/api/main.go` injects it as the forwarded request's `Authorization: Bearer`); the runner reads/validates the same value via `PLATFORM_API_TOKEN` (`cmd/execute-runner/main.go`). Each hop uses its own token; the client never sees the runner token.
- **Concurrency cap (shipped)**: the runner limits in-flight requests via `EXECUTE_MAX_CONCURRENCY` (default 64); on overload it sheds with **HTTP 429 + Retry-After** rather than hanging the pod (`cmd/execute-runner/main.go`).

### 6.1 Convergence Model: pluginId First (Calibrated from the Official Pattern)

In the official pattern each shortcut connects to its own backend; we **converge to a single runner**, so requests prefer `pluginId`:
- **`pluginId` (recommended, convergence)**: api (call-chain B) fetches the registered DSL from the definition store (Bitable Store) by id and forwards it to the runner. The webview only sends `{pluginId, inputs}`, **does not expose the DSL to the client**, and the set of outbound domains is centrally decided/audited by the backend-registered definitions.
- **inline `dsl` (already implemented, for debugging / no-store scenarios)**: stuff the DSL directly into the request. The runner still runs `Validate()` defensively.
- The forwarding logic of looking up the store by pluginId = **M3** (api → runner); the runner-side `/execute` supports both input forms.

### 6.2 Request Verification Model (Corresponding to the Official baseSignature + packID)

Official: the shortcut request to the external backend carries `baseSignature` (Feishu signature) + `packID`, and the backend verifies the signature with the developer's public key to confirm the origin. Our equivalent:
- **call-chain B (recommended)**: webview→api uses the read-only Bearer (`PLATFORM_READ_TOKEN`) + CORS narrowed to the Feishu webview Origin; api→runner is in-cluster + Bearer (`EXECUTE_RUNNER_TOKEN`, read by the runner as `PLATFORM_API_TOKEN`); the runner is not exposed to the public network. Verification is centralized in api, and the runner only trusts api.
- **call-chain A (webview connects to runner directly)**: only then do you need to move the official `baseSignature` verification onto the runner (public-key signature verification + `packID` validation) to prevent others from hitting the runner directly. Going with B by default avoids this.
- When self-hosted both ends are within the customer's domain, so the trust boundary is shorter; but Bearer + CORS + TLS + domain whitelist remain the baseline.

### 6.3 Egress Ledger (shipped)

"Which external networks did this plugin reach, and did the call succeed" went from a design goal to a **per-hop audited fact**. Every outbound attempt (each hop in a multi-step chain) emits **one** `action=execute.egress` audit event at the `execrt.fetch` chokepoint via the `EgressRecorder` interface, written into the **same** `audit_log` table as the platform audit and reviewable through the admin `GET /api/audit`:

- **Fields**: `actor=plugin:<id>` (attribution = the platform pluginId forwarded by api's `/api/execute`, falling back to `fs.ID`), `target=<host>`, `detail=method/outcome/step`.
- **Blocks are audited too**: SSRF / redirect / domain-whitelist blocks are recorded with `outcome=error` — a "blocked egress" leaves a trail just like a successful one.
- **Hot-path-safe**: stdout is **always** written first; persistence runs on a **single async-buffered worker** (1024-buffer; when full it sheds the audit write and never blocks execute, accounting the drop count separately).
- **Graceful drain**: `SIGTERM → HTTP drains first → the worker flushes the buffer before exiting` (10s cap), so a pod restart drops no buffered record.
- **Persistence prerequisite**: the runner needs `FEISHU_APP_ID/SECRET/BITABLE_APP_TOKEN` + `FEISHU_AUDIT_TABLE_ID` to persist; any missing = stdout-only (the `audit_log` table is created by `bitable-bootstrap`).
- Implementation: `internal/execrt/engine.go` (`EgressRecorder` interface + `fetch` chokepoint), `cmd/execute-runner/main.go` (async buffer + drain), `internal/api/execute.go` (forwards pluginId for attribution). Tests: `internal/execrt/egress_test.go`.

---

## 7. Integration with the Existing deploy/k8s (Implementation Checklist)

| File | Change |
|---|---|
| `deploy/k8s/15-execute-runner.yaml` (new) | `Deployment` (image `feishu-plugin-platform/execute-runner`, securityContext copied from `20-api.yaml`) + `Service` + `HPA` |
| `deploy/k8s/40-netpol.yaml` | Add: ① inbound — only allow `app: api` to reach `app: execute-runner`; ② outbound — `execute-runner` egress only allows DNS + whitelisted hosts (requires Calico/Cilium to truly take effect; flannel/kindnet is a no-op, as noted in the docs) |
| `deploy/k8s/00-namespace-config.yaml` | Add `EXECUTE_RUNNER_URL` (for api forwarding), timeout/size-cap parameters, etc. to the ConfigMap |
| `cmd/execute-runner/` (new Go service) | Reuse `internal/shortcut` (DSL types + `expr.go` evaluation + Domains validation); pure standard-library HTTP; isomorphic with api/generator |
| `internal/shortcut/dsl.go` | Correct the header comment "runtime is ALWAYS Feishu FaaS / no self-hostable runtime" to "dual hosts: plugins uploaded to Feishu = basekit upload; container-renderer / self-hosted track = self-hosted execute-runner" |

> **Optional implementation of the outbound whitelist proxy**: if the cluster CNI does not support egress netpol, use a forward proxy (e.g., squid/envoy with an allowlist) as the sole outbound exit for `execute-runner`, point the runner's `HTTP_PROXY` at it, and have the proxy allow the union of all cluster plugins' `Domains`. This way red line ② does not depend on the CNI.

---

## 8. Decisions (User Settled "Per Recommendation" on 2026-06-23) + Remaining Open Items

**Settled:**
1. **Call chain = B** (webview→api→runner, runner not exposed to the public network, verification/rate-limiting/auditing centralized in api).
2. **Runtime read-only** — strictly fetch + map + return, holds no Bitable credentials, no write path (red line ③). Writing back to a host column (if needed in the future) goes through the webview SDK under the user's permissions, as a separate case.
3. **User Auth credentials** — when self-hosted they are only stored in the customer's cluster (Secret / definition table) and never leave the cluster; at runtime they are carried in with the request, discarded after use, and not persisted.
4. **Execution model = interpret declarative DSL** (red line ① guaranteed by construction); the Phase 4b quickjs arbitrary-code sandbox is **not done by default**, only added when templates cannot cover the case.

**Shipped (formerly open items):**
- **Per-hop outbound auditing** ✅ — every outbound hop / block is now recorded in the `execute.egress` audit ledger (see §6.3); "which external networks this plugin reached and whether the call succeeded" is reviewable via `GET /api/audit`. No longer an open item.

**Remaining open items:**
5. **Outbound whitelist enforcement layer**: CNI egress netpol (requires Calico/Cilium) or a forward proxy? Depends on the customer's cluster CNI (decided in M3/M4 per the customer's environment).
6. **Observability (metrics layer)**: the success rate / latency of execute calls could still get a metrics layer (the audit ledger already covers the per-hop facts; echoing the existing discipline of "spot balance/failures at the first moment").
7. **Credential reuse**: should multiple executions of the same plugin store one encrypted copy of the credential in the customer's cluster for reuse, or have it carried in by the webview config each time? (Affects §6.2 and the credential UX.)

---

## 9. Milestones

- **M1 Design** (this doc) ✅.
- **M2 Interpreter service** ✅ **completed and verified for real**: `cmd/execute-runner` + `/execute` API; `internal/execrt` (`eval.go` Go evaluator + `engine.go` multi-step/single-step fetch + mapping + SSRF guard); reuses `internal/shortcut`'s validation/Domains. Unit-test coverage: arithmetic/functions/conditionals/paths/rand, func parity, multi-step chains, Domains rejection, SSRF rejection, single-step QueryParam auth, invalid-DSL rejection, **egress ledger (`egress_test.go`: per-hop events + blocks=error + async-buffer/drain)**. `go build/vet/test ./...` all green. **Real end-to-end smoke test**: the service actually ran the "city → weather" DSL and returned, for the real Open-Meteo, Beijing 26.3℃ / Tokyo 19.6℃ / London 30.6℃ / Paris 33.9℃ (multi-step chain: geocoding → weather, pure self-hosted interpretation, no Feishu FaaS).
- **M3 Live** ✅ **completed and verified end-to-end for real (2026-06-25)** — **production = single-node docker compose + Caddy auto-TLS on an AWS EC2 host (Let's Encrypt issued via a `<ip>.sslip.io` magic-DNS host), `STORE=bitable`**; `deploy/k8s/` is the **optional future/scale-out path, NOT the primary**:
  - ✅ `deploy/compose/docker-compose.prod.yml` runs execute-runner alongside api/generator on EC2; the k8s assets (`deploy/k8s/15-execute-runner.yaml` Deployment×2 + Service + HPA, securityContext copied from api, SSRF guard ON; `Dockerfile.execute-runner` distroless nonroot; netpol `allow-api-to-execute-runner` + `execute-runner-egress`) are retained as the optional scale-out path.
  - ✅ ConfigMap/compose injects `EXECUTE_RUNNER_URL` + `EXECUTE_RUNNER_TOKEN` (api side) / `PLATFORM_API_TOKEN` (runner reads the same value to validate) + `EXECUTE_MAX_CONCURRENCY`.
  - ✅ **Call chain B implemented and verified for real**: `POST /api/execute` (`internal/api/execute.go`, client uses the read-only `PLATFORM_READ_TOKEN`) forwards to the runner; supports inline `dsl` and `pluginId` (fetches DSL from session + plugin store, convergence model); unit-test coverage (503 when unconfigured / inline forward + Bearer / missing params / pluginId requires login).
  - ✅ **Real production end-to-end (EC2, `STORE=bitable`)**: real client → real api (`/api/execute`) → real runner → real Open-Meteo, with every `execute.egress` hop persisted into the Feishu Base `audit_log` table (reviewable via `GET /api/audit`).
- **M4 Hardening**: resource quotas, outbound proxy, (as needed) Phase 4b sandbox per-app pod isolation.

---

## Appendix: Relationship to the ROADMAP Red Lines

`ROADMAP.en.md` lists execute/connectors in the 🔴 red zone, with three uncrossable red lines = do not execute user JS · do not introduce new outbound domains · do not write. This design **does not break the red lines; rather it gives the red zone a controlled landing container**: an interpreter (not JS execution) + a double-layer Domains whitelist (no new outbound networking) + read-only fetch (no writes). The green/yellow zones continue to run in the webview client, and the red zone runs in a self-hosted k8s runner — a clean layering.
