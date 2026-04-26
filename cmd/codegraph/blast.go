package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/rish-0-0/code-graph-rag/internal/graph"
	"github.com/rish-0-0/code-graph-rag/internal/output"
	"github.com/rish-0-0/code-graph-rag/internal/query"
)

func runBlast(args []string) int {
	fs := newFlagSet("blast", "print reverse-dependency (blast radius) of a symbol")
	root := fs.String("root", ".", "root directory (only used when rebuilding)")
	symbol := fs.String("symbol", "", "fully-qualified symbol or suffix match (e.g. pkg.Func)")
	depth := fs.Int("depth", 10, "max BFS depth (0 = unlimited)")
	asJSON := fs.Bool("json", false, "emit JSON instead of text")
	persistPath := fs.String("persist", output.CanonicalPath, "canonical graph file to read")
	rebuild := fs.Bool("rebuild", false, "re-run build even if a persisted graph exists")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if *symbol == "" {
		fmt.Fprintln(os.Stderr, "blast: --symbol is required")
		return 2
	}
	g, err := loadOrBuild(*persistPath, *root, *rebuild)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	result := query.Blast(g, *symbol, *depth)
	if *asJSON {
		return emitJSON(result)
	}
	fmt.Printf("blast radius for %s (depth=%d): %d callers\n", *symbol, *depth, len(result.Callers))
	for _, c := range result.Callers {
		fmt.Printf("  [%d] %s  (%s)\n", c.Depth, c.ID, c.Kind)
	}
	return 0
}

// loadOrBuild returns a graph from the persisted canonical file if present,
// or runs a fresh build if not (or when forced). Keeps blast/broken fast:
// one build per session, not per invocation.
func loadOrBuild(persistPath, root string, forceRebuild bool) (graph.Graph, error) {
	if !forceRebuild {
		if _, err := os.Stat(persistPath); err == nil {
			g, err := output.Load(persistPath)
			if err != nil {
				return nil, fmt.Errorf("load %s: %w", persistPath, err)
			}
			fmt.Fprintf(os.Stderr, "loaded %d nodes, %d edges from %s\n", len(g.Nodes()), len(g.Edges()), persistPath)
			return g, nil
		} else if !errors.Is(err, os.ErrNotExist) {
			return nil, err
		}
		fmt.Fprintf(os.Stderr, "no persisted graph at %s — running fresh build\n", persistPath)
	}
	g, err := buildGraph(root, "./...", "", true, true, nil, nil, nil)
	if err != nil {
		return nil, err
	}
	if err := output.Persist(g, persistPath); err != nil {
		fmt.Fprintf(os.Stderr, "warn: could not persist: %v\n", err)
	}
	return g, nil
}

func emitJSON(v any) int {
	enc := json.NewEncoder(os.Stdout)
	enc.SetIndent("", "  ")
	if err := enc.Encode(v); err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	return 0
}
