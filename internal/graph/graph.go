package graph

import (
	"sort"
	"sync"
)

type Graph interface {
	AddNode(Node)
	AddEdge(Edge)
	Node(id string) (Node, bool)
	Nodes() []Node
	Edges() []Edge
	EdgesFrom(id string) []Edge
	EdgesTo(id string) []Edge
	EdgesByKind(EdgeKind) []Edge
	NodesByKind(NodeKind) []Node
}

type memGraph struct {
	mu    sync.RWMutex
	nodes map[string]Node
	edges map[string]Edge
	out   map[string][]string
	in    map[string][]string

	sortedNodes []Node
	sortedEdges []Edge
}

func New() Graph {
	return NewWithCapacity(0, 0)
}

// NewWithCapacity preallocates the underlying maps. Pass rough upper-bound
// hints from the caller (e.g. expected package count * avg nodes/pkg) to cut
// map rehashing on large repos. Zero hints behave like New().
func NewWithCapacity(nodeHint, edgeHint int) Graph {
	if nodeHint < 0 {
		nodeHint = 0
	}
	if edgeHint < 0 {
		edgeHint = 0
	}
	return &memGraph{
		nodes: make(map[string]Node, nodeHint),
		edges: make(map[string]Edge, edgeHint),
		out:   make(map[string][]string, nodeHint),
		in:    make(map[string][]string, nodeHint),
	}
}

func (g *memGraph) invalidate() {
	g.sortedNodes = nil
	g.sortedEdges = nil
}

func (g *memGraph) AddNode(n Node) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, ok := g.nodes[n.ID]; ok {
		return
	}
	g.nodes[n.ID] = n
	g.invalidate()
}

func (g *memGraph) AddEdge(e Edge) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if e.ID == "" {
		// Cheap concat — avoids fmt's reflection path on a hot loop.
		e.ID = string(e.Kind) + "|" + e.From + "|" + e.To
	}
	if _, ok := g.edges[e.ID]; ok {
		return
	}
	g.edges[e.ID] = e
	g.out[e.From] = append(g.out[e.From], e.ID)
	g.in[e.To] = append(g.in[e.To], e.ID)
	g.invalidate()
}

func (g *memGraph) Node(id string) (Node, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	n, ok := g.nodes[id]
	return n, ok
}

func (g *memGraph) Nodes() []Node {
	g.mu.RLock()
	if g.sortedNodes != nil {
		out := g.sortedNodes
		g.mu.RUnlock()
		return out
	}
	g.mu.RUnlock()

	g.mu.Lock()
	defer g.mu.Unlock()
	if g.sortedNodes != nil {
		return g.sortedNodes
	}
	out := make([]Node, 0, len(g.nodes))
	for _, n := range g.nodes {
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	g.sortedNodes = out
	return out
}

func (g *memGraph) Edges() []Edge {
	g.mu.RLock()
	if g.sortedEdges != nil {
		out := g.sortedEdges
		g.mu.RUnlock()
		return out
	}
	g.mu.RUnlock()

	g.mu.Lock()
	defer g.mu.Unlock()
	if g.sortedEdges != nil {
		return g.sortedEdges
	}
	out := make([]Edge, 0, len(g.edges))
	for _, e := range g.edges {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	g.sortedEdges = out
	return out
}

func (g *memGraph) EdgesFrom(id string) []Edge {
	g.mu.RLock()
	defer g.mu.RUnlock()
	ids := g.out[id]
	out := make([]Edge, 0, len(ids))
	for _, eid := range ids {
		out = append(out, g.edges[eid])
	}
	return out
}

func (g *memGraph) EdgesTo(id string) []Edge {
	g.mu.RLock()
	defer g.mu.RUnlock()
	ids := g.in[id]
	out := make([]Edge, 0, len(ids))
	for _, eid := range ids {
		out = append(out, g.edges[eid])
	}
	return out
}

func (g *memGraph) EdgesByKind(k EdgeKind) []Edge {
	g.mu.RLock()
	defer g.mu.RUnlock()
	var out []Edge
	for _, e := range g.edges {
		if e.Kind == k {
			out = append(out, e)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (g *memGraph) NodesByKind(k NodeKind) []Node {
	g.mu.RLock()
	defer g.mu.RUnlock()
	var out []Node
	for _, n := range g.nodes {
		if n.Kind == k {
			out = append(out, n)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}
