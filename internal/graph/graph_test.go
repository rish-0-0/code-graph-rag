package graph

import "testing"

func TestInMemoryGraph(t *testing.T) {
	g := New()
	g.AddNode(Node{ID: "a", Kind: NodePackage, Name: "a"})
	g.AddNode(Node{ID: "b", Kind: NodeFunction, Name: "b"})
	g.AddNode(Node{ID: "a", Kind: NodePackage, Name: "dup"}) // dedupe
	g.AddEdge(Edge{Kind: EdgeContains, From: "a", To: "b"})
	g.AddEdge(Edge{Kind: EdgeContains, From: "a", To: "b"}) // dedupe

	if n, _ := g.Node("a"); n.Name != "a" {
		t.Fatalf("dedupe broke: got %q", n.Name)
	}
	if len(g.Nodes()) != 2 {
		t.Fatalf("want 2 nodes, got %d", len(g.Nodes()))
	}
	if len(g.Edges()) != 1 {
		t.Fatalf("want 1 edge, got %d", len(g.Edges()))
	}
	if len(g.EdgesFrom("a")) != 1 || len(g.EdgesTo("b")) != 1 {
		t.Fatal("adjacency wrong")
	}
	if len(g.NodesByKind(NodeFunction)) != 1 {
		t.Fatal("kind filter wrong")
	}
}
