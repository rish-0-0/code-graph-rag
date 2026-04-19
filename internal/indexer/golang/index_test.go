package golangidx

import (
	"path/filepath"
	"strings"
	"testing"

	"github.com/rish-0-0/code-graph-rag/internal/discover"
	"github.com/rish-0-0/code-graph-rag/internal/graph"
)

func buildGraph(t *testing.T, fixture string) graph.Graph {
	t.Helper()
	root, _ := filepath.Abs("../../../testdata/" + fixture)
	res, err := discover.Discover(root, discover.Options{FollowReplace: true, UseGit: false})
	if err != nil {
		t.Fatal(err)
	}
	g := graph.New()
	discover.Emit(g, res)
	if err := Index(g, res, "./..."); err != nil {
		t.Fatalf("Index: %v", err)
	}
	return g
}

func nodeIDsMatching(g graph.Graph, suffix string) []string {
	var out []string
	for _, n := range g.Nodes() {
		if strings.HasSuffix(n.ID, suffix) {
			out = append(out, n.ID)
		}
	}
	return out
}

func TestIndex01Hello(t *testing.T) {
	g := buildGraph(t, "01-hello")

	if len(g.NodesByKind(graph.NodePackage)) < 1 {
		t.Fatal("no Package nodes")
	}
	if len(g.NodesByKind(graph.NodeFunction)) < 1 {
		t.Fatal("no Function nodes (expected NewGreeter, Run)")
	}
	if len(g.NodesByKind(graph.NodeMethod)) < 1 {
		t.Fatal("no Method nodes (expected Greeter.Say)")
	}
	if len(g.NodesByKind(graph.NodeInterface)) < 1 {
		t.Fatal("no Interface nodes (expected Talker)")
	}
	if len(g.EdgesByKind(graph.EdgeImplements)) < 1 {
		t.Fatal("expected IMPLEMENTS edge: Greeter implements Talker")
	}
	if len(g.EdgesByKind(graph.EdgeCalls)) < 1 {
		t.Fatal("expected CALLS edges")
	}
	if ids := nodeIDsMatching(g, ".DefaultName"); len(ids) == 0 {
		t.Fatal("expected Constant DefaultName")
	}
}

func TestIndex02MultiModuleCrossModuleCall(t *testing.T) {
	g := buildGraph(t, "02-multi-module")

	if got := len(g.NodesByKind(graph.NodeModule)); got != 3 {
		t.Fatalf("want 3 Module nodes (moda, modb, modc), got %d", got)
	}
	if got := len(g.EdgesByKind(graph.EdgeReplaces)); got != 2 {
		t.Fatalf("want 2 REPLACES edges (modb→moda, modc→moda), got %d", got)
	}
	if got := len(g.EdgesByKind(graph.EdgeResolvesTo)); got != 1 {
		t.Fatalf("want 1 RESOLVES_TO edge (moda@* dedupes across consumers), got %d", got)
	}

	// The CALLS edge from modb.CallA must terminate on moda.ExportedFn's real
	// Function node in module A (not a placeholder).
	var callA string
	for _, n := range g.NodesByKind(graph.NodeFunction) {
		if strings.HasSuffix(n.ID, ".CallA") {
			callA = n.ID
		}
	}
	if callA == "" {
		t.Fatal("modb.CallA not indexed")
	}
	var foundCross bool
	for _, e := range g.EdgesFrom(callA) {
		if e.Kind != graph.EdgeCalls {
			continue
		}
		target, ok := g.Node(e.To)
		if !ok {
			continue
		}
		if strings.Contains(e.To, "example.com/moda") && strings.HasSuffix(e.To, ".ExportedFn") && target.Kind == graph.NodeFunction {
			foundCross = true
			break
		}
	}
	if !foundCross {
		t.Fatal("CALLS edge from modb.CallA → moda.ExportedFn not found or not resolved to real node")
	}

	// modc.TripleAndAddOne must also land on moda.ExportedFn's real node.
	var tripleFn string
	for _, n := range g.NodesByKind(graph.NodeFunction) {
		if strings.HasSuffix(n.ID, ".TripleAndAddOne") {
			tripleFn = n.ID
		}
	}
	if tripleFn == "" {
		t.Fatal("modc.TripleAndAddOne not indexed")
	}
	var foundCrossC bool
	for _, e := range g.EdgesFrom(tripleFn) {
		if e.Kind != graph.EdgeCalls {
			continue
		}
		target, ok := g.Node(e.To)
		if !ok {
			continue
		}
		if strings.Contains(e.To, "example.com/moda") && strings.HasSuffix(e.To, ".ExportedFn") && target.Kind == graph.NodeFunction {
			foundCrossC = true
			break
		}
	}
	if !foundCrossC {
		t.Fatal("CALLS edge from modc.TripleAndAddOne → moda.ExportedFn not resolved to real node")
	}
}
