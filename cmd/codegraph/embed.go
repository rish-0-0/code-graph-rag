package main

import (
	"fmt"
	"os"
	"time"

	"github.com/rish-0-0/code-graph-rag/internal/embed"
	"github.com/rish-0-0/code-graph-rag/internal/output"
)

func runEmbed(args []string) int {
	fs := newFlagSet("embed", "extract embeddable text from the graph and ship it to the embeddings service")
	persistPath := fs.String("persist", output.CanonicalPath, "canonical graph file to read")
	batchSize := fs.Int("batch", 64, "items per /embed POST")
	snippetLines := fs.Int("snippet-lines", 12, "max source lines to include per symbol")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	c, err := embed.NewClientFromEnv()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		fmt.Fprintln(os.Stderr, "set EMBEDDINGS_API_ENDPOINT (and optionally EMBEDDINGS_API_AUTH).")
		return 1
	}

	g, err := output.Load(*persistPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "load %s: %v\n(run `codegraph build` first)\n", *persistPath, err)
		return 1
	}

	items := embed.BuildItems(g, *snippetLines)
	total := len(items)
	if total == 0 {
		fmt.Fprintln(os.Stderr, "no embeddable nodes found")
		return 0
	}
	fmt.Fprintf(os.Stderr, "embedding %d items in batches of %d → %s\n", total, *batchSize, c.Endpoint)
	start := time.Now()
	upserted := 0
	for i := 0; i < total; i += *batchSize {
		end := i + *batchSize
		if end > total {
			end = total
		}
		n, err := c.UpsertEmbed(items[i:end])
		if err != nil {
			fmt.Fprintf(os.Stderr, "embed batch %d-%d: %v\n", i, end, err)
			return 1
		}
		upserted += n
		fmt.Fprintf(os.Stderr, "[%8s] upsert %d/%d (+%d)\n",
			time.Since(start).Truncate(time.Millisecond), end, total, n)
	}
	fmt.Fprintf(os.Stderr, "done: %d items upserted in %s\n", upserted, time.Since(start).Truncate(time.Millisecond))
	return 0
}

func runSearch(args []string) int {
	fs := newFlagSet("search", "semantic search across embedded symbols")
	k := fs.Int("k", 10, "number of results")
	if err := fs.Parse(args); err != nil {
		return 2
	}
	if fs.NArg() == 0 {
		fmt.Fprintln(os.Stderr, "usage: codegraph search [--k N] <query...>")
		return 2
	}
	query := ""
	for i, a := range fs.Args() {
		if i > 0 {
			query += " "
		}
		query += a
	}

	c, err := embed.NewClientFromEnv()
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	results, err := c.Search(query, *k)
	if err != nil {
		fmt.Fprintf(os.Stderr, "search: %v\n", err)
		return 1
	}
	for i, r := range results {
		fmt.Printf("%d. [%.4f] %s %s\n   %s\n", i+1, r.Score, r.Kind, r.NodeID, r.Pkg)
		if r.Name != "" {
			fmt.Printf("   name: %s\n", r.Name)
		}
	}
	if len(results) == 0 {
		fmt.Fprintln(os.Stderr, "(no results)")
	}
	return 0
}
