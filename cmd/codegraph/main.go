package main

import (
	"flag"
	"fmt"
	"os"
)

const usage = `codegraph — Go codebase → graph

Commands:
  build    Parse a Go module tree and persist a graph (.codegraph/graph.jsonl)
  blast    Print reverse-dependency (blast radius) of a symbol
  broken   Report dangling references, unsatisfied interfaces, orphaned exports
  query    List packages, interfaces, up/downstream callers, or neighbors
  schema   Print the node/edge vocabulary and sample Cypher queries
  embed    Ship symbol docs+source to the embeddings/pgvector service
  search   Semantic search over embedded symbols

Typical flow:
  codegraph schema                 # learn the vocabulary
  codegraph build --root .         # index once, persisted to .codegraph/graph.jsonl
  codegraph blast  --symbol Foo    # fast, reads persisted graph
  codegraph broken                 # fast, reads persisted graph

Run "codegraph <command> --help" for per-command flags.
`

func main() {
	if len(os.Args) < 2 {
		fmt.Fprint(os.Stderr, usage)
		os.Exit(2)
	}
	cmd, args := os.Args[1], os.Args[2:]
	switch cmd {
	case "build":
		os.Exit(runBuild(args))
	case "blast":
		os.Exit(runBlast(args))
	case "broken":
		os.Exit(runBroken(args))
	case "query":
		os.Exit(runQuery(args))
	case "schema":
		os.Exit(runSchema(args))
	case "embed":
		os.Exit(runEmbed(args))
	case "search":
		os.Exit(runSearch(args))
	case "-h", "--help", "help":
		fmt.Print(usage)
	default:
		fmt.Fprintf(os.Stderr, "unknown command: %s\n\n%s", cmd, usage)
		os.Exit(2)
	}
}

func newFlagSet(name, desc string) *flag.FlagSet {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.Usage = func() {
		fmt.Fprintf(fs.Output(), "codegraph %s — %s\n\nFlags:\n", name, desc)
		fs.PrintDefaults()
	}
	return fs
}
