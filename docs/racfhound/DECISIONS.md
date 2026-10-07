# RACFHound Design Decisions

An append-only log of cross-cutting design/engineering decisions for this fork. Record a decision
here when it affects how the fork is structured, how it integrates with upstream, or how it's
maintained — anything a future contributor (or AI session) would otherwise have to re-derive.

Keep entries short. Newest first. One decision per entry:

> **YYYY-MM-DD — Title**
> **Decision:** what we chose.
> **Why:** the reason / problem it solves.
> **Alternatives:** what we rejected and why (optional).
> **Refs:** files, PRs, or docs.

Per-feature UI/behavior docs live beside this file in `docs/racfhound/`; the maintenance workflow
lives in [`MAINTAINING.md`](MAINTAINING.md). This log is for the "why", not the "how".

---

> **2026-08-24 — RACF panel queries never sort in Cypher; sorting is client-side**
> **Decision:** The relationship-panel query builders in `groupMembers.ts` emit `RETURN DISTINCT x` with
> no `ORDER BY`. `fetchRACFRelatedNodes` sorts the normalised results by name instead.
> **Why:** BloodHound's PostgreSQL graph driver translates `RETURN DISTINCT x ORDER BY x.name` into
> `SELECT DISTINCT ... ORDER BY x.name` with the sort key missing from the select list, which Postgres
> rejects: `ERROR: for SELECT DISTINCT, ORDER BY expressions must appear in select list (SQLSTATE
> 42P10)`. Every RACF relationship panel 500s on a pg-backed instance. Neo4j accepts the same Cypher,
> so this is invisible until the driver changes. Result sets here are a panel's worth of rows, so
> client-side sorting costs nothing.
> **Alternatives:** Adding the sort key to the RETURN — rejected, it changes the result shape the
> panels consume. Keeping Neo4j — rejected, see the ingest-performance entry below.
> **Refs:** `cmd/ui/src/racfhound/groupMembers.ts`, `relatedNodes.ts`, `groupMembers.test.ts`
> (guards the invariant across all eight builders).

> **2026-08-24 — Prefer the PostgreSQL graph driver for RACF-sized graphs**
> **Decision:** Run the dev stack with `bhe_graph_driver=pg`. Treat Neo4j as unsuitable for realistic
> RACF ingest until upstream fixes indexing for custom kinds.
> **Why:** Measured on a real 68 MB IRRDBU00 unload (80,566 nodes / 186,178 edges): Neo4j spent 20+
> minutes inside a *single* edge batch and committed nothing before being killed; Postgres completed
> the whole job in **3m06s**, with the graph write taking ~65s. Cause: BHCE's edge ingest merges
> endpoints without a label (`merge (s {objectid: ...})`), and `AssertSchema` only ever asserts
> `graphschema.DefaultGraphSchema()`, so custom kinds like RACF never get indexes — and a label-less
> MERGE could not use them anyway. Every row becomes a full node scan, so cost grows with the graph.
> **Note:** the driver is resolved by `tools.LookupGraphDriver` — the `database_switch` table wins if
> a row exists, otherwise the configured driver. Setting the env var alone does not persist.
> **Refs:** `cmd/api/src/api/tools/dbswitch.go`, `cmd/api/src/api/tools/pg.go` (`SwitchPostgreSQL`);
> `docs/racfhound/racf-ingest-page.md`.

> **2026-08-21 — The exporter is the source of truth for RACF kinds; the fork's lists are guarded**
> **Decision:** `mfpandas_racfhound` decides which node and edge kinds exist. The fork's three
> mirrors — `RACF_NODE_KINDS`, the `relationship` map in `commonSearchesRACF.ts`, and the two lists in
> `cmd/api/src/racfhound/pathfinding.go` — are kept complete against it, and each now has a test that
> fails on drift in either direction (a kind the exporter emits but the fork ignores, *and* a kind the
> fork lists that the exporter never emits).
> **Why:** All three had rotted silently. `pathfinding.go` still allowed `RACFCanUpdate`,
> `RACFCanControl`, `RACFCanAlter`, `RACFCanSubmitAs`, `RACFStartedAs`, `RACFAffects` and
> `RACFSubgroupOf` — none of which the exporter has emitted for some time — while missing every edge
> added since, so program-control and operator-command escalation paths were invisible to Pathfinder.
> `RACF_NODE_KINDS` was missing `RACFProgram`/`RACFOperCmd` and carried two speculative kinds
> (`RACFFinding`, `RACFPath`) that nothing has ever produced. Nothing failed loudly — paths just
> silently did not appear.
> **Alternatives:** Generating the Go/TS lists from `custom-types.json` at build time — rejected for
> now: it couples the fork's build to a file in another repo. The drift tests give most of the benefit
> at none of that cost.
> **Refs:** `packages/javascript/bh-shared-ui/src/utils/racfNodeIcons.ts`,
> `packages/javascript/bh-shared-ui/src/commonSearchesRACF.ts`,
> `cmd/api/src/racfhound/pathfinding.go` and `pathfinding_test.go`; RACFHound `docs/graph-model.md`.

> **2026-08-21 — RACF ingest runs the Python transform as a sidecar, not a Go port**
> **Decision:** The RACF Ingest page uploads to a Go handler that forwards the files to a
> `racfhound serve` container (`racfhound-svc`) and feeds the returned OpenGraph JSON through the
> ordinary ingest pipeline. The sidecar lives in its own `racf` compose profile and publishes no port.
> **Why:** IRRDBU00 parsing is fixed-width record work already implemented and tested in `mfpandas` /
> `mfpandas-racfhound`. Porting it to Go would fork the RACF data model across two languages and two
> repos. Reusing the ingest pipeline means the generated graph needs no special downstream handling
> and shows up in the existing File Ingest table.
> **Alternatives:** (a) Port the parser to Go — rejected, duplicates the data model. (b) Embed Python
> in the API image — rejected, bloats an upstream-owned Dockerfile. (c) Have the browser call the
> Python service directly — rejected, it would need its own auth and CORS surface.
> **Refs:** `docs/racfhound/racf-ingest-page.md`, `cmd/api/src/api/v2/racfingest.go`,
> `cmd/api/src/racfhound/transform.go`, `racfhound/racfhound/service.py`.

> **2026-08-21 — RACF ingest answers 202 and reports progress as an ingest job**
> **Decision:** The handler saves uploads to a temp directory it owns, creates an ingest job, returns
> 202, and does the transform in a background goroutine. Failures mark the job `Failed` with the
> sidecar's message.
> **Why:** A production RACF unload parses for minutes — far past the proxy's read timeout, so a
> synchronous request would break on exactly the inputs that matter. The uploads must be copied out
> of the request's own multipart temp files because `net/http` deletes those once the handler returns.
> **Refs:** `cmd/api/src/api/v2/racfingest.go` (`saveRACFUploads`, `runRACFTransform`).

> **2026-08-21 — RACF route constants live in `cmd/ui/src/racfhound/routes.ts`**
> **Decision:** `ROUTE_ADMINISTRATION_RACF_INGEST` is declared in the RACF folder and imported (and
> re-exported) by `src/routes/constants.ts`, never the reverse. RACF pages link other admin pages by
> path string, as `FileUploadDialog` already does.
> **Why:** `constants.ts` lazy-imports the RACF page; a page that imports a constant back out of it
> forms a cycle and the component resolves to `undefined` at render time, with a misleading "element
> type is invalid" error. Keeping the dependency one-way also fits the fork's "touch shared files
> minimally" preference.
> **Refs:** `cmd/ui/src/racfhound/routes.ts`, `cmd/ui/src/routes/constants.ts`.

> **2026-08-01 — Pin the toolchain in fork-only files, not `package.json` engines**
> **Decision:** Pin Node (`.nvmrc`, `.tool-versions`) and Go (`.tool-versions`, matches `go.mod`);
> let Corepack manage yarn via the existing `packageManager` field. Do **not** add an `engines` block
> to the root `package.json`.
> **Why:** Consistent toolchain across machines without editing an upstream-owned file. The root
> `package.json` comes from SpecterOps; adding `engines` there creates merge surface on every sync.
> **Alternatives:** `engines` guardrail in `package.json` (active enforcement) — rejected to avoid
> upstream conflicts, consistent with the fork's "touch zero shared files" preference.
> **Refs:** `.nvmrc`, `.tool-versions`, `docs/racfhound/MAINTAINING.md`.

> **2026-07-31 — Object-info panel: inject RACF sections as `priorityTables`**
> **Decision:** The app-local `GraphItemInformationPanel` wrapper injects RACF relationship sections
> via `priorityTables`, not `additionalTables`.
> **Why:** `EntityInfoContent` only renders `additionalTables` for built-in kinds; custom/OpenGraph
> kinds like RACF are routed to `KindInfoItems`, which drops them — the sections silently vanish.
> `priorityTables` render unconditionally.
> **Alternatives:** Editing shared `EntityInfoContent` to render `additionalTables` for custom kinds —
> rejected as it touches an actively-refactored upstream file. Durable end-state: upstream a proper
> relationship-table slot for custom kinds.
> **Refs:** `cmd/ui/src/views/Explore/GraphItemInformationPanel.tsx`, `racfAdditionalTables.tsx`, and
> the two guarding tests; PR #6.

> **2026-07-27 — App-local wrapper for the object-information panel**
> **Decision:** Keep an app-local `GraphItemInformationPanel` wrapper that handles RACF nodes and
> delegates everything else to the shared upstream panel; `GraphView.tsx` imports the local wrapper.
> **Why:** Upstream moved the panel into `bh-shared-ui`, which cannot import app-local RACF
> components. The wrapper keeps all RACF UI in the app layer and touches zero shared-library files.
> **Refs:** PR #6; `docs/racfhound/MAINTAINING.md` (panel pattern).

> **2026-07-27 — Always merge upstream on a branch, never directly into `racf-main`**
> **Decision:** Upstream syncs happen on a `merge/*` branch, validated, then PR'd into `racf-main`.
> `main` stays a clean mirror of SpecterOps.
> **Why:** Keeps `racf-main` reviewable and revertible; isolates conflict resolution.
> **Refs:** PRs #6 and #8; `docs/racfhound/MAINTAINING.md` (syncing with upstream).

> **2026-07-27 — Guard the CLA Assistant workflow to the upstream org**
> **Decision:** The `cla.yml` CLA Assistant workflow should be guarded with
> `if: github.repository_owner == 'SpecterOps'` (or disabled on the fork).
> **Why:** On the fork the owner is a user, not an org, so `GET /orgs/<owner>/members` 404s and the
> `jq` step dies (`Cannot index string with string "login"`). The CLA secrets don't exist on the fork
> either. The check has no meaning on a personal fork.
> **Refs:** `.github/workflows/cla.yml`; `docs/racfhound/MAINTAINING.md` (CI section).
