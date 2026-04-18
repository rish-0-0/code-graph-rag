// Package output emits an in-memory graph to file formats suitable for
// ingestion into Neo4j, Memgraph, or any graph DB the user prefers. All
// writers are pure functions over (Graph, io.Writer) so they can be unit
// tested without a temp dir.
package output

import (
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/rish-0-0/code-graph-rag/internal/graph"
)

// WriteAll writes graph g to outDir in the given format. format is one of
// "csv", "cypher", "jsonl". For csv, two files (nodes.csv, rels.csv) are
// written in neo4j-admin import format. For cypher a single graph.cypher,
// for jsonl a single graph.jsonl.
func WriteAll(g graph.Graph, format, outDir string) error {
	switch format {
	case "csv":
		nf, err := os.Create(filepath.Join(outDir, "nodes.csv"))
		if err != nil {
			return err
		}
		defer nf.Close()
		rf, err := os.Create(filepath.Join(outDir, "rels.csv"))
		if err != nil {
			return err
		}
		defer rf.Close()
		return WriteCSV(g, nf, rf)
	case "cypher":
		f, err := os.Create(filepath.Join(outDir, "graph.cypher"))
		if err != nil {
			return err
		}
		defer f.Close()
		return WriteCypher(g, f)
	case "jsonl":
		f, err := os.Create(filepath.Join(outDir, "graph.jsonl"))
		if err != nil {
			return err
		}
		defer f.Close()
		return WriteJSONL(g, f)
	default:
		return fmt.Errorf("unknown output format: %s (want csv|cypher|jsonl)", format)
	}
}

// WriteJSONL writes one JSON object per line: {"type":"node",...} or
// {"type":"edge",...}. Nodes precede edges; both are sorted by ID.
func WriteJSONL(g graph.Graph, w io.Writer) error {
	enc := json.NewEncoder(w)
	for _, n := range g.Nodes() {
		if err := enc.Encode(struct {
			Type string `json:"type"`
			graph.Node
		}{"node", n}); err != nil {
			return err
		}
	}
	for _, e := range g.Edges() {
		if err := enc.Encode(struct {
			Type string `json:"type"`
			graph.Edge
		}{"edge", e}); err != nil {
			return err
		}
	}
	return nil
}

// WriteCSV emits neo4j-admin import format: two files with typed headers.
func WriteCSV(g graph.Graph, nodes, rels io.Writer) error {
	nw := csv.NewWriter(nodes)
	rw := csv.NewWriter(rels)
	if err := nw.Write([]string{"id:ID", "name", ":LABEL", "props"}); err != nil {
		return err
	}
	for _, n := range g.Nodes() {
		props, _ := json.Marshal(n.Props)
		if err := nw.Write([]string{n.ID, n.Name, string(n.Kind), string(props)}); err != nil {
			return err
		}
	}
	nw.Flush()
	if err := nw.Error(); err != nil {
		return err
	}
	if err := rw.Write([]string{":START_ID", ":END_ID", ":TYPE", "props"}); err != nil {
		return err
	}
	for _, e := range g.Edges() {
		props, _ := json.Marshal(e.Props)
		if err := rw.Write([]string{e.From, e.To, string(e.Kind), string(props)}); err != nil {
			return err
		}
	}
	rw.Flush()
	return rw.Error()
}

// WriteCypher emits a CREATE script. Node IDs become the `id` property.
// Use MERGE on re-import to stay idempotent.
func WriteCypher(g graph.Graph, w io.Writer) error {
	bw := newBufWriter(w)
	for _, n := range g.Nodes() {
		props := map[string]any{"id": n.ID, "name": n.Name}
		for k, v := range n.Props {
			props[k] = v
		}
		bw.writef("MERGE (n:%s {id:%q}) SET n += %s;\n", string(n.Kind), n.ID, cypherMap(props))
	}
	for _, e := range g.Edges() {
		props := map[string]any{}
		for k, v := range e.Props {
			props[k] = v
		}
		bw.writef("MATCH (a {id:%q}), (b {id:%q}) MERGE (a)-[r:%s]->(b) SET r += %s;\n",
			e.From, e.To, string(e.Kind), cypherMap(props))
	}
	return bw.err
}

func cypherMap(m map[string]any) string {
	if len(m) == 0 {
		return "{}"
	}
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	// deterministic order
	sortStrings(keys)
	var b strings.Builder
	b.WriteByte('{')
	for i, k := range keys {
		if i > 0 {
			b.WriteByte(',')
		}
		fmt.Fprintf(&b, "%s:", k)
		v, _ := json.Marshal(m[k])
		b.Write(v)
	}
	b.WriteByte('}')
	return b.String()
}

func sortStrings(s []string) {
	for i := 1; i < len(s); i++ {
		for j := i; j > 0 && s[j-1] > s[j]; j-- {
			s[j-1], s[j] = s[j], s[j-1]
		}
	}
}

type bufWriter struct {
	w   io.Writer
	err error
}

func newBufWriter(w io.Writer) *bufWriter { return &bufWriter{w: w} }
func (b *bufWriter) writef(f string, a ...any) {
	if b.err != nil {
		return
	}
	_, b.err = fmt.Fprintf(b.w, f, a...)
}
