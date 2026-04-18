# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

A Go binary (`codegraph`) that parses a Go codebase and builds a granular
property graph — modules, packages, files, types, methods, functions, imports,
calls, interface satisfaction, cross-module `replace` links. The graph is held
in memory and can be exported to CSV (neo4j-admin format), Cypher, or JSONL so
it can be loaded into Neo4j, Memgraph, or any graph DB the user runs.

The end use case: let an agent **discover** an unfamiliar codebase, compute the
**blast radius** of a proposed change up front, and report **broken links**
introduced by a change at the end — all backed by the AST rather than grep.

## Common commands

```
go test ./...                                                # full suite
go test ./internal/indexer/golang/... -run TestIndex02       # single fixture
go build -o codegraph ./cmd/codegraph
go run ./cmd/codegraph schema                                # learn the node/edge vocabulary
go run ./cmd/codegraph build --root .                        # writes .codegraph/graph.jsonl
go run ./cmd/codegraph blast --symbol Foo                    # reads persisted graph; add --rebuild to force
go run ./cmd/codegraph broken                                # reads persisted graph
go run ./cmd/codegraph build --root . --output cypher --out-dir ./graph-out  # also export for Neo4j
```

Every subcommand supports `--help`.

## Persistence model

`build` always writes a canonical self-describing JSONL file to
`.codegraph/graph.jsonl`. First line is the schema record (node kinds, edge
kinds, ID conventions, sample Cypher); subsequent lines are one node or edge
each. `blast` and `broken` read this file by default, so they're ~instant
after the first build. Pass `--rebuild` to force a fresh index. The separate
`--output` export (csv/cypher/jsonl in `./graph-out/`) is for loading into
an external graph DB — it is distinct from the canonical file.

## Architecture — the parts that need multiple files to understand

The pipeline has three stages, each in its own `internal/` package, connected
only through the shared `graph.Graph` interface:

1. **`internal/discover`** runs FIRST, before any AST work. It walks `--root`
   for every `go.mod`, parses require/replace directives via
   `golang.org/x/mod/modfile`, resolves local `replace` targets, optionally
   enriches with git commit/tag, and emits `Module` / `ModuleVersion` /
   `REQUIRES` / `REPLACES` / `RESOLVES_TO` nodes and edges. This is the step
   that makes multi-module repos index correctly — without it, cross-module
   references become duplicated external-stub nodes instead of landing on the
   right local symbol.

2. **`internal/indexer/golang`** runs per discovered module
   (`go/packages` does not tolerate a multi-module root). It uses two passes:
   - Pass 1: `packages.Load` each module's `./...` and record every loaded
     package in a global `importPath → loadedPkg` map keyed by import path.
     The map owns the module + version context each package came from.
   - Pass 2: walk each package's AST, emit nodes, and — crucially — resolve
     every `CALLS` / `IMPORTS` edge against the Pass 1 map. Cross-module
     calls (e.g., `modb.CallA` → `moda.ExportedFn`) get their `to` set to the
     real `Function` node in module A rather than a placeholder.
   Type resolution is driven by `go/types` via `TypesInfo.Uses` / `Defs`, and
   interface satisfaction via `types.Implements` scoped to each package.

3. **`internal/output`** and **`internal/query`** are pure consumers of the
   finished graph. Writers are `func(graph.Graph, io.Writer) error` so they
   test with `bytes.Buffer`. Queries (`Blast`, `Broken`) only use the public
   graph API.

## Node / edge schema

Defined once in `internal/graph/schema.go`. Node IDs are deterministic strings:

- Modules: `mod:<path>@<version>`
- Packages: `pkg:<modulePath>@<version>/<importPath>`
- Symbols: `<pkgID>.<Name>` (functions, types, constants, variables)
- Methods: `<pkgID>.<TypeName>.<MethodName>`
- Files: `file:<absolutePath>`
- Imports (external, unresolved): `imp:<pkgID>->importPath`

Deterministic IDs are load-bearing for the Pass 2 link step AND for
re-imports into Neo4j being idempotent via `MERGE`.

## Design constraints worth preserving

- **Testability first.** The Go indexer does not take a directory — it takes a
  `*discover.Result` (which you can fake). Writers take `io.Writer`, not
  filenames. Fixture tests live next to the code they exercise.
- **Stores are file-based in v1.** No live Bolt driver yet. Add one later
  behind a new `Store` interface; do not thread a DB dependency through the
  indexer.
- **Multi-module is not optional.** Any change in indexer code must keep the
  Pass 1 → Pass 2 pattern intact. If you consider skipping the global package
  map, the `02-multi-module` test will fail.
- **Discovery must run before indexing.** `Emit` writes the module topology
  the indexer's link pass depends on — don't reorder.

## Adding a new language later

Only add a sibling `internal/indexer/<lang>` package that reads the same
`*discover.Result` (or a language-specific equivalent) and writes to the same
`graph.Graph`. Keep the schema language-agnostic; add new `NodeKind`/`EdgeKind`
constants only when an existing one truly doesn't fit.
