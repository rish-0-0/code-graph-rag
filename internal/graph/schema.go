package graph

type NodeKind string

const (
	NodeWorkspace     NodeKind = "Workspace"
	NodeModule        NodeKind = "Module"
	NodeModuleVersion NodeKind = "ModuleVersion"
	NodePackage       NodeKind = "Package"
	NodeFile          NodeKind = "File"
	NodeImport        NodeKind = "Import"
	NodeType          NodeKind = "Type"
	NodeField         NodeKind = "Field"
	NodeFunction      NodeKind = "Function"
	NodeMethod        NodeKind = "Method"
	NodeReceiver      NodeKind = "Receiver"
	NodeParam         NodeKind = "Param"
	NodeResult        NodeKind = "Result"
	NodeVariable      NodeKind = "Variable"
	NodeConstant      NodeKind = "Constant"
	NodeTypeParam     NodeKind = "TypeParam"
	NodeInterface     NodeKind = "Interface"
	NodeLiteral       NodeKind = "Literal"
)

type EdgeKind string

const (
	EdgeContains            EdgeKind = "CONTAINS"
	EdgeImports             EdgeKind = "IMPORTS"
	EdgeDeclares            EdgeKind = "DECLARES"
	EdgeCalls               EdgeKind = "CALLS"
	EdgeReferences          EdgeKind = "REFERENCES"
	EdgeImplements          EdgeKind = "IMPLEMENTS"
	EdgeEmbeds              EdgeKind = "EMBEDS"
	EdgeHasMethod           EdgeKind = "HAS_METHOD"
	EdgeHasField            EdgeKind = "HAS_FIELD"
	EdgeHasParam            EdgeKind = "HAS_PARAM"
	EdgeReturns             EdgeKind = "RETURNS"
	EdgeOfType              EdgeKind = "OF_TYPE"
	EdgeAssigns             EdgeKind = "ASSIGNS"
	EdgeInstantiates        EdgeKind = "INSTANTIATES"
	EdgeSatisfiesConstraint EdgeKind = "SATISFIES_CONSTRAINT"
	EdgeReads               EdgeKind = "READS"
	EdgeWrites              EdgeKind = "WRITES"
	EdgeRequires            EdgeKind = "REQUIRES"
	EdgeReplaces            EdgeKind = "REPLACES"
	EdgeResolvesTo          EdgeKind = "RESOLVES_TO"
	EdgeWorkspaces          EdgeKind = "WORKSPACES"
)
