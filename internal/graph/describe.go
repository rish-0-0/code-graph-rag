package graph

// Schema describes the node/edge vocabulary of the graph plus the stable ID
// conventions used by this indexer. It is written as the first line of every
// persisted graph so the file is self-describing — readers (including LLMs
// authoring Cypher) can learn the vocabulary without scraping the code.
type Schema struct {
	Version       string          `json:"version"`
	NodeKinds     []KindDoc       `json:"nodeKinds"`
	EdgeKinds     []KindDoc       `json:"edgeKinds"`
	IDConventions map[string]string `json:"idConventions"`
	SampleQueries []SampleQuery   `json:"sampleQueries"`
}

type KindDoc struct {
	Name string `json:"name"`
	Doc  string `json:"doc"`
}

type SampleQuery struct {
	Name   string `json:"name"`
	Cypher string `json:"cypher"`
}

// Describe returns the canonical schema for this version of codegraph.
func Describe() Schema {
	return Schema{
		Version: "1",
		NodeKinds: []KindDoc{
			{string(NodeWorkspace), "A go.work workspace grouping modules"},
			{string(NodeModule), "A Go module discovered on disk (has a go.mod)"},
			{string(NodeModuleVersion), "A referenced module + version string; may be unresolved (external) or resolve to a local Module"},
			{string(NodePackage), "A Go package (importable)"},
			{string(NodeFile), "A single .go file"},
			{string(NodeImport), "An external (unresolved) import path referenced by a package"},
			{string(NodeType), "A named type (struct, alias, named)"},
			{string(NodeInterface), "An interface type"},
			{string(NodeField), "A field of a struct type"},
			{string(NodeFunction), "A top-level function"},
			{string(NodeMethod), "A method (function with receiver)"},
			{string(NodeReceiver), "Receiver declaration of a method"},
			{string(NodeParam), "Parameter of a function/method"},
			{string(NodeResult), "Result (return) of a function/method"},
			{string(NodeVariable), "Package-level variable"},
			{string(NodeConstant), "Package-level constant"},
			{string(NodeTypeParam), "Generic type parameter"},
			{string(NodeLiteral), "Notable literal value"},
		},
		EdgeKinds: []KindDoc{
			{string(EdgeContains), "Parent → child containment (module→package, package→file, file→symbol)"},
			{string(EdgeDeclares), "File → symbol declaration"},
			{string(EdgeImports), "Package → import target (external Import node OR local Package node)"},
			{string(EdgeCalls), "Function/Method → the Function/Method it invokes (with call-site position)"},
			{string(EdgeReferences), "Generic symbol reference that is not a call"},
			{string(EdgeImplements), "Type → Interface it satisfies"},
			{string(EdgeEmbeds), "Type embeds another type"},
			{string(EdgeHasMethod), "Type → its method"},
			{string(EdgeHasField), "Type → its field"},
			{string(EdgeHasParam), "Function → param"},
			{string(EdgeReturns), "Function → result"},
			{string(EdgeOfType), "Symbol → its declared type"},
			{string(EdgeAssigns), "Assignment edge"},
			{string(EdgeInstantiates), "Generic → concrete instantiation"},
			{string(EdgeSatisfiesConstraint), "Type → type constraint it satisfies"},
			{string(EdgeReads), "Function/Method → variable or chan it reads"},
			{string(EdgeWrites), "Function/Method → variable or chan it writes"},
			{string(EdgeRequires), "Module → ModuleVersion in its require block"},
			{string(EdgeReplaces), "Module → ModuleVersion or Module target of a replace directive"},
			{string(EdgeResolvesTo), "ModuleVersion → local Module it was resolved to"},
			{string(EdgeWorkspaces), "Workspace → Module member via go.work"},
		},
		IDConventions: map[string]string{
			"Module":        "mod:<path>@<version>",
			"ModuleVersion": "mod:<path>@<version>  (version may be '*' if unspecified)",
			"Package":       "pkg:<modulePath>@<version>/<importPath>",
			"Function":      "<packageID>.<Name>",
			"Method":        "<packageID>.<TypeName>.<MethodName>",
			"Type":          "<packageID>.<Name>",
			"Field":         "<typeID>.<FieldName>",
			"File":          "file:<absolutePath>",
			"Import":        "imp:<packageID>-><importPath>",
		},
		SampleQueries: []SampleQuery{
			{
				Name:   "direct callers of a function",
				Cypher: `MATCH (c)-[:CALLS]->(f {name:"ExportedFn"}) RETURN c.id`,
			},
			{
				Name:   "everything a package exports that nothing inside the module calls",
				Cypher: `MATCH (p:Package {importPath:"example.com/mod/pkg"})-[:CONTAINS]->(sym) WHERE NOT ( ()-[:CALLS|REFERENCES]->(sym) ) RETURN sym.id`,
			},
			{
				Name:   "types implementing an interface",
				Cypher: `MATCH (t)-[:IMPLEMENTS]->(i:Interface {name:"Talker"}) RETURN t.id`,
			},
			{
				Name:   "cross-module calls (caller module != callee module)",
				Cypher: `MATCH (caller)-[:CALLS]->(callee), (mA:Module)-[:CONTAINS*1..3]->(caller), (mB:Module)-[:CONTAINS*1..3]->(callee) WHERE mA <> mB RETURN mA.path, mB.path, caller.id, callee.id`,
			},
			{
				Name:   "replace-resolved module links",
				Cypher: `MATCH (m:Module)-[:REPLACES]->(target) RETURN m.path, target.path`,
			},
		},
	}
}
