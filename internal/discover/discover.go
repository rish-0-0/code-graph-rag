// Package discover walks a root directory, finds every go.mod (and any go.work),
// parses require/replace directives, and models the resulting module topology
// before any AST work happens. The output is a set of Module / ModuleVersion
// records plus the relationships between them, which downstream indexing uses
// to route cross-module references to the correct local module instead of
// duplicating them as unresolved externals.
package discover

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/mod/modfile"
)

// Options controls discovery behavior.
type Options struct {
	// Module, if non-empty, restricts discovery to the module with this path.
	Module string
	// FollowReplace resolves `replace` directives to local modules when the
	// target is a relative path to another discovered module.
	FollowReplace bool
	// UseGit enriches each module with git commit/tag when available.
	UseGit bool
}

// Module is a discovered Go module on disk.
type Module struct {
	Path    string // module path as declared in go.mod
	Dir     string // absolute directory containing go.mod
	Version string // git tag or "(devel)"
	Commit  string
	Dirty   bool

	Requires []Require // parsed `require` entries
	Replaces []Replace // parsed `replace` entries
}

// Require is a require directive entry.
type Require struct {
	Path    string
	Version string
}

// Replace is a replace directive entry. If NewPath is a relative path,
// NewDir holds its resolved absolute directory; NewLocal points at the
// discovered local Module when FollowReplace is enabled.
type Replace struct {
	OldPath    string
	OldVersion string
	NewPath    string
	NewVersion string
	NewDir     string  // set if NewPath was a local filesystem path
	NewLocal   *Module // set if resolved to a discovered local module
}

// Workspace is a go.work file, if any.
type Workspace struct {
	Dir     string
	Modules []*Module
}

// Result bundles everything discovered under a root.
type Result struct {
	Root      string
	Modules   []*Module
	Workspace *Workspace
}

// Discover walks root and returns the module topology.
func Discover(root string, opts Options) (*Result, error) {
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, err
	}
	res := &Result{Root: absRoot}

	if err := filepath.WalkDir(absRoot, func(path string, d os.DirEntry, err error) error {
		if err != nil {
			return nil
		}
		if d.IsDir() {
			name := d.Name()
			if name == ".git" || name == "vendor" || name == "node_modules" {
				return filepath.SkipDir
			}
			return nil
		}
		if d.Name() != "go.mod" {
			return nil
		}
		m, err := parseModule(filepath.Dir(path))
		if err != nil {
			return fmt.Errorf("%s: %w", path, err)
		}
		if opts.Module != "" && m.Path != opts.Module {
			return nil
		}
		if opts.UseGit {
			enrichGit(m)
		}
		res.Modules = append(res.Modules, m)
		return nil
	}); err != nil {
		return nil, err
	}

	if ws, err := findWorkspace(absRoot, res.Modules); err == nil && ws != nil {
		res.Workspace = ws
	}

	if opts.FollowReplace {
		resolveReplaces(res.Modules)
	}
	return res, nil
}

func parseModule(dir string) (*Module, error) {
	gomodPath := filepath.Join(dir, "go.mod")
	data, err := os.ReadFile(gomodPath)
	if err != nil {
		return nil, err
	}
	f, err := modfile.Parse(gomodPath, data, nil)
	if err != nil {
		return nil, err
	}
	m := &Module{
		Path:    f.Module.Mod.Path,
		Dir:     dir,
		Version: "(devel)",
	}
	for _, r := range f.Require {
		m.Requires = append(m.Requires, Require{Path: r.Mod.Path, Version: r.Mod.Version})
	}
	for _, r := range f.Replace {
		rep := Replace{
			OldPath:    r.Old.Path,
			OldVersion: r.Old.Version,
			NewPath:    r.New.Path,
			NewVersion: r.New.Version,
		}
		if isLocalPath(r.New.Path) {
			rep.NewDir = filepath.Clean(filepath.Join(dir, r.New.Path))
		}
		m.Replaces = append(m.Replaces, rep)
	}
	return m, nil
}

func isLocalPath(p string) bool {
	return strings.HasPrefix(p, "./") || strings.HasPrefix(p, "../") || strings.HasPrefix(p, "/") ||
		(len(p) >= 2 && p[1] == ':') // windows drive
}

func resolveReplaces(mods []*Module) {
	byDir := map[string]*Module{}
	for _, m := range mods {
		byDir[filepath.Clean(m.Dir)] = m
	}
	for _, m := range mods {
		for i := range m.Replaces {
			rep := &m.Replaces[i]
			if rep.NewDir == "" {
				continue
			}
			if target, ok := byDir[filepath.Clean(rep.NewDir)]; ok {
				rep.NewLocal = target
			}
		}
	}
}

func findWorkspace(root string, mods []*Module) (*Workspace, error) {
	// Look for go.work at root.
	p := filepath.Join(root, "go.work")
	data, err := os.ReadFile(p)
	if err != nil {
		return nil, nil
	}
	f, err := modfile.ParseWork(p, data, nil)
	if err != nil {
		return nil, err
	}
	ws := &Workspace{Dir: root}
	byDir := map[string]*Module{}
	for _, m := range mods {
		byDir[filepath.Clean(m.Dir)] = m
	}
	for _, u := range f.Use {
		abs := filepath.Clean(filepath.Join(root, u.Path))
		if m, ok := byDir[abs]; ok {
			ws.Modules = append(ws.Modules, m)
		}
	}
	return ws, nil
}
