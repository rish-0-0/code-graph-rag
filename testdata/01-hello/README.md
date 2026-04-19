# Fixture 01-hello

A single-module fixture exercising interfaces, multiple packages,
deeply nested call chains, and sibling nodes.

## Layout

```
testdata/01-hello
├── go.mod                  module example.com/hello
├── main.go                 Run (entry point) + Goodbye + Version const
├── greeter.go              Greeter, Talker (interface), NewGreeter, DefaultName
├── greet/
│   └── formatter.go        Formatter, Renderer interfaces; FancyFormatter,
│                           PlainFormatter, DefaultRenderer; deep chain
│                           FormatChain → applyFormat → finalize → polish
└── util/
    └── strings.go          Shout, Repeat, Reverse
```

The call graph at a glance:

```
hello.Run
  ├── hello.NewGreeter
  ├── hello.Talker.Say          (interface dispatch → hello.Greeter.Say)
  ├── util.Shout
  └── greet.FormatChain
        └── greet.applyFormat
              ├── greet.Formatter.Format   (interface dispatch)
              └── greet.finalize
                    └── greet.polish
                          └── strings.ToUpper
```

## Build the graph

```
./codegraph.exe build --root ./testdata/01-hello \
  --persist ./testdata/01-hello/.codegraph/graph.jsonl
```

→ `graph: 41 nodes, 85 edges`.

All commands below assume `--persist ./testdata/01-hello/.codegraph/graph.jsonl`.

## Expected outputs

### `query list packages`

```
3 result(s)
  Package  pkg:.../hello/greet       in=2  out=16
  Package  pkg:.../hello             in=1  out=14
  Package  pkg:.../hello/util        in=2  out=5
```

### `query list interfaces --symbol Greeter`

Dispatch on `Type` → interfaces implemented:

```
1 result(s)
  Interface  pkg:.../hello.Talker
```

### `query list interfaces --symbol Talker`

Dispatch on `Interface` → methods declared:

```
1 result(s)
  Method  pkg:.../hello.Talker.Say
```

### `query list interfaces --symbol Formatter`

```
1 result(s)
  Method  pkg:.../hello/greet.Formatter.Format
```

### `query list upstream --symbol FormatChain`

One direct caller (`Run`):

```
1 result(s)
  Function  pkg:.../hello.Run
```

### `query list downstream --symbol FormatChain`

One direct callee (`applyFormat`). Deeper callees surface via `blast`.

```
1 result(s)
  Function  pkg:.../hello/greet.applyFormat
```

### `query list neighbors --symbol Run`

Siblings of `Run` in package `hello` (+ enclosing files):

```
10 result(s)
  File       main.go
  File       greeter.go
  Constant   DefaultName
  Constant   Version
  Function   Goodbye
  Function   NewGreeter
  Type       Greeter
  Interface  Talker
  Method     Greeter.Say
  Method     Talker.Say
```

### `blast --symbol polish` — deep chain verification

Reverse BFS over CALLS/REFERENCES (+ IMPLEMENTS expansion):

```
blast radius for polish (depth=10): 4 callers
  [1] greet.finalize     (Function)
  [2] greet.applyFormat  (Function)
  [3] greet.FormatChain  (Function)
  [4] hello.Run          (Function)
```

Depth is the BFS distance, so `polish` sits 4 hops below `Run`.

### `blast --symbol Greeter.Say`

Interface-implements expansion means callers that dispatch through
`Talker.Say` are surfaced even though they don't reference `Greeter.Say`
directly:

```
blast radius for Greeter.Say (depth=10): 1 callers
  [1] hello.Run  (Function)
```
