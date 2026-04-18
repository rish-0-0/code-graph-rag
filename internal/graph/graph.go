package graph

import (
	"fmt"
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
}

func New() Graph {
	return &memGraph{
		nodes: map[string]Node{},
		edges: map[string]Edge{},
		out:   map[string][]string{},
		in:    map[string][]string{},
	}
}

func (g *memGraph) AddNode(n Node) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if _, ok := g.nodes[n.ID]; ok {
		return
	}
	g.nodes[n.ID] = n
}

func (g *memGraph) AddEdge(e Edge) {
	g.mu.Lock()
	defer g.mu.Unlock()
	if e.ID == "" {
		e.ID = fmt.Sprintf("%s|%s|%s", e.Kind, e.From, e.To)
	}
	if _, ok := g.edges[e.ID]; ok {
		return
	}
	g.edges[e.ID] = e
	g.out[e.From] = append(g.out[e.From], e.ID)
	g.in[e.To] = append(g.in[e.To], e.ID)
}

func (g *memGraph) Node(id string) (Node, bool) {
	g.mu.RLock()
	defer g.mu.RUnlock()
	n, ok := g.nodes[id]
	return n, ok
}

func (g *memGraph) Nodes() []Node {
	g.mu.RLock()
	defer g.mu.RUnlock()
	out := make([]Node, 0, len(g.nodes))
	for _, n := range g.nodes {
		out = append(out, n)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
	return out
}

func (g *memGraph) Edges() []Edge {
	g.mu.RLock()
	defer g.mu.RUnlock()
	out := make([]Edge, 0, len(g.edges))
	for _, e := range g.edges {
		out = append(out, e)
	}
	sort.Slice(out, func(i, j int) bool { return out[i].ID < out[j].ID })
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
