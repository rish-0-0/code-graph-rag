// Package golangidx indexes Go source into the graph using go/packages +
// go/types. It loads each discovered module separately (go/packages errors
// on multi-module roots), then runs a second pass that emits nodes/edges
// with IDs resolved against the cross-module package map. That map is what
// lets a CALLS edge from module B land on module A's actual Function node
// instead of a dangling placeholder.
package golangidx

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"strings"

	"github.com/rish-0-0/code-graph-rag/internal/discover"
	"github.com/rish-0-0/code-graph-rag/internal/graph"
	"golang.org/x/tools/go/packages"
)

const loadMode = packages.NeedName |
	packages.NeedFiles |
	packages.NeedCompiledGoFiles |
	packages.NeedImports |
	packages.NeedDeps |
	packages.NeedTypes |
	packages.NeedTypesInfo |
	packages.NeedSyntax |
	packages.NeedModule

type loadedPkg struct {
	pkg     *packages.Package
	module  *discover.Module
	pkgID   string // pkg:<modPath>@<ver>/<importPath>
	modID   string
}

// Index loads Go packages for each discovered module and emits nodes/edges.
func Index(g graph.Graph, res *discover.Result, pattern string) error {
	if res == nil {
		return nil
	}
	// Pass 1: load every module, record (importPath -> loadedPkg).
	byImport := map[string]*loadedPkg{}
	for _, m := range res.Modules {
		cfg := &packages.Config{Mode: loadMode, Dir: m.Dir}
		pkgs, err := packages.Load(cfg, pattern)
		if err != nil {
			return fmt.Errorf("load %s: %w", m.Path, err)
		}
		modID := discover.ModuleID(m.Path, m.Version)
		for _, p := range pkgs {
			if p == nil || p.Types == nil {
				continue
			}
			lp := &loadedPkg{
				pkg:    p,
				module: m,
				modID:  modID,
				pkgID:  fmt.Sprintf("pkg:%s@%s/%s", m.Path, m.Version, p.PkgPath),
			}
			if _, exists := byImport[p.PkgPath]; !exists {
				byImport[p.PkgPath] = lp
			}
		}
	}
	// Pass 2: emit.
	for _, lp := range byImport {
		emitPackage(g, lp, byImport)
	}
	return nil
}

func emitPackage(g graph.Graph, lp *loadedPkg, all map[string]*loadedPkg) {
	p := lp.pkg
	g.AddNode(graph.Node{
		ID: lp.pkgID, Kind: graph.NodePackage, Name: p.Name,
		Props: map[string]any{"importPath": p.PkgPath, "module": lp.module.Path},
	})
	g.AddEdge(graph.Edge{Kind: graph.EdgeContains, From: lp.modID, To: lp.pkgID})

	for i, file := range p.Syntax {
		fileName := ""
		if i < len(p.CompiledGoFiles) {
			fileName = p.CompiledGoFiles[i]
		} else if i < len(p.GoFiles) {
			fileName = p.GoFiles[i]
		}
		fileID := "file:" + fileName
		g.AddNode(graph.Node{ID: fileID, Kind: graph.NodeFile, Name: fileName,
			Props: map[string]any{"path": fileName}})
		g.AddEdge(graph.Edge{Kind: graph.EdgeContains, From: lp.pkgID, To: fileID})

		for _, imp := range file.Imports {
			impPath := strings.Trim(imp.Path.Value, `"`)
			// If we loaded this import path as a local package, link to its real pkg node.
			if target, ok := all[impPath]; ok {
				g.AddEdge(graph.Edge{Kind: graph.EdgeImports, From: lp.pkgID, To: target.pkgID,
					Pos: position(p.Fset, imp.Pos())})
			} else {
				impID := fmt.Sprintf("imp:%s->%s", lp.pkgID, impPath)
				g.AddNode(graph.Node{ID: impID, Kind: graph.NodeImport, Name: impPath,
					Props: map[string]any{"path": impPath, "external": true}})
				g.AddEdge(graph.Edge{Kind: graph.EdgeImports, From: lp.pkgID, To: impID,
					Pos: position(p.Fset, imp.Pos())})
			}
		}
		emitDecls(g, lp, all, fileID, file)
	}
	emitInterfaceSatisfaction(g, lp, all)
}

func emitDecls(g graph.Graph, lp *loadedPkg, all map[string]*loadedPkg, fileID string, file *ast.File) {
	p := lp.pkg
	for _, decl := range file.Decls {
		switch d := decl.(type) {
		case *ast.GenDecl:
			emitGenDecl(g, lp, fileID, d)
		case *ast.FuncDecl:
			emitFuncDecl(g, lp, all, fileID, d)
		}
	}
	_ = p
}

func emitGenDecl(g graph.Graph, lp *loadedPkg, fileID string, d *ast.GenDecl) {
	for _, spec := range d.Specs {
		switch s := spec.(type) {
		case *ast.TypeSpec:
			emitTypeSpec(g, lp, fileID, s)
		case *ast.ValueSpec:
			for _, name := range s.Names {
				kind := graph.NodeVariable
				if d.Tok == token.CONST {
					kind = graph.NodeConstant
				}
				id := symbolID(lp.pkgID, name.Name)
				g.AddNode(graph.Node{ID: id, Kind: kind, Name: name.Name,
					Pos: position(lp.pkg.Fset, name.Pos())})
				g.AddEdge(graph.Edge{Kind: graph.EdgeDeclares, From: fileID, To: id})
				g.AddEdge(graph.Edge{Kind: graph.EdgeContains, From: lp.pkgID, To: id})
			}
		}
	}
}

func emitTypeSpec(g graph.Graph, lp *loadedPkg, fileID string, s *ast.TypeSpec) {
	id := symbolID(lp.pkgID, s.Name.Name)
	kind := graph.NodeType
	if _, isIface := s.Type.(*ast.InterfaceType); isIface {
		kind = graph.NodeInterface
	}
	g.AddNode(graph.Node{ID: id, Kind: kind, Name: s.Name.Name,
		Pos: position(lp.pkg.Fset, s.Name.Pos())})
	g.AddEdge(graph.Edge{Kind: graph.EdgeDeclares, From: fileID, To: id})
	g.AddEdge(graph.Edge{Kind: graph.EdgeContains, From: lp.pkgID, To: id})

	if st, ok := s.Type.(*ast.StructType); ok && st.Fields != nil {
		for _, f := range st.Fields.List {
			for _, fn := range f.Names {
				fid := id + "." + fn.Name
				g.AddNode(graph.Node{ID: fid, Kind: graph.NodeField, Name: fn.Name,
					Pos: position(lp.pkg.Fset, fn.Pos())})
				g.AddEdge(graph.Edge{Kind: graph.EdgeHasField, From: id, To: fid})
			}
		}
	}

	// Interface methods: emit Method nodes so CALLS edges to interface-dispatched
	// methods resolve to a real node instead of dangling.
	if it, ok := s.Type.(*ast.InterfaceType); ok && it.Methods != nil {
		for _, m := range it.Methods.List {
			if _, isFunc := m.Type.(*ast.FuncType); !isFunc {
				continue // embedded interface, skip
			}
			for _, mn := range m.Names {
				mid := id + "." + mn.Name
				g.AddNode(graph.Node{ID: mid, Kind: graph.NodeMethod, Name: mn.Name,
					Pos: position(lp.pkg.Fset, mn.Pos())})
				g.AddEdge(graph.Edge{Kind: graph.EdgeHasMethod, From: id, To: mid})
				g.AddEdge(graph.Edge{Kind: graph.EdgeContains, From: lp.pkgID, To: mid})
			}
		}
	}
}

func emitFuncDecl(g graph.Graph, lp *loadedPkg, all map[string]*loadedPkg, fileID string, d *ast.FuncDecl) {
	var id string
	kind := graph.NodeFunction
	if d.Recv != nil && len(d.Recv.List) > 0 {
		kind = graph.NodeMethod
		recvType := receiverTypeName(d.Recv.List[0].Type)
		id = lp.pkgID + "." + recvType + "." + d.Name.Name
		recvID := symbolID(lp.pkgID, recvType)
		g.AddEdge(graph.Edge{Kind: graph.EdgeHasMethod, From: recvID, To: id})
	} else {
		id = symbolID(lp.pkgID, d.Name.Name)
	}
	g.AddNode(graph.Node{ID: id, Kind: kind, Name: d.Name.Name,
		Pos: position(lp.pkg.Fset, d.Name.Pos())})
	g.AddEdge(graph.Edge{Kind: graph.EdgeDeclares, From: fileID, To: id})
	g.AddEdge(graph.Edge{Kind: graph.EdgeContains, From: lp.pkgID, To: id})

	if d.Body == nil {
		return
	}
	ast.Inspect(d.Body, func(n ast.Node) bool {
		call, ok := n.(*ast.CallExpr)
		if !ok {
			return true
		}
		targetID := resolveCallTarget(lp, all, call.Fun)
		if targetID == "" {
			return true
		}
		g.AddEdge(graph.Edge{Kind: graph.EdgeCalls, From: id, To: targetID,
			Pos: position(lp.pkg.Fset, call.Lparen)})
		return true
	})
}

func resolveCallTarget(lp *loadedPkg, all map[string]*loadedPkg, fun ast.Expr) string {
	var ident *ast.Ident
	switch fn := fun.(type) {
	case *ast.Ident:
		ident = fn
	case *ast.SelectorExpr:
		ident = fn.Sel
	default:
		return ""
	}
	obj := lp.pkg.TypesInfo.Uses[ident]
	if obj == nil {
		obj = lp.pkg.TypesInfo.Defs[ident]
	}
	if obj == nil || obj.Pkg() == nil {
		return ""
	}
	return resolveObjectID(obj, all)
}

func resolveObjectID(obj types.Object, all map[string]*loadedPkg) string {
	pkgPath := obj.Pkg().Path()
	owner, ok := all[pkgPath]
	if !ok {
		return "" // external — skip for now
	}
	name := obj.Name()
	if fn, isFn := obj.(*types.Func); isFn {
		if sig, isSig := fn.Type().(*types.Signature); isSig && sig.Recv() != nil {
			recv := sig.Recv().Type()
			if ptr, isPtr := recv.(*types.Pointer); isPtr {
				recv = ptr.Elem()
			}
			if named, isNamed := recv.(*types.Named); isNamed {
				return owner.pkgID + "." + named.Obj().Name() + "." + name
			}
		}
	}
	return symbolID(owner.pkgID, name)
}

// emitInterfaceSatisfaction: for every named type T in this package, for every
// interface I visible in this package's scope (including its own), if T
// implements I, emit an IMPLEMENTS edge.
func emitInterfaceSatisfaction(g graph.Graph, lp *loadedPkg, all map[string]*loadedPkg) {
	scope := lp.pkg.Types.Scope()
	var typesInScope []*types.TypeName
	var ifaces []*types.TypeName
	for _, name := range scope.Names() {
		tn, ok := scope.Lookup(name).(*types.TypeName)
		if !ok {
			continue
		}
		if _, isIface := tn.Type().Underlying().(*types.Interface); isIface {
			ifaces = append(ifaces, tn)
		} else {
			typesInScope = append(typesInScope, tn)
		}
	}
	for _, t := range typesInScope {
		for _, i := range ifaces {
			iface, _ := i.Type().Underlying().(*types.Interface)
			if iface == nil || iface.Empty() {
				continue
			}
			if types.Implements(t.Type(), iface) || types.Implements(types.NewPointer(t.Type()), iface) {
				typeID := symbolID(lp.pkgID, t.Name())
				ifaceID := symbolID(lp.pkgID, i.Name())
				g.AddEdge(graph.Edge{Kind: graph.EdgeImplements, From: typeID, To: ifaceID})
			}
		}
	}
}

func symbolID(pkgID, name string) string {
	return pkgID + "." + name
}

func receiverTypeName(expr ast.Expr) string {
	switch t := expr.(type) {
	case *ast.StarExpr:
		return receiverTypeName(t.X)
	case *ast.Ident:
		return t.Name
	case *ast.IndexExpr:
		return receiverTypeName(t.X)
	case *ast.IndexListExpr:
		return receiverTypeName(t.X)
	}
	return "?"
}

func position(fset *token.FileSet, pos token.Pos) graph.Position {
	if !pos.IsValid() || fset == nil {
		return graph.Position{}
	}
	p := fset.Position(pos)
	return graph.Position{File: p.Filename, Line: p.Line, Column: p.Column}
}
