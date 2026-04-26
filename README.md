# codegraph

A Go AST → property-graph indexer with a semantic + structural exploration UI.

The Go CLI parses every `go.mod` under a root, builds a granular graph
(modules, packages, types, methods, calls, interface satisfaction, cross-module
links via `replace` / `go.work`), and persists it to
`.codegraph/graph.jsonl`. The optional `embeddings/` Docker stack ingests that
file into Postgres + pgvector (for semantic search) and Neo4j (for call-graph
traversal), and serves a Bootstrap UI with vis-network for interactive
exploration.

```
go.mod tree                 .codegraph/                 docker compose
─────────────                graph.jsonl ───┬──────►  pgvector  (semantic neighbours)
   │                                        │
   ▼                                        └──────►  neo4j     (call hierarchy)
codegraph build  ──── writes ───┐                          ▲
                                │                          │
codegraph embed  ──── POST /embed ──────►  indexer  ───────┘
codegraph search ──── POST /search        (FastAPI + vis-network UI)
                                                  ▲
                              browser  ◄──────────┘  http://localhost:8088
```

## Install

```bash
go install github.com/rish-0-0/code-graph-rag/cmd/codegraph@latest
```

From source:

```bash
git clone https://github.com/rish-0-0/code-graph-rag
cd code-graph-rag
make build         # produces ./codegraph (or ./codegraph.exe on Windows)
```

Requires Go 1.23+.

## Quickstart — full stack

The `Makefile` orchestrates everything. From the repo root:

```bash
make all
# 1. boots pgvector + ollama + neo4j + indexer (docker compose)
# 2. runs `codegraph build` against the current dir
# 3. runs `codegraph embed` to ship docs/snippets to pgvector
# 4. POSTs /graph/refresh so Neo4j reloads the call graph
```

When it finishes, open **http://localhost:8088/**:

| Page                            | What you get                                                 |
|---------------------------------|--------------------------------------------------------------|
| `/`                             | Search bar + index stats (counts by kind, top packages, recent symbols). |
| `/search?query=…`               | Paginated semantic results (cosine over pgvector).           |
| `/symbol?id=<node_id>`          | Embedded text + semantic neighbours + **View call hierarchy** button. |
| `/flow?id=<node_id>&depth=5`    | Interactive vis-network call graph (Neo4j-backed).           |

The bottom-left graph icon on every page re-runs `graph.jsonl` ingest into
Neo4j without restarting any container — click it after every `make index`.

### Indexing a different repository

```bash
make all ROOT=/path/to/your/go/repo
```

`ROOT` flows into `codegraph build --root` and reaches the indexer container
via the read-only `../.codegraph` volume mount in `embeddings/docker-compose.yml`.
For a repo outside this folder, run `codegraph build --root /elsewhere` so it
writes to `/elsewhere/.codegraph/graph.jsonl`, then either bind-mount that
into the indexer or copy/symlink it into `code-graph-rag/.codegraph/`.

### Without the Makefile

```bash
# 1. stack
cd embeddings && cp .env.example .env && docker compose up -d --build && cd ..

# 2. index
codegraph build --root .
EMBEDDINGS_API_ENDPOINT=http://localhost:8088 codegraph embed

# 3. point the UI at the new graph
curl -X POST http://localhost:8088/graph/refresh
```

## Make targets

```bash
make help               # full list

# Go CLI
make build              # ./codegraph
make install            # go install
make test               # go test ./...
make build-darwin-arm64 # cross-compile
make clean

# Docker stack
make stack-up           # pgvector + ollama + neo4j + indexer
make stack-down         # stop, keep data
make stack-clean        # stop + drop volumes (wipes pg + neo4j)
make stack-logs         # tail indexer logs
make stack-status       # docker compose ps

# Indexing pipeline
make index              # codegraph build → .codegraph/graph.jsonl
make embed              # codegraph embed → pgvector
make refresh            # POST /graph/refresh → Neo4j re-ingests jsonl
make search Q='blast radius across modules'
make all                # stack-up + index + embed + refresh

# Open in browser
make open-ui            # http://localhost:8088
make open-neo4j         # http://localhost:7474 (login neo4j / codegraph)
```

`ENDPOINT` and `ROOT` are overridable: `make all ROOT=/srv/myrepo ENDPOINT=http://192.168.1.5:8088`.

## CLI

Every subcommand has `--help`.

```bash
codegraph build   --root .                     # parse + persist
codegraph embed                                # ship to pgvector
codegraph search  "compute reverse dependency blast radius"
codegraph blast   --symbol Foo                 # reverse-dep set, fast (reads jsonl)
codegraph broken                               # dangling refs, unsatisfied interfaces
codegraph query   …                            # packages, callers, neighbours
codegraph schema                               # node/edge vocabulary + sample Cypher
```

## Indexing multiple repos at once

Common setup: a parent folder with several local clones from the same GitHub
org, each its own Go module that `require`s the others.

```
~/work/<org>/
├── moduleA/   (require github.com/<org>/moduleB, moduleC)
├── moduleB/
├── moduleC/
└── scripts/   (bash, no go.mod)
```

Plain `codegraph build --root .` will resolve cross-org imports against
`$GOMODCACHE` instead of the local clones, so cross-repo `CALLS` edges land
on cached code (or get dropped). Drop a `go.work` at the parent first:

```bash
cd ~/work/<org>
go work init ./moduleA ./moduleB ./moduleC
codegraph build --root .
```

`codegraph` detects the `go.work`, emits a `Workspace` node with `WORKSPACES`
edges to each module, and Pass 2 of the indexer resolves cross-module CALLS
to the **real** Function nodes in the sibling clones.

Non-Go subfolders (like `./scripts/` above) are silently ignored — `discover`
only recognises directories containing a `go.mod`. But `go work init ./*/`
will fail on them; enumerate Go modules explicitly, or filter:

```bash
go work init $(for d in */; do [ -f "$d/go.mod" ] && echo "./$d"; done)
```

### Pruning the walk

```bash
codegraph build --root . --ignore scripts,docs,apps/legacy
codegraph build --root . --only   moduleA,moduleB
```

`--ignore` skips the named directories during the walk (in addition to the
always-skipped `.git`, `vendor`, `node_modules`). `--only` keeps only modules
whose directory matches one of the entries. Each value is a directory
basename (`scripts`) or a root-relative path (`apps/legacy`).

### Smoke-testing your workspace setup

If you suspect the workspace isn't wired up, this is the smoking gun:

```bash
codegraph broken --json | jq '.dangling | length'
```

A high count where `to` IDs reference local org modules means cross-module
resolution is failing — re-check `go.work`.

## Stack details

The four services and their ports (override in `embeddings/.env`):

| Service    | Image                       | Default port(s)        | Role                                              |
|------------|-----------------------------|------------------------|---------------------------------------------------|
| `pgvector` | `pgvector/pgvector:pg16`    | `5432`                 | Embeddings + symbol metadata.                     |
| `ollama`   | `ollama/ollama`             | `11434`                | Embedding model (`nomic-embed-text`, 768-dim).    |
| `neo4j`    | `neo4j:5-community`         | `7474` (UI), `7687` (bolt) | Property graph, queried via APOC.             |
| `indexer`  | local `./embeddings/indexer` | `8088`                | FastAPI bridge + UI; the only thing you talk to.  |

Service contract:

```
POST /embed           { "items": [Item, …] }                  → { "upserted": N }
POST /search          { "query": str, "k": int }              → { "results": [...] }
GET  /graph/status                                            → { loaded, nodes, edges, ... }
POST /graph/refresh                                           → reloads CODEGRAPH_GRAPH_PATH into Neo4j
GET  /graph/flow?id=&depth=&edges=&direction=                 → vis-network {nodes, edges, truncated}
```

JSON endpoints honour `EMBEDDINGS_API_AUTH` (bearer token). HTML pages are
open (local-dev tool).

More details, env vars, and port-collision tips: [`embeddings/README.md`](embeddings/README.md).

## Contribute

```bash
make test
```

Tests use the fixtures under `testdata/`:

- `testdata/01-hello` — single-module baseline (types, methods, interface
  satisfaction, calls, constants).
- `testdata/02-multi-module` — two modules + a `replace` directive; exercises
  cross-module call resolution.

When you add a schema feature, add (or extend) a fixture that exercises it
and a test under `internal/indexer/golang/` that asserts the expected
nodes/edges. Keep fixtures minimal — one file per concept where possible.
