"""FastAPI service that bridges codegraph -> ollama -> pgvector.

Endpoints:
  POST /embed   { "items": [Item, ...] }      -> { "upserted": N }
  POST /search  { "query": str, "k": int }    -> { "results": [...] }
  GET  /healthz                               -> { "ok": true }

Auth: optional bearer token via EMBEDDINGS_API_AUTH env var. If set, requests
      must send `Authorization: Bearer <token>`. If unset, the service is open
      (intended for local dev).
"""
from __future__ import annotations

import os
from contextlib import asynccontextmanager
from typing import Optional

import httpx
import psycopg
from fastapi import Depends, FastAPI, Header, HTTPException, Query, Request
from fastapi.responses import HTMLResponse, JSONResponse
from fastapi.templating import Jinja2Templates
from pydantic import BaseModel, Field

from graph_store import GraphStore


# ---- config -------------------------------------------------------------

DB_HOST = os.environ.get("DB_HOST", "localhost")
DB_PORT = int(os.environ.get("DB_PORT", "5432"))
DB_USER = os.environ.get("DB_USER", "codegraph")
DB_PASSWORD = os.environ.get("DB_PASSWORD", "codegraph")
DB_NAME = os.environ.get("DB_NAME", "codegraph")

OLLAMA_URL = os.environ.get("OLLAMA_URL", "http://localhost:11434").rstrip("/")
OLLAMA_MODEL = os.environ.get("OLLAMA_MODEL", "jina/jina-embeddings-v2-base-code")
EMBED_DIM = int(os.environ.get("EMBED_DIM", "768"))

API_AUTH = os.environ.get("EMBEDDINGS_API_AUTH", "").strip()

NEO4J_URI = os.environ.get("NEO4J_URI", "bolt://localhost:7687")
NEO4J_USER = os.environ.get("NEO4J_USER", "neo4j")
NEO4J_PASSWORD = os.environ.get("NEO4J_PASSWORD", "codegraph")
NEO4J_BROWSER_URL = os.environ.get("NEO4J_BROWSER_URL", "http://localhost:7474")
CODEGRAPH_GRAPH_PATH = os.environ.get("CODEGRAPH_GRAPH_PATH", "/data/graph.jsonl")


def conninfo() -> str:
    return (
        f"host={DB_HOST} port={DB_PORT} user={DB_USER} "
        f"password={DB_PASSWORD} dbname={DB_NAME}"
    )


# ---- models -------------------------------------------------------------

class Item(BaseModel):
    node_id: str
    kind: str
    module: str | None = None
    pkg: str | None = None
    name: str = ""
    text: str
    pos_file: str | None = None
    pos_line: int | None = None


class EmbedRequest(BaseModel):
    items: list[Item]


class SearchRequest(BaseModel):
    query: str
    k: int = Field(default=10, ge=1, le=200)


class SearchResult(BaseModel):
    node_id: str
    kind: str
    name: str
    pkg: str
    score: float
    text: str


# ---- lifespan -----------------------------------------------------------

state: dict = {}


@asynccontextmanager
async def lifespan(_: FastAPI):
    # One long-lived HTTP client (keeps Ollama connections warm).
    state["http"] = httpx.AsyncClient(timeout=120.0)
    state["graph"] = GraphStore(
        uri=NEO4J_URI,
        user=NEO4J_USER,
        password=NEO4J_PASSWORD,
        graph_path=CODEGRAPH_GRAPH_PATH,
        browser_url=NEO4J_BROWSER_URL,
    )
    # Ensure schema exists. Idempotent. Skips silently if file is missing
    # (compose mounts it as an init script too).
    schema_path = os.path.join(os.path.dirname(__file__), "schema.sql")
    if os.path.exists(schema_path):
        with open(schema_path, "r", encoding="utf-8") as f:
            sql = f.read()
        # Substitute dim if user changed EMBED_DIM at runtime.
        sql = sql.replace("vector(768)", f"vector({EMBED_DIM})")
        try:
            with psycopg.connect(conninfo()) as conn:
                with conn.cursor() as cur:
                    cur.execute(sql)
                conn.commit()
        except Exception as e:
            # Don't fail boot — the operator can run schema.sql manually.
            print(f"warn: could not apply schema: {e}")
    # Best-effort initial graph load — never block startup on it.
    try:
        state["graph"].refresh()
    except Exception as e:
        print(f"warn: initial graph load failed: {e}")
    try:
        yield
    finally:
        await state["http"].aclose()
        try:
            state["graph"].close()
        except Exception:
            pass


app = FastAPI(lifespan=lifespan)
templates = Jinja2Templates(directory=os.path.join(os.path.dirname(__file__), "templates"))

PAGE_SIZE = 25
MAX_PAGE = 8  # cap pagination depth — vector relevance drops off fast.


# ---- auth ---------------------------------------------------------------

def require_auth(authorization: Optional[str] = Header(default=None)) -> None:
    if not API_AUTH:
        return
    if not authorization or not authorization.lower().startswith("bearer "):
        raise HTTPException(status_code=401, detail="missing bearer token")
    if authorization.split(None, 1)[1].strip() != API_AUTH:
        raise HTTPException(status_code=401, detail="bad token")


# ---- ollama -------------------------------------------------------------

async def embed_one(text: str) -> list[float]:
    """Call Ollama's /api/embeddings for a single string."""
    r = await state["http"].post(
        f"{OLLAMA_URL}/api/embeddings",
        json={"model": OLLAMA_MODEL, "prompt": text},
    )
    r.raise_for_status()
    data = r.json()
    vec = data.get("embedding")
    if not vec:
        raise HTTPException(status_code=502, detail=f"ollama returned no embedding: {data}")
    if len(vec) != EMBED_DIM:
        raise HTTPException(
            status_code=500,
            detail=(
                f"embedding dim mismatch: got {len(vec)}, expected {EMBED_DIM}. "
                "Set EMBED_DIM env var to match your model and re-run schema.sql."
            ),
        )
    return vec


def vec_literal(v: list[float]) -> str:
    # pgvector accepts the textual form '[1,2,3]'.
    return "[" + ",".join(f"{x:.7f}" for x in v) + "]"


# ---- routes -------------------------------------------------------------

@app.get("/healthz")
async def healthz():
    return {"ok": True, "model": OLLAMA_MODEL, "dim": EMBED_DIM}


@app.post("/embed", dependencies=[Depends(require_auth)])
async def embed(req: EmbedRequest):
    if not req.items:
        return {"upserted": 0}

    # Embed sequentially. Ollama serves one at a time anyway; bursting hurts.
    embeds: list[list[float]] = []
    for it in req.items:
        embeds.append(await embed_one(it.text))

    upsert_sql = """
        INSERT INTO code_embeddings
            (node_id, kind, module, pkg, name, text, pos_file, pos_line, embedding, updated_at)
        VALUES (%s, %s, %s, %s, %s, %s, %s, %s, %s::vector, now())
        ON CONFLICT (node_id) DO UPDATE SET
            kind = EXCLUDED.kind,
            module = EXCLUDED.module,
            pkg = EXCLUDED.pkg,
            name = EXCLUDED.name,
            text = EXCLUDED.text,
            pos_file = EXCLUDED.pos_file,
            pos_line = EXCLUDED.pos_line,
            embedding = EXCLUDED.embedding,
            updated_at = now()
    """
    rows = [
        (
            it.node_id,
            it.kind,
            it.module,
            it.pkg,
            it.name,
            it.text,
            it.pos_file,
            it.pos_line,
            vec_literal(v),
        )
        for it, v in zip(req.items, embeds)
    ]
    with psycopg.connect(conninfo()) as conn:
        with conn.cursor() as cur:
            cur.executemany(upsert_sql, rows)
        conn.commit()
    return {"upserted": len(rows)}


@app.post("/search", dependencies=[Depends(require_auth)])
async def search(req: SearchRequest):
    qvec = await embed_one(req.query)
    sql = """
        SELECT node_id, kind, name, COALESCE(pkg, ''), text,
               1 - (embedding <=> %s::vector) AS score
        FROM code_embeddings
        ORDER BY embedding <=> %s::vector
        LIMIT %s
    """
    qlit = vec_literal(qvec)
    with psycopg.connect(conninfo()) as conn:
        with conn.cursor() as cur:
            cur.execute(sql, (qlit, qlit, req.k))
            rows = cur.fetchall()
    results = [
        SearchResult(
            node_id=r[0], kind=r[1], name=r[2] or "", pkg=r[3], text=r[4], score=float(r[5])
        )
        for r in rows
    ]
    return {"results": [r.model_dump() for r in results]}


# ---- HTML UI ------------------------------------------------------------

def _index_stats() -> dict:
    """Cheap aggregates for the landing page. Returns zeros on any error so
    the page still renders before the first embed run."""
    out = {"total": 0, "by_kind": [], "top_pkgs": [], "recent": []}
    try:
        with psycopg.connect(conninfo()) as conn:
            with conn.cursor() as cur:
                cur.execute("SELECT count(*) FROM code_embeddings")
                out["total"] = int(cur.fetchone()[0])
                cur.execute(
                    "SELECT kind, count(*) FROM code_embeddings "
                    "GROUP BY kind ORDER BY count(*) DESC"
                )
                out["by_kind"] = [(r[0], int(r[1])) for r in cur.fetchall()]
                cur.execute(
                    "SELECT COALESCE(NULLIF(pkg, ''), '(no pkg)') AS p, count(*) "
                    "FROM code_embeddings GROUP BY p ORDER BY count(*) DESC LIMIT 10"
                )
                out["top_pkgs"] = [(r[0], int(r[1])) for r in cur.fetchall()]
                cur.execute(
                    "SELECT node_id, kind, name, COALESCE(pkg, '') "
                    "FROM code_embeddings ORDER BY updated_at DESC LIMIT 10"
                )
                out["recent"] = [
                    {"node_id": r[0], "kind": r[1], "name": r[2] or "", "pkg": r[3]}
                    for r in cur.fetchall()
                ]
    except Exception:
        pass
    return out


@app.get("/", response_class=HTMLResponse)
async def ui_index(request: Request):
    stats = _index_stats()
    return templates.TemplateResponse(
        "index.html",
        {"request": request, "model": OLLAMA_MODEL, **stats},
    )


@app.get("/search", response_class=HTMLResponse)
async def ui_search(
    request: Request,
    query: str = Query(..., min_length=1),
    page: int = Query(1, ge=1, le=MAX_PAGE),
):
    qvec = await embed_one(query)
    qlit = vec_literal(qvec)
    offset = (page - 1) * PAGE_SIZE
    sql = """
        SELECT node_id, kind, name, COALESCE(pkg, ''), text,
               1 - (embedding <=> %s::vector) AS score
        FROM code_embeddings
        ORDER BY embedding <=> %s::vector
        LIMIT %s OFFSET %s
    """
    with psycopg.connect(conninfo()) as conn:
        with conn.cursor() as cur:
            cur.execute(sql, (qlit, qlit, PAGE_SIZE, offset))
            rows = cur.fetchall()
    results = [
        {
            "node_id": r[0], "kind": r[1], "name": r[2] or "",
            "pkg": r[3], "text": r[4] or "", "score": float(r[5]),
        }
        for r in rows
    ]
    return templates.TemplateResponse(
        "search.html",
        {
            "request": request,
            "query": query,
            "page": page,
            "page_size": PAGE_SIZE,
            "max_page": MAX_PAGE,
            "max_total": PAGE_SIZE * MAX_PAGE,
            "results": results,
        },
    )


@app.get("/symbol", response_class=HTMLResponse)
async def ui_symbol(request: Request, id: str = Query(..., min_length=1)):
    fetch_sql = """
        SELECT node_id, kind, name, COALESCE(pkg, ''), text,
               COALESCE(pos_file, ''), COALESCE(pos_line, 0), embedding
        FROM code_embeddings
        WHERE node_id = %s
    """
    related_sql = """
        SELECT node_id, kind, name, COALESCE(pkg, ''),
               1 - (embedding <=> %s::vector) AS score
        FROM code_embeddings
        WHERE node_id <> %s
        ORDER BY embedding <=> %s::vector
        LIMIT 10
    """
    with psycopg.connect(conninfo()) as conn:
        with conn.cursor() as cur:
            cur.execute(fetch_sql, (id,))
            row = cur.fetchone()
            if not row:
                raise HTTPException(status_code=404, detail=f"unknown node_id: {id}")
            # The embedding column comes back as a string like "[0.1,0.2,...]"
            # via psycopg's default decoder for the vector type — pass it back
            # to pgvector verbatim with a ::vector cast.
            sym = {
                "node_id": row[0], "kind": row[1], "name": row[2] or "",
                "pkg": row[3], "text": row[4] or "",
                "pos_file": row[5], "pos_line": row[6],
            }
            embedding_lit = row[7] if isinstance(row[7], str) else str(row[7])
            cur.execute(related_sql, (embedding_lit, id, embedding_lit))
            rrows = cur.fetchall()
    related = [
        {
            "node_id": r[0], "kind": r[1], "name": r[2] or "",
            "pkg": r[3], "score": float(r[4]),
        }
        for r in rrows
    ]
    return templates.TemplateResponse(
        "symbol.html", {"request": request, "sym": sym, "related": related},
    )


# ---- graph (Neo4j-backed) ------------------------------------------------

@app.get("/graph/status")
async def graph_status():
    return JSONResponse(state["graph"].status())


@app.post("/graph/refresh", dependencies=[Depends(require_auth)])
async def graph_refresh():
    return JSONResponse(state["graph"].refresh())


@app.get("/graph/flow")
async def graph_flow(
    id: str = Query(..., min_length=1),
    depth: int = Query(5, ge=1, le=10),
    edges: str = Query("CALLS,IMPLEMENTS,HAS_METHOD"),
    direction: str = Query("both", pattern="^(both|in|out)$"),
):
    kinds = [k for k in (e.strip() for e in edges.split(",")) if k]
    return JSONResponse(
        state["graph"].flow(
            node_id=id, depth=depth, edge_kinds=kinds, direction=direction,
        )
    )


@app.get("/flow", response_class=HTMLResponse)
async def ui_flow(
    request: Request,
    id: str = Query(..., min_length=1),
    depth: int = Query(5, ge=1, le=10),
):
    return templates.TemplateResponse(
        "flow.html",
        {
            "request": request,
            "node_id": id,
            "depth": depth,
            "browser_url": NEO4J_BROWSER_URL,
        },
    )

