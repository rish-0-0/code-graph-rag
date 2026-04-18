package main

import (
	"fmt"
	"os"

	"github.com/rish-0-0/code-graph-rag/internal/output"
	"github.com/rish-0-0/code-graph-rag/internal/query"
)

func runBroken(args []string) int {
	fs := newFlagSet("broken", "report dangling refs, unresolved imports")
	root := fs.String("root", ".", "root directory (only used when rebuilding)")
	asJSON := fs.Bool("json", false, "emit JSON instead of text")
	persistPath := fs.String("persist", output.CanonicalPath, "canonical graph file to read")
	rebuild := fs.Bool("rebuild", false, "re-run build even if a persisted graph exists")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	g, err := loadOrBuild(*persistPath, *root, *rebuild)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	report := query.Broken(g)
	if *asJSON {
		return emitJSON(report)
	}
	fmt.Printf("dangling references: %d\n", len(report.Dangling))
	for _, d := range report.Dangling {
		fmt.Printf("  %s -> %s (%s)\n", d.From, d.To, d.Kind)
	}
	fmt.Printf("unresolved imports: %d\n", len(report.UnresolvedImports))
	for _, u := range report.UnresolvedImports {
		fmt.Printf("  %s\n", u)
	}
	return 0
}
