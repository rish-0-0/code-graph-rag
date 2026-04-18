package output

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"

	"github.com/rishabhanand42/code-graph-rag/internal/graph"
)

// CanonicalPath is the default on-disk location for the persistent graph.
// Subcommands (blast, broken) load from this path by default; build writes it.
const CanonicalPath = ".codegraph/graph.jsonl"

type record struct {
	Type   string         `json:"type"`
	Schema *graph.Schema  `json:"schema,omitempty"`
	Node   *graph.Node    `json:"node,omitempty"`
	Edge   *graph.Edge    `json:"edge,omitempty"`
}

// Persist writes g to path as self-describing JSONL: first line is the
// schema record, followed by nodes then edges. Creates parent dirs as needed.
func Persist(g graph.Graph, path string) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	f, err := os.Create(path)
	if err != nil {
		return err
	}
	defer f.Close()
	return WriteCanonical(g, f)
}

// WriteCanonical writes the self-describing JSONL stream to w.
func WriteCanonical(g graph.Graph, w io.Writer) error {
	enc := json.NewEncoder(w)
	sc := graph.Describe()
	if err := enc.Encode(record{Type: "schema", Schema: &sc}); err != nil {
		return err
	}
	for _, n := range g.Nodes() {
		n := n
		if err := enc.Encode(record{Type: "node", Node: &n}); err != nil {
			return err
		}
	}
	for _, e := range g.Edges() {
		e := e
		if err := enc.Encode(record{Type: "edge", Edge: &e}); err != nil {
			return err
		}
	}
	return nil
}

// Load reads a canonical JSONL file produced by Persist and rebuilds a Graph.
// The schema record is skipped (it's informational).
func Load(path string) (graph.Graph, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	g := graph.New()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 1<<20), 1<<24)
	for sc.Scan() {
		var r record
		if err := json.Unmarshal(sc.Bytes(), &r); err != nil {
			return nil, fmt.Errorf("parse record: %w", err)
		}
		switch r.Type {
		case "node":
			if r.Node != nil {
				g.AddNode(*r.Node)
			}
		case "edge":
			if r.Edge != nil {
				g.AddEdge(*r.Edge)
			}
		}
	}
	return g, sc.Err()
}
