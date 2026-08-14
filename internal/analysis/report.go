package analysis

import (
	"cmp"
	"slices"

	"github.com/primandproper/platform-go/v10/encoding"
)

// perfectScore is the grade reported for a package with nothing to grade.
const perfectScore = 100

// Function identifies a single function declaration and says whether a TestXxx
// body references it directly. That is a weaker claim than the one the tool is
// named for: a reference is evidence that a test was written for the function,
// not that the test asserts anything. File is relative to the analyzed
// directory when it sits underneath it, so reports are stable across machines
// and check-outs.
type Function struct {
	Package string `json:"package"`
	File    string `json:"file"`
	Name    string `json:"name"`
	// PackagePath is the import path Package is the clause name of. It is what
	// actually identifies a package — a module of any size holds several named
	// config — and so it is what per-package grading groups on. Left out of the
	// JSON with the rest of the renderer-only fields below.
	PackagePath string `json:"-"`
	// Path, EndLine, and Tested are carried on the Go value but left out of the
	// JSON: the wire shape lists only what is missing, and it names files the
	// way a person reads them. A caller that renders source instead — the
	// coverage view — needs to find the file on disk, know how far the
	// declaration extends, and have the verdict on every function rather than
	// only the failures.
	Path    string `json:"-"`
	Line    int    `json:"line"`
	EndLine int    `json:"-"`
	Tested  bool   `json:"-"`
}

// Report is the outcome of an analysis run.
//
// Functions is sorted by file and then by declaration line, so two runs over
// unchanged source produce byte-identical output.
type Report struct {
	// Root is the module root the analyzed packages live under, absolute, or
	// empty when the target sits in no module. Like Function.Path it is carried
	// on the Go value and left out of the JSON: a renderer that has to state
	// where a file sits — SARIF, whose URIs are relative to a declared base —
	// needs a root to make paths relative to, and the analyzed directory is not
	// it. The wire shape is pinned verbatim in analysis_test.go.
	Root       string
	Functions  []Function
	Warnings   []string
	Strictness Strictness
}

// reportJSON is the wire shape of a Report. It is spelled out separately so the
// JSON contract other tools parse cannot drift when the Go struct is
// rearranged — for field alignment, say.
type reportJSON struct {
	Strictness string     `json:"strictness"`
	Untested   []Function `json:"untested"`
	Warnings   []string   `json:"warnings"`
	Declared   int        `json:"declared"`
	Tested     int        `json:"tested"`
	Score      int        `json:"score"`
}

// MarshalJSON implements json.Marshaler, so that anything holding a Report can
// hand it to a standard encoder. The bytes themselves come from platform-go's
// encoding package, which keeps the content type one decision made in one
// place; EncodeJSON returns exactly what json.Marshal would, with no trailing
// newline.
//
// The directive below is tarp's answer to the one shape it cannot see:
// TestReportMarshalJSON asserts this method thoroughly and never writes its
// name, because json.Marshal reaches it by reflection. Every Stringer,
// driver.Valuer, and interface satisfied for a framework's benefit reads the
// same way, and the honest fix is a reason naming the test, not a looser rule.
//
//tarp:ignore -- reached by reflection through json.Marshal, so no test can name it; asserted by TestReportMarshalJSON
func (r Report) MarshalJSON() ([]byte, error) {
	out := reportJSON{
		Strictness: r.Strictness.String(),
		Untested:   r.Untested(),
		Warnings:   r.Warnings,
		Declared:   r.Declared(),
		Tested:     r.Tested(),
		Score:      r.Score(),
	}

	if out.Warnings == nil {
		out.Warnings = []string{}
	}

	return encoding.EncodeJSON(out)
}

// Declared is the number of functions the report holds to the standard.
func (r Report) Declared() int {
	return len(r.Functions)
}

// Tested is the number of declared functions carrying a direct test.
func (r Report) Tested() int {
	tested := 0

	for i := range r.Functions {
		if r.Functions[i].Tested {
			tested++
		}
	}

	return tested
}

// Untested lists the functions with no direct test, in report order.
func (r Report) Untested() []Function {
	untested := make([]Function, 0, len(r.Functions))

	for i := range r.Functions {
		if !r.Functions[i].Tested {
			untested = append(untested, r.Functions[i])
		}
	}

	return untested
}

// Score is the percentage of declared functions carrying a direct test,
// truncated rather than rounded: two of three is 66%, and only a genuinely
// complete package reaches 100. A package that declares nothing scores 100.
func (r Report) Score() int {
	return score(r.Tested(), r.Declared())
}

// Package is one package's grade, for reports that span more than one.
type Package struct {
	// Path is the import path, which is the identity. Name is the package
	// clause, which is what a person reads.
	Path     string
	Name     string
	Declared int
	Tested   int
}

// Score grades a single package on the same arithmetic as the whole report.
func (p Package) Score() int {
	return score(p.Tested, p.Declared)
}

// Packages groups the report by the package each function was declared in,
// sorted by import path so two runs over unchanged source agree.
//
// Only packages that declared something appear: a package with nothing to grade
// would score a meaningless 100 and pad the table it is read from.
func (r Report) Packages() []Package {
	indices := make(map[string]int, len(r.Functions))
	packages := make([]Package, 0, len(r.Functions))

	for i := range r.Functions {
		fn := &r.Functions[i]

		index, seen := indices[fn.PackagePath]
		if !seen {
			index = len(packages)
			indices[fn.PackagePath] = index
			packages = append(packages, Package{Path: fn.PackagePath, Name: fn.Package})
		}

		packages[index].Declared++
		if fn.Tested {
			packages[index].Tested++
		}
	}

	slices.SortFunc(packages, func(a, b Package) int {
		return cmp.Compare(a.Path, b.Path)
	})

	return packages
}

// score is the grading arithmetic, in one place so a package and the report it
// belongs to can never disagree about what 2 of 3 is.
func score(tested, declared int) int {
	if declared == 0 {
		return perfectScore
	}

	return tested * perfectScore / declared
}

// sortFunctions orders functions by file, then declaration line, then name, so
// output never depends on map iteration order.
func sortFunctions(functions []Function) {
	slices.SortFunc(functions, func(a, b Function) int {
		return cmp.Or(
			cmp.Compare(a.File, b.File),
			cmp.Compare(a.Line, b.Line),
			cmp.Compare(a.Name, b.Name),
		)
	})
}
