"""Neo4j-backed loader and traversal for the codegraph property graph.

The Go CLI writes a self-describing JSONL file (`.codegraph/graph.jsonl`) on
every `codegraph build`. This module:

1. Streams that JSONL into Neo4j on startup or on demand (`/graph/refresh`),
   wiping the database first so removed symbols don't haunt later queries.
2. Exposes a single BFS-style "flow" query that returns a capped subgraph
   (default 500 nodes) around a given node ID, using APOC's
   `apoc.path.subgraphAll` so the cap is enforced inside Neo4j.

We use APOC's `apoc.create.node` / `apoc.create.relationship` because the
Cypher language doesn't allow dynamic labels / relationship types in pure
MERGE statements, and we want to preserve the codegraph kind (e.g. Function,
Method, Type) as the Neo4j node label so they can be styled and filtered.
"""
from __future__ import annotations

import json
import os
import time
from dataclasses import dataclass, field
from datetime import datetime, timezone
from typing import Any, Iterable

from neo4j import GraphDatabase, Driver


BATCH = 5000  # rows per UNWIND


@dataclass
class LoadStats:
    nodes: int = 0
    edges: int = 0
    duration_s: float = 0.0
    path: str = ""
    loaded_at: str | None = None
    error: str | None = None
    extra: dict[str, Any] = field(default_factory=dict)

    def to_dict(self) -> dict[str, Any]:
        return {
            "loaded": self.loaded_at is not None and self.error is None,
            "nodes": self.nodes,
            "edges": self.edges,
            "duration_s": round(self.duration_s, 2),
            "path": self.path,
            "loaded_at": self.loaded_at,
            "error": self.error,
            **self.extra,
        }


class GraphStore:
    """Owns the Neo4j driver + ingest/query helpers."""

    def __init__(
        self,
        uri: str,
        user: str,
        password: str,
        graph_path: str,
        browser_url: str = "",
    ) -> None:
        self.uri = uri
        self.user = user
        self.password = password
        self.graph_path = graph_path
        self.browser_url = browser_url
        self._driver: Driver | None = None
        self._stats = LoadStats(path=graph_path)

    # ---- driver lifecycle --------------------------------------------------

    @property
    def driver(self) -> Driver:
        if self._driver is None:
            self._driver = GraphDatabase.driver(self.uri, auth=(self.user, self.password))
        return self._driver

    def close(self) -> None:
        if self._driver is not None:
            self._driver.close()
            self._driver = None

    # ---- public API --------------------------------------------------------

    def status(self) -> dict[str, Any]:
        out = self._stats.to_dict()
        out["browser_url"] = self.browser_url
        return out

    def refresh(self) -> dict[str, Any]:
        """Wipe Neo4j and re-load CODEGRAPH_GRAPH_PATH. Synchronous — for
        large graphs the caller should expect this to take seconds."""
        start = time.time()
        try:
            if not os.path.exists(self.graph_path):
                raise FileNotFoundError(
                    f"graph file not found at {self.graph_path}. "
                    f"Run `codegraph build` so it produces .codegraph/graph.jsonl, "
                    f"or set CODEGRAPH_GRAPH_PATH."
                )
            n_nodes, n_edges = self._reload_into_neo4j(self.graph_path)
            self._stats = LoadStats(
                nodes=n_nodes,
                edges=n_edges,
                duration_s=time.time() - start,
                path=self.graph_path,
                loaded_at=datetime.now(timezone.utc).isoformat(),
                error=None,
            )
        except Exception as e:
            self._stats = LoadStats(
                duration_s=time.time() - start,
                path=self.graph_path,
                error=f"{type(e).__name__}: {e}",
            )
        return self.status()

    def flow(
        self,
        node_id: str,
        depth: int = 5,
        edge_kinds: Iterable[str] = ("CALLS", "IMPLEMENTS", "HAS_METHOD"),
        direction: str = "both",
        node_limit: int = 500,
    ) -> dict[str, Any]:
        """Return a capped subgraph around `node_id` for vis-network."""
        depth = max(1, min(int(depth), 10))
        kinds = [k.strip().upper() for k in edge_kinds if k and k.strip()]
        if not kinds:
            kinds = ["CALLS", "IMPLEMENTS", "HAS_METHOD"]
        # APOC relationshipFilter syntax:
        #   "TYPE>"  outgoing only,  "<TYPE"  incoming only,  "TYPE"  both
        suffix = {"both": "", "out": ">", "in": "<"}.get(direction, "")
        rel_filter = "|".join(f"{k}{suffix}" for k in kinds) if suffix else "|".join(kinds)

        cypher = """
        MATCH (start {id: $id})
        CALL apoc.path.subgraphAll(start, {
            maxLevel: $depth,
            relationshipFilter: $rel_filter,
            limit: $limit
        })
        YIELD nodes, relationships
        RETURN nodes, relationships
        """
        with self.driver.session() as sess:
            rec = sess.run(
                cypher,
                id=node_id,
                depth=depth,
                rel_filter=rel_filter,
                limit=node_limit,
            ).single()
            if rec is None:
                return {
                    "nodes": [],
                    "edges": [],
                    "truncated": False,
                    "error": f"node not found: {node_id}",
                }
            ns, rs = rec["nodes"], rec["relationships"]
            return {
                "nodes": [_node_to_vis(n, center_id=node_id) for n in ns],
                "edges": [_edge_to_vis(r) for r in rs],
                "truncated": len(ns) >= node_limit,
                "center": node_id,
                "depth": depth,
                "edge_kinds": kinds,
                "direction": direction,
            }

    # ---- internal: bulk load -----------------------------------------------

    def _reload_into_neo4j(self, path: str) -> tuple[int, int]:
        nodes_buf: list[dict[str, Any]] = []
        edges_buf: list[dict[str, Any]] = []
        n_nodes = 0
        n_edges = 0

        with self.driver.session() as sess:
            sess.run("MATCH (n) DETACH DELETE n")
            sess.run(
                "CREATE INDEX codegraph_id IF NOT EXISTS "
                "FOR (n:CodegraphNode) ON (n.id)"
            )
            with open(path, "r", encoding="utf-8") as f:
                for line in f:
                    line = line.strip()
                    if not line:
                        continue
                    try:
                        rec = json.loads(line)
                    except json.JSONDecodeError:
                        continue
                    rtype = rec.get("type")
                    if rtype == "node" and rec.get("node"):
                        nodes_buf.append(_node_record(rec["node"]))
                        if len(nodes_buf) >= BATCH:
                            n_nodes += _flush_nodes(sess, nodes_buf)
                            nodes_buf.clear()
                    elif rtype == "edge" and rec.get("edge"):
                        edges_buf.append(_edge_record(rec["edge"]))
                        if len(edges_buf) >= BATCH:
                            n_edges += _flush_edges(sess, edges_buf)
                            edges_buf.clear()
            if nodes_buf:
                n_nodes += _flush_nodes(sess, nodes_buf)
            if edges_buf:
                n_edges += _flush_edges(sess, edges_buf)
        return n_nodes, n_edges


# ---- helpers ---------------------------------------------------------------

def _node_record(n: dict[str, Any]) -> dict[str, Any]:
    props = dict(n.get("props") or {})
    pos = n.get("pos") or {}
    if pos.get("file"):
        props.setdefault("pos_file", pos["file"])
    if pos.get("line"):
        props.setdefault("pos_line", pos["line"])
    if pos.get("column"):
        props.setdefault("pos_column", pos["column"])
    # Neo4j can't store nested dicts/lists-of-dicts; flatten anything weird.
    props = {k: _scalar(v) for k, v in props.items()}
    return {
        "id": n.get("id"),
        "kind": n.get("kind") or "Unknown",
        "name": n.get("name") or "",
        "props": props,
    }


def _edge_record(e: dict[str, Any]) -> dict[str, Any]:
    props = dict(e.get("props") or {})
    pos = e.get("pos") or {}
    if pos.get("file"):
        props.setdefault("pos_file", pos["file"])
    if pos.get("line"):
        props.setdefault("pos_line", pos["line"])
    props = {k: _scalar(v) for k, v in props.items()}
    return {
        "id": e.get("id"),
        "kind": e.get("kind") or "REL",
        "from": e.get("from"),
        "to": e.get("to"),
        "props": props,
    }


def _scalar(v: Any) -> Any:
    """Coerce non-primitive values to strings — Neo4j properties must be
    primitives or arrays of primitives."""
    if v is None or isinstance(v, (bool, int, float, str)):
        return v
    if isinstance(v, (list, tuple)):
        return [x if isinstance(x, (bool, int, float, str)) else json.dumps(x) for x in v]
    return json.dumps(v)


_NODES_QUERY = """
UNWIND $batch AS row
CALL apoc.create.node(['CodegraphNode', row.kind],
    apoc.map.merge({id: row.id, name: row.name, kind: row.kind}, row.props))
YIELD node
RETURN count(node) AS c
"""

_EDGES_QUERY = """
UNWIND $batch AS row
MATCH (a:CodegraphNode {id: row.from})
MATCH (b:CodegraphNode {id: row.to})
CALL apoc.create.relationship(a, row.kind,
    apoc.map.merge({id: row.id}, row.props), b) YIELD rel
RETURN count(rel) AS c
"""


def _flush_nodes(session, batch: list[dict]) -> int:
    rec = session.run(_NODES_QUERY, batch=batch).single()
    return int(rec["c"]) if rec else 0


def _flush_edges(session, batch: list[dict]) -> int:
    # Edges to nodes that don't exist yet are silently skipped (MATCH fails).
    # That can happen if the build didn't emit a node for an external import.
    rec = session.run(_EDGES_QUERY, batch=batch).single()
    return int(rec["c"]) if rec else 0


def _node_to_vis(neo_node, center_id: str = "") -> dict[str, Any]:
    """Convert a neo4j.Node into the dict shape vis-network expects."""
    p = dict(neo_node)
    nid = p.get("id")
    label = p.get("name") or nid or ""
    if len(label) > 40:
        label = label[:37] + "…"
    kind = p.get("kind") or (next(iter(neo_node.labels - {"CodegraphNode"}), "Node"))
    out = {
        "id": nid,
        "label": label,
        "group": kind,
        "title": f"{kind}: {p.get('name') or ''}\n{nid}",
    }
    if nid == center_id:
        out["borderWidth"] = 3
        out["shape"] = "dot"
        out["size"] = 22
    return out


def _edge_to_vis(rel) -> dict[str, Any]:
    p = dict(rel)
    return {
        "id": p.get("id") or rel.element_id,
        "from": rel.start_node["id"],
        "to": rel.end_node["id"],
        "label": rel.type,
        "arrows": "to",
    }
