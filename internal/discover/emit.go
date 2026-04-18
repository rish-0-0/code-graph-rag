package discover

import (
	"fmt"

	"github.com/rish-0-0/code-graph-rag/internal/graph"
)

// ModuleID returns the stable graph ID for a module.
func ModuleID(path, version string) string {
	return fmt.Sprintf("mod:%s@%s", path, version)
}

// Emit writes the discovery result into the graph as Workspace / Module /
// ModuleVersion nodes plus REQUIRES / REPLACES / RESOLVES_TO / WORKSPACES edges.
func Emit(g graph.Graph, res *Result) {
	if res == nil {
		return
	}
	var wsID string
	if res.Workspace != nil {
		wsID = "ws:" + res.Workspace.Dir
		g.AddNode(graph.Node{ID: wsID, Kind: graph.NodeWorkspace, Name: res.Workspace.Dir,
			Props: map[string]any{"dir": res.Workspace.Dir}})
	}
	for _, m := range res.Modules {
		mid := ModuleID(m.Path, m.Version)
		g.AddNode(graph.Node{ID: mid, Kind: graph.NodeModule, Name: m.Path, Props: map[string]any{
			"path":    m.Path,
			"dir":     m.Dir,
			"version": m.Version,
			"commit":  m.Commit,
			"dirty":   m.Dirty,
		}})
		if wsID != "" {
			for _, wsMod := range res.Workspace.Modules {
				if wsMod == m {
					g.AddEdge(graph.Edge{Kind: graph.EdgeWorkspaces, From: wsID, To: mid})
					break
				}
			}
		}
		for _, r := range m.Requires {
			mvID := ModuleID(r.Path, r.Version)
			g.AddNode(graph.Node{ID: mvID, Kind: graph.NodeModuleVersion, Name: r.Path,
				Props: map[string]any{"path": r.Path, "version": r.Version}})
			g.AddEdge(graph.Edge{Kind: graph.EdgeRequires, From: mid, To: mvID})
		}
		for _, rep := range m.Replaces {
			oldID := ModuleID(rep.OldPath, or(rep.OldVersion, "*"))
			g.AddNode(graph.Node{ID: oldID, Kind: graph.NodeModuleVersion, Name: rep.OldPath,
				Props: map[string]any{"path": rep.OldPath, "version": or(rep.OldVersion, "*")}})
			var newID string
			if rep.NewLocal != nil {
				newID = ModuleID(rep.NewLocal.Path, rep.NewLocal.Version)
				g.AddEdge(graph.Edge{Kind: graph.EdgeResolvesTo, From: oldID, To: newID})
			} else {
				newID = ModuleID(rep.NewPath, or(rep.NewVersion, "*"))
				g.AddNode(graph.Node{ID: newID, Kind: graph.NodeModuleVersion, Name: rep.NewPath,
					Props: map[string]any{"path": rep.NewPath, "version": or(rep.NewVersion, "*"), "localDir": rep.NewDir}})
			}
			g.AddEdge(graph.Edge{Kind: graph.EdgeReplaces, From: mid, To: newID,
				Props: map[string]any{"oldPath": rep.OldPath, "oldVersion": rep.OldVersion}})
		}
	}
}

func or(a, b string) string {
	if a != "" {
		return a
	}
	return b
}
