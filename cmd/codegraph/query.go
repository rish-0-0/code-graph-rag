package main

import (
	"flag"
	"fmt"
	"os"

	"github.com/rish-0-0/code-graph-rag/internal/graph"
	"github.com/rish-0-0/code-graph-rag/internal/output"
	"github.com/rish-0-0/code-graph-rag/internal/query"
)

const queryUsage = `codegraph query — structural discovery over the persisted graph

Subcommands:
  list packages                    List every Package node
  list interfaces  --symbol S      If S is a Type: interfaces it implements
                                   If S is an Interface: methods declared on it
  list upstream    --symbol S      Direct callers of S (one hop)
  list downstream  --symbol S      Direct callees of S (one hop)
  list neighbors   --symbol S      Siblings of S (share a CONTAINS parent)

All listing commands accept:
  --sort-by in|out|name   (default: name)
  --order   asc|desc      (default: asc)
  --limit   N             (default: 0 = unlimited)
  --json                  (emit JSON instead of text)
  --persist PATH          (default: .codegraph/graph.jsonl)
  --rebuild               (force re-index before querying)
  --root DIR              (only used when rebuilding)
`

func runQuery(args []string) int {
	if len(args) < 1 {
		fmt.Fprint(os.Stderr, queryUsage)
		return 2
	}
	if args[0] == "-h" || args[0] == "--help" || args[0] == "help" {
		fmt.Print(queryUsage)
		return 0
	}
	if args[0] != "list" {
		fmt.Fprintf(os.Stderr, "unknown query subcommand: %s\n\n%s", args[0], queryUsage)
		return 2
	}
	if len(args) < 2 {
		fmt.Fprint(os.Stderr, queryUsage)
		return 2
	}
	noun, rest := args[1], args[2:]
	return runList(noun, rest)
}

type listFlags struct {
	symbol      *string
	sortBy      *string
	order       *string
	limit       *int
	asJSON      *bool
	persistPath *string
	rebuild     *bool
	root        *string
}

func registerListFlags(fs *flag.FlagSet, needSymbol bool) *listFlags {
	lf := &listFlags{
		sortBy:      fs.String("sort-by", "name", "sort key: in|out|name"),
		order:       fs.String("order", "asc", "sort order: asc|desc"),
		limit:       fs.Int("limit", 0, "max rows (0 = unlimited)"),
		asJSON:      fs.Bool("json", false, "emit JSON instead of text"),
		persistPath: fs.String("persist", output.CanonicalPath, "canonical graph file to read"),
		rebuild:     fs.Bool("rebuild", false, "re-run build even if a persisted graph exists"),
		root:        fs.String("root", ".", "root directory (only used when rebuilding)"),
	}
	if needSymbol {
		lf.symbol = fs.String("symbol", "", "fully-qualified symbol or suffix match (required)")
	}
	return lf
}

func runList(noun string, args []string) int {
	fs := newFlagSet("query list "+noun, "list "+noun)
	needSymbol := noun != "packages"
	lf := registerListFlags(fs, needSymbol)
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if needSymbol && (lf.symbol == nil || *lf.symbol == "") {
		fmt.Fprintf(os.Stderr, "query list %s: --symbol is required\n", noun)
		return 2
	}
	g, err := loadOrBuild(*lf.persistPath, *lf.root, *lf.rebuild)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	items, err := runListNoun(g, noun, lf)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}

	query.SortItems(items, *lf.sortBy, *lf.order)
	if *lf.limit > 0 && len(items) > *lf.limit {
		items = items[:*lf.limit]
	}

	if *lf.asJSON {
		return emitJSON(items)
	}
	return printListText(noun, lf, items)
}

func runListNoun(g graph.Graph, noun string, lf *listFlags) ([]query.ListItem, error) {
	switch noun {
	case "packages":
		return query.ListPackages(g), nil
	case "interfaces":
		return query.ListInterfaces(g, *lf.symbol)
	case "upstream":
		return query.ListUpstream(g, *lf.symbol)
	case "downstream":
		return query.ListDownstream(g, *lf.symbol)
	case "neighbors":
		return query.ListNeighbors(g, *lf.symbol)
	default:
		return nil, fmt.Errorf("unknown list noun: %s (want packages|interfaces|upstream|downstream|neighbors)", noun)
	}
}

func printListText(noun string, lf *listFlags, items []query.ListItem) int {
	header := fmt.Sprintf("query list %s", noun)
	if lf.symbol != nil && *lf.symbol != "" {
		header += " --symbol " + *lf.symbol
	}
	fmt.Printf("%s: %d result(s)\n", header, len(items))
	for _, it := range items {
		fmt.Printf("  %-9s  in=%-3d out=%-3d  %s\n", it.Kind, it.InDegree, it.OutDegree, it.ID)
	}
	return 0
}
