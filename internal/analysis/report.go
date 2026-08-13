package analysis

import (
	"cmp"
	"slices"

	"github.com/primandproper/platform-go/v10/encoding"
)

// perfectScore is the grade reported for a package with nothing to grade.
const perfectScore = 100

// Function identifies a single function declaration and says whether a test
// asserts its behavior directly. File is relative to the analyzed directory
// when it sits underneath it, so reports are stable across machines and
// check-outs.
type Function struct {
	Package string `json:"package"`
	File    string `json:"file"`
	Name    string `json:"name"`
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
	declared := r.Declared()
	if declared == 0 {
		return perfectScore
	}

	return r.Tested() * perfectScore / declared
}

// sortFunctions orders functions by file, then declaration line, then name, so
// output never depends on map iteration order (PRD 4.5).
func sortFunctions(functions []Function) {
	slices.SortFunc(functions, func(a, b Function) int {
		return cmp.Or(
			cmp.Compare(a.File, b.File),
			cmp.Compare(a.Line, b.Line),
			cmp.Compare(a.Name, b.Name),
		)
	})
}
