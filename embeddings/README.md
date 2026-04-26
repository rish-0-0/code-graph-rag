# codegraph embeddings stack

Four services wired together with `docker compose`:

| Service    | Image                       | Role                                                                            |
|------------|-----------------------------|---------------------------------------------------------------------------------|
| `pgvector` | `pgvector/pgvector:pg16`    | Postgres + pgvector, schema applied on init.                                    |
| `ollama`   | `ollama/ollama`             | Hosts the embedding model (default `nomic-embed-text`, 768-dim).                |
| `neo4j`    | `neo4j:5-community`         | Stores the property graph for call-hierarchy queries (APOC bundled).            |
| `indexer`  | local `./indexer` (FastAPI) | Bridge: embeds text, upserts pgvector, ingests `graph.jsonl`, serves the UI.    |

The Go `codegraph` CLI **never talks to Postgres or Neo4j directly** — it only
POSTs to the indexer. That keeps the binary small and the data plane in one
place.

## Quickstart

```bash
cd embeddings
cp .env.example .env             # edit ports/passwords if you like
docker compose up -d --build
# wait ~30s the first time while ollama pulls the model and neo4j boots
curl localhost:8088/healthz       # → {"ok":true,"model":"...","dim":768}
```

Then back at the repo root:

```bash
export EMBEDDINGS_API_ENDPOINT=http://localhost:8088

codegraph build --root .          # writes .codegraph/graph.jsonl
codegraph embed                   # ships docs/snippets into pgvector
```

Open `http://localhost:8088/` — landing page with stats, semantic search, and
links into the call-hierarchy viewer.

## Pages

| URL                                | What                                                       |
|------------------------------------|------------------------------------------------------------|
| `/`                                | Search bar + index stats (counts by kind, top packages, recent symbols). |
| `/search?query=…&page=N`           | Paginated semantic results (page size 25).                 |
| `/symbol?id=<node_id>`             | One symbol's embedded text + semantic neighbors + **View call hierarchy** button. |
| `/flow?id=<node_id>&depth=5`       | Interactive call-graph view (vis-network, Neo4j-backed).   |

The bottom-left graph icon (visible on every page) re-runs `graph.jsonl`
ingest into Neo4j without restarting any container — click it after every
`codegraph build`.

## Call-hierarchy view

`/flow` uses APOC's `apoc.path.subgraphAll` against the Neo4j graph to render
a capped subgraph (≤500 nodes) around the chosen symbol. Toolbar lets you
toggle edge kinds (`CALLS`, `IMPLEMENTS`, `HAS_METHOD`, `CONTAINS`, …),
direction (callers / callees / both), and depth (1–10).

- Single-click a node → opens its `/symbol` page in a new tab.
- Double-click a node → re-centers the graph on it.
- "Open Neo4j Browser ↗" link in the corner drops you into raw Cypher at
  `http://localhost:${NEO4J_HTTP_PORT:-7474}` (login `neo4j` / `codegraph`).

## Service contract

```
POST /embed           { "items": [Item, …] }                   → { "upserted": N }
POST /search          { "query": str, "k": int }               → { "results": [...] }
GET  /graph/status                                             → { loaded, nodes, edges, ... }
POST /graph/refresh                                            → same as /graph/status
GET  /graph/flow?id=&depth=&edges=&direction=                  → vis-network {nodes, edges, truncated}
```

JSON endpoints honour the optional `EMBEDDINGS_API_AUTH` bearer token. HTML
pages are open (local-dev tool).

## Choosing a different model

Change `OLLAMA_MODEL` and `EMBED_DIM` in `.env` so they match. The schema's
`vector(N)` column is rewritten on container start to track `EMBED_DIM`. If
you're switching models on an existing volume, drop the table first:

```sql
DROP TABLE code_embeddings;
```

## Direct connection vars

The Go CLI doesn't need these — they're consumed by `indexer`:

- pgvector: `DB_HOST`, `DB_PORT`, `DB_USER`, `DB_PASSWORD`, `DB_NAME`
- neo4j:    `NEO4J_URI`, `NEO4J_USER`, `NEO4J_PASSWORD`, `NEO4J_BROWSER_URL`
- graph file path inside the container: `CODEGRAPH_GRAPH_PATH` (default `/data/graph.jsonl`; compose mounts the host's `.codegraph/` here read-only).

## Port collisions

If you already run Neo4j on `7474/7687`, set `NEO4J_HTTP_PORT` /
`NEO4J_BOLT_PORT` in `.env` to free ports. Same idea if 8088 or 5432 are
taken.
