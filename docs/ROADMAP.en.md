> 🌐 [中文](ROADMAP.md) · **English**

# Capability Roadmap: Feishu Plugin Ecosystem × Our Container/DSL Platform Coverage

> Production method: 6-way parallel web research on the Feishu plugin ecosystem → graded against this platform's "pre-reviewed container + read-only data DSL" model → adversarial review and finalization (2026-06-22).
> After deduplication, roughly **30 categories**: 🟢 5 / 🟡 15 / 🔴 12.

## 0. Core Principle (where the boundary comes from)

The **sole prerequisite** for "one-click generation + zero review" to hold: the output is a **DSL (data)**, interpreted and rendered by a **container that is reviewed only once**.
The range that can be generated in one click ≡ **the range the container DSL can express**. The bottleneck is not natural-language generation (DeepSeek has long been good enough), it's the capability of the container interpreter.

**Three red lines that must never be crossed** (crossing any one = breaching the zero-review prerequisite → must downgrade to "export a standalone plugin"):
1. **Never execute** arbitrary JS from the user's output.
2. **Never introduce** new outbound domains (hard-constrained by Feishu's "server domain allowlist"—proven on a real device).
3. **Never write to** host data / never change already-reviewed permission scopes. **Writing is the true watershed of the grading, not "read-only vs. visualization."**
   - ⚠️ Hidden trap: rendering Markdown/rich text inside a host cell, if not strongly sanitized, = JS can be injected without a single line of user JS (data-channel XSS). Bound fields must default to plain-text escaping across the board.

---

## 🟢 Green Zone: Covered directly by the current DSL / a micro-extension away (true one-click + zero review)

| Type | Status |
|---|---|
| Basic charts (column/bar/line/area/pie/donut) | `chart` implemented (x=grouping / y=aggregation) + `bind.tableId`; **the real-device order dashboard has verified correct rendering with real data** |
| Metric card / KPI | `stat` implemented (sum/avg/count/max/min); on a real device "total order amount / order count / total sales volume" are already correct |
| Metric trend (line grouped by time) | = `chart(line, x=date field)`, no new building block |
| Leaderboard / TopN | Add a `sort desc + limit N` modifier to the existing aggregation pipeline; micro-extension, read-only |
| Text/heading block | `text` exists; **bound fields must be force-escaped to plain text, not parsed as HTML/Markdown** |

---

## 🟡 Yellow Zone: Doable by the container, but needs new building blocks / a second container (cost increasing S→L)

| Type | DSL building block to add | Size | Notes |
|---|---|---|---|
| **Container interpreter / rendering layer (foundation)** | Two layers of "aggregation operator + renderer" (register pattern); cross-table read-only data access layer | M | **The real asset.** Adding a chart = registering a renderer, adding a statistic = registering an operator, the interpreter itself is reviewed only once. stat/chart/text already run on a real device, but the explicit abstraction is missing |
| **Detail table (select columns + filter)** | table renderer; **truly implement the filter operator** (currently a "filter not executed" placeholder stub); row-level read-only (note batch read ≤200) | M | **A prerequisite that blocks several items: first make filter a real operator**, otherwise every "display after filtering" is an empty promise |
| Progress chart / goal attainment (gauge) | gauge/progress renderer (current + target) | S | Logic is simple, listed yellow only because a new renderer is needed |
| Cell Markdown / long-text preview | markdown renderer + **DOMPurify-grade strong sanitization** (forbid script/iframe/on*/javascript:) | M | **Safety red-line cell**: a hole in the sanitization layer = the zero-review prerequisite fails |
| Pivot table / pivot chart | pivot building block (two grouping fields + one aggregation) + 2D grouping | M | Pure aggregation, no new atomic operator |
| Advanced chart family (funnel/radar/scatter/sankey/waterfall/histogram/box plot/word cloud/heatmap) | chart.type enum extension + new operators (cumsum/quantile/bucketing/flow/word frequency) | L | Ship the pure-aggregation ones first (funnel/radar/scatter), defer the ones needing new operators |
| Timeline | timeline renderer (date + event, read-only layout) | S | Close to the green zone |
| Hierarchy / tree | tree renderer (parent / bidirectional link → build the tree) | M | Read-only display, controlled collapsing OK |
| Read-only native view mirror (kanban/calendar/gallery) | read-only kanban / calendar / gallery renderer | L | **Explicitly "read-only presentation," never promise drag-and-drop write-back** (write-back drops it into the red zone) |
| Record view extension (single-record layout / card / print) | record-layout DSL + read-only active record + browser-native print | M | **Requires a second reviewed container** (record-view blockTypeID); recommended for v2 |
| Countdown | countdown renderer + interpreter-built-in controlled tick | S | The dynamics are provided by the interpreter, not user JS |
| **Map view** | map renderer + the map vendor's domain fixed into the reviewed allowlist + key held by the backend + geocoding pre-processed by the backend | L | **The most dangerous cell in the yellow zone**: the only one requiring an outbound third party. Letting users fill in their own map source/key drops it into the red zone |
| **Templated light automation** (scheduled / threshold → send a card / write one record) | automation-lite DSL + platform backend scheduler + platform bot identity | L | **Includes writing, crosses the pure-read-only boundary**: writes may only happen on the backend with platform credentials, the action set must be an enumerable allowlist; otherwise it moves to the red zone. Head-to-head with the official "App Mode" |

---

## 🔴 Red Zone: impossible with zero review → take the "export a standalone plugin + submit for review" escape hatch

Needs "arbitrary code / third-party API·OAuth / server-side logic / write-back workflow / new native capability," outside the container's capability domain:

- Automation extensions (execute server-side logic, e.g. OCR write-back)
- AI / formula field shortcuts (batch write-back based on Coze / enterprise APIs)
- Field extensions (custom field types), formula extensions (custom functions)
- Data connectors (e.g. bidirectional Jira sync)
- App bots / workflow bots / link previews / message card callbacks
- Web apps (H5) / mini programs (Gadget)
- Workspace widgets / cloud-document widgets (write-back + new host slot)
- Browser extensions (web clip → write to Bitable)
- aily agents / aPaaS low-code / AnyCross / **the official "App Mode" (head-on competitor)**
- Large business-SaaS categories such as CRM / project / HRM / finance / BI / OA / ticketing

> Escape-hatch positioning: turn these into a **paid value-added product line** of "**one-click generated scaffold code + guided submission for review**," not a zero-review promise.

---

## Strategic Takeaways

1. **The moat = a container reviewed only once + a pure-data DSL**; sustained by strictly holding the three red lines above. Any feature that lets users fill in their own API/key/code (even in one spot) breaches the prerequisite—better to drop it into the red-zone export.
2. **The first version strictly holds to a "read-only data dashboard"**: the green zone + low-cost yellow zone (filter/table/progress/timeline/pivot) can absorb almost all of Feishu's "dashboard + read-only visualization" demand.
3. **The highest-priority engineering item = turn filter from a "not executed" badge into a real operator**—it blocks several items such as detail tables / filtered dashboards.
4. **Abstract the container into two layers (aggregation operator + renderer)**: this turns the yellow-zone advanced charts from "added one by one linearly" into "add a building block, reviewed only once." This is the engineering realization of the "bypass case-by-case review" insight.
5. **Draw a clear line between two product lines**: zero-review container output (data DSL) vs. review-required standalone plugins (code), to avoid over-promising to users.
6. **The competitor = the official "App Mode + AI Workflow" (launched 2025-11)**: on the "build a complete business system" dimension we can't win. Differentiation = (a) one-click reuse of cross-tenant templates; (b) lighter, read-only dashboards that work the moment you embed them in any Bitable view slot; (c) treat field shortcuts / Coze as an outlet, not an opponent.

---

## In One Sentence

**What we generate in one click is a "declarative data view," not an "arbitrary plugin."** Treat this sentence as the product boundary, and the green zone + low-cost yellow zone are the clear, winnable battlefield where the zero-review moat holds.

---
---

# Generator Mainline Roadmap (field shortcuts / automation + enterprise listing)

> The above is the coverage of the **container/view-component** model; this section addresses the now-mainline **field shortcut (addField) / automation (addAction) NL generator + export mode**.
> Production method: multi-agent workflow—parallel audit of the real code (`internal/shortcut/*`, `internal/generator/*`, `publisher/`) + web research on Feishu enterprise listing + fact-checking + synthesis (2026-06-23).

## A. Capability-Building Roadmap

The core lever = the **"test-to-generation" loop**: the platform already renders `test/index.ts` (running testField/testAction), but the generator **never executes it**—the auto-fix loop only consumes static `Validate()` errors (`shortcut_llm.go:215`) → a plugin can "compile but return wrong data." Wiring this up is what upgrades from "compiles" to "actually returns the right value," and it's also the scoring signal for trustworthy auto-publish.

Ranked by cost-effectiveness (value/effort):

| Priority | Capability | Value | Size | Key points |
|---|---|---|---|---|
| ✅1 | **Expression conditional logic** (comparison + ternary) — **completed 2026-06-23** | High | M | Added `eq/ne/gt/gte/lt/lte` + `and/or/not` + `if/coalesce/default` + `floor/ceil/abs/min/max` as **allowlisted pure-JS functions** (`expr.go`); the parser and the `< > = ? : & \|` ban are unchanged, still no eval. Verified on a real device: `plugin-center/idcard-gender` determines gender by the parity of the 17th digit of the ID number, build:field compiles + testField truly runs (`…0011`→male / `…0028`→female); NL→DeepSeek also correctly produces `if(eq(substr(...) % 2,1),'男','女')`. Fixed the TS strict-arithmetic error on `string % 2` (helper return type marked `any`) |
| ✅2 | **Validated template library / few-shot** (completed 2026-06-23) | High | S | `internal/generator/exemplars/{field,action}.json` embeds 6+1 validated "NL→DSL" examples (go:embed); `exemplars.go` retrieves the 2-3 most relevant by Chinese bigram + English word overlap and injects them into the system prompt. A unit test guarantees the examples always `Validate()` (no drift). On a real device: NL "English-to-Chinese" → the model reproduces the nested path `res.responseData.translatedText` from the example and compiles |
| ✅3 | **Test→generation loop** (completed 2026-06-23) | High | L | `verify.go`'s `Verifier`: after `Validate()` passes, compile **against the real SDK** with `block-basekit-cli build:field`, feeding compile errors back into the same fix loop (which previously only consumed static Validate errors). Opt-in (`VERIFY_BUILD=1` + `BASEKIT_NODE_MODULES`), gracefully skipped without a toolchain (`errVerifyUnavailable`). Class proven captured on a real device: `substr(in.text)` (missing argument) **passes Validate but compiles with TS2554** → caught by build; the unit-test fakeVerifier proves a compile failure drives a fix round |
| ✅4 | The **write path stack** all completed (2026-06-23): ✅action bodyJson + ✅`PUT/PATCH/DELETE` + ✅custom header + ✅multi-step chaining | High/Mid | M→S→L | `ValidMethods` adds PUT/PATCH/DELETE; `Execute.Headers`; body/bodyJson apply to POST/PUT/PATCH; unified `renderFetchInit`. **Multi-step chaining** (`steps.go`): `Steps []Step` (≤3), each step's response bound to `s_<id>`, the last step aliased `res`; placeholders `{input}` and `{priorStepId.json.path}` (array indices supported) are resolved in url/headers/body; forward references / coexisting with execute / coexisting with auth are all rejected. On a real device: an httpbin two-step chain (field·testField + action·testAction: step2 uses step1's .method, echoed back correctly); **live NL "city → lat/long → weather" generates the cross-step array reference `${s_geo?.[0]?.lat}` and passes validation**. **The technical prerequisites for connectors (structured body + REST write + chaining) are all in place.** |
| ✅5 | Result-column type extension **all completed** (scalar + Url + MultiSelect, 2026-06-23) | Mid | M | Added **Phone/Email/Currency/Progress/Rating/Barcode** (scalar) + **Url** (`{text,link}` cell) + **MultiSelect** (`string[]`, with the new `split(text, ',')` expr function producing an array); per the SDK, fixed that the **primary column must be Text\|Number**. All `build:field`-compiled against the real SDK + testField shapes correct; **both Url's `{text,link}` and MultiSelect's `string[]` are confirmed write/read against real Bitable fields** (write code:0, read-back correct). plugin-center adds `github-homepage` (Url), `tags-split` (MultiSelect). **No significant gap remains in result-column types.** |
| 6 | Pagination / array iteration; attachment binaries | Mid/Low | L | Currently only fetches a fixed-length `res.list.0.x`; attachment SDK has high uncertainty → defer to last |
| ✅🎯 | **Connector / write-back of Bitable records** (Option A done + real-device write confirmed 2026-06-23) | High | L | Chose A = action calls the Feishu OpenAPI. **Key finding: multi-step chaining unlocks it directly, zero new generator code**—a 2-step pipeline (step1 fetches `tenant_access_token` → step2 `bitable/v1 .../records/batch_create`, header `Authorization: Bearer {token.tenant_access_token}`, body `{records:[{fields:{column:{input}}}]}`). **End-to-end real-device verification (user-authorized)**: testAction → actually wrote 1 record to a test Bitable, `code:0 record_id=recvnkoBDe0ohs`, read back fields `{title, body}` exactly correct, the temp table has been cleaned up. Also: DSL validation + scaffold, tsc compiles against the real SDK, the token chain returns `code:0`, and live NL can also generate it. The generator added an action-prompt connector recipe + few-shot + plugin-center `feishu-record-writeback` (credentials/Bitable are runtime inputs, never baked in). **The connector (Bitable record write-back) holds.** The client-side view track (Option B) is still not open—revisit on demand |
| Side branch | Dashboard / custom view, Basic/OAuth2 completion | Low/Mid | L/M | Dashboard code is in the dormant `internal/dsl` track with zero real-device verification; another SDK + another review point, revisit on demand |

**Suggested cadence**: ⭐1 + ⭐2 (days-scale, immediate quality lift) → ⭐3 loop → write path stack → connector strategic sprint.

## B. Publishing Flow Compliant with Enterprise Listing

**Conclusion (xinchuang / government-enterprise): go with "export mode + disable review exemption + manual admin review."**

| # | Stage | Who | Compliance gate |
|---|---|---|---|
| 0 | Intake: one sentence → DSL, `dsl.json` stores NL + DSL | Business side / platform | Starting point of the audit trail (provenance) |
| 1 | Compile-time hard gate: expr allowlist no-eval, `url host ⊆ domains`, result ≤20, minimal scope | Platform | Machine pre-screen of source + outbound allowlist + least-privilege permissions |
| 2 | **Source-code security human review**: line-by-line reading of `index.ts`/`register.ts` + SCA/SBOM | Security & compliance (run by the enterprise itself) | **The core human review**—Feishu self-built apps do not go through an official source review, so government-enterprise must run this gate |
| 3 | Create the app + register the extension to obtain APP_ID/blockTypeID (publisher RPA) | Platform / admin | Carrier ready + minimal scope granted |
| 4 | Push the bundle with the official `block-basekit-cli upload` | Platform / admin | Runtime outbound: runtime enforces `addDomainList` + `context.fetch` |
| 5 | Create a version + apply to publish: **converge the availability range to a small group / test cohort first** | Platform / admin | Version and availability range + any change forces a re-review (falsifiable hook) |
| 6 | **Admin manual approval to release** (or `PATCH app_versions status=1`) | Admin + compliance | The only mandatory approval gate—**explicitly turn off review exemption**, don't let versions go live silently |
| 7 | Go live + real-device joint debugging + member-behavior audit acceptance, then gradually widen the availability range | Admin / compliance | Audit-trail closure + data-security acceptance |

**Container vs. export** (reviewed once vs. each reviewed):
- **Export mode** (mainline): each plugin is independently auditable TS, uploaded one by one and reviewed one by one, every outbound domain / scope / version forced through review = the falsifiable hook that government-enterprise wants.
- **Container mode**: the container is reviewed only once, the output is DSL data. Backend auth is now **capability-split** (the client embeds only the read-only `PLATFORM_READ_TOKEN`; the admin `PLATFORM_API_TOKEN` / a session handle write+delete), so even a leaked client token can only read. **Residual**: the widget is not yet true per-user identity inside the Bitable webview (needs webview-OAuth), so strong-multi-tenant / external-sensitive cases should still use export mode or add per-user.

**Gaps that must be filled** (otherwise it doesn't count as enterprise-grade compliance):
1. ~~**Publish audit ledger**~~ → **Catalog write/delete audit is done**: a persisted Bitable ledger + `GET /api/audit` (admin, append-only, newest-first), see [PRODUCTION](PRODUCTION.en.md) §11. **Still pending**: the egress ledger (per-call execute-runtime egress) and bundle↔reviewed-source hashing (attestation), see "Enterprise enhancements" #2 below.
2. ~~**Shared-token upgrade**~~ → **Done**: capability-split auth shipped (client read-only / admin write+delete / session), see [PRODUCTION](PRODUCTION.en.md) §7 "Auth / security".

> ⚠️ **Must be verified firsthand in the target console** (low-confidence fact-check): ① Feishu's official intro page currently confirms the GA extension types as **record view / data-table view / automation action + field shortcut (server capability)**; "connector / dashboard" as official plugin types are not confirmed on the official page (or belong to the roadmap / an older version). ② The default headcount for review exemption and the OAuth2 behavior of private deployments are often wrong online—defer to the private-deployment console.

---

# Enterprise enhancements (make it delightful at 100k seats)

> How it was produced: a multi-agent workflow — 5 enterprise lenses (governance / authoring UX / adoption & distribution / integration & connectors / ops & trust) ideated in parallel, deduped & prioritized, plus a 4-dimension adversarially-verified code review (2026-06-24).
> Ordered by enterprise gain per unit effort (high → low). Effort: S ≈ 1 day · M ≈ 2–4 days · L ≈ 1 week+. **All build on the already-shipped capability-split auth + Bitable storage + self-hosted execute-runner — mostly thin reuse, not rewrites.**

## Tier 1 (highest ROI)

| # | Capability | Eff. | What it solves | How to build (reuse what exists) |
|---|-----------|------|----------------|----------------------------------|
| ✅1 | **Persisted audit ledger in a Bitable + read-only viewer** — **done** | M | Turns the ephemeral `AUDIT` stdout line (lost on restart) into a filterable, tamper-resistant who/when/what/which-version trail — exactly "gap #1" above | Shipped: `BitableAuditStore` (replays the BitablePluginStore pattern, append-only) + `server.go` writes/deletes go through `recordAudit` (stdout + persisted) + `GET /api/audit` (admin, newest-first) + `bitable-bootstrap` creates the `audit_log` table + the `FEISHU_AUDIT_TABLE_ID` knob. See PRODUCTION §11 |
| ✅2 | **Execute-runtime per-call egress ledger** — **done** | M | Records "which plugin sent which row's data to which external domain, for whom, allowed/blocked" — the DLP egress evidence a 信创 security team demands, backing the README's already-sold "egress audited in one place" | Shipped: an `EgressRecorder` interface records at the `fetch` choke point per hop (host/method/outcome; SSRF/redirect blocks = error) + `WithPluginID` attribution; the runner's `egressRecorder` maps events to `execute.egress` audit records and appends them to the same audit table via an **async-buffered single worker** (hot-path-safe: drops with a logged count, never slows execute). See PRODUCTION §11 |
| ✅3 | **In-UI dry-run ("试运行")** — **done** | M | A non-engineer sees the plugin call the real API and produce real values *before* the upload+review chain — kills the "generate a black box, upload blind, wait for review, find it was wrong" loop | Shipped (zero backend change): the field-shortcut result in `web/shortcut.html` gains a "试运行" panel — a sample-input form per formItem (+ a credential field), POSTs `{dsl,inputs,auth}` to the existing SSRF-guarded `/api/execute`, and shows the real mapped output (the value written to the cell); friendly message when the execute-runner isn't configured |
| ✅4 | **Human-readable failure explanations** — **done** | S | A non-engineer hitting a TS compiler stack-trace just gives up; friendly, actionable messages keep them in the flow and cut support load | Shipped: `generator.Explain(err)→(hint,detail)` maps developer-grade errors (TS2554 missing-arg / domain-allowlist / primary-column type / missing field / model+network / repair-exhaustion) to a plain-language hint; the repair loops carry the last concrete reason into the exhaustion error (so the hint is specific); all three generate handlers return `{error:hint, detail:raw}`; the UI shows the hint + a collapsible technical detail |

## Tier 2

| # | Capability | Eff. | What it solves | How to build |
|---|-----------|------|----------------|--------------|
| 5 | **Per-user / per-tenant quotas** | M | Stops one user (or a leaked read token) from burning the shared LLM budget / hammering external APIs — the baseline fair-use control any enterprise review asks for; the natural complement to the AI kill-switch | A GenerateRPM limiter + the auth identity already exist; key `/api/generate` on the authenticated OpenID and `/api/execute` on plugin/actor. Reuses the existing middleware |
| 6 | **Approval/publish workflow (draft → admin-approved → live catalog)** | M | No plugin a business user authored reaches colleagues' Bitables until a compliance owner approves — implements the one mandatory approval gate | Add a `status` field (draft/approved); gate the renderer-read `GET /api/apps` to approved while authors still see their drafts; approval is an admin-only PATCH logged to the #1 ledger |
| 7 | **My-plugins library: fix silent duplicate + version history/rollback** | M | Authors iterate fearlessly with revert; admins get a per-plugin change trail; the confusing duplicate pile-up that makes the library feel broken goes away | `SaveForUser` is *already* insert-or-replace by ID — the duplicate bug is the frontend sending a blank ID, so thread the ID back on re-save; version history = keep prior revisions in a new column; rollback = re-save an old revision |
| 8 | **Server-side credential vault** | L | Enterprises won't have each analyst paste a Jira/internal-API token into a cell; a central, admin-managed, audited vault is the line between a demo and IT approving 10k seats | Today `executeRequest.Auth` is user-supplied per call and never stored; add an admin-managed encrypted vault, the runner resolves auth refs server-side at execute time so secrets never touch the client, every resolution logs to the #2 ledger. Unblocks the whole enterprise connector track |
| 9 | **OAuth2 client-credentials runtime + token caching** | M | Most enterprise/SaaS APIs (and Feishu's own tenant_access_token) are OAuth2 client-credentials; without caching a 200-row recalc fires 200 token requests and trips upstream throttles | The runner already does multi-step chains (step 1 fetches a token, step 2 uses it); add a TTL token cache in `execrt.Engine` keyed by (credential-ref, token-url). Best paired with #8 |
| 10 | **Catalog manifest + searchable template gallery** | M | A 100k-person org discovers and reuses the ~14 already-verified, already-reviewed building blocks once — collapsing duplicate generation, duplicate LLM spend, duplicate review | The blocks already exist as `plugin-center/*`; generate a manifest and serve `GET /api/templates` (read token) with title/kind/description/domains for search. Prerequisite for "apply to my table" / in-Bitable discovery |
| 11 | **Scheduled / threshold automation** | L | Moves connectors from pull-only ("a human edits a row") to "every morning refresh the FX column" / "when status flips to Overdue, write to the escalation table" — the most-requested automation shape, competing with the official "App Mode" on the lightweight end | The yellow zone scopes this as automation-lite (writes only via platform creds, enumerable action allowlist); add a backend scheduler that invokes stored DSLs through execute-runner, writing back via the proven batch_create chain. Heaviest lift; depends on #1/#2/#8 |

## Explicitly deprioritized / overlaps already-built work
- **Read-only admin console (L)**: high value but largely a *packaging* of #1/#2/#5/#6; build the underlying ledgers/quotas/approvals first (each independently usable + viewable natively in the Base UI), assemble the console only when government-enterprise acceptance demands one pane of glass.
- **Artifact provenance manifest + content hash (M)**: meaningful for the attestation gap above, but partially served by the auditable source + Created-by provenance header; the high-value increment (bundle↔source hash comparison) can ride on #1's ledger as one extra column.

> ✅ **Tier 1 (#1–#4) is fully shipped** (audit ledger / egress ledger / dry-run / plain-language errors) — both the compliance and authoring-UX threads are nailed down, all thin reuse with zero new dependencies. **Pull Tier 2 by actual customer demand.**
