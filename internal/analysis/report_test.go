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
