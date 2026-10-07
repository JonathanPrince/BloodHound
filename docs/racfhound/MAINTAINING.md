# RACFHound Fork — Maintainer Notes

Practical, hard-won knowledge for maintaining this fork: how it relates to upstream, how
to sync, where conflicts hide, how to run the dev environment, and the non-obvious gotchas.
Keep this file updated as things are learned — it is meant to travel with the repo so any
machine/contributor can pick up.

## What this fork is

- This repo is a **RACFHound fork** of [SpecterOps/BloodHound](https://github.com/SpecterOps/BloodHound).
- `origin` is **JonathanPrince/BloodHound** (the fork). Note the owner is a **user, not an org** — this matters (see CLA below).
- **`racf-main`** is the working/integration branch: upstream BloodHound **plus** the RACF feature work.
- **`main`** is kept as a clean mirror of SpecterOps `main`. It is often stale locally; update it before syncing.
- The SpecterOps repo is **not** configured as a git remote by default.

Most RACF code lives in **isolated new files** that can't conflict with upstream:
`cmd/ui/src/racfhound/`, `cmd/api/src/racfhound/`, `packages/javascript/bh-shared-ui/src/commonSearchesRACF.ts`,
`packages/javascript/bh-shared-ui/src/utils/racfNodeIcons.ts`, and `docs/racfhound/`.
The real conflict surface is a handful of shared files that RACF also edits (see below).

## Syncing with upstream

You can sync either from your own `main` (if you keep it mirrored to SpecterOps) or directly from the SpecterOps remote.

```bash
# Option A: your fork's main already tracks SpecterOps — just update it, then merge
git fetch origin
git branch -f main origin/main            # fast-forward the mirror (main has no local-only commits)

# Option B: pull straight from SpecterOps
git remote add upstream https://github.com/SpecterOps/BloodHound.git   # one-time
git fetch upstream main --no-tags          # large repo — the first fetch can take minutes
```

Then **always merge on a dedicated branch** and open a PR into `racf-main` (do not merge directly):

```bash
git checkout -b merge/main-<date> racf-main
git merge --no-edit origin/main            # or upstream/main
# ...resolve conflicts (see hotspots), then validate (see below)
```

Assess conflict size before committing to it — a quick throwaway test merge tells you the real count:

```bash
git checkout -b _test racf-main && git merge --no-commit --no-ff origin/main
git diff --name-only --diff-filter=U       # conflicted files
git merge --abort && git checkout racf-main && git branch -D _test
```

### History so far
- **2026-07 (PR #6):** merged SpecterOps `main` @ 2026-07-24 into `racf-main`. This was the big one (152 upstream commits, ~45 RACF files); only **2** real conflicts.
- **2026-07-31:** incremental catch-up merge of SpecterOps @ 2026-07-31 (~53 commits) — **0** conflicts, because `racf-main` already contained the 07-24 base.

## Conflict hotspots (shared files RACF edits)

These are the files most likely to conflict on a sync. Everything else RACF added is isolated.

- `cmd/api/src/api/v2/pathfinding.go` — RACF concatenates `racfhound.PathfindingRelationships()` into the valid-kinds sets, and uses `racfhound.IsNonPathfindingRelationship`.
- `cmd/api/src/queries/graph.go` — `GetAllShortestPaths` uses `analysis.FetchNodeByObjectIDIncludeOpenGraph` (OpenGraph fallback lets RACF custom nodes join mixed paths).
- `cmd/ui/src/views/Explore/GraphView.tsx` — imports the **app-local** `GraphItemInformationPanel` wrapper instead of the shared one.
- `cmd/ui/src/views/Explore/GraphItemInformationPanel.tsx` — the wrapper (see panel pattern below).
- `packages/javascript/bh-shared-ui/src/utils/index.ts` — barrel export includes `racfNodeIcons`.
- `packages/javascript/bh-shared-ui/src/views/Explore/ExploreSearch/SavedQueries/QuerySearchFilter.tsx` — adds the `RACF` query-category menu item.
- `cmd/ui/src/routes/constants.ts` — adds the **RACF Ingest** entry to `adminSections` and re-exports `ROUTE_ADMINISTRATION_RACF_INGEST` from `src/racfhound/routes.ts`. Upstream edits this file whenever it adds an admin page, so expect conflicts here; the fix is almost always to keep both entries.
- `cmd/api/src/api/registration/v2.go` — one line registering `POST /api/v2/racf/ingest`.
- `docker-compose.dev.yml` — the `racfhound-svc` service and the two `RACFHOUND_*` env vars on `bh-api`.

### Keeping the RACF kind lists in step with the exporter

`mfpandas_racfhound` decides which node and edge kinds exist; the fork mirrors that list in **three**
places, and all three have rotted before. When the exporter gains or renames a kind, update:

| File | Holds |
|---|---|
| `packages/javascript/bh-shared-ui/src/utils/racfNodeIcons.ts` | `RACF_NODE_KINDS` + fallback icons |
| `packages/javascript/bh-shared-ui/src/commonSearchesRACF.ts` | the `relationship` edge map |
| `cmd/api/src/racfhound/pathfinding.go` | pathfinding / non-pathfinding edge split |

Each has a drift test that fails in **both** directions — a kind the exporter emits that the fork
ignores, and a kind the fork lists that the exporter never emits. The failure mode without them is
silent: Pathfinder simply never proposes paths across the unknown edge, and nothing errors.

Authoritative model: the RACFHound repo's `docs/graph-model.md`, with
`racfhound/data/custom-types.json` as the icon registry. Ground-truth check when the docs look stale:

```bash
grep -ohE '"RACF[A-Za-z_]+"' mfpandas_racfhound/*.py | sort -u
```

**Semantic-merge warning:** the backend files above frequently *auto-merge textually* even when both sides edited them. Auto-merge success ≠ correctness — always run the Go build + RACF tests after, and confirm the helper functions RACF calls still exist upstream (e.g. `FetchNodeByObjectIDIncludeOpenGraph`).

## The object-information panel pattern (important)

Upstream moved `GraphItemInformationPanel` into `bh-shared-ui`, which **cannot import app-local RACF
components**. So the fork keeps an **app-local wrapper** at
`cmd/ui/src/views/Explore/GraphItemInformationPanel.tsx` that:

- renders RACF nodes directly via `EntityInfoPanel`, injecting RACF relationship sections, and
- delegates every other case (edges, errors, loading, non-RACF nodes) to the shared upstream panel.

RACF table resolution lives in `cmd/ui/src/racfhound/racfAdditionalTables.tsx`. This touches **zero
shared-library files**, minimizing future conflicts. `GraphView.tsx` imports this local wrapper.

Node shape note: `NodeDetails.kinds` is `NodeKindRef[]` (objects with `.name`), **not** `string[]`.

### Gotcha: inject sections as `priorityTables`, not `additionalTables`
`EntityInfoContent` only renders `additionalTables` (via `EntityInfoList`) for **built-in** kinds.
Custom / OpenGraph kinds like RACF are routed to `KindInfoItems` (static markdown docs from
`node.info`), which **ignores `additionalTables`** — so RACF sections silently vanish. Inject them
as **`priorityTables`** (rendered unconditionally by `EntityInfoDataTablePriorityList`, above the
object info) so they show for custom kinds.

**This regression is invisible to component-level tests** that render the RACF table components
directly — you must render through the real panel. Guarded by:
- `cmd/ui/src/views/Explore/GraphItemInformationPanel.test.tsx` (renders a RACF node through the real panel)
- `cmd/ui/src/racfhound/racfAdditionalTables.test.tsx` (kind detection against the `NodeKindRef[]` shape)

Durable end-state: contribute an upstream relationship-table slot for custom kinds, so the fork
needs no panel customization at all.

## Validating a merge

```bash
# Dependencies — upstream changes package.json/yarn.lock frequently. ALWAYS run this after a merge:
yarn install
# A stale node_modules produces confusing "Cannot find module 'x'" errors in UPSTREAM files, not yours.

# Backend
( cd cmd/api/src && CGO_ENABLED=0 go build ./... )

# Frontend typecheck (fast path)
( cd cmd/ui && npx tsc --noEmit )          # or `yarn check-types` (also builds shared workspaces first)

# RACF tests
( cd cmd/api/src && go test ./racfhound/... )
( cd cmd/ui && TZ=America/Los_Angeles npx vitest run src/racfhound src/views/Explore/GraphItemInformationPanel )
( cd packages/javascript/bh-shared-ui && TZ=America/Los_Angeles npx vitest run src/utils/racfNodeIcons )
```

### Windows test caveat
The full Go/UI suites show failures **when run natively on Windows** that are pure environment
artifacts, not real bugs:
- path separators (`assets\test.html` vs `assets/test.html`)
- directory `fsync` (`sync ...: Access is denied.`)
- temp-file locking on cleanup (`being used by another process`)
- timezone/ICU date formatting (`GMT+1` vs `GMT+01:00`)
- coarse clock resolution (two timestamps "not greater than" each other)

The project's real test runs happen **inside Linux containers** (`just test` → stbernard), where
these pass. Don't chase them on Windows.

## New machine setup

Get an identical toolchain and running app on a fresh clone:

```bash
# 1. Toolchain — versions are pinned in .tool-versions (mise/asdf) and .nvmrc.
mise install                 # or: asdf install   (installs Node 22 + Go 1.26.4)
# or with nvm/fnm:  nvm use   (reads .nvmrc → Node 22)
corepack enable              # activates yarn@4.13.0 from package.json "packageManager"

# 2. First-time repo/config setup (copies local config templates, syncs the go workspace)
just init                    # see the justfile; use `just init clean` to reset config files

# 3. Bring up the dev stack
just bh-dev up -d            # or the raw docker compose command (see below)
```

Pinned versions live in fork-only files so they don't conflict with upstream: **Node 22**
(`.nvmrc`, `.tool-versions`), **Go 1.26.4** (`.tool-versions`, matches `go.mod`), **yarn 4.13.0**
(Corepack, via `package.json` `packageManager`). Keep these in step with CI
(`.github/workflows/*` use Node 22) when upstream bumps them.

## Tracking design decisions

Cross-cutting decisions (structure, upstream integration, maintenance policy) go in an append-only
log at [`DECISIONS.md`](DECISIONS.md) — the "why". Per-feature UI/behavior stays in the per-panel
docs here — the "how". Record a decision whenever a future contributor would otherwise have to
re-derive it.

## Running the dev environment

```bash
docker compose --profile dev -f docker-compose.dev.yml up -d
```

Open **http://bloodhound.localhost** (or http://localhost via the proxy). API on :8080, Neo4j browser
:7474, Postgres :5432, pgAdmin :5050. `bh-api` runs under `air` (live-reloads Go changes); `bh-ui`
runs Vite dev.

### Dev gotchas (mostly Windows)
- **`just bh-dev up -d` fails with `could not find the shell 'sh'`** — `just` runs recipes via `sh`,
  which needs `C:\Program Files\Git\usr\bin` on PATH (only `...\Git\cmd` is there by default). Either
  add it to PATH or run the `docker compose ...` command directly.
- **`docker compose` fails with `//./pipe/docker_engine ... cannot find the file`** — the Docker engine
  isn't running. Check `wsl -l -v`; if `docker-desktop` is *Stopped* while `Docker Desktop.exe` is
  running, Docker Desktop is hung. Fix: kill the `Docker Desktop`/`com.docker.*` processes,
  `wsl --shutdown`, relaunch Docker Desktop, wait for the WSL backend. The correct docker context for
  the WSL2 backend is `desktop-linux` (`docker context use desktop-linux`).
- **UI shows `Failed to resolve import "<pkg>"` after a merge** — the `bh-ui` container bind-mounts only
  the `src` folders; `node_modules` is **baked into the image** at build time. When a merge adds/changes
  JS deps, **rebuild the image**, not just host `yarn install`:
  `docker compose --profile dev -f docker-compose.dev.yml up -d --build bh-ui`. Same applies to `bh-api`
  if Go deps change.
- `tree-sitter-*` native rebuild failures during `yarn install` on Windows are harmless (optional deps).

### Graph driver: use `pg`, not Neo4j

```bash
bhe_graph_driver=pg docker compose --profile dev --profile racf -f docker-compose.dev.yml up -d
```

Neo4j cannot ingest a realistically-sized RACF graph. Measured on a 68 MB unload (80k nodes / 186k
edges): Neo4j sat 20+ minutes in one edge batch and committed nothing; pg finished the whole job in
about three minutes. Custom kinds never get indexes (`AssertSchema` only asserts
`DefaultGraphSchema()`), and BHCE's edge ingest merges endpoints without a label, so every row is a
full node scan. See DECISIONS.md for the full write-up.

Two consequences worth remembering:

- **The env var does not persist.** `tools.LookupGraphDriver` prefers the `database_switch` table and
  falls back to the configured driver. Export `bhe_graph_driver=pg`, or call `SwitchPostgreSQL`
  (`cmd/api/src/api/tools/pg.go`), which writes the row *and* asserts the pg schema.
- **pg is stricter about Cypher than Neo4j.** `RETURN DISTINCT x ORDER BY x.name` is valid on Neo4j
  and a hard 500 on pg (`SQLSTATE 42P10`). Any new RACF query must be tested against pg — the RACF
  panels all broke this way. Sort client-side.

### The RACF transform sidecar

The **RACF Ingest** page needs `racfhound-svc`, a Python service that turns an IRRDBU00 unload into
OpenGraph JSON. Its image is built from the **RACFHound workspace**, not this repo, because
`racfhound` depends on its sibling `mfpandas-racfhound`:

```bash
cd ../RACFHound-workspace
docker build -f racfhound/Dockerfile -t racfhound-svc:local .
```

It lives in its own `racf` profile so a stock `--profile dev` up does not fail when that image is
missing:

```bash
docker compose --profile dev --profile racf -f docker-compose.dev.yml up -d
```

`bh-api` reaches it via `RACFHOUND_SERVICE_URL` (default `http://racfhound-svc:8000`) and bounds a
single transform with `RACFHOUND_SERVICE_TIMEOUT` (default `60m`). If the page returns **503**, the
sidecar isn't up — that check is a deliberate pre-flight so a multi-gigabyte upload doesn't have to
fail at the end. To run it on the host instead of in compose:
`pip install 'racfhound[serve]' && racfhound serve` and set
`RACFHOUND_SERVICE_URL=http://host.docker.internal:8000`.

Full design notes: [`racf-ingest-page.md`](racf-ingest-page.md).

## CI: CLA Assistant fails on the fork

`.github/workflows/cla.yml` is a **SpecterOps-org-only** workflow. On the fork it calls
`GET /orgs/JonathanPrince/members` — but the owner is a *user, not an org* — so the API returns a 404
object and `jq '.[] | .login'` dies with `Cannot index string with string "login"` (exit 5). The CLA
secrets don't exist on the fork either. Fix by guarding the job to the upstream org (survives future
merges):

```yaml
jobs:
  CLAssistant:
    runs-on: ubuntu-latest
    if: github.repository_owner == 'SpecterOps'
```

Or disable it entirely on the fork with no file change: `gh workflow disable "CLA Assistant"`.

## RACF feature docs

Per-panel behavior is documented alongside this file in `docs/racfhound/`:
`group-members-panel.md`, `group-relationships-panel.md`, `user-groups-panel.md`,
`user-relationships-panel.md`, `class-authorities-panel.md`. The upload/ingest flow is in
`racf-ingest-page.md`. See also `LLM_INSTRUCTIONS_RACFHOUND.md` at the repo root.
