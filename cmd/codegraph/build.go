package main

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/rish-0-0/code-graph-rag/internal/discover"
	"github.com/rish-0-0/code-graph-rag/internal/graph"
	golangidx "github.com/rish-0-0/code-graph-rag/internal/indexer/golang"
	"github.com/rish-0-0/code-graph-rag/internal/output"
)

func splitCSV(s string) []string {
	if s == "" {
		return nil
	}
	parts := strings.Split(s, ",")
	out := parts[:0]
	for _, p := range parts {
		if p = strings.TrimSpace(p); p != "" {
			out = append(out, p)
		}
	}
	return out
}

// stderrReporter emits stage events to an io.Writer with elapsed time so users
// can see the build is making progress on slow repos.
type stderrReporter struct {
	w     io.Writer
	start time.Time
}

func newStderrReporter(w io.Writer) *stderrReporter {
	return &stderrReporter{w: w, start: time.Now()}
}

func (r *stderrReporter) Event(stage, msg string) {
	elapsed := time.Since(r.start).Truncate(time.Millisecond)
	fmt.Fprintf(r.w, "[%8s] %s: %s\n", elapsed, stage, msg)
}

func runBuild(args []string) int {
	fs := newFlagSet("build", "parse a Go module tree into a persisted graph")
	root := fs.String("root", ".", "root directory to scan for Go modules")
	pkgPat := fs.String("pkg", "./...", "package pattern passed to go/packages")
	format := fs.String("output", "", "additional export format: csv | cypher | jsonl (canonical graph is always persisted)")
	outDir := fs.String("out-dir", "./graph-out", "directory for --output exports")
	persistPath := fs.String("persist", output.CanonicalPath, "canonical graph file (used by blast/broken)")
	module := fs.String("module", "", "restrict to one module path in a multi-module tree")
	followReplace := fs.Bool("follow-replace", true, "resolve replace directives to local modules")
	useGit := fs.Bool("git", true, "enrich module nodes with git commit/tag info when available")
	ignore := fs.String("ignore", "", "comma-separated dir names or root-relative paths to skip (e.g. 'scripts,docs,apps/legacy')")
	only := fs.String("only", "", "comma-separated dir names or root-relative paths to include exclusively")
	quiet := fs.Bool("quiet", false, "suppress per-module progress output")
	if err := fs.Parse(args); err != nil {
		return 2
	}

	var reporter golangidx.Reporter
	if !*quiet {
		reporter = newStderrReporter(os.Stderr)
	}

	g, err := buildGraph(*root, *pkgPat, *module, *followReplace, *useGit, splitCSV(*ignore), splitCSV(*only), reporter)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		return 1
	}
	fmt.Fprintf(os.Stderr, "graph: %d nodes, %d edges\n", len(g.Nodes()), len(g.Edges()))

	if err := output.Persist(g, *persistPath); err != nil {
		fmt.Fprintf(os.Stderr, "persist: %v\n", err)
		return 1
	}
	abs, _ := filepath.Abs(*persistPath)
	fmt.Fprintf(os.Stderr, "persisted canonical graph to %s\n", abs)

	if *format != "" {
		if err := os.MkdirAll(*outDir, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "mkdir: %v\n", err)
			return 1
		}
		if err := output.WriteAll(g, *format, *outDir); err != nil {
			fmt.Fprintf(os.Stderr, "output: %v\n", err)
			return 1
		}
		abs, _ := filepath.Abs(*outDir)
		fmt.Fprintf(os.Stderr, "wrote %s export to %s\n", *format, abs)
	}
	return 0
}

// buildGraph runs discover + index and returns the in-memory graph.
func buildGraph(root, pattern, module string, followReplace, useGit bool, ignore, only []string, reporter golangidx.Reporter) (graph.Graph, error) {
	g := graph.New()
	res, err := discover.Discover(root, discover.Options{
		Module:        module,
		FollowReplace: followReplace,
		UseGit:        useGit,
		Ignore:        ignore,
		Only:          only,
	})
	if err != nil {
		return nil, fmt.Errorf("discover: %w", err)
	}
	discover.Emit(g, res)
	if err := golangidx.IndexWithReporter(g, res, pattern, reporter); err != nil {
		return nil, fmt.Errorf("index: %w", err)
	}
	return g, nil
}
