// Package embed extracts embeddable text from a graph and ships it to an
// external embeddings + pgvector service over HTTP.
//
// The service contract is intentionally narrow:
//
//	POST {endpoint}/embed   — body: {"items": [Item, ...]}              → 200 {"upserted": N}
//	POST {endpoint}/search  — body: {"query": "...", "k": 10}           → 200 {"results": [Result, ...]}
//
// Auth is a single bearer token in the EMBEDDINGS_API_AUTH env var; the Go
// side does not touch Postgres directly.
package embed

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"

	"github.com/rish-0-0/code-graph-rag/internal/graph"
)

// Item is one unit of text to embed + index, keyed by a graph node ID.
type Item struct {
	NodeID  string `json:"node_id"`
	Kind    string `json:"kind"`
	Module  string `json:"module,omitempty"`
	Pkg     string `json:"pkg,omitempty"`
	Name    string `json:"name"`
	Text    string `json:"text"`
	PosFile string `json:"pos_file,omitempty"`
	PosLine int    `json:"pos_line,omitempty"`
}

// SearchResult is one hit from /search.
type SearchResult struct {
	NodeID string  `json:"node_id"`
	Kind   string  `json:"kind"`
	Name   string  `json:"name"`
	Pkg    string  `json:"pkg"`
	Score  float64 `json:"score"`
	Text   string  `json:"text"`
}

// Client posts to the embeddings/indexer service.
type Client struct {
	Endpoint string
	Auth     string
	HTTP     *http.Client
}

// NewClientFromEnv reads EMBEDDINGS_API_ENDPOINT and EMBEDDINGS_API_AUTH.
func NewClientFromEnv() (*Client, error) {
	ep := strings.TrimRight(os.Getenv("EMBEDDINGS_API_ENDPOINT"), "/")
	if ep == "" {
		return nil, fmt.Errorf("EMBEDDINGS_API_ENDPOINT is not set")
	}
	return &Client{
		Endpoint: ep,
		Auth:     os.Getenv("EMBEDDINGS_API_AUTH"),
		HTTP:     &http.Client{Timeout: 120 * time.Second},
	}, nil
}

func (c *Client) post(path string, body, out any) error {
	buf, err := json.Marshal(body)
	if err != nil {
		return err
	}
	req, err := http.NewRequest("POST", c.Endpoint+path, bytes.NewReader(buf))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	if c.Auth != "" {
		req.Header.Set("Authorization", "Bearer "+c.Auth)
	}
	resp, err := c.HTTP.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode/100 != 2 {
		b, _ := io.ReadAll(resp.Body)
		return fmt.Errorf("%s %s: %s: %s", req.Method, req.URL, resp.Status, strings.TrimSpace(string(b)))
	}
	if out == nil {
		return nil
	}
	return json.NewDecoder(resp.Body).Decode(out)
}

// UpsertEmbed sends a batch of items to /embed.
func (c *Client) UpsertEmbed(items []Item) (int, error) {
	var out struct {
		Upserted int `json:"upserted"`
	}
	if err := c.post("/embed", map[string]any{"items": items}, &out); err != nil {
		return 0, err
	}
	return out.Upserted, nil
}

// Search posts a query to /search.
func (c *Client) Search(query string, k int) ([]SearchResult, error) {
	var out struct {
		Results []SearchResult `json:"results"`
	}
	if err := c.post("/search", map[string]any{"query": query, "k": k}, &out); err != nil {
		return nil, err
	}
	return out.Results, nil
}

// embeddable returns true for node kinds we want to embed. Files, modules,
// imports, and individual fields are skipped — they're either too coarse
// (file) or too sparse (field) to be useful for code RAG.
func embeddable(k graph.NodeKind) bool {
	switch k {
	case graph.NodePackage,
		graph.NodeFunction,
		graph.NodeMethod,
		graph.NodeType,
		graph.NodeInterface,
		graph.NodeConstant,
		graph.NodeVariable:
		return true
	}
	return false
}

// snippet reads up to maxLines lines from path starting at line. Best-effort:
// returns "" on any error rather than failing the whole batch.
func snippet(path string, line, maxLines int) string {
	if path == "" || line <= 0 {
		return ""
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return ""
	}
	lines := strings.Split(string(b), "\n")
	if line-1 >= len(lines) {
		return ""
	}
	end := line - 1 + maxLines
	if end > len(lines) {
		end = len(lines)
	}
	return strings.Join(lines[line-1:end], "\n")
}

// BuildItems walks g and produces one Item per embeddable node. The text
// blends doc + signature (snippet) so the embedding captures both intent
// and shape.
func BuildItems(g graph.Graph, snippetLines int) []Item {
	if snippetLines <= 0 {
		snippetLines = 12
	}
	pkgOf := map[string]string{} // nodeID -> package importPath, derived from contains edges
	for _, e := range g.Edges() {
		if e.Kind != graph.EdgeContains {
			continue
		}
		from, ok := g.Node(e.From)
		if !ok || from.Kind != graph.NodePackage {
			continue
		}
		ip, _ := from.Props["importPath"].(string)
		pkgOf[e.To] = ip
	}

	items := make([]Item, 0, 1024)
	for _, n := range g.Nodes() {
		if !embeddable(n.Kind) {
			continue
		}
		doc, _ := n.Props["doc"].(string)
		ip := pkgOf[n.ID]
		if n.Kind == graph.NodePackage {
			ip, _ = n.Props["importPath"].(string)
		}
		mod, _ := n.Props["module"].(string)

		var snip string
		if n.Kind != graph.NodePackage {
			snip = snippet(n.Pos.File, n.Pos.Line, snippetLines)
		}

		var b strings.Builder
		fmt.Fprintf(&b, "%s %s\n", n.Kind, n.Name)
		if ip != "" {
			fmt.Fprintf(&b, "package: %s\n", ip)
		}
		if doc != "" {
			b.WriteString(doc)
			b.WriteString("\n")
		}
		if snip != "" {
			b.WriteString("\n")
			b.WriteString(snip)
		}
		text := strings.TrimSpace(b.String())
		if text == "" {
			continue
		}

		items = append(items, Item{
			NodeID:  n.ID,
			Kind:    string(n.Kind),
			Module:  mod,
			Pkg:     ip,
			Name:    n.Name,
			Text:    text,
			PosFile: n.Pos.File,
			PosLine: n.Pos.Line,
		})
	}
	return items
}
