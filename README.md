# codegraph

A Go AST → graph indexer. Produces a highly granular graph (modules, packages,
types, methods, calls, interface satisfaction, cross-module links via `replace`
directives) that can be consumed in-memory or exported to Neo4j / Memgraph /
any Cypher-compatible DB.

Designed as a Claude Code skill: `--help` on every subcommand is the contract.

## Install

```
go install github.com/rish-0-0/code-graph-rag/cmd/codegraph@latest
```

From source:

```
git clone https://github.com/rish-0-0/code-graph-rag
cd code-graph-rag
go build -o codegraph ./cmd/codegraph
```

Requires Go 1.21+.

## Contribute

```
go test ./...
```

Tests use the fixtures under `testdata/`:

- `testdata/01-hello` — single-module baseline (types, methods, interface
  satisfaction, calls, constants).
- `testdata/02-multi-module` — two modules + a `replace` directive;
  exercises cross-module call resolution.

When you add a schema feature, add (or extend) a fixture that exercises it and
a test under `internal/indexer/golang/` that asserts the expected nodes/edges.
Keep fixtures minimal — one file per concept where possible.
