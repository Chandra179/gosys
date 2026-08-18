// Package astsite maps a source location (file + line) reported by a pprof
// profile to the enclosing AST nodes in a loaded repository, so rules can
// inspect the code that produced an allocation.
package astsite

import (
	"fmt"
	"go/ast"
	"go/token"
	"go/types"
	"os"
	"path/filepath"
	"strings"

	"golang.org/x/tools/go/ast/astutil"
	"golang.org/x/tools/go/packages"
)

// Index holds every syntax tree loaded from a repository, keyed for lookup
// by the absolute source file path.
type Index struct {
	Fset  *token.FileSet
	files map[string]*ast.File      // absolute path -> file
	info  map[*ast.File]*types.Info // file -> its package's type info
}

// Load parses and type-checks every package under repoDir (via "./...")
// and returns an Index that can resolve pprof source locations against it.
func Load(repoDir string) (*Index, error) {
	fset := token.NewFileSet()
	cfg := &packages.Config{
		Mode: packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles |
			packages.NeedImports | packages.NeedDeps | packages.NeedSyntax |
			packages.NeedTypes | packages.NeedTypesInfo,
		Dir:  repoDir,
		Fset: fset,
	}
	pkgs, err := packages.Load(cfg, "./...")
	if err != nil {
		return nil, fmt.Errorf("load packages: %w", err)
	}

	idx := &Index{Fset: fset, files: make(map[string]*ast.File), info: make(map[*ast.File]*types.Info)}
	for _, pkg := range pkgs {
		for _, f := range pkg.Syntax {
			pos := fset.Position(f.Pos())
			if pos.Filename == "" {
				continue
			}
			abs, err := filepath.Abs(pos.Filename)
			if err != nil {
				abs = pos.Filename
			}
			idx.files[abs] = f
			if pkg.TypesInfo != nil {
				idx.info[f] = pkg.TypesInfo
			}
		}
	}
	return idx, nil
}

// TypeOf returns the type of expr as determined by the type checker, or nil
// if type information isn't available (e.g. its package failed to
// type-check, or file wasn't loaded by this Index).
func (idx *Index) TypeOf(file *ast.File, expr ast.Expr) types.Type {
	info, ok := idx.info[file]
	if !ok {
		return nil
	}
	return info.TypeOf(expr)
}

// PathAt resolves the pprof-reported file/line to the enclosing AST node
// path (innermost node first) and the *ast.File it lives in. ok is false if
// the file couldn't be matched or the line has no corresponding AST node
// (e.g. it's inside a generated or non-Go frame).
func (idx *Index) PathAt(file string, line int64) (path []ast.Node, astFile *ast.File, ok bool) {
	astFile = idx.resolveFile(file)
	if astFile == nil {
		return nil, nil, false
	}

	tf := idx.Fset.File(astFile.Pos())
	if tf == nil || line < 1 || int(line) > tf.LineCount() {
		return nil, nil, false
	}

	start := tf.LineStart(int(line))
	end := start
	// Extend to the end of the line (or file) so PathEnclosingInterval has
	// a non-empty interval to search, rather than a single position that
	// may fall exactly on a node boundary.
	if int(line) < tf.LineCount() {
		end = tf.LineStart(int(line)+1) - 1
	} else {
		end = astFile.End()
	}

	path, exact := astutil.PathEnclosingInterval(astFile, start, end)
	if !exact && len(path) == 0 {
		return nil, nil, false
	}
	return path, astFile, true
}

// NodeSource returns the raw source text spanning node, by re-reading the
// file it belongs to and slicing it at the node's recorded byte offsets.
func (idx *Index) NodeSource(node ast.Node) (string, error) {
	f := idx.Fset.File(node.Pos())
	if f == nil {
		return "", fmt.Errorf("position not in file set")
	}
	data, err := os.ReadFile(f.Name())
	if err != nil {
		return "", fmt.Errorf("read %s: %w", f.Name(), err)
	}
	start, end := f.Offset(node.Pos()), f.Offset(node.End())
	if start < 0 || end > len(data) || start > end {
		return "", fmt.Errorf("node range out of bounds in %s", f.Name())
	}
	return string(data[start:end]), nil
}

func (idx *Index) resolveFile(file string) *ast.File {
	if f, ok := idx.files[file]; ok {
		return f
	}
	abs, err := filepath.Abs(file)
	if err == nil {
		if f, ok := idx.files[abs]; ok {
			return f
		}
	}
	// Fall back to matching by trailing path components, since pprof
	// embeds whatever path the binary was built with (which may differ
	// from the path the repo is checked out at, e.g. under -trimpath or
	// a different GOPATH).
	want := filepath.ToSlash(file)
	for path, f := range idx.files {
		if strings.HasSuffix(filepath.ToSlash(path), want) || strings.HasSuffix(want, filepath.ToSlash(path)) {
			return f
		}
	}
	base := filepath.Base(file)
	var candidate *ast.File
	matches := 0
	for path, f := range idx.files {
		if filepath.Base(path) == base {
			candidate = f
			matches++
		}
	}
	if matches == 1 {
		return candidate
	}
	return nil
}
