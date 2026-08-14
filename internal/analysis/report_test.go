package analysis_test

import (
	"encoding/json"
	"testing"

	"github.com/primandproper/tarpaulin/internal/analysis"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// sampleReport is two functions tested and one not.
func sampleReport() analysis.Report {
	return analysis.Report{
		Strictness: analysis.StrictnessPackage,
		Functions: []analysis.Function{
			{Package: "sample", File: "a.go", Name: "First", Line: 3, Tested: true},
			{Package: "sample", File: "a.go", Name: "Second", Line: 7},
			{Package: "sample", File: "b.go", Name: "Third", Line: 3, Tested: true},
		},
	}
}

func TestReportDeclared(t *testing.T) {
	t.Parallel()

	test.Eq(t, 3, sampleReport().Declared())
	test.Eq(t, 0, analysis.Report{}.Declared())
}

func TestReportTested(t *testing.T) {
	t.Parallel()

	test.Eq(t, 2, sampleReport().Tested())
	test.Eq(t, 0, analysis.Report{}.Tested())
}

func TestReportUntested(t *testing.T) {
	t.Parallel()

	untested := sampleReport().Untested()

	must.SliceLen(t, 1, untested)
	test.Eq(t, "Second", untested[0].Name)
	test.SliceEmpty(t, analysis.Report{}.Untested())
}

func TestReportScore(t *testing.T) {
	t.Parallel()

	t.Run("truncates rather than rounds", func(t *testing.T) {
		t.Parallel()

		// Two of three is 66%, not 67%: the tool measures against the strict
		// ideal, so it does not round a package up to a nicer number.
		test.Eq(t, 66, sampleReport().Score())
	})

	t.Run("scores an empty package as complete", func(t *testing.T) {
		t.Parallel()

		test.Eq(t, 100, analysis.Report{}.Score())
	})
}

// TestReportMarshalJSON uses encoding/json rather than platform-go's encoding
// package on purpose: Report's job here is to satisfy json.Marshaler for
// whoever holds one, and that is the contract worth pinning. The bytes are
// produced by platform-go either way — MarshalJSON calls encoding.EncodeJSON.
func TestPackageScore(t *testing.T) {
	t.Parallel()

	// The same arithmetic as the whole report: truncated, not rounded, and a
	// package with nothing to grade is perfect rather than zero.
	test.Eq(t, 66, analysis.Package{Declared: 3, Tested: 2}.Score())
	test.Eq(t, 100, analysis.Package{Declared: 4, Tested: 4}.Score())
	test.Eq(t, 0, analysis.Package{Declared: 4}.Score())
	test.Eq(t, 100, analysis.Package{}.Score())
}

func TestReportPackages(t *testing.T) {
	t.Parallel()

	t.Run("groups on the import path, not the clause name", func(t *testing.T) {
		t.Parallel()

		// Two packages that are both named config, which is the ordinary case
		// in a module of any size: platform-go has forty-eight of them.
		report := analysis.Report{Functions: []analysis.Function{
			{Package: "config", PackagePath: "example.com/m/audit/config", Name: "A", Tested: true},
			{Package: "config", PackagePath: "example.com/m/analytics/config", Name: "B"},
			{Package: "config", PackagePath: "example.com/m/analytics/config", Name: "C", Tested: true},
		}}

		packages := report.Packages()

		must.SliceLen(t, 2, packages)
		// Sorted by path, so two runs over unchanged source agree.
		test.Eq(t, "example.com/m/analytics/config", packages[0].Path)
		test.Eq(t, "example.com/m/audit/config", packages[1].Path)
		// Merging these would have reported one package at 66% instead of two,
		// and hidden which of them needs the work.
		test.Eq(t, 50, packages[0].Score())
		test.Eq(t, 100, packages[1].Score())
		test.Eq(t, "config", packages[0].Name)
	})

	t.Run("counts every function against its own package", func(t *testing.T) {
		t.Parallel()

		packages := sampleReport().Packages()

		must.SliceLen(t, 1, packages)
		test.Eq(t, 3, packages[0].Declared)
		test.Eq(t, 2, packages[0].Tested)
		test.Eq(t, 66, packages[0].Score())
	})

	t.Run("has nothing to say about a report with no functions", func(t *testing.T) {
		t.Parallel()

		// A package that declared nothing would score a meaningless 100 and pad
		// the table it is read from, so it never appears.
		test.SliceEmpty(t, analysis.Report{}.Packages())
	})

	t.Run("agrees with the report it came from", func(t *testing.T) {
		t.Parallel()

		report := sampleReport()

		declared, tested := 0, 0
		for _, pkg := range report.Packages() {
			declared += pkg.Declared
			tested += pkg.Tested
		}

		// The per-package rows and the total are the same functions counted
		// twice; a table whose rows do not add up to its total is worse than no
		// table.
		test.Eq(t, report.Declared(), declared)
		test.Eq(t, report.Tested(), tested)
	})
}

func TestReportMarshalJSON(t *testing.T) {
	t.Parallel()

	t.Run("lists only what is missing", func(t *testing.T) {
		t.Parallel()

		encoded, err := json.Marshal(sampleReport())
		must.NoError(t, err)

		test.Eq(t,
			`{"strictness":"package","untested":[{"package":"sample","file":"a.go","name":"Second","line":7}],`+
				`"warnings":[],"declared":3,"tested":2,"score":66}`,
			string(encoded))
	})

	t.Run("renders empty collections rather than null", func(t *testing.T) {
		t.Parallel()

		// Consumers index into these; null would make every one of them
		// special-case an empty run.
		encoded, err := json.Marshal(analysis.Report{})
		must.NoError(t, err)

		test.Eq(t, `{"strictness":"file","untested":[],"warnings":[],"declared":0,"tested":0,"score":100}`, string(encoded))
	})
}
