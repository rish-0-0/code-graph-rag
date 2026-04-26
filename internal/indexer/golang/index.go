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
	"runtime"
	"sort"
	"strings"
	"sync"

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

// Reporter receives stage/message events from the indexer so callers can show
// progress. Nil-safe via the reportf helper.
type Reporter interface {
	Event(stage, msg string)
}

type loadedPkg struct {
	pkg    *packages.Package
	module *discover.Module
	pkgID  string // pkg:<modPath>@<ver>/<importPath>
	modID  string
}

func reportf(r Reporter, stage, format string, args ...any) {
	if r == nil {
		return
	}
	r.Event(stage, fmt.Sprintf(format, args...))
}

// Index loads Go packages for each discovered module and emits nodes/edges.
func Index(g graph.Graph, res *discover.Result, pattern string) error {
	return IndexWithReporter(g, res, pattern, nil)
}

// IndexWithReporter is Index plus progress events. Pass nil for silent mode.
func IndexWithReporter(g graph.Graph, res *discover.Result, pattern string, r Reporter) error {
	if res == nil {
		return nil
	}

	// Pass 1: load every module in parallel. packages.Load is itself
	// internally concurrent, so cap workers to avoid oversubscription.
	workers := runtime.GOMAXPROCS(0) / 2
	if workers < 2 {
		workers = 2
	}
	if workers > len(res.Modules) {
		workers = len(res.Modules)
	}
	if workers < 1 {
		workers = 1
	}

	type loadResult struct {
		idx  int
		mod  *discover.Module
		pkgs []*packages.Package
		err  error
	}

	total := len(res.Modules)
	reportf(r, "discover", "found %d modules", total)

	jobs := make(chan int, total)
	results := make(chan loadResult, total)
	var wg sync.WaitGroup
	for w := 0; w < workers; w++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				m := res.Modules[i]
				reportf(r, "load", "[%d/%d] %s", i+1, total, m.Path)
				cfg := &packages.Config{Mode: loadMode, Dir: m.Dir}
				pkgs, err := packages.Load(cfg, pattern)
				results <- loadResult{idx: i, mod: m, pkgs: pkgs, err: err}
			}
		}()
	}
	for i := range res.Modules {
		jobs <- i
	}
	close(jobs)
	go func() { wg.Wait(); close(results) }()

	// Merge in deterministic module order (first-wins preserved on dup
	// import paths).
	collected := make([]loadResult, total)
	for lr := range results {
		if lr.err != nil {
			return fmt.Errorf("load %s: %w", lr.mod.Path, lr.err)
		}
		collected[lr.idx] = lr
	}

	byImport := make(map[string]*loadedPkg, total*64)
	for _, lr := range collected {
		modID := discover.ModuleID(lr.mod.Path, lr.mod.Version)
		for _, p := range lr.pkgs {
			if p == nil || p.Types == nil {
				continue
			}
			if _, exists := byImport[p.PkgPath]; exists {
				continue
			}
			byImport[p.PkgPath] = &loadedPkg{
				pkg:    p,
				module: lr.mod,
				modID:  modID,
				pkgID:  fmt.Sprintf("pkg:%s@%s/%s", lr.mod.Path, lr.mod.Version, p.PkgPath),
			}
		}
	}

	// Pass 2: emit. Iterate in sorted order so progress events (and any
	// downstream consumers) see a stable sequence. Release AST + TypesInfo
	// per package immediately after emit so GC can reclaim them — cross-
	// package edge resolution only reads strings (pkgID, name).
	paths := make([]string, 0, len(byImport))
	for p := range byImport {
		paths = append(paths, p)
	}
	sort.Strings(paths)

	for i, path := range paths {
		lp := byImport[path]
		reportf(r, "index", "[%d/%d] %s", i+1, len(paths), path)
		emitPackage(g, lp, byImport)
		// Drop heavy fields; keep pkgID/modID/name on loadedPkg for any
		// future lookup. Types is retained only as long as this lp is
		// alive elsewhere — but emitPackage no longer touches other pkgs'
		// AST/TypesInfo, so dropping here is safe.
		lp.pkg.Syntax = nil
		lp.pkg.TypesInfo = nil
	}
	reportf(r, "done", "indexed %d packages", len(paths))
	return nil
}

// docText returns the trimmed text of a doc comment group, or "" if nil.
func docText(cg *ast.CommentGroup) string {
	if cg == nil {
		return ""
	}
	return strings.TrimSpace(cg.Text())
}

// packageDoc picks the first non-empty File.Doc across the package's syntax.
func packageDoc(p *packages.Package) string {
	for _, f := range p.Syntax {
		if d := docText(f.Doc); d != "" {
			return d
		}
	}
	return ""
}

func emitPackage(g graph.Graph, lp *loadedPkg, all map[string]*loadedPkg) {
	p := lp.pkg
	pkgProps := map[string]any{"importPath": p.PkgPath, "module": lp.module.Path}
	if d := packageDoc(p); d != "" {
		pkgProps["doc"] = d
	}
	g.AddNode(graph.Node{
		ID: lp.pkgID, Kind: graph.NodePackage, Name: p.Name,
		Props: pkgProps,
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
			emitTypeSpec(g, lp, fileID, d, s)
		case *ast.ValueSpec:
			doc := docText(s.Doc)
			if doc == "" {
				doc = docText(d.Doc)
			}
			for _, name := range s.Names {
				kind := graph.NodeVariable
				if d.Tok == token.CONST {
					kind = graph.NodeConstant
				}
				id := symbolID(lp.pkgID, name.Name)
				props := map[string]any{}
				if doc != "" {
					props["doc"] = doc
				}
				g.AddNode(graph.Node{ID: id, Kind: kind, Name: name.Name,
					Pos: position(lp.pkg.Fset, name.Pos()), Props: props})
				g.AddEdge(graph.Edge{Kind: graph.EdgeDeclares, From: fileID, To: id})
				g.AddEdge(graph.Edge{Kind: graph.EdgeContains, From: lp.pkgID, To: id})
			}
		}
	}
}

func emitTypeSpec(g graph.Graph, lp *loadedPkg, fileID string, gd *ast.GenDecl, s *ast.TypeSpec) {
	id := symbolID(lp.pkgID, s.Name.Name)
	kind := graph.NodeType
	if _, isIface := s.Type.(*ast.InterfaceType); isIface {
		kind = graph.NodeInterface
	}
	doc := docText(s.Doc)
	if doc == "" {
		doc = docText(gd.Doc)
	}
	props := map[string]any{}
	if doc != "" {
		props["doc"] = doc
	}
	g.AddNode(graph.Node{ID: id, Kind: kind, Name: s.Name.Name,
		Pos: position(lp.pkg.Fset, s.Name.Pos()), Props: props})
	g.AddEdge(graph.Edge{Kind: graph.EdgeDeclares, From: fileID, To: id})
	g.AddEdge(graph.Edge{Kind: graph.EdgeContains, From: lp.pkgID, To: id})

	if st, ok := s.Type.(*ast.StructType); ok && st.Fields != nil {
		for _, f := range st.Fields.List {
			fdoc := docText(f.Doc)
			if fdoc == "" {
				fdoc = docText(f.Comment)
			}
			for _, fn := range f.Names {
				fid := id + "." + fn.Name
				fprops := map[string]any{}
				if fdoc != "" {
					fprops["doc"] = fdoc
				}
				g.AddNode(graph.Node{ID: fid, Kind: graph.NodeField, Name: fn.Name,
					Pos: position(lp.pkg.Fset, fn.Pos()), Props: fprops})
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
			mdoc := docText(m.Doc)
			if mdoc == "" {
				mdoc = docText(m.Comment)
			}
			for _, mn := range m.Names {
				mid := id + "." + mn.Name
				mprops := map[string]any{}
				if mdoc != "" {
					mprops["doc"] = mdoc
				}
				g.AddNode(graph.Node{ID: mid, Kind: graph.NodeMethod, Name: mn.Name,
					Pos: position(lp.pkg.Fset, mn.Pos()), Props: mprops})
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
	fnProps := map[string]any{}
	if doc := docText(d.Doc); doc != "" {
		fnProps["doc"] = doc
	}
	g.AddNode(graph.Node{ID: id, Kind: kind, Name: d.Name.Name,
		Pos: position(lp.pkg.Fset, d.Name.Pos()), Props: fnProps})
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
	var ifaces []*types.Interface
	var ifaceNames []*types.TypeName
	for _, name := range scope.Names() {
		tn, ok := scope.Lookup(name).(*types.TypeName)
		if !ok {
			continue
		}
		if iface, isIface := tn.Type().Underlying().(*types.Interface); isIface {
			if iface.Empty() {
				continue // every type implements it; don't emit noise
			}
			ifaces = append(ifaces, iface)
			ifaceNames = append(ifaceNames, tn)
		} else {
			typesInScope = append(typesInScope, tn)
		}
	}
	if len(ifaces) == 0 {
		return
	}
	for _, t := range typesInScope {
		// Skip types whose value + pointer method sets are both empty:
		// those can only implement empty interfaces (already filtered).
		valMS := types.NewMethodSet(t.Type())
		ptrMS := types.NewMethodSet(types.NewPointer(t.Type()))
		if valMS.Len() == 0 && ptrMS.Len() == 0 {
			continue
		}
		hasPtrOnly := ptrMS.Len() > valMS.Len()
		typeID := symbolID(lp.pkgID, t.Name())
		for idx, iface := range ifaces {
			if types.Implements(t.Type(), iface) ||
				(hasPtrOnly && types.Implements(types.NewPointer(t.Type()), iface)) {
				ifaceID := symbolID(lp.pkgID, ifaceNames[idx].Name())
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
