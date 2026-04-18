package query

import (
	"testing"

	"github.com/rish-0-0/code-graph-rag/internal/graph"
)

func TestBlast(t *testing.T) {
	g := graph.New()
	g.AddNode(graph.Node{ID: "pkg.A", Kind: graph.NodeFunction, Name: "A"})
	g.AddNode(graph.Node{ID: "pkg.B", Kind: graph.NodeFunction, Name: "B"})
	g.AddNode(graph.Node{ID: "pkg.C", Kind: graph.NodeFunction, Name: "C"})
	g.AddEdge(graph.Edge{Kind: graph.EdgeCalls, From: "pkg.B", To: "pkg.A"})
	g.AddEdge(graph.Edge{Kind: graph.EdgeCalls, From: "pkg.C", To: "pkg.B"})

	r := Blast(g, "A", 0)
	if len(r.Callers) != 2 {
		t.Fatalf("want 2 callers, got %+v", r.Callers)
	}
	r = Blast(g, "A", 1)
	if len(r.Callers) != 1 {
		t.Fatalf("depth=1 should yield 1 caller, got %d", len(r.Callers))
	}
}

func TestBroken(t *testing.T) {
	g := graph.New()
	g.AddNode(graph.Node{ID: "a", Kind: graph.NodeFunction, Name: "a"})
	g.AddEdge(graph.Edge{Kind: graph.EdgeCalls, From: "a", To: "ghost"})
	g.AddNode(graph.Node{ID: "imp1", Kind: graph.NodeImport, Name: "fmt",
		Props: map[string]any{"external": true}})

	r := Broken(g)
	if len(r.Dangling) != 1 || r.Dangling[0].To != "ghost" {
		t.Fatalf("dangling mismatch: %+v", r.Dangling)
	}
	if len(r.UnresolvedImports) != 1 {
		t.Fatalf("unresolved imports: %+v", r.UnresolvedImports)
	}
}
