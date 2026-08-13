package analysis

import (
	"go/ast"
	"go/types"
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
	"golang.org/x/tools/go/packages"
)

// asPackage dresses a checked snippet up as the loaded package the collector
// expects.
func asPackage(source checked) *packages.Package {
	return &packages.Package{
		Name:      source.pkg.Name(),
		Types:     source.pkg,
		TypesInfo: source.info,
		Syntax:    []*ast.File{source.file},
	}
}

// declAt builds a declaration standing in for one in /src/foo.go.
func declAt(file, slot string) *declaration {
	return &declaration{
		key:  declKey{Filename: "/src/" + file, Line: 3, Column: 1},
		dir:  "/src",
		slot: slot,
		name: "Subject",
	}
}

func TestRefIndexSatisfies(t *testing.T) {
	t.Parallel()

	decl := declAt("foo.go", "foo")

	cases := map[string]struct {
		accepted map[Strictness]bool
		site     refSite
	}{
		"the declaring file's own test": {
			site: refSite{dir: "/src", slot: "foo", inTestFn: true},
			accepted: map[Strictness]bool{
				StrictnessFile: true, StrictnessPackage: true, StrictnessAny: true,
			},
		},
		"a sibling file's test": {
			site: refSite{dir: "/src", slot: "bar", inTestFn: true},
			accepted: map[Strictness]bool{
				StrictnessFile: false, StrictnessPackage: true, StrictnessAny: true,
			},
		},
		"a helper in the declaring file's test": {
			site: refSite{dir: "/src", slot: "foo", inTestFn: false},
			accepted: map[Strictness]bool{
				StrictnessFile: false, StrictnessPackage: false, StrictnessAny: true,
			},
		},
		"another package's test": {
			// A test file always sits beside the package it tests, so a
			// reference from elsewhere is somebody else's coverage.
			site: refSite{dir: "/other", slot: "foo", inTestFn: true},
			accepted: map[Strictness]bool{
				StrictnessFile: false, StrictnessPackage: false, StrictnessAny: false,
			},
		},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			index := refIndex{decl.key: {testCase.site: struct{}{}}}

			for strictness, expected := range testCase.accepted {
				test.Eq(t, expected, index.satisfies(decl, strictness),
					test.Sprintf("at --strictness=%s", strictness))
			}
		})
	}

	t.Run("no references at all", func(t *testing.T) {
		t.Parallel()

		for _, strictness := range []Strictness{StrictnessFile, StrictnessPackage, StrictnessAny} {
			test.False(t, refIndex{}.satisfies(decl, strictness))
		}
	})
}

func TestIsTestFunc(t *testing.T) {
	t.Parallel()

	source := checkSource(t, `package example

import "testing"

func TestSubject(t *testing.T) {}

func TestMain(m *testing.M) {}

func Testify(t *testing.T) {}

func TestingHelper(t *testing.T) {}

func BenchmarkSubject(b *testing.B) {}

func ExampleSubject() {}

func TestNoParameters() {}
`)

	cases := map[string]bool{
		"TestSubject": true,
		// TestMain sets up the process and asserts nothing, so a reference from
		// it is not evidence that anything was tested.
		"TestMain": false,
		// Go's own rule: the character after "Test" must not be lower case.
		"Testify":          false,
		"TestingHelper":    false,
		"BenchmarkSubject": false,
		"ExampleSubject":   false,
		"TestNoParameters": false,
	}

	for name, expected := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			test.Eq(t, expected, isTestFunc(source.funcDecl(t, name), source.info))
		})
	}
}

func TestSoleImplementation(t *testing.T) {
	t.Parallel()

	t.Run("credits the only implementer", func(t *testing.T) {
		t.Parallel()

		source := checkSource(t, `package example

type Doer interface{ Do() string }

type Impl struct{}

func (Impl) Do() string { return "impl" }
`)

		method := interfaceMethod(t, source, "Doer", "Do")

		sole := soleImplementation(method, ifaceOf(t, source, "Doer"))

		must.NotNil(t, sole)
		test.Eq(t, "Impl.Do", funcName(sole))
	})

	t.Run("credits nobody when two types implement", func(t *testing.T) {
		t.Parallel()

		source := checkSource(t, `package example

type Doer interface{ Do() string }

type First struct{}

func (First) Do() string { return "first" }

type Second struct{}

func (*Second) Do() string { return "second" }
`)

		method := interfaceMethod(t, source, "Doer", "Do")

		// The tool cannot know which implementation ran, and guessing would be
		// worse than admitting it.
		test.Nil(t, soleImplementation(method, ifaceOf(t, source, "Doer")))
	})

	t.Run("credits nobody when nothing implements", func(t *testing.T) {
		t.Parallel()

		source := checkSource(t, `package example

type Doer interface{ Do() string }

type Unrelated struct{}
`)

		method := interfaceMethod(t, source, "Doer", "Do")

		test.Nil(t, soleImplementation(method, ifaceOf(t, source, "Doer")))
	})
}

// ifaceOf returns the named interface type from a checked snippet.
func ifaceOf(t *testing.T, source checked, name string) *types.Interface {
	t.Helper()

	named, ok := source.pkg.Scope().Lookup(name).Type().Underlying().(*types.Interface)
	must.True(t, ok, must.Sprintf("%s is not an interface", name))

	return named
}

// interfaceMethod returns the named method of the named interface.
func interfaceMethod(t *testing.T, source checked, ifaceName, methodName string) *types.Func {
	t.Helper()

	iface := ifaceOf(t, source, ifaceName)

	for method := range iface.Methods() {
		if method.Name() == methodName {
			return method
		}
	}

	t.Fatalf("interface %s has no method %s", ifaceName, methodName)

	return nil
}

func TestPackageLevelVars(t *testing.T) {
	t.Parallel()

	source := checkSource(t, `package example

func Subject() string { return "subject" }

var single = Subject

var first, second = Subject, Subject

var paired, also = pair()

func pair() (string, string) { return "", "" }

func local() { inner := Subject; _ = inner }
`)

	// packageLevelVars keys on the *types.Var, so look the names up through the
	// package scope rather than trusting declaration order.
	vars := packageLevelVars(asPackage(source), []*ast.File{source.file})

	for _, name := range []string{"single", "first", "second", "paired", "also"} {
		declared, ok := source.pkg.Scope().Lookup(name).(*types.Var)
		must.True(t, ok, must.Sprintf("%s is not a var", name))
		test.MapContainsKey(t, vars, declared, test.Sprintf("initializer recorded for %s", name))
	}

	test.MapLen(t, 5, vars, test.Sprint("only package-level vars are recorded"))
}
