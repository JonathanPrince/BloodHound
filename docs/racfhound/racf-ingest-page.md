# RACF Ingest page

## Purpose

**Administration → Data Collection → RACF Ingest** takes the two artifacts a RACF
assessment starts from and turns them into graph data without leaving the UI:

- an **IRRDBU00 unload** of the RACF database (required)
- a **runtime inventory** YAML naming the datasets that are not in the RACF
  database — `apf`, `parmlib`, `proclib`, `lnklst`, `lpa`, `racf_db` (optional)

Before this page the same result took a local Python install and three commands
(`racfhound export`, `racfhound login`, `racfhound upload`).

## Why a sidecar

Parsing IRRDBU00 is fixed-width record work that lives in `mfpandas` and
`mfpandas-racfhound` — Python. Rather than port it to Go, the fork runs the
existing `racfhound` package as a small HTTP service and has the API call it.

```text
Browser ──multipart──▶ POST /api/v2/racf/ingest        (bh-api, Go)
                            │  saves uploads to a temp dir it owns
                            │  202 Accepted + ingest job
                            ▼
                       racfhound-svc  POST /transform   (Python, FastAPI)
                            │  mfpandas parse → to_bloodhound()
                            ▼
                       OpenGraph JSON
                            │
                            ▼
                       ordinary ingest pipeline (validate → task → datapipe)
```

The generated graph goes through exactly the same validation and datapipe path as
a hand-uploaded OpenGraph file, so it needs no special handling downstream and
appears in the **File Ingest** table as `racf_opengraph.json`.

## Behavior

- The page requires the `GraphDBIngestManage` permission; without it the inputs
  are disabled and a warning is shown.
- Before accepting an upload the API probes the sidecar's `/healthz`. If it is
  not running the request fails immediately with **503** rather than after a
  multi-gigabyte upload.
- The response is **202 Accepted** as soon as the uploads are on local disk. The
  transform runs in the background — minutes on a production RACF database — so
  progress is followed on the **File Ingest** page, not this one.
- A failed transform marks the ingest job **Failed** with the sidecar's error as
  the status message, so it never sits at "Running" forever.
- Uploads are streamed to a temp directory the API owns, not the request's own
  multipart temp files, because `net/http` deletes those once the handler
  returns and the background transform outlives it.

## Running the sidecar

The image is built from the RACFHound workspace, not this repo, because
`racfhound` depends on its sibling `mfpandas-racfhound`:

```bash
cd ../RACFHound-workspace
docker build -f racfhound/Dockerfile -t racfhound-svc:local .
```

Then bring it up alongside the dev stack. It sits in its own `racf` profile so a
stock `--profile dev` up does not fail when the image has not been built:

```bash
docker compose --profile dev --profile racf -f docker-compose.dev.yml up -d
```

`bh-api` finds it through two environment variables, both with working defaults:

| Variable | Default | Meaning |
|---|---|---|
| `RACFHOUND_SERVICE_URL` | `http://racfhound-svc:8000` | Where the sidecar listens |
| `RACFHOUND_SERVICE_TIMEOUT` | `60m` | Ceiling on a single transform |

The sidecar has **no authentication** and publishes no port — only `bh-api`
should reach it. Do not expose it.

## Implementation boundary

| Layer | File |
|---|---|
| Page | `cmd/ui/src/racfhound/RACFIngest.tsx` |
| API call / hook | `cmd/ui/src/racfhound/racfIngestApi.ts` |
| Route constant | `cmd/ui/src/racfhound/routes.ts` |
| Handler | `cmd/api/src/api/v2/racfingest.go` |
| Sidecar client | `cmd/api/src/racfhound/transform.go` |
| Service | `racfhound/racfhound/service.py` (RACFHound repo) |

Only three shared files are touched: `cmd/ui/src/routes/constants.ts` (one nav
entry plus a re-export), `cmd/api/src/api/registration/v2.go` (one route), and
`docker-compose.dev.yml` (one service plus two env vars).

### Two naming traps

- **`racfIngestApi.ts`, not `racfIngest.ts`.** A module differing from
  `RACFIngest.tsx` only by case resolves to the wrong file on case-insensitive
  filesystems (Windows, macOS) — the page's default export silently becomes
  `undefined` at render time.
- **The page must not import `src/routes/constants`.** That module lazy-imports
  this page; importing a constant back out of it forms a cycle with the same
  `undefined`-component symptom. The RACF route constant therefore lives in
  `cmd/ui/src/racfhound/routes.ts`, which `constants.ts` imports and re-exports.

## Adding a new dataset list

The per-list override fields (`apf`, `parmlib`, …) are accepted by the API and
the sidecar but are not exposed on the page — the inventory YAML already carries
every list. Adding a new one means one row in each of:

- `racfhound/inventory.py` — `LIST_INPUTS`
- `mfpandas_racfhound/nodes.py` — `DATASET_FLAGS`
- `cmd/api/src/racfhound/transform.go` — `ListFields`

`TestAcceptedFieldsCoversDumpInventoryAndEveryList` fails when the Go list drifts
from the Python one.
