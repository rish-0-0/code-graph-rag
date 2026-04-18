package output

import (
	"bytes"
	"strings"
	"testing"

	"github.com/rishabhanand42/code-graph-rag/internal/graph"
)

func sampleGraph() graph.Graph {
	g := graph.New()
	g.AddNode(graph.Node{ID: "a", Kind: graph.NodePackage, Name: "a",
		Props: map[string]any{"importPath": "example.com/a"}})
	g.AddNode(graph.Node{ID: "b", Kind: graph.NodeFunction, Name: "B"})
	g.AddEdge(graph.Edge{Kind: graph.EdgeContains, From: "a", To: "b"})
	return g
}

func TestJSONL(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteJSONL(sampleGraph(), &buf); err != nil {
		t.Fatal(err)
	}
	lines := strings.Split(strings.TrimSpace(buf.String()), "\n")
	if len(lines) != 3 {
		t.Fatalf("want 3 lines, got %d: %s", len(lines), buf.String())
	}
}

func TestCypher(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteCypher(sampleGraph(), &buf); err != nil {
		t.Fatal(err)
	}
	out := buf.String()
	if !strings.Contains(out, "MERGE (n:Package") || !strings.Contains(out, "CONTAINS") {
		t.Fatalf("unexpected cypher: %s", out)
	}
}

func TestCSV(t *testing.T) {
	var n, r bytes.Buffer
	if err := WriteCSV(sampleGraph(), &n, &r); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(n.String(), ":LABEL") || !strings.Contains(r.String(), ":TYPE") {
		t.Fatalf("csv headers missing: %q %q", n.String(), r.String())
	}
}
