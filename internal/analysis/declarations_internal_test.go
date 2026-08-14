package analysis

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"slices"
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
	"golang.org/x/tools/go/packages"
)

// checked is a type-checked snippet, which is all several of these helpers need
// to be exercised honestly: they take go/types objects, so handing them
// hand-built stubs would test the stubs.
type checked struct {
	info  *types.Info
	pkg   *types.Package
	fset  *token.FileSet
	files []*ast.File
}

// sourceFile is one named file of a snippet. The name is load-bearing rather
// than decoration: the collector reads a reference's directory and test slot
// straight off the filename, and declarations are keyed by position, so a
// helper that called every file main.go could not exercise either.
type sourceFile struct {
	name string
	src  string
}

// checkSource type-checks a single-file package, importing dependencies from
// source so a snippet may use the standard library.
func checkSource(t *testing.T, src string) checked {
	t.Helper()

	return checkFiles(t, sourceFile{name: "main.go", src: src})
}

// checkFiles type-checks a package assembled from named files, in the order
// given.
func checkFiles(t *testing.T, files ...sourceFile) checked {
	t.Helper()

	fset := token.NewFileSet()

	parsed := make([]*ast.File, 0, len(files))

	for i := range files {
		syntax, err := parser.ParseFile(fset, files[i].name, files[i].src, parser.ParseComments)
		must.NoError(t, err, must.Sprintf("parsing %s", files[i].name))

		parsed = append(parsed, syntax)
	}

	info := &types.Info{
		Defs: make(map[*ast.Ident]types.Object),
		Uses: make(map[*ast.Ident]types.Object),
	}

	config := types.Config{Importer: importer.ForCompiler(fset, "source", nil)}

	pkg, err := config.Check("example", fset, parsed, info)
	must.NoError(t, err)

	return checked{files: parsed, info: info, pkg: pkg, fset: fset}
}

// funcDecl returns the named declaration from a checked snippet.
func (c checked) funcDecl(t *testing.T, name string) *ast.FuncDecl {
	t.Helper()

	for _, file := range c.files {
		for _, decl := range file.Decls {
			if funcDecl, ok := decl.(*ast.FuncDecl); ok && funcDecl.Name.Name == name {
				return funcDecl
			}
		}
	}

	t.Fatalf("no declaration named %q in snippet", name)

	return nil
}

// fileNamed returns the file the snippet was given under name.
func (c checked) fileNamed(t *testing.T, name string) *ast.File {
	t.Helper()

	for _, file := range c.files {
		if c.fset.Position(file.Package).Filename == name {
			return file
		}
	}

	t.Fatalf("no file named %q in snippet", name)

	return nil
}

// fn returns the *types.Func for the named declaration.
func (c checked) fn(t *testing.T, name string) *types.Func {
	t.Helper()

	fn, ok := c.info.Defs[c.funcDecl(t, name).Name].(*types.Func)
	must.True(t, ok, must.Sprintf("%s is not a function", name))

	return fn
}

func TestFuncName(t *testing.T) {
	t.Parallel()

	source := checkSource(t, `package example

type Thing struct{}

func Plain() {}

func (Thing) Value() {}

func (*Thing) Pointer() {}

type Box[T any] struct{ v T }

func (Box[T]) Generic() {}
`)

	cases := map[string]string{
		"Plain":   "Plain",
		"Value":   "Thing.Value",
		"Pointer": "(*Thing).Pointer",
		"Generic": "Box[T].Generic",
	}

	for declared, expected := range cases {
		t.Run(expected, func(t *testing.T) {
			t.Parallel()

			test.Eq(t, expected, funcName(source.fn(t, declared)))
		})
	}
}

func TestIsNeverReported(t *testing.T) {
	t.Parallel()

	source := checkSource(t, `package main

type Thing struct{}

func init() {}

func main() {}

func Regular() {}

func (Thing) Init() {}
`)

	t.Run("init and main in package main", func(t *testing.T) {
		t.Parallel()

		test.True(t, isNeverReported(source.fn(t, "init"), source.funcDecl(t, "init"), "main"))
		test.True(t, isNeverReported(source.fn(t, "main"), source.funcDecl(t, "main"), "main"))
	})

	t.Run("main outside package main is an ordinary function", func(t *testing.T) {
		t.Parallel()

		// A library may reasonably export a function called main-something; only
		// the entrypoint is exempt.
		test.False(t, isNeverReported(source.fn(t, "main"), source.funcDecl(t, "main"), "library"))
	})

	t.Run("methods are never exempt by name", func(t *testing.T) {
		t.Parallel()

		test.False(t, isNeverReported(source.fn(t, "Init"), source.funcDecl(t, "Init"), "main"))
		test.False(t, isNeverReported(source.fn(t, "Regular"), source.funcDecl(t, "Regular"), "main"))
	})
}

func TestIgnoreDirective(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		comment  string
		reason   string
		directed bool
	}{
		"absent":                {comment: "// just a comment", reason: "", directed: false},
		"with a dashed reason":  {comment: IgnoreDirective + " -- shells out to a live API", reason: "shells out to a live API", directed: true},
		"with a colon":          {comment: IgnoreDirective + ": shells out to a live API", reason: "shells out to a live API", directed: true},
		"with a bare reason":    {comment: IgnoreDirective + " shells out to a live API", reason: "shells out to a live API", directed: true},
		"with no reason at all": {comment: IgnoreDirective, reason: "", directed: true},
		"with only a dash":      {comment: IgnoreDirective + " --", reason: "", directed: true},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			reason, directed := ignoreDirective(&ast.CommentGroup{
				List: []*ast.Comment{{Text: testCase.comment}},
			})

			test.Eq(t, testCase.directed, directed)
			test.Eq(t, testCase.reason, reason)
		})
	}

	t.Run("no doc comment at all", func(t *testing.T) {
		t.Parallel()

		reason, directed := ignoreDirective(nil)

		test.False(t, directed)
		test.Eq(t, "", reason)
	})
}

// declaredNames renders a collected declaration set as sorted names, so a
// failure says which declaration went missing rather than how many did.
func declaredNames(declarations map[declKey]*declaration) []string {
	names := make([]string, 0, len(declarations))
	for _, decl := range declarations {
		names = append(names, decl.name)
	}

	slices.Sort(names)

	return names
}

func TestCollectDeclarations(t *testing.T) {
	t.Parallel()

	source := checkFiles(t,
		sourceFile{name: "/src/subject.go", src: `package example

func Subject() {}

//tarp:ignore
func Reasonless() {}
`},
		sourceFile{name: "/src/subject_test.go", src: `package example

func helper() {}
`},
		sourceFile{name: "/src/generated.go", src: `// Code generated by tarp. DO NOT EDIT.

package example

func Generated() {}
`},
	)

	t.Run("holds ordinary declarations accountable and nothing else", func(t *testing.T) {
		t.Parallel()

		// A test file's own functions are not the code under test, and generated
		// code is nobody's to write a test for.
		declarations, _ := collectDeclarations(source.fset, []*packages.Package{asPackage(source)})

		test.Eq(t, []string{"Reasonless", "Subject"}, declaredNames(declarations))
	})

	t.Run("labels a declaration with the shortest path that claims it", func(t *testing.T) {
		t.Parallel()

		// Every variant of a package repeats the same source files, so the
		// unbracketed path has to win no matter what order they arrive in.
		variant := asPackage(source)
		variant.PkgPath = "example [example.test]"

		declarations, _ := collectDeclarations(source.fset, []*packages.Package{variant, asPackage(source)})

		recorded, ok := declarations[source.fset.Position(source.fn(t, "Subject").Pos())]
		must.True(t, ok, must.Sprint("Subject was not collected"))
		test.Eq(t, "example", recorded.pkgPath)
	})

	t.Run("warns once about a bare directive seen in every variant", func(t *testing.T) {
		t.Parallel()

		pkg := asPackage(source)

		_, warnings := collectDeclarations(source.fset, []*packages.Package{pkg, pkg, pkg})

		must.SliceLen(t, 1, warnings)
		test.Eq(t, "Reasonless", warnings[0].name)
		test.Eq(t, "/src/subject.go", warnings[0].file)
		test.Eq(t, 6, warnings[0].line)
	})

	t.Run("sorts warnings by file, line, and name", func(t *testing.T) {
		t.Parallel()

		// Two files' worth, handed over in the wrong order, so that a report is
		// the same on every machine.
		unsorted := checkFiles(t,
			sourceFile{name: "/src/zeta.go", src: `package example

//tarp:ignore
func Zeta() {}
`},
			sourceFile{name: "/src/alpha.go", src: `package example

//tarp:ignore
func Second() {}

//tarp:ignore
func First() {}
`},
		)

		_, warnings := collectDeclarations(unsorted.fset, []*packages.Package{asPackage(unsorted)})

		must.SliceLen(t, 3, warnings)
		test.Eq(t, []warning{
			{file: "/src/alpha.go", line: 4, name: "Second"},
			{file: "/src/alpha.go", line: 7, name: "First"},
			{file: "/src/zeta.go", line: 4, name: "Zeta"},
		}, warnings)
	})

	t.Run("collects nothing from nothing", func(t *testing.T) {
		t.Parallel()

		// The warnings are rendered into the report verbatim, where a nil slice
		// would encode as null rather than [].
		declarations, warnings := collectDeclarations(token.NewFileSet(), nil)

		test.MapEmpty(t, declarations)
		test.SliceEmpty(t, warnings)
		test.NotNil(t, warnings)
	})
}

func TestCollectDeclaration(t *testing.T) {
	t.Parallel()

	source := checkFiles(t, sourceFile{name: "/src/subject.go", src: `package example

func Subject() {
	_ = 1
}

//tarp:ignore -- talks to a live payment processor; covered by the e2e suite
func Excused() {}

//tarp:ignore
func Reasonless() {}

func init() {}
`})

	// collect runs collectDeclaration over one of the snippet's declarations,
	// which is all its callers ever do with it.
	collect := func(t *testing.T, name string) (map[declKey]*declaration, []warning) {
		t.Helper()

		declarations := make(map[declKey]*declaration)
		warnings := make([]warning, 0)

		collectDeclaration(source.fset, asPackage(source), source.funcDecl(t, name), declarations, &warnings)

		return declarations, warnings
	}

	t.Run("records where the declaration is, and where it ends", func(t *testing.T) {
		t.Parallel()

		declarations, warnings := collect(t, "Subject")

		must.MapLen(t, 1, declarations)
		test.SliceEmpty(t, warnings)

		recorded, ok := declarations[source.fset.Position(source.fn(t, "Subject").Pos())]
		must.True(t, ok, must.Sprint("Subject was not keyed by its own position"))

		test.Eq(t, "Subject", recorded.name)
		test.Eq(t, "example", recorded.pkgName)
		test.Eq(t, "example", recorded.pkgPath)
		test.Eq(t, "/src", recorded.dir)
		// The slot is the filename without its extension: subject.go is tested by
		// subject_test.go and subject_internal_test.go, and by nothing else.
		test.Eq(t, "subject", recorded.slot)
		test.Eq(t, 3, recorded.key.Line)
		// The end line is what lets a consumer holding a line number — a coverage
		// block — ask which function it fell inside.
		test.Eq(t, 5, recorded.endLine)
	})

	t.Run("obeys a directive that gives a reason", func(t *testing.T) {
		t.Parallel()

		declarations, warnings := collect(t, "Excused")

		test.MapEmpty(t, declarations)
		test.SliceEmpty(t, warnings)
	})

	t.Run("warns about a bare directive and holds the function accountable anyway", func(t *testing.T) {
		t.Parallel()

		// The reason is the whole point of the escape hatch: without one, the
		// directive is a way to make the score go up, so it is not honored.
		declarations, warnings := collect(t, "Reasonless")

		test.Eq(t, []string{"Reasonless"}, declaredNames(declarations))
		must.SliceLen(t, 1, warnings)
		test.Eq(t, warning{file: "/src/subject.go", line: 11, name: "Reasonless"}, warnings[0])
	})

	t.Run("skips what is never reported", func(t *testing.T) {
		t.Parallel()

		declarations, warnings := collect(t, "init")

		test.MapEmpty(t, declarations)
		test.SliceEmpty(t, warnings)
	})

	t.Run("keeps the first record of a position", func(t *testing.T) {
		t.Parallel()

		// The same file is compiled into every variant of its package, so the
		// same declaration arrives repeatedly; the labels must not depend on
		// which arrival was last.
		declarations := make(map[declKey]*declaration)
		warnings := make([]warning, 0)

		variant := asPackage(source)
		variant.Name, variant.PkgPath = "example_test", "example [example.test]"

		collectDeclaration(source.fset, asPackage(source), source.funcDecl(t, "Subject"), declarations, &warnings)
		collectDeclaration(source.fset, variant, source.funcDecl(t, "Subject"), declarations, &warnings)

		must.MapLen(t, 1, declarations)
		test.Eq(t, "example", declarations[source.fset.Position(source.fn(t, "Subject").Pos())].pkgPath)
	})

	t.Run("ignores a declaration the type checker did not resolve", func(t *testing.T) {
		t.Parallel()

		// Nothing in the package's Defs answers for a declaration from somewhere
		// else, and inventing an entry for it would report a function that the
		// analyzed package does not have.
		elsewhere := checkSource(t, `package example

func Stranger() {}
`)

		declarations := make(map[declKey]*declaration)
		warnings := make([]warning, 0)

		collectDeclaration(source.fset, asPackage(source), elsewhere.funcDecl(t, "Stranger"), declarations, &warnings)

		test.MapEmpty(t, declarations)
		test.SliceEmpty(t, warnings)
	})
}

func TestIsTestFile(t *testing.T) {
	t.Parallel()

	test.True(t, isTestFile("/src/foo_test.go"))
	test.True(t, isTestFile("/src/foo_internal_test.go"))
	test.False(t, isTestFile("/src/foo.go"))
	test.False(t, isTestFile("/src/test.go"))
}
