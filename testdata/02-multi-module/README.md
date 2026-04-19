# Fixture 02-multi-module

Three separate Go modules in sibling directories. `modb` and `modc`
both consume `moda` via a local `replace` directive. This fixture proves
that cross-module `CALLS` edges resolve to the **real** Function node
in module A, not a dangling stub — which only works because
`internal/discover` runs before the indexer and builds the module
topology the indexer's link pass relies on.

## Layout

```
testdata/02-multi-module
├── moda/
│   ├── go.mod              module example.com/moda
│   └── core.go             ExportedFn(x int) int
├── modb/
│   ├── go.mod              require + replace ../moda
│   └── user.go             CallA → moda.ExportedFn
└── modc/
    ├── go.mod              require + replace ../moda
    └── caller.go           TripleAndAddOne → moda.ExportedFn (twice)
```

Topology:

```
modb ──replace──▶ moda ◀──replace── modc
                   │
                   └── ExportedFn
                         ▲       ▲
                         │       │
                      CallA   TripleAndAddOne
```

## Build + export for Neo4j

```
./codegraph.exe build --root ./testdata/02-multi-module \
  --persist ./testdata/02-multi-module/.codegraph/graph.jsonl \
  --output cypher --out-dir ./testdata/02-multi-module/graph-out
```

→ `graph: 14 nodes, 21 edges`. Cypher export lands in `graph-out/graph.cypher`.

## Expected query outputs

All commands assume `--persist ./testdata/02-multi-module/.codegraph/graph.jsonl`.

### `query list packages`

```
3 result(s)
  Package  pkg:.../moda   in=3  out=2
  Package  pkg:.../modb   in=1  out=3
  Package  pkg:.../modc   in=1  out=3
```

`moda` has `in=3` because both `modb` and `modc` IMPORTS it *and* the
moda module itself CONTAINS it.

### `query list upstream --symbol ExportedFn`

Both consumer modules land here:

```
2 result(s)
  Function  modb.CallA
  Function  modc.TripleAndAddOne
```

### `query list downstream --symbol TripleAndAddOne`

Resolves across the module boundary to the real Function node in
moda (not a dangling import stub):

```
1 result(s)
  Function  moda.ExportedFn
```

### `blast --symbol ExportedFn`

```
blast radius for ExportedFn (depth=10): 2 callers
  [1] modb.CallA                 (Function)
  [1] modc.TripleAndAddOne       (Function)
```

## Viewing in Neo4j UI

```
make build ROOT=./testdata/02-multi-module    # writes graph-out/graph.cypher
make neo4j                                    # starts container named codegraph-neo4j
make neo4j-import                             # streams graph.cypher in
```

Open http://localhost:7474 (neo4j / test12345) and run the queries below.

### What you should see

#### 1. Module topology

```cypher
MATCH p = (m:Module)-[:REPLACES|RESOLVES_TO*1..2]->(target)
RETURN p
```

Three `Module` nodes (moda, modb, modc). Two `REPLACES` edges (one from
modb, one from modc), both pointing at a `ModuleVersion` stub for
`moda@*`, which fans into a single `RESOLVES_TO` edge landing on the
real `moda@<version>` module. RESOLVES_TO dedupes on (from,to), so the
two replacers share one edge.

#### 2. Cross-module calls land on real nodes

```cypher
MATCH (caller)-[:CALLS]->(callee:Function {name:"ExportedFn"})
RETURN caller.id, callee.id
```

Two rows: `modb.CallA → moda.ExportedFn` and
`modc.TripleAndAddOne → moda.ExportedFn`. Both `callee.id` values are
identical — the indexer resolved the cross-module target to one node
rather than creating per-consumer stubs.

Run `./codegraph.exe broken --persist ...` to confirm there are zero
dangling CALLS edges; the REPLACES/RESOLVES_TO chain is what prevents
them.

#### 3. Blast radius visually

```cypher
MATCH (callee:Function {name:"ExportedFn"})<-[:CALLS]-(caller)
RETURN callee, caller
```

`ExportedFn` as a hub with two incoming edges from the consumer
modules — the visual analogue of `./codegraph.exe blast --symbol ExportedFn`.
