package analysis

import (
	"go/ast"
	"go/importer"
	"go/parser"
	"go/token"
	"go/types"
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// checked is a type-checked snippet, which is all several of these helpers need
// to be exercised honestly: they take go/types objects, so handing them
// hand-built stubs would test the stubs.
type checked struct {
	file *ast.File
	info *types.Info
	pkg  *types.Package
	fset *token.FileSet
}

// checkSource type-checks a single-file package, importing dependencies from
// source so a snippet may use the standard library.
func checkSource(t *testing.T, src string) checked {
	t.Helper()

	fset := token.NewFileSet()

	file, err := parser.ParseFile(fset, "main.go", src, parser.ParseComments)
	must.NoError(t, err)

	info := &types.Info{
		Defs: make(map[*ast.Ident]types.Object),
		Uses: make(map[*ast.Ident]types.Object),
	}

	config := types.Config{Importer: importer.ForCompiler(fset, "source", nil)}

	pkg, err := config.Check("example", fset, []*ast.File{file}, info)
	must.NoError(t, err)

	return checked{file: file, info: info, pkg: pkg, fset: fset}
}

// funcDecl returns the named declaration from a checked snippet.
func (c checked) funcDecl(t *testing.T, name string) *ast.FuncDecl {
	t.Helper()

	for _, decl := range c.file.Decls {
		if funcDecl, ok := decl.(*ast.FuncDecl); ok && funcDecl.Name.Name == name {
			return funcDecl
		}
	}

	t.Fatalf("no declaration named %q in snippet", name)

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

func TestIsTestFile(t *testing.T) {
	t.Parallel()

	test.True(t, isTestFile("/src/foo_test.go"))
	test.True(t, isTestFile("/src/foo_internal_test.go"))
	test.False(t, isTestFile("/src/foo.go"))
	test.False(t, isTestFile("/src/test.go"))
}
