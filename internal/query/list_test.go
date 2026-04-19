package query

import (
	"testing"

	"github.com/rish-0-0/code-graph-rag/internal/graph"
)

// fixture: pkg P contains Type T and Interface I. T implements I. I has method I.M.
// Function F is also in P. F calls T.M and T.M calls I.M.
func listFixture() graph.Graph {
	g := graph.New()
	g.AddNode(graph.Node{ID: "P", Kind: graph.NodePackage, Name: "p"})
	g.AddNode(graph.Node{ID: "P.T", Kind: graph.NodeType, Name: "T"})
	g.AddNode(graph.Node{ID: "P.I", Kind: graph.NodeInterface, Name: "I"})
	g.AddNode(graph.Node{ID: "P.I.M", Kind: graph.NodeMethod, Name: "M"})
	g.AddNode(graph.Node{ID: "P.T.M", Kind: graph.NodeMethod, Name: "M"})
	g.AddNode(graph.Node{ID: "P.F", Kind: graph.NodeFunction, Name: "F"})

	g.AddEdge(graph.Edge{Kind: graph.EdgeContains, From: "P", To: "P.T"})
	g.AddEdge(graph.Edge{Kind: graph.EdgeContains, From: "P", To: "P.I"})
	g.AddEdge(graph.Edge{Kind: graph.EdgeContains, From: "P", To: "P.F"})
	g.AddEdge(graph.Edge{Kind: graph.EdgeContains, From: "P", To: "P.T.M"})
	g.AddEdge(graph.Edge{Kind: graph.EdgeContains, From: "P", To: "P.I.M"})
	g.AddEdge(graph.Edge{Kind: graph.EdgeHasMethod, From: "P.I", To: "P.I.M"})
	g.AddEdge(graph.Edge{Kind: graph.EdgeHasMethod, From: "P.T", To: "P.T.M"})
	g.AddEdge(graph.Edge{Kind: graph.EdgeImplements, From: "P.T", To: "P.I"})
	g.AddEdge(graph.Edge{Kind: graph.EdgeCalls, From: "P.F", To: "P.T.M"})
	g.AddEdge(graph.Edge{Kind: graph.EdgeCalls, From: "P.T.M", To: "P.I.M"})
	return g
}

func idsOf(items []ListItem) []string {
	out := make([]string, len(items))
	for i, it := range items {
		out[i] = it.ID
	}
	return out
}

func contains(ss []string, want string) bool {
	for _, s := range ss {
		if s == want {
			return true
		}
	}
	return false
}

func TestListPackages(t *testing.T) {
	g := listFixture()
	got := ListPackages(g)
	if len(got) != 1 || got[0].ID != "P" {
		t.Fatalf("want [P], got %v", idsOf(got))
	}
}

func TestListInterfacesOnType(t *testing.T) {
	g := listFixture()
	got, err := ListInterfaces(g, "T")
	if err != nil {
		t.Fatal(err)
	}
	if ids := idsOf(got); len(ids) != 1 || ids[0] != "P.I" {
		t.Fatalf("want [P.I], got %v", ids)
	}
}

func TestListInterfacesOnInterface(t *testing.T) {
	g := listFixture()
	got, err := ListInterfaces(g, "I")
	if err != nil {
		t.Fatal(err)
	}
	if ids := idsOf(got); len(ids) != 1 || ids[0] != "P.I.M" {
		t.Fatalf("want [P.I.M], got %v", ids)
	}
}

func TestListUpstream(t *testing.T) {
	g := listFixture()
	got, err := ListUpstream(g, "T.M")
	if err != nil {
		t.Fatal(err)
	}
	if ids := idsOf(got); len(ids) != 1 || ids[0] != "P.F" {
		t.Fatalf("want [P.F], got %v", ids)
	}
}

func TestListDownstream(t *testing.T) {
	g := listFixture()
	got, err := ListDownstream(g, "F")
	if err != nil {
		t.Fatal(err)
	}
	if ids := idsOf(got); len(ids) != 1 || ids[0] != "P.T.M" {
		t.Fatalf("want [P.T.M], got %v", ids)
	}
}

func TestListNeighbors(t *testing.T) {
	g := listFixture()
	got, err := ListNeighbors(g, "F")
	if err != nil {
		t.Fatal(err)
	}
	ids := idsOf(got)
	// F's parent is P; siblings are T, I, T.M, I.M (but not F itself).
	for _, want := range []string{"P.T", "P.I", "P.T.M", "P.I.M"} {
		if !contains(ids, want) {
			t.Fatalf("missing %s in neighbors: %v", want, ids)
		}
	}
	if contains(ids, "P.F") {
		t.Fatalf("self should be excluded, got %v", ids)
	}
}

func TestSortItems(t *testing.T) {
	items := []ListItem{
		{ID: "b", Name: "b", InDegree: 1, OutDegree: 3},
		{ID: "a", Name: "a", InDegree: 2, OutDegree: 1},
		{ID: "c", Name: "c", InDegree: 2, OutDegree: 2},
	}
	SortItems(items, "in", "desc")
	if items[0].ID != "a" && items[0].ID != "c" {
		t.Fatalf("expected a or c first by in-desc, got %v", idsOf(items))
	}
	SortItems(items, "name", "asc")
	if idsOf(items)[0] != "a" {
		t.Fatalf("expected a first, got %v", idsOf(items))
	}
}

func TestListSymbolNotFound(t *testing.T) {
	g := listFixture()
	if _, err := ListUpstream(g, "nope"); err == nil {
		t.Fatal("expected error for missing symbol")
	}
}
