package main

import (
	"encoding/json"
	"fmt"
	"os"

	"github.com/rishabhanand42/code-graph-rag/internal/graph"
)

func runSchema(args []string) int {
	fs := newFlagSet("schema", "print the graph schema (node/edge kinds, ID conventions, sample Cypher)")
	asJSON := fs.Bool("json", false, "emit JSON instead of human-readable text")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	sc := graph.Describe()
	if *asJSON {
		enc := json.NewEncoder(os.Stdout)
		enc.SetIndent("", "  ")
		_ = enc.Encode(sc)
		return 0
	}
	fmt.Printf("codegraph schema v%s\n\n", sc.Version)
	fmt.Println("Node kinds:")
	for _, k := range sc.NodeKinds {
		fmt.Printf("  %-16s %s\n", k.Name, k.Doc)
	}
	fmt.Println("\nEdge kinds:")
	for _, k := range sc.EdgeKinds {
		fmt.Printf("  %-22s %s\n", k.Name, k.Doc)
	}
	fmt.Println("\nID conventions:")
	for k, v := range sc.IDConventions {
		fmt.Printf("  %-14s %s\n", k, v)
	}
	fmt.Println("\nSample Cypher queries:")
	for _, q := range sc.SampleQueries {
		fmt.Printf("  # %s\n  %s\n\n", q.Name, q.Cypher)
	}
	return 0
}
