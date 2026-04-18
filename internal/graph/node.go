package graph

type Position struct {
	File   string `json:"file,omitempty"`
	Line   int    `json:"line,omitempty"`
	Column int    `json:"column,omitempty"`
}

type Node struct {
	ID    string         `json:"id"`
	Kind  NodeKind       `json:"kind"`
	Name  string         `json:"name"`
	Pos   Position       `json:"pos,omitempty"`
	End   Position       `json:"end,omitempty"`
	Hash  string         `json:"hash,omitempty"`
	Props map[string]any `json:"props,omitempty"`
}

type Edge struct {
	ID    string         `json:"id"`
	Kind  EdgeKind       `json:"kind"`
	From  string         `json:"from"`
	To    string         `json:"to"`
	Pos   Position       `json:"pos,omitempty"`
	Props map[string]any `json:"props,omitempty"`
}
