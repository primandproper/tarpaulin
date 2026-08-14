package analysis

import (
	"cmp"
	"go/ast"
	"go/token"
	"go/types"
	"slices"
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
		PkgPath:   source.pkg.Path(),
		Types:     source.pkg,
		TypesInfo: source.info,
		Syntax:    source.files,
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

// newCollector returns a collector over a checked snippet, with the empty index
// it is about to fill.
func newCollector(source checked) *collector {
	return &collector{fset: source.fset, index: make(refIndex)}
}

// sitesFor returns the sites recorded against the named declaration of a
// snippet, sorted, which is what every assertion about the collector comes down
// to.
func (c *collector) sitesFor(t *testing.T, source checked, name string) []refSite {
	t.Helper()

	sites := make([]refSite, 0, 1)
	for site := range c.index[source.fset.Position(source.fn(t, name).Pos())] {
		sites = append(sites, site)
	}

	slices.SortFunc(sites, func(a, b refSite) int {
		return cmp.Or(cmp.Compare(a.dir, b.dir), cmp.Compare(a.slot, b.slot), boolCompare(a.inTestFn, b.inTestFn))
	})

	return sites
}

// boolCompare orders false before true.
func boolCompare(a, b bool) int {
	switch {
	case a == b:
		return 0
	case b:
		return -1
	default:
		return 1
	}
}

func TestCollectorSiteFor(t *testing.T) {
	t.Parallel()

	source := checkFiles(t,
		sourceFile{name: "/src/subject_test.go", src: "package example\n"},
		sourceFile{name: "/src/subject_internal_test.go", src: "package example\n"},
		sourceFile{name: "/src/other_test.go", src: "package example\n"},
	)

	cases := map[string]struct {
		filename string
		expected refSite
	}{
		// The pair is the slot: Go forbids an external test file from touching
		// unexported identifiers, so subject_internal_test.go is the only place
		// an unexported function in subject.go can be tested from.
		"an external test file": {
			filename: "/src/subject_test.go",
			expected: refSite{dir: "/src", slot: "subject"},
		},
		"the internal half of the same slot": {
			filename: "/src/subject_internal_test.go",
			expected: refSite{dir: "/src", slot: "subject"},
		},
		"a sibling file's test": {
			filename: "/src/other_test.go",
			expected: refSite{dir: "/src", slot: "other"},
		},
	}

	collector := newCollector(source)

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			test.Eq(t, testCase.expected, collector.siteFor(source.fileNamed(t, testCase.filename)))
		})
	}
}

func TestCollectorRecord(t *testing.T) {
	t.Parallel()

	site := refSite{dir: "/src", slot: "subject", inTestFn: true}

	t.Run("credits a function at its declaration", func(t *testing.T) {
		t.Parallel()

		source := checkSource(t, `package example

func Subject() {}
`)

		collector := newCollector(source)
		collector.record(source.fn(t, "Subject"), site)

		test.Eq(t, []refSite{site}, collector.sitesFor(t, source, "Subject"))
	})

	t.Run("collapses every instantiation of a generic onto the generic", func(t *testing.T) {
		t.Parallel()

		// Box[int].Get and Box[string].Get are distinct objects; both are the one
		// declaration a reader would write a test for.
		source := checkFiles(t, sourceFile{name: "/src/subject.go", src: `package example

type Box[T any] struct{ v T }

func (Box[T]) Get() {}

func use() {
	Box[int]{}.Get()
	Box[string]{}.Get()
}
`})

		collector := newCollector(source)

		instantiations := 0

		for ident, used := range source.info.Uses {
			fn, ok := used.(*types.Func)
			if !ok || ident.Name != "Get" {
				continue
			}

			instantiations++

			collector.record(fn, site)
		}

		must.Eq(t, 2, instantiations, must.Sprint("the snippet no longer instantiates Box twice"))
		test.MapLen(t, 1, collector.index)
		test.Eq(t, []refSite{site}, collector.sitesFor(t, source, "Get"))
	})

	t.Run("credits the sole implementer behind an interface", func(t *testing.T) {
		t.Parallel()

		source := checkSource(t, `package example

type Doer interface{ Do() }

type Impl struct{}

func (Impl) Do() {}
`)

		collector := newCollector(source)
		collector.record(interfaceMethod(t, source, "Doer", "Do"), site)

		// Calling Doer.Do can only have run Impl.Do, so Impl.Do is what the test
		// exercised.
		test.MapLen(t, 1, collector.index)
		test.Eq(t, []refSite{site}, collector.sitesFor(t, source, "Do"))
	})

	t.Run("credits nobody when the interface is ambiguous", func(t *testing.T) {
		t.Parallel()

		source := checkSource(t, `package example

type Doer interface{ Do() }

type First struct{}

func (First) Do() {}

type Second struct{}

func (Second) Do() {}
`)

		collector := newCollector(source)
		collector.record(interfaceMethod(t, source, "Doer", "Do"), site)

		// Guessing which implementation ran would be worse than admitting the
		// tool cannot know.
		test.MapEmpty(t, collector.index)
	})

	t.Run("drops a function with no declaration to credit", func(t *testing.T) {
		t.Parallel()

		source := checkSource(t, "package example\n")

		// The index is keyed by position, so an object the type checker
		// synthesized rather than read out of a file has nowhere to go; keying it
		// at the zero position would collide every such object with every other.
		synthetic := types.NewFunc(token.NoPos, source.pkg, "Synthetic", types.NewSignatureType(nil, nil, nil, nil, nil, false))

		collector := newCollector(source)
		collector.record(synthetic, site)

		test.MapEmpty(t, collector.index)
	})
}

func TestCollectorWalk(t *testing.T) {
	t.Parallel()

	source := checkFiles(t, sourceFile{name: "/src/subject.go", src: `package example

func Subject() {}

func Deeper() {}

func helper() {
	Subject()
}

func caller() {
	helper()
	defer Subject()
	_ = Deeper
}
`})

	site := refSite{dir: "/src", slot: "subject"}

	t.Run("records what the node itself mentions", func(t *testing.T) {
		t.Parallel()

		// Never "is this a call?": a deferred function and one named as a value
		// resolve to the same object a call would, which is why neither needs a
		// case of its own.
		collector := newCollector(source)
		collector.walk(asPackage(source), source.funcDecl(t, "caller").Body, site)

		test.Eq(t, []refSite{site}, collector.sitesFor(t, source, "helper"))
		test.Eq(t, []refSite{site}, collector.sitesFor(t, source, "Subject"))
		test.Eq(t, []refSite{site}, collector.sitesFor(t, source, "Deeper"))
	})

	t.Run("follows nothing", func(t *testing.T) {
		t.Parallel()

		// helper calls Subject, but walking caller must not credit Subject
		// through it: following calls arbitrarily is what collapses a direct-test
		// question back into a coverage question.
		collector := newCollector(source)
		collector.walk(asPackage(source), source.funcDecl(t, "helper").Body, site)

		test.Eq(t, []refSite{site}, collector.sitesFor(t, source, "Subject"))
		test.SliceEmpty(t, collector.sitesFor(t, source, "Deeper"))
	})
}

func TestCollectorWalkTestBody(t *testing.T) {
	t.Parallel()

	source := checkFiles(t,
		sourceFile{name: "/src/subject.go", src: `package example

func Subject() {}
`},
		sourceFile{name: "/src/subject_test.go", src: `package example

import "testing"

var table = []func(){Subject}

var alias = table

func TestSubject(t *testing.T) {
	for _, fn := range table {
		fn()
	}
}

func TestAlias(t *testing.T) {
	_ = alias
}

func TestNothing(t *testing.T) {}
`},
	)

	pkg := asPackage(source)
	site := refSite{dir: "/src", slot: "subject", inTestFn: true}
	tables := packageLevelVars(pkg, []*ast.File{source.fileNamed(t, "/src/subject_test.go")})

	t.Run("takes one hop into a package-level test table", func(t *testing.T) {
		t.Parallel()

		// A table-driven test names its subject in the table, not in the body, so
		// refusing the hop would report every table-tested function as untested.
		collector := newCollector(source)
		collector.walkTestBody(pkg, source.funcDecl(t, "TestSubject").Body, site, tables)

		test.Eq(t, []refSite{site}, collector.sitesFor(t, source, "Subject"))
	})

	t.Run("takes exactly one", func(t *testing.T) {
		t.Parallel()

		// alias initializes from table, so a second hop would reach Subject.
		// Hopping recursively would quietly collapse file strictness into any.
		collector := newCollector(source)
		collector.walkTestBody(pkg, source.funcDecl(t, "TestAlias").Body, site, tables)

		test.SliceEmpty(t, collector.sitesFor(t, source, "Subject"))
	})

	t.Run("records nothing for an empty body", func(t *testing.T) {
		t.Parallel()

		collector := newCollector(source)
		collector.walkTestBody(pkg, source.funcDecl(t, "TestNothing").Body, site, tables)

		test.MapEmpty(t, collector.index)
	})

	t.Run("records nothing for a declaration with no body", func(t *testing.T) {
		t.Parallel()

		// An assembly stub is declared with no body at all, and asking ast.Inspect
		// to walk that nil is a panic.
		collector := newCollector(source)
		collector.walkTestBody(pkg, nil, site, tables)

		test.MapEmpty(t, collector.index)
	})
}

func TestCollectorCollectPackage(t *testing.T) {
	t.Parallel()

	t.Run("separates what a TestXxx body reached from what the file merely mentions", func(t *testing.T) {
		t.Parallel()

		source := checkFiles(t,
			sourceFile{name: "/src/subject.go", src: `package example

func Tested() {}

func ViaHelper() {}

func Untouched() {}
`},
			sourceFile{name: "/src/subject_test.go", src: `package example

import "testing"

func helper() {
	ViaHelper()
}

func TestTested(t *testing.T) {
	Tested()
}
`},
		)

		collector := newCollector(source)
		collector.collectPackage(asPackage(source))

		// Each file is walked twice: wholesale, which is what any accepts, and
		// once per TestXxx body, which is what file and package require. So a
		// function named in a test body is recorded both ways, and one named only
		// in a helper is recorded the one way that any accepts.
		test.Eq(t, []refSite{
			{dir: "/src", slot: "subject"},
			{dir: "/src", slot: "subject", inTestFn: true},
		}, collector.sitesFor(t, source, "Tested"))
		test.Eq(t, []refSite{{dir: "/src", slot: "subject"}},
			collector.sitesFor(t, source, "ViaHelper"))
		test.SliceEmpty(t, collector.sitesFor(t, source, "Untouched"))
	})

	t.Run("reads references out of test files only", func(t *testing.T) {
		t.Parallel()

		source := checkFiles(t, sourceFile{name: "/src/subject.go", src: `package example

func Subject() {}

func caller() {
	Subject()
}
`})

		// A package with no test files has nothing to say about what is tested,
		// however much of itself it calls.
		collector := newCollector(source)
		collector.collectPackage(asPackage(source))

		test.MapEmpty(t, collector.index)
	})
}

func TestCollectReferences(t *testing.T) {
	t.Parallel()

	source := checkFiles(t,
		sourceFile{name: "/src/subject.go", src: `package example

func Subject() {}
`},
		sourceFile{name: "/src/subject_test.go", src: `package example

import "testing"

func TestSubject(t *testing.T) {
	Subject()
}
`},
		sourceFile{name: "/src/other_test.go", src: `package example

import "testing"

func TestOther(t *testing.T) {
	Subject()
}
`},
	)

	// One package's variants, as go/packages hands them over: the same source
	// file compiled into each, with a different test file alongside it. The
	// index has to union them, because they are evidence about one function.
	internal := asPackage(source)
	internal.Syntax = []*ast.File{
		source.fileNamed(t, "/src/subject.go"),
		source.fileNamed(t, "/src/subject_test.go"),
	}

	external := asPackage(source)
	external.Syntax = []*ast.File{
		source.fileNamed(t, "/src/subject.go"),
		source.fileNamed(t, "/src/other_test.go"),
	}

	index := collectReferences(source.fset, []*packages.Package{internal, external})

	decl := &declaration{
		key:  source.fset.Position(source.fn(t, "Subject").Pos()),
		dir:  "/src",
		slot: "subject",
		name: "Subject",
	}

	test.True(t, index.satisfies(decl, StrictnessFile),
		test.Sprint("the declaring file's own test was not recorded"))

	// Each variant contributes two sites for its own test file: the wholesale
	// walk and the TestXxx body.
	slots := make([]string, 0, 4)
	for site := range index[decl.key] {
		slots = append(slots, site.slot)
	}

	slices.Sort(slots)
	test.Eq(t, []string{"other", "other", "subject", "subject"}, slots)
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

func TestImplementingMethod(t *testing.T) {
	t.Parallel()

	// namedOf returns the named type behind a snippet's declaration.
	namedOf := func(t *testing.T, source checked, name string) *types.Named {
		t.Helper()

		named, ok := source.pkg.Scope().Lookup(name).Type().(*types.Named)
		must.True(t, ok, must.Sprintf("%s is not a named type", name))

		return named
	}

	t.Run("finds a value receiver's implementation", func(t *testing.T) {
		t.Parallel()

		source := checkSource(t, `package example

type Doer interface{ Do() string }

type Impl struct{}

func (Impl) Do() string { return "impl" }
`)

		implementation := implementingMethod(
			namedOf(t, source, "Impl"),
			ifaceOf(t, source, "Doer"),
			interfaceMethod(t, source, "Doer", "Do"),
		)

		must.NotNil(t, implementation)
		test.Eq(t, "Impl.Do", funcName(implementation))
	})

	t.Run("finds a pointer receiver's implementation", func(t *testing.T) {
		t.Parallel()

		// Only *Impl implements Doer, and the value type is what a package scope
		// holds, so the pointer form has to be tried too.
		source := checkSource(t, `package example

type Doer interface{ Do() string }

type Impl struct{}

func (*Impl) Do() string { return "impl" }
`)

		implementation := implementingMethod(
			namedOf(t, source, "Impl"),
			ifaceOf(t, source, "Doer"),
			interfaceMethod(t, source, "Doer", "Do"),
		)

		must.NotNil(t, implementation)
		test.Eq(t, "(*Impl).Do", funcName(implementation))
	})

	t.Run("normalizes a generic implementer back to the generic", func(t *testing.T) {
		t.Parallel()

		source := checkSource(t, `package example

type Doer interface{ Do() string }

type Box[T any] struct{ v T }

func (Box[T]) Do() string { return "box" }
`)

		implementation := implementingMethod(
			namedOf(t, source, "Box"),
			ifaceOf(t, source, "Doer"),
			interfaceMethod(t, source, "Doer", "Do"),
		)

		must.NotNil(t, implementation)
		test.Eq(t, "Box[T].Do", funcName(implementation))
	})

	t.Run("reports a type that does not implement the interface", func(t *testing.T) {
		t.Parallel()

		source := checkSource(t, `package example

type Doer interface{ Do() string }

type Unrelated struct{}

func (Unrelated) Other() {}
`)

		test.Nil(t, implementingMethod(
			namedOf(t, source, "Unrelated"),
			ifaceOf(t, source, "Doer"),
			interfaceMethod(t, source, "Doer", "Do"),
		))
	})
}

func TestRecordValueSpec(t *testing.T) {
	t.Parallel()

	source := checkSource(t, `package example

func Subject() string { return "subject" }

func Other() string { return "other" }

func pair() (string, string) { return "", "" }

var single = Subject

var first, second = Subject, Other

var paired, also = pair()

var uninitialized string
`)

	// specNamed returns the var spec that declares name, which is the unit
	// recordValueSpec is handed.
	specNamed := func(t *testing.T, name string) *ast.ValueSpec {
		t.Helper()

		for _, decl := range source.files[0].Decls {
			genDecl, ok := decl.(*ast.GenDecl)
			if !ok || genDecl.Tok != token.VAR {
				continue
			}

			for _, spec := range genDecl.Specs {
				valueSpec, isValueSpec := spec.(*ast.ValueSpec)
				if !isValueSpec {
					continue
				}

				for _, declared := range valueSpec.Names {
					if declared.Name == name {
						return valueSpec
					}
				}
			}
		}

		t.Fatalf("no var spec declaring %q in snippet", name)

		return nil
	}

	// initializers returns the source text of what a var was recorded against.
	initializers := func(t *testing.T, vars map[*types.Var][]ast.Expr, name string) []string {
		t.Helper()

		declared, ok := source.pkg.Scope().Lookup(name).(*types.Var)
		must.True(t, ok, must.Sprintf("%s is not a var", name))

		rendered := make([]string, 0, len(vars[declared]))
		for _, expr := range vars[declared] {
			rendered = append(rendered, types.ExprString(expr))
		}

		return rendered
	}

	t.Run("pairs one name with its own initializer", func(t *testing.T) {
		t.Parallel()

		vars := make(map[*types.Var][]ast.Expr)
		recordValueSpec(asPackage(source), specNamed(t, "single"), vars)

		test.Eq(t, []string{"Subject"}, initializers(t, vars, "single"))
	})

	t.Run("pairs each name with the initializer in its own position", func(t *testing.T) {
		t.Parallel()

		vars := make(map[*types.Var][]ast.Expr)
		recordValueSpec(asPackage(source), specNamed(t, "first"), vars)

		test.MapLen(t, 2, vars)
		test.Eq(t, []string{"Subject"}, initializers(t, vars, "first"))
		test.Eq(t, []string{"Other"}, initializers(t, vars, "second"))
	})

	t.Run("gives every name of a multi-value initializer the whole expression", func(t *testing.T) {
		t.Parallel()

		// `var a, b = f()` has one expression and two names, and either name is
		// reason enough to walk into f.
		vars := make(map[*types.Var][]ast.Expr)
		recordValueSpec(asPackage(source), specNamed(t, "paired"), vars)

		test.Eq(t, []string{"pair()"}, initializers(t, vars, "paired"))
		test.Eq(t, []string{"pair()"}, initializers(t, vars, "also"))
	})

	t.Run("records nothing for a declaration with no initializer", func(t *testing.T) {
		t.Parallel()

		vars := make(map[*types.Var][]ast.Expr)
		recordValueSpec(asPackage(source), specNamed(t, "uninitialized"), vars)

		test.MapEmpty(t, vars)
	})
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
	vars := packageLevelVars(asPackage(source), source.files)

	for _, name := range []string{"single", "first", "second", "paired", "also"} {
		declared, ok := source.pkg.Scope().Lookup(name).(*types.Var)
		must.True(t, ok, must.Sprintf("%s is not a var", name))
		test.MapContainsKey(t, vars, declared, test.Sprintf("initializer recorded for %s", name))
	}

	test.MapLen(t, 5, vars, test.Sprint("only package-level vars are recorded"))
}
