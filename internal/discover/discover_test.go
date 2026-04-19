package discover

import (
	"path/filepath"
	"testing"

	"github.com/rish-0-0/code-graph-rag/internal/graph"
)

func TestDiscoverHello(t *testing.T) {
	root, _ := filepath.Abs("../../testdata/01-hello")
	res, err := Discover(root, Options{UseGit: false})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Modules) != 1 || res.Modules[0].Path != "example.com/hello" {
		t.Fatalf("want 1 module 'example.com/hello', got %+v", res.Modules)
	}
}

func TestDiscoverMultiModuleWithReplace(t *testing.T) {
	root, _ := filepath.Abs("../../testdata/02-multi-module")
	res, err := Discover(root, Options{FollowReplace: true, UseGit: false})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Modules) != 3 {
		t.Fatalf("want 3 modules (moda, modb, modc), got %d", len(res.Modules))
	}
	var modb *Module
	for _, m := range res.Modules {
		if m.Path == "example.com/modb" {
			modb = m
		}
	}
	if modb == nil || len(modb.Replaces) != 1 {
		t.Fatalf("modb/replace missing: %+v", modb)
	}
	if modb.Replaces[0].NewLocal == nil || modb.Replaces[0].NewLocal.Path != "example.com/moda" {
		t.Fatalf("replace did not resolve to local moda: %+v", modb.Replaces[0])
	}

	g := graph.New()
	Emit(g, res)
	if got := len(g.NodesByKind(graph.NodeModule)); got != 3 {
		t.Fatalf("want 3 Module nodes, got %d", got)
	}
	if got := len(g.EdgesByKind(graph.EdgeReplaces)); got != 2 {
		t.Fatalf("want 2 REPLACES edges, got %d", got)
	}
	if got := len(g.EdgesByKind(graph.EdgeResolvesTo)); got != 1 {
		t.Fatalf("want 1 RESOLVES_TO edge (dedup), got %d", got)
	}
}
