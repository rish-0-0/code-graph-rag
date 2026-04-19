package query

import (
	"fmt"
	"sort"
	"strings"

	"github.com/rish-0-0/code-graph-rag/internal/graph"
)

// ListItem is the uniform row type returned by every list-style query.
// Callers can rank by InDegree/OutDegree to triage discovery — high fan-in
// tends to surface widely-used APIs, high fan-out surfaces orchestrators.
type ListItem struct {
	ID        string         `json:"id"`
	Kind      graph.NodeKind `json:"kind"`
	Name      string         `json:"name"`
	InDegree  int            `json:"inDegree"`
	OutDegree int            `json:"outDegree"`
}

func toItem(g graph.Graph, id string) (ListItem, bool) {
	n, ok := g.Node(id)
	if !ok {
		return ListItem{}, false
	}
	return ListItem{
		ID:        id,
		Kind:      n.Kind,
		Name:      n.Name,
		InDegree:  len(g.EdgesTo(id)),
		OutDegree: len(g.EdgesFrom(id)),
	}, true
}

// ListPackages returns every Package node in the graph.
func ListPackages(g graph.Graph) []ListItem {
	pkgs := g.NodesByKind(graph.NodePackage)
	out := make([]ListItem, 0, len(pkgs))
	for _, n := range pkgs {
		if item, ok := toItem(g, n.ID); ok {
			out = append(out, item)
		}
	}
	return out
}

// ListInterfaces dispatches on the kind of the resolved symbol:
//   - Type  → interfaces it implements (IMPLEMENTS out-edges)
//   - Interface → methods declared on it (HAS_METHOD out-edges)
func ListInterfaces(g graph.Graph, symbol string) ([]ListItem, error) {
	id := FindSymbol(g, symbol)
	if id == "" {
		return nil, fmt.Errorf("symbol not found: %s", symbol)
	}
	n, _ := g.Node(id)
	var edgeKind graph.EdgeKind
	switch n.Kind {
	case graph.NodeType:
		edgeKind = graph.EdgeImplements
	case graph.NodeInterface:
		edgeKind = graph.EdgeHasMethod
	default:
		return nil, fmt.Errorf("list interfaces: symbol %s is a %s; expected Type or Interface", symbol, n.Kind)
	}
	var out []ListItem
	for _, e := range g.EdgesFrom(id) {
		if e.Kind != edgeKind {
			continue
		}
		if item, ok := toItem(g, e.To); ok {
			out = append(out, item)
		}
	}
	return out, nil
}

// ListUpstream returns direct callers of symbol (one hop over CALLS/REFERENCES).
func ListUpstream(g graph.Graph, symbol string) ([]ListItem, error) {
	id := FindSymbol(g, symbol)
	if id == "" {
		return nil, fmt.Errorf("symbol not found: %s", symbol)
	}
	seen := map[string]bool{}
	var out []ListItem
	for _, e := range g.EdgesTo(id) {
		if e.Kind != graph.EdgeCalls && e.Kind != graph.EdgeReferences {
			continue
		}
		if seen[e.From] {
			continue
		}
		seen[e.From] = true
		if item, ok := toItem(g, e.From); ok {
			out = append(out, item)
		}
	}
	return out, nil
}

// ListDownstream returns direct callees of symbol (one hop over CALLS/REFERENCES).
func ListDownstream(g graph.Graph, symbol string) ([]ListItem, error) {
	id := FindSymbol(g, symbol)
	if id == "" {
		return nil, fmt.Errorf("symbol not found: %s", symbol)
	}
	seen := map[string]bool{}
	var out []ListItem
	for _, e := range g.EdgesFrom(id) {
		if e.Kind != graph.EdgeCalls && e.Kind != graph.EdgeReferences {
			continue
		}
		if seen[e.To] {
			continue
		}
		seen[e.To] = true
		if item, ok := toItem(g, e.To); ok {
			out = append(out, item)
		}
	}
	return out, nil
}

// ListNeighbors returns siblings: nodes sharing a CONTAINS parent with symbol.
// Siblings need not be connected to each other — the definition is structural
// (same parent package/file/type), not behavioral.
func ListNeighbors(g graph.Graph, symbol string) ([]ListItem, error) {
	id := FindSymbol(g, symbol)
	if id == "" {
		return nil, fmt.Errorf("symbol not found: %s", symbol)
	}
	var parents []string
	for _, e := range g.EdgesTo(id) {
		if e.Kind == graph.EdgeContains {
			parents = append(parents, e.From)
		}
	}
	seen := map[string]bool{id: true}
	var out []ListItem
	for _, p := range parents {
		for _, e := range g.EdgesFrom(p) {
			if e.Kind != graph.EdgeContains {
				continue
			}
			if seen[e.To] {
				continue
			}
			seen[e.To] = true
			if item, ok := toItem(g, e.To); ok {
				out = append(out, item)
			}
		}
	}
	return out, nil
}

// SortItems sorts in place by the named key and order. Invalid combinations
// fall back to name-asc. Tie-break on ID keeps output deterministic.
func SortItems(items []ListItem, by, order string) {
	less := func(a, b ListItem) bool {
		switch by {
		case "in":
			if a.InDegree != b.InDegree {
				return a.InDegree < b.InDegree
			}
		case "out":
			if a.OutDegree != b.OutDegree {
				return a.OutDegree < b.OutDegree
			}
		default: // name
			if a.Name != b.Name {
				return a.Name < b.Name
			}
		}
		return a.ID < b.ID
	}
	sort.SliceStable(items, func(i, j int) bool {
		if strings.EqualFold(order, "desc") {
			return less(items[j], items[i])
		}
		return less(items[i], items[j])
	})
}
