---
name: codegraph
description: Use this skill when working in a Go codebase to (a) discover unfamiliar code, (b) compute the blast radius of a proposed change before editing, or (c) verify nothing is broken after a refactor. Triggers on requests like "what does X call", "who uses Y", "what will I break if I change Z", "list all interfaces in this repo", "find dangling references", or any Go refactor/rename/extract task. Skips for non-Go code or repos without a go.mod.
---

# codegraph

`codegraph` is a Go binary that parses a Go codebase into a property graph
(modules, packages, files, types, methods, functions, imports, calls,
interface satisfaction, cross-module replace links) and answers structural
queries against it. The graph is persisted to `.codegraph/graph.jsonl` so
queries are millisecond-fast after the first build.

Use it instead of grep when the question is structural ("who calls",
"what implements", "what's in this package", "what breaks if I change X").
Grep is still right for intent-based questions ("where does payment
retry happen") — codegraph has no embeddings.

## Prerequisite

The `codegraph` binary must be on `$PATH`. If `which codegraph` (or
`where codegraph` on Windows) fails, build it from
`github.com/rish-0-0/code-graph-rag` first:

```
go build -o codegraph ./cmd/codegraph
```

## Multi-repo / multi-module setup

If `--root` points at a folder containing **multiple sibling clones**
(each with its own `go.mod`), Go's `packages.Load` will resolve
cross-repo imports to `$GOMODCACHE` versions, not the local clones —
cross-module CALLS will be missing or wrong. Before running `build`,
ensure one of these is true:

1. **A `go.work` at the parent** listing every Go module to index:
   ```
   go work init ./moduleA ./moduleB ./moduleC
   ```
   Skip non-Go subfolders (bash scripts, docs, etc.) — `go work init`
   fails on directories without a `go.mod`. Use:
   ```
   go work init $(for d in */; do [ -f "$d/go.mod" ] && echo "./$d"; done)
   ```
2. **`replace` directives** in each `go.mod` pointing at sibling
   clones (worse — pollutes per-repo files; use only if `go.work` is
   unavailable).

Non-Go sibling folders are otherwise harmless — `discover` only emits
nodes for directories containing `go.mod`, so a `./scripts/` folder of
bash is silently ignored.

To prune the walk explicitly (e.g. skip a giant generated module, or
focus on just a couple of repos), `build` accepts:

```
codegraph build --root . --ignore scripts,docs,apps/legacy
codegraph build --root . --only   moduleA,moduleB
```

Each entry is a directory name (matched against the leaf segment) or a
root-relative path like `apps/legacy`.

If unsure whether the workspace is wired up, run:
```
codegraph broken --json | jq '.dangling | length'
```
A high count where the `to` IDs point at `github.com/<org>/...` modules
that exist locally is the smoking gun for a missing `go.work`.

## Always: build once per session

Before the first query in a session, ensure the graph is fresh. Cheap
heuristic: if `.codegraph/graph.jsonl` is missing **or** any `.go` file
has been modified since the file's mtime, run:

```
codegraph build --root .
```

Subsequent queries reuse the persisted graph (~ms). Pass `--rebuild` to
force a re-index after large edits.

## Discovery flow — exploring an unfamiliar repo

Walk top-down. Always pass `--json` so output is structured.

```
codegraph query list packages --sort-by in --order desc --json
```

High in-degree packages are the widely-used ones — start there. Then
for any package or symbol of interest:

```
codegraph query list interfaces --symbol <Type-or-Interface> --json
codegraph query list neighbors  --symbol <Symbol> --json
codegraph query list downstream --symbol <Function> --json
codegraph query list upstream   --symbol <Function> --json
```

`--symbol` accepts either a fully-qualified node ID or a suffix match
like `pkg.Func` or `Type.Method`.

## Pre-edit flow — blast radius before changing a symbol

Before editing any function, method, or type the user names:

```
codegraph blast --symbol <Symbol> --json
```

Read the caller list. If the blast is small (< ~10 callers), proceed.
If it's large, **surface the count to the user** before editing — they
may want to scope the change differently. The result includes interface
implementations: changing a concrete method also surfaces callers
dispatching through interfaces it satisfies.

Default depth is 10. Override with `--depth N` (`--depth 0` = unlimited).

## Post-edit flow — verify nothing dangles

After any refactor, rename, or symbol removal:

```
codegraph build --rebuild   # re-index after edits
codegraph broken --json
```

Investigate every dangling CALLS edge — they indicate references to
symbols that no longer exist. Unresolved imports are usually external
deps and can be ignored unless the user asked about them.

## Subcommand reference

| Command | Purpose |
|---|---|
| `build` | Index the repo; writes `.codegraph/graph.jsonl` |
| `blast --symbol S` | Reverse-reachable callers of S (transitive, depth-limited) |
| `broken` | Dangling CALLS edges + unresolved imports |
| `query list packages` | Every Package node |
| `query list interfaces --symbol S` | If S is a Type: interfaces it implements. If Interface: methods declared on it |
| `query list upstream --symbol S` | Direct callers of S (one hop) |
| `query list downstream --symbol S` | Direct callees of S (one hop) |
| `query list neighbors --symbol S` | Siblings of S — share a CONTAINS parent (package, file, or type) |
| `schema` | Dump the node/edge vocabulary + sample Cypher |

All `query list` commands accept `--sort-by in|out|name`, `--order asc|desc`, `--limit N`, and `--json`.

## When NOT to use codegraph

- Non-Go codebases (no other language indexer exists yet).
- Questions about *intent* or *behavior* — "where does retry logic live"
  is a grep/read job, not a graph job.
- Build/test/lint failures — codegraph indexes structure, not errors.
- Files outside the indexed module — anything in `vendor/` or behind an
  unmatched build tag may not appear in the graph.

## Output handling

Always pass `--json` and parse the result. Text output is human-formatted
and changes layout; JSON is stable. The stderr line `loaded N nodes, M
edges from .codegraph/graph.jsonl` is informational and should be
ignored when piping to `jq`.

## Troubleshooting

- **"symbol not found"** — try a longer suffix (`Pkg.Type.Method`
  instead of just `Method`) or a fully-qualified ID. Symbol resolution
  is suffix-match against node IDs.
- **`broken` reports many dangling CALLS** after editing — re-run
  `codegraph build --rebuild` first; the persisted graph is from before
  your edits.
- **Empty results on a real repo** — check the build succeeded for that
  module. `go/packages` errors print to stderr during build.
