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

## Indexing multiple repos at once

Common setup: a parent folder containing several local clones from the
same GitHub org, each its own Go module that `require`s the others.

```
~/work/<org>/
├── moduleA/   (require github.com/<org>/moduleB, moduleC)
├── moduleB/
├── moduleC/
└── scripts/   (bash, no go.mod)
```

If you just `cd ~/work/<org> && codegraph build --root .`, Go's
`packages.Load` will resolve `github.com/<org>/moduleB` to the version
in `$GOMODCACHE` rather than the local `./moduleB/` clone — cross-repo
CALLS will land on cached code (or be dropped). Each clone gets indexed
in isolation.

**Fix: drop a `go.work` at the parent before building.**

```
cd ~/work/<org>
go work init ./moduleA ./moduleB ./moduleC
codegraph build --root .
```

`codegraph` detects the `go.work`, emits a `Workspace` node with
`WORKSPACES` edges to each module, and now Pass 2 of the indexer
resolves cross-module CALLS to the **real** Function nodes in the
sibling clones.

Non-Go subfolders (like `./scripts/` above) are silently ignored —
`discover` only recognizes directories containing a `go.mod`. But
`go work init ./*/` will fail on them; enumerate Go modules explicitly,
or filter:

```
go work init $(for d in */; do [ -f "$d/go.mod" ] && echo "./$d"; done)
```

### Pruning the walk

```
codegraph build --root . --ignore scripts,docs,apps/legacy
codegraph build --root . --only   moduleA,moduleB
```

`--ignore` skips the named directories during the walk (in addition to
the always-skipped `.git`, `vendor`, `node_modules`). `--only` keeps
only modules whose directory matches one of the entries. Each value is
a directory basename (`scripts`) or a root-relative path
(`apps/legacy`).

### Smoke-testing your workspace setup

If you suspect the workspace isn't wired up, this is the smoking gun:

```
codegraph broken --json | jq '.dangling | length'
```

A high count where `to` IDs reference local org modules means
cross-module resolution is failing — re-check `go.work`.

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
