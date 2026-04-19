// Package query implements the two user-facing graph queries: blast (reverse
// BFS over CALLS/REFERENCES to compute a change's blast radius) and broken
// (surface dangling refs, unresolved imports, and similar integrity issues).
package query

import (
	"strings"

	"github.com/rish-0-0/code-graph-rag/internal/graph"
)

// Caller is one node reached during a blast-radius walk.
type Caller struct {
	ID    string         `json:"id"`
	Kind  graph.NodeKind `json:"kind"`
	Name  string         `json:"name"`
	Depth int            `json:"depth"`
}

// BlastResult summarizes a blast radius query.
type BlastResult struct {
	Symbol  string   `json:"symbol"`
	Depth   int      `json:"depth"`
	Callers []Caller `json:"callers"`
}

// Blast returns reverse-reachable callers of the given symbol name, matched
// by suffix against node IDs so callers can pass either a fully-qualified
// ID or a short "pkgPath.Func" form. depth == 0 means unlimited.
func Blast(g graph.Graph, symbol string, depth int) BlastResult {
	start := FindSymbol(g, symbol)
	out := BlastResult{Symbol: symbol, Depth: depth}
	if start == "" {
		return out
	}
	seeds := append([]string{start}, interfaceMethodSeeds(g, start)...)
	visited := map[string]int{}
	for _, s := range seeds {
		visited[s] = 0
	}
	frontier := seeds
	for d := 1; depth == 0 || d <= depth; d++ {
		var next []string
		for _, id := range frontier {
			for _, e := range g.EdgesTo(id) {
				if e.Kind != graph.EdgeCalls && e.Kind != graph.EdgeReferences {
					continue
				}
				if _, seen := visited[e.From]; seen {
					continue
				}
				visited[e.From] = d
				next = append(next, e.From)
			}
		}
		if len(next) == 0 {
			break
		}
		frontier = next
	}
	for _, s := range seeds {
		delete(visited, s)
	}
	for id, d := range visited {
		if n, ok := g.Node(id); ok {
			out.Callers = append(out.Callers, Caller{ID: id, Kind: n.Kind, Name: n.Name, Depth: d})
		}
	}
	return out
}

// interfaceMethodSeeds returns interface-method node IDs that correspond to
// the given concrete method. For a start node `pkg.Greeter.Say` where Greeter
// implements Talker, it returns `pkg.Talker.Say`. Changing a concrete method
// affects callers dispatching through any interface it satisfies, so those
// interface-method nodes belong in the blast frontier.
func interfaceMethodSeeds(g graph.Graph, start string) []string {
	n, ok := g.Node(start)
	if !ok || n.Kind != graph.NodeMethod {
		return nil
	}
	dot := strings.LastIndex(start, ".")
	if dot < 0 {
		return nil
	}
	typeID, methodName := start[:dot], start[dot+1:]
	var seeds []string
	for _, e := range g.EdgesFrom(typeID) {
		if e.Kind != graph.EdgeImplements {
			continue
		}
		candidate := e.To + "." + methodName
		if _, ok := g.Node(candidate); ok {
			seeds = append(seeds, candidate)
		}
	}
	return seeds
}

func FindSymbol(g graph.Graph, symbol string) string {
	if n, ok := g.Node(symbol); ok {
		return n.ID
	}
	for _, n := range g.Nodes() {
		if strings.HasSuffix(n.ID, "."+symbol) || strings.HasSuffix(n.ID, "/"+symbol) {
			return n.ID
		}
	}
	return ""
}

// Dangling is an edge whose target was never resolved to a concrete node.
type Dangling struct {
	From string         `json:"from"`
	To   string         `json:"to"`
	Kind graph.EdgeKind `json:"kind"`
}

// BrokenReport holds integrity issues found in the graph.
type BrokenReport struct {
	Dangling          []Dangling `json:"dangling"`
	UnresolvedImports []string   `json:"unresolvedImports"`
}

// Broken scans the graph for references whose target node is missing, plus
// external (unresolved) imports.
func Broken(g graph.Graph) BrokenReport {
	var rep BrokenReport
	for _, e := range g.Edges() {
		if _, ok := g.Node(e.To); !ok {
			rep.Dangling = append(rep.Dangling, Dangling{From: e.From, To: e.To, Kind: e.Kind})
		}
	}
	for _, n := range g.NodesByKind(graph.NodeImport) {
		if ext, _ := n.Props["external"].(bool); ext {
			rep.UnresolvedImports = append(rep.UnresolvedImports, n.Name)
		}
	}
	return rep
}
