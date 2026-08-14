package analysis

import (
	"cmp"
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"
	"slices"
	"strings"

	"golang.org/x/tools/go/packages"
)

// IgnoreDirective exempts the declaration it precedes. A reason is required —
// the escape hatch is what decides whether the tool is adoptable on a real
// codebase, and one sentence of justification is what keeps it from becoming a
// way to make the score go up (PRD 9.2).
//
//	//tarp:ignore -- talks to a live payment processor; covered by the e2e suite
const IgnoreDirective = "//tarp:ignore"

// mainPackage is the package name whose main function is an entrypoint rather
// than a function anybody would unit test.
const mainPackage = "main"

// declKey identifies a function by where it is declared. Object identity is not
// usable here: the same function yields different *types.Func pointers in the
// source variant and the test variants of its package, so references are unioned
// by position instead (PRD 4.2).
type declKey = token.Position

// declaration is one function that the tool holds to the standard. endLine is
// the line of the declaration's last token, so a consumer holding a line number
// — a coverage block, say — can ask which function it fell inside.
type declaration struct {
	// pkgName is the package clause name, which is what decides whether a
	// declaration is exempt for being main. pkgPath is the import path, which
	// is what identifies a package: a module of any size has several packages
	// named config, and only the path tells them apart.
	pkgName string
	pkgPath string
	dir     string
	slot    string
	name    string
	key     declKey
	endLine int
}

// warning is something the user should know about their own source, rendered
// into a message once the analyzed directory is known.
type warning struct {
	file string
	name string
	line int
}

// collectDeclarations gathers every function the report can hold accountable,
// along with warnings about directives that were not honored.
func collectDeclarations(fset *token.FileSet, pkgs []*packages.Package) (map[declKey]*declaration, []warning) {
	declarations := make(map[declKey]*declaration)
	warnings := make([]warning, 0)

	// Package variants repeat the same source files, so iterate in a fixed order
	// and let the first (shortest, unbracketed) path win — the warnings and
	// package labels then do not depend on load order.
	ordered := slices.Clone(pkgs)
	slices.SortFunc(ordered, func(a, b *packages.Package) int {
		return cmp.Or(cmp.Compare(len(a.PkgPath), len(b.PkgPath)), cmp.Compare(a.PkgPath, b.PkgPath))
	})

	for _, pkg := range ordered {
		for _, file := range pkg.Syntax {
			filename := fset.Position(file.Package).Filename
			if isTestFile(filename) || ast.IsGenerated(file) {
				continue
			}

			for _, decl := range file.Decls {
				funcDecl, ok := decl.(*ast.FuncDecl)
				if !ok {
					continue
				}

				collectDeclaration(fset, pkg, funcDecl, declarations, &warnings)
			}
		}
	}

	slices.SortFunc(warnings, func(a, b warning) int {
		return cmp.Or(cmp.Compare(a.file, b.file), cmp.Compare(a.line, b.line), cmp.Compare(a.name, b.name))
	})

	return declarations, slices.Compact(warnings)
}

// collectDeclaration records a single function declaration unless it is exempt.
func collectDeclaration(
	fset *token.FileSet,
	pkg *packages.Package,
	funcDecl *ast.FuncDecl,
	declarations map[declKey]*declaration,
	warnings *[]warning,
) {
	fn, ok := pkg.TypesInfo.Defs[funcDecl.Name].(*types.Func)
	if !ok {
		return
	}

	if isNeverReported(fn, funcDecl, pkg.Name) {
		return
	}

	position := fset.Position(fn.Pos())

	reason, directed := ignoreDirective(funcDecl.Doc)
	if directed {
		if reason != "" {
			return
		}

		*warnings = append(*warnings, warning{
			file: position.Filename,
			line: position.Line,
			name: funcName(fn),
		})
	}

	if _, exists := declarations[position]; exists {
		return
	}

	declarations[position] = &declaration{
		key:     position,
		pkgName: strings.TrimSuffix(pkg.Name, "_test"),
		pkgPath: strings.TrimSuffix(pkg.PkgPath, "_test"),
		dir:     filepath.Dir(position.Filename),
		slot:    strings.TrimSuffix(filepath.Base(position.Filename), ".go"),
		name:    funcName(fn),
		endLine: fset.Position(funcDecl.End()).Line,
	}
}

// isNeverReported covers the declarations that are always exempt: nobody writes
// a unit test for init or for main, and demanding one only teaches users to
// distrust the report (PRD 3.7).
func isNeverReported(fn *types.Func, funcDecl *ast.FuncDecl, pkgName string) bool {
	if funcDecl.Recv != nil {
		return false
	}

	switch fn.Name() {
	case "init":
		return true
	case mainPackage:
		return pkgName == mainPackage
	default:
		return false
	}
}

// ignoreDirective reports whether doc carries the ignore directive, along with
// the reason given for it. A directive without a reason is reported as found
// with an empty reason so the caller can warn rather than silently obey.
func ignoreDirective(doc *ast.CommentGroup) (string, bool) {
	if doc == nil {
		return "", false
	}

	for _, comment := range doc.List {
		if !strings.HasPrefix(comment.Text, IgnoreDirective) {
			continue
		}

		reason := strings.TrimPrefix(comment.Text, IgnoreDirective)
		reason = strings.TrimLeft(reason, " \t")
		reason = strings.TrimPrefix(reason, "--")
		reason = strings.TrimPrefix(reason, ":")

		return strings.TrimSpace(reason), true
	}

	return "", false
}

// funcName renders a function the way Go documentation does: Foo, Thing.Method,
// or (*Thing).Method.
func funcName(fn *types.Func) string {
	signature, ok := fn.Type().(*types.Signature)
	if !ok || signature.Recv() == nil {
		return fn.Name()
	}

	recv := types.TypeString(signature.Recv().Type(), func(*types.Package) string { return "" })
	if strings.HasPrefix(recv, "*") {
		return "(" + recv + ")." + fn.Name()
	}

	return recv + "." + fn.Name()
}

// isTestFile reports whether the named file is a Go test file.
func isTestFile(filename string) bool {
	return strings.HasSuffix(filename, "_test.go")
}
