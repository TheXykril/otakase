package internal

import (
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
)

// `curd -u` downloaded its replacement binary from the upstream repository
// while every other update path already read this fork, so updating happily
// overwrote a forked build with one that lacks its providers and fixes. The
// repository belongs in one constant, and nothing may name another one.
func TestUpdatePathsUseThisFork(t *testing.T) {
	if DefaultUpdateRepo != "TheXykril/curd" {
		t.Fatalf("updates point at %q, not the fork", DefaultUpdateRepo)
	}

	// A literal owner/repo anywhere in the update path is the bug returning:
	// it would silently win over the constant at whichever call site holds it.
	for _, dir := range []string{".", "../cmd/curd"} {
		fset := token.NewFileSet()
		pkgs, err := parser.ParseDir(fset, dir, func(fi os.FileInfo) bool {
			return !strings.HasSuffix(fi.Name(), "_test.go")
		}, 0)
		if err != nil {
			t.Fatalf("parsing %s: %v", dir, err)
		}
		for _, pkg := range pkgs {
			for name, file := range pkg.Files {
				ast.Inspect(file, func(n ast.Node) bool {
					lit, ok := n.(*ast.BasicLit)
					if !ok || lit.Kind != token.STRING {
						return true
					}
					value, err := strconv.Unquote(lit.Value)
					if err != nil {
						return true
					}
					// The module path is github.com/wraient/curd/... and must stay.
					if strings.Contains(value, "github.com/wraient/curd/") {
						return true
					}
					if strings.EqualFold(value, "wraient/curd") {
						t.Errorf("%s:%d: hardcodes the upstream repo %q; use DefaultUpdateRepo",
							filepath.Base(name), fset.Position(lit.Pos()).Line, value)
					}
					return true
				})
			}
		}
	}
}
