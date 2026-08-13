package analysis

import (
	"go/ast"
	"go/token"
	"go/types"
	"path/filepath"
	"strings"
	"unicode"

	"golang.org/x/tools/go/packages"
)

// internalTestSuffix names the second file of a test slot. Go forbids
// package foo_test from referencing unexported identifiers in package foo, so
// when foo_test.go is external, foo_internal_test.go is the only place an
// unexported function can be tested from — the pair is the slot (PRD 3.3).
const internalTestSuffix = "_internal"

// refSite records where a reference was found. Everything the strictness dial
// needs to decide is in here: which directory the referring test file lives in,
// which test slot it fills, and whether the reference sat lexically inside a
// TestXxx body rather than in a helper.
type refSite struct {
	dir      string
	slot     string
	inTestFn bool
}

// refIndex maps a declaration to every place a test file mentions it.
type refIndex map[declKey]map[refSite]struct{}

// satisfies reports whether the recorded references are close enough to decl to
// count as a direct test at the given strictness.
func (r refIndex) satisfies(decl *declaration, strictness Strictness) bool {
	for site := range r[decl.key] {
		// A test file always sits in the same directory as the package it
		// tests, so a reference from elsewhere is another package's business.
		if site.dir != decl.dir {
			continue
		}

		switch strictness {
		case StrictnessAny:
			return true
		case StrictnessPackage:
			if site.inTestFn {
				return true
			}
		case StrictnessFile:
			if site.inTestFn && site.slot == decl.slot {
				return true
			}
		}
	}

	return false
}

// collector walks test files and records what their identifiers resolve to.
type collector struct {
	fset  *token.FileSet
	index refIndex
}

// collectReferences builds the reference index for every loaded package variant.
func collectReferences(fset *token.FileSet, pkgs []*packages.Package) refIndex {
	c := &collector{fset: fset, index: make(refIndex)}

	for _, pkg := range pkgs {
		c.collectPackage(pkg)
	}

	return c.index
}

// collectPackage records references from every test file in one package
// variant. Each file is walked twice: once wholesale, which is what `any`
// accepts, and once per TestXxx body, which is what `file` and `package`
// require.
func (c *collector) collectPackage(pkg *packages.Package) {
	testFiles := make([]*ast.File, 0, len(pkg.Syntax))

	for _, file := range pkg.Syntax {
		if isTestFile(c.fset.Position(file.Package).Filename) {
			testFiles = append(testFiles, file)
		}
	}

	if len(testFiles) == 0 {
		return
	}

	tables := packageLevelVars(pkg, testFiles)

	for _, file := range testFiles {
		site := c.siteFor(file)

		c.walk(pkg, file, site)

		for _, decl := range file.Decls {
			funcDecl, ok := decl.(*ast.FuncDecl)
			if !ok || !isTestFunc(funcDecl, pkg.TypesInfo) {
				continue
			}

			testSite := site
			testSite.inTestFn = true

			c.walkTestBody(pkg, funcDecl.Body, testSite, tables)
		}
	}
}

// siteFor derives the directory and test slot a file belongs to.
func (c *collector) siteFor(file *ast.File) refSite {
	filename := c.fset.Position(file.Package).Filename
	slot := strings.TrimSuffix(filepath.Base(filename), "_test.go")

	return refSite{
		dir:  filepath.Dir(filename),
		slot: strings.TrimSuffix(slot, internalTestSuffix),
	}
}

// walkTestBody records references inside a TestXxx body, taking exactly one hop
// into the initializers of package-level vars declared in test files so that
// package-level test tables count (PRD 3.5). The hop is deliberately not
// recursive: following calls arbitrarily would quietly collapse `file` mode
// into `any`.
func (c *collector) walkTestBody(pkg *packages.Package, body ast.Node, site refSite, tables map[*types.Var][]ast.Expr) {
	if body == nil {
		return
	}

	ast.Inspect(body, func(node ast.Node) bool {
		ident, ok := node.(*ast.Ident)
		if !ok {
			return true
		}

		switch used := pkg.TypesInfo.Uses[ident].(type) {
		case *types.Func:
			c.record(used, site)
		case *types.Var:
			for _, initializer := range tables[used] {
				c.walk(pkg, initializer, site)
			}
		}

		return true
	})
}

// walk records every function reference under node without following anything.
func (c *collector) walk(pkg *packages.Package, node ast.Node, site refSite) {
	ast.Inspect(node, func(n ast.Node) bool {
		ident, ok := n.(*ast.Ident)
		if !ok {
			return true
		}

		if fn, isFunc := pkg.TypesInfo.Uses[ident].(*types.Func); isFunc {
			c.record(fn, site)
		}

		return true
	})
}

// record credits a reference, normalizing generic instantiations back to the
// generic they came from and resolving interface dispatch where the answer is
// knowable.
func (c *collector) record(fn *types.Func, site refSite) {
	fn = fn.Origin()

	if signature, ok := fn.Type().(*types.Signature); ok && signature.Recv() != nil {
		if iface, isInterface := signature.Recv().Type().Underlying().(*types.Interface); isInterface {
			sole := soleImplementation(fn, iface)
			if sole == nil {
				// Two or more types implement the interface (or none do), so
				// there is no honest way to say whose method ran.
				return
			}

			fn = sole
		}
	}

	position := c.fset.Position(fn.Pos())
	if !position.IsValid() {
		return
	}

	sites, known := c.index[position]
	if !known {
		sites = make(map[refSite]struct{})
		c.index[position] = sites
	}

	sites[site] = struct{}{}
}

// soleImplementation returns the single named type's method satisfying the
// interface method, or nil when the answer is ambiguous. This is rung one of
// PRD 3.6: a cheap, explainable heuristic in place of whole-program analysis
// that would cost more than the entire run's time budget.
func soleImplementation(method *types.Func, iface *types.Interface) *types.Func {
	pkg := method.Pkg()
	if pkg == nil {
		return nil
	}

	var found []*types.Func

	scope := pkg.Scope()

	for _, name := range scope.Names() {
		typeName, ok := scope.Lookup(name).(*types.TypeName)
		if !ok || typeName.IsAlias() {
			continue
		}

		named, ok := typeName.Type().(*types.Named)
		if !ok {
			continue
		}

		if _, isInterface := named.Underlying().(*types.Interface); isInterface {
			continue
		}

		implementation := implementingMethod(named, iface, method)
		if implementation != nil {
			found = append(found, implementation)
		}
	}

	if len(found) != 1 {
		return nil
	}

	return found[0]
}

// implementingMethod returns named's implementation of method, considering both
// the value and pointer receiver forms, or nil when named does not implement
// the interface at all.
func implementingMethod(named *types.Named, iface *types.Interface, method *types.Func) *types.Func {
	candidate := types.Type(named)
	if !types.Implements(candidate, iface) {
		candidate = types.NewPointer(named)
		if !types.Implements(candidate, iface) {
			return nil
		}
	}

	selection, _, _ := types.LookupFieldOrMethod(candidate, false, method.Pkg(), method.Name())

	implementation, ok := selection.(*types.Func)
	if !ok {
		return nil
	}

	return implementation.Origin()
}

// packageLevelVars maps every package-level var declared in a test file to its
// initializer expressions, which is what the one-hop rule follows into.
func packageLevelVars(pkg *packages.Package, testFiles []*ast.File) map[*types.Var][]ast.Expr {
	vars := make(map[*types.Var][]ast.Expr)

	for _, file := range testFiles {
		for _, decl := range file.Decls {
			genDecl, ok := decl.(*ast.GenDecl)
			if !ok || genDecl.Tok != token.VAR {
				continue
			}

			for _, spec := range genDecl.Specs {
				valueSpec, isValueSpec := spec.(*ast.ValueSpec)
				if !isValueSpec {
					continue
				}

				recordValueSpec(pkg, valueSpec, vars)
			}
		}
	}

	return vars
}

// recordValueSpec associates each name in a var spec with the expressions that
// initialize it.
func recordValueSpec(pkg *packages.Package, spec *ast.ValueSpec, vars map[*types.Var][]ast.Expr) {
	for i, name := range spec.Names {
		declared, ok := pkg.TypesInfo.Defs[name].(*types.Var)
		if !ok {
			continue
		}

		switch {
		case len(spec.Values) == len(spec.Names):
			vars[declared] = []ast.Expr{spec.Values[i]}
		case len(spec.Values) > 0:
			// A multi-value initializer such as `var a, b = f()`: every name
			// shares the same expressions.
			vars[declared] = spec.Values
		}
	}
}

// isTestFunc reports whether decl is a TestXxx function.
//
// TestMain is deliberately excluded: it takes *testing.M, sets up the process,
// and asserts nothing, so a reference from it is not evidence that anything was
// tested. It still counts at `any`, where every reference in a test file does.
func isTestFunc(decl *ast.FuncDecl, info *types.Info) bool {
	if decl.Recv != nil || decl.Body == nil || !strings.HasPrefix(decl.Name.Name, "Test") {
		return false
	}

	if rest := strings.TrimPrefix(decl.Name.Name, "Test"); rest != "" && unicode.IsLower(rune(rest[0])) {
		return false
	}

	fn, ok := info.Defs[decl.Name].(*types.Func)
	if !ok {
		return false
	}

	signature, ok := fn.Type().(*types.Signature)
	if !ok || signature.Params().Len() != 1 || signature.Results().Len() != 0 {
		return false
	}

	pointer, ok := signature.Params().At(0).Type().(*types.Pointer)
	if !ok {
		return false
	}

	named, ok := pointer.Elem().(*types.Named)
	if !ok {
		return false
	}

	return named.Obj().Name() == "T" && named.Obj().Pkg() != nil && named.Obj().Pkg().Path() == "testing"
}
