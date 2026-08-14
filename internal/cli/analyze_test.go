package cli

import (
	"bytes"
	"errors"
	"path/filepath"
	"testing"

	"github.com/primandproper/tarpaulin/internal/analysis"

	"github.com/primandproper/platform-go/v10/encoding"
	platformerrors "github.com/primandproper/platform-go/v10/errors"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// fixture locates a corpus package from the CLI package's directory.
func fixture(name string) string {
	return filepath.Join("..", "analysis", "testdata", name)
}

// errBrokenWriter is what brokenWriter fails with.
var errBrokenWriter = errors.New("the writer is broken")

// brokenWriter is an io.Writer that always fails, so the paths that report a
// gate can be checked for surfacing the write failure instead of swallowing it.
type brokenWriter struct{}

func (brokenWriter) Write([]byte) (int, error) { return 0, errBrokenWriter }

// runAnalyzeCommand executes the analyze subcommand as the binary would, and
// returns what it wrote to stdout and stderr.
func runAnalyzeCommand(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()

	out, errOut := new(bytes.Buffer), new(bytes.Buffer)

	cmd := (&application{}).newAnalyzeCommand()
	// The root command silences both, and Execute prints failures itself. A
	// bare subcommand does not inherit that, so without this the harness would
	// let cobra staple an "Error:" line onto stderr that the binary never
	// writes — and every assertion about stderr would be measuring the harness.
	cmd.SilenceUsage, cmd.SilenceErrors = true, true
	cmd.SetOut(out)
	cmd.SetErr(errOut)
	cmd.SetArgs(args)

	err = cmd.ExecuteContext(t.Context())

	return out.String(), errOut.String(), err
}

func TestAnalyzeCommand(t *testing.T) {
	t.Parallel()

	t.Run("renders the report", func(t *testing.T) {
		t.Parallel()

		stdout, stderr, err := runAnalyzeCommand(t, "--package", fixture("simple"))
		must.NoError(t, err)

		test.Eq(t, "Functions without direct unit tests:\nin main.go:\n\tB on line 7\n\nGrade: 75% (3/4 functions)\n", stdout)
		test.Eq(t, "", stderr)
	})

	t.Run("renders JSON", func(t *testing.T) {
		t.Parallel()

		stdout, _, err := runAnalyzeCommand(t, "--package", fixture("simple"), "--json")
		must.NoError(t, err)

		var decoded struct {
			Strictness string `json:"strictness"`
			Untested   []struct {
				Package string `json:"package"`
				File    string `json:"file"`
				Name    string `json:"name"`
				Line    int    `json:"line"`
			} `json:"untested"`
			Warnings []string `json:"warnings"`
			Declared int      `json:"declared"`
			Tested   int      `json:"tested"`
			Score    int      `json:"score"`
		}

		must.NoError(t, encoding.DecodeJSON([]byte(stdout), &decoded))

		test.Eq(t, "file", decoded.Strictness)
		test.Eq(t, 4, decoded.Declared)
		test.Eq(t, 3, decoded.Tested)
		test.Eq(t, 75, decoded.Score)
		must.SliceLen(t, 1, decoded.Untested)
		test.Eq(t, "B", decoded.Untested[0].Name)
		test.Eq(t, "main.go", decoded.Untested[0].File)
		test.Eq(t, 7, decoded.Untested[0].Line)
	})

	t.Run("renders SARIF", func(t *testing.T) {
		t.Parallel()

		stdout, _, err := runAnalyzeCommand(t, "--package", fixture("simple"), "--format", "sarif")
		must.NoError(t, err)

		// What the command owes is a valid document with the report wired into
		// it; the document's shape is pinned field by field in internal/sarif.
		decoded := map[string]any{}
		must.NoError(t, encoding.DecodeJSON([]byte(stdout), &decoded))
		test.Eq(t, "2.1.0", decoded["version"])

		test.StrContains(t, stdout, `"ruleId":"tarp/untested-function"`)

		// End to end, the URI is stated against the module root rather than the
		// analyzed directory — this repository's own go.mod, since the fixture
		// lives underneath it. Getting this wrong is how a consumer resolves a
		// finding to a file that does not exist.
		test.StrContains(t, stdout, `"uri":"internal/analysis/testdata/simple/main.go","uriBaseId":"SRCROOT"`)
	})

	t.Run("refuses a format it does not have", func(t *testing.T) {
		t.Parallel()

		_, _, err := runAnalyzeCommand(t, "--package", fixture("simple"), "--format", "xml")

		must.ErrorIs(t, err, platformerrors.ErrUnrecognizedInputValue)
		test.StrContains(t, err.Error(), "unknown format")
	})

	t.Run("refuses --json alongside a contradicting --format", func(t *testing.T) {
		t.Parallel()

		_, _, err := runAnalyzeCommand(t, "--package", fixture("simple"), "--format", "sarif", "--json")

		must.ErrorIs(t, err, platformerrors.ErrUnrecognizedInputValue)
		test.StrContains(t, err.Error(), "different output")
	})

	t.Run("fails on found, without burying the report in an error", func(t *testing.T) {
		t.Parallel()

		stdout, _, err := runAnalyzeCommand(t, "--package", fixture("simple"), "--fail-on-found")

		must.ErrorIs(t, err, errFunctionsFound)
		test.StrContains(t, stdout, "B on line 7")
	})

	t.Run("passes a score gate it meets exactly", func(t *testing.T) {
		t.Parallel()

		stdout, stderr, err := runAnalyzeCommand(t, "--package", fixture("simple"), "--min-score", "75")
		must.NoError(t, err)

		test.StrContains(t, stdout, "Grade: 75%")
		test.Eq(t, "", stderr)
	})

	t.Run("fails a score gate it misses, saying what it needed", func(t *testing.T) {
		t.Parallel()

		stdout, stderr, err := runAnalyzeCommand(t, "--package", fixture("simple"), "--min-score", "76")

		must.ErrorIs(t, err, errScoreBelowMinimum)
		// The report still prints in full, and the reason the gate tripped goes
		// to stderr — the grade alone never says what it was measured against.
		test.StrContains(t, stdout, "Grade: 75%")
		test.Eq(t, "score 75% is below the required minimum of 76%\n", stderr)
	})

	t.Run("keeps stdout parseable when the score gate trips", func(t *testing.T) {
		t.Parallel()

		stdout, _, err := runAnalyzeCommand(t, "--package", fixture("simple"), "--min-score", "76", "--json")

		must.ErrorIs(t, err, errScoreBelowMinimum)
		must.NoError(t, encoding.DecodeJSON([]byte(stdout), &map[string]any{}))
	})

	t.Run("prefers the stronger gate when both trip", func(t *testing.T) {
		t.Parallel()

		_, stderr, err := runAnalyzeCommand(t,
			"--package", fixture("simple"), "--fail-on-found", "--min-score", "76")

		// A score below any minimum implies something was reported, so
		// --fail-on-found subsumes it and the score line would be redundant.
		must.ErrorIs(t, err, errFunctionsFound)
		test.Eq(t, "", stderr)
	})

	t.Run("rejects a minimum score no grade could satisfy", func(t *testing.T) {
		t.Parallel()

		_, _, err := runAnalyzeCommand(t, "--package", fixture("simple"), "--min-score", "150")

		must.ErrorIs(t, err, platformerrors.ErrUnrecognizedInputValue)
		test.StrContains(t, err.Error(), "out of range")
	})

	t.Run("succeeds on a package with nothing to report", func(t *testing.T) {
		t.Parallel()

		stdout, _, err := runAnalyzeCommand(t, "--package", fixture("deeply_nested"), "--fail-on-found")
		must.NoError(t, err)

		test.Eq(t, "Grade: 100% (1/1 functions)\n", stdout)
	})

	t.Run("loosens the dial", func(t *testing.T) {
		t.Parallel()

		strict, _, err := runAnalyzeCommand(t, "--package", fixture("helper_only"))
		must.NoError(t, err)
		test.StrContains(t, strict, "ViaHelper on line 8")

		loose, _, err := runAnalyzeCommand(t, "--package", fixture("helper_only"), "--strictness", "any")
		must.NoError(t, err)
		test.Eq(t, "Grade: 100% (2/2 functions)\n", loose)
	})

	t.Run("rejects an unknown strictness", func(t *testing.T) {
		t.Parallel()

		_, _, err := runAnalyzeCommand(t, "--strictness", "somewhat")

		must.Error(t, err)
		test.StrContains(t, err.Error(), "unknown strictness")
	})

	t.Run("reports directive warnings on stderr, keeping stdout parseable", func(t *testing.T) {
		t.Parallel()

		stdout, stderr, err := runAnalyzeCommand(t, "--package", fixture("ignore_directive"), "--json")
		must.NoError(t, err)

		test.StrContains(t, stderr, "no reason")
		must.NoError(t, encoding.DecodeJSON([]byte(stdout), &map[string]any{}))
	})

	t.Run("surfaces a broken package as a diagnostic", func(t *testing.T) {
		t.Parallel()

		_, _, err := runAnalyzeCommand(t, "--package", fixture("broken_package"))

		must.Error(t, err)
		test.StrContains(t, err.Error(), "undefined: undefinedIdentifier")
	})
}

func TestRender(t *testing.T) {
	t.Parallel()

	report := &analysis.Report{
		Strictness: analysis.StrictnessFile,
		Functions: []analysis.Function{
			{File: "alpha.go", Name: "Tested", Line: 3, Tested: true},
			{File: "alpha.go", Name: "ShortName", Line: 9},
			{File: "beta.go", Name: "AConsiderablyLongerName", Line: 4},
		},
	}

	t.Run("as text", func(t *testing.T) {
		t.Parallel()

		out := new(bytes.Buffer)
		must.NoError(t, render(out, report, formatText))

		// Names are right-aligned into a column, and each file is announced
		// once — the 2017 output shape, which was fine.
		test.Eq(t, "Functions without direct unit tests:\n"+
			"in alpha.go:\n"+
			"\t              ShortName on line 9\n"+
			"in beta.go:\n"+
			"\tAConsiderablyLongerName on line 4\n"+
			"\nGrade: 33% (1/3 functions)\n", out.String())
	})

	t.Run("as JSON", func(t *testing.T) {
		t.Parallel()

		out := new(bytes.Buffer)
		must.NoError(t, render(out, report, formatJSON))

		test.StrHasSuffix(t, "\n", out.String())
		must.NoError(t, encoding.DecodeJSON(out.Bytes(), &map[string]any{}))
	})

	t.Run("as a markdown table, one row per package", func(t *testing.T) {
		t.Parallel()

		spanning := &analysis.Report{
			Strictness: analysis.StrictnessFile,
			Functions: []analysis.Function{
				{PackagePath: "example.com/m/beta", File: "b.go", Name: "One", Line: 3},
				{PackagePath: "example.com/m/alpha", File: "a.go", Name: "Two", Line: 4, Tested: true},
				{PackagePath: "example.com/m/alpha", File: "a.go", Name: "Three", Line: 9},
			},
		}

		out := new(bytes.Buffer)
		must.NoError(t, render(out, spanning, formatMarkdown))

		// Packages in path order, numbers right-aligned, and a total row that
		// adds up to the rows above it.
		test.Eq(t, "| Package | Score | Tested | Declared |\n"+
			"| --- | ---: | ---: | ---: |\n"+
			"| `example.com/m/alpha` | 50% | 1 | 2 |\n"+
			"| `example.com/m/beta` | 0% | 0 | 1 |\n"+
			"| **Total** | **33%** | **1** | **3** |\n"+
			"\nGraded at `file` strictness.\n", out.String())
	})

	t.Run("as a markdown table with nothing to grade", func(t *testing.T) {
		t.Parallel()

		out := new(bytes.Buffer)
		must.NoError(t, render(out, &analysis.Report{}, formatMarkdown))

		// Still a well-formed table: something that gets pasted into a summary
		// should not stop being markdown on the repository that passed.
		test.StrContains(t, out.String(), "| Package | Score | Tested | Declared |")
		test.StrContains(t, out.String(), "| **Total** | **100%** | **0** | **0** |")
	})

	t.Run("says nothing but the grade when there is nothing to report", func(t *testing.T) {
		t.Parallel()

		out := new(bytes.Buffer)
		must.NoError(t, render(out, &analysis.Report{}, formatText))

		test.Eq(t, "Grade: 100% (0/0 functions)\n", out.String())
	})
}

func TestCheckMinScore(t *testing.T) {
	t.Parallel()

	test.NoError(t, checkMinScore(0))
	test.NoError(t, checkMinScore(50))
	test.NoError(t, checkMinScore(100))

	// Both ends are rejected: a negative minimum is unsatisfiable in the other
	// direction, and cobra will happily parse one from `--min-score -1`.
	test.ErrorIs(t, checkMinScore(-1), platformerrors.ErrUnrecognizedInputValue)
	test.ErrorIs(t, checkMinScore(101), platformerrors.ErrUnrecognizedInputValue)
}

func TestCheckScore(t *testing.T) {
	t.Parallel()

	t.Run("says nothing when the gate is unset", func(t *testing.T) {
		t.Parallel()

		out := new(bytes.Buffer)

		// The default minimum is 0 and a score is never negative, so an unset
		// flag cannot trip the gate even for a package that scores nothing.
		must.NoError(t, checkScore(out, 0, 0))
		test.Eq(t, "", out.String())
	})

	t.Run("says nothing when the score meets the minimum", func(t *testing.T) {
		t.Parallel()

		out := new(bytes.Buffer)

		must.NoError(t, checkScore(out, 50, 50))
		test.Eq(t, "", out.String())
	})

	t.Run("explains itself when the score misses", func(t *testing.T) {
		t.Parallel()

		out := new(bytes.Buffer)

		test.ErrorIs(t, checkScore(out, 49, 50), errScoreBelowMinimum)
		test.Eq(t, "score 49% is below the required minimum of 50%\n", out.String())
	})

	t.Run("surfaces a broken writer rather than the gate", func(t *testing.T) {
		t.Parallel()

		err := checkScore(brokenWriter{}, 49, 50)

		test.ErrorIs(t, err, errBrokenWriter)
		test.False(t, errors.Is(err, errScoreBelowMinimum))
	})
}

func TestLongestName(t *testing.T) {
	t.Parallel()

	test.Eq(t, 0, longestName(nil))
	test.Eq(t, 5, longestName([]analysis.Function{{Name: "Short"}, {Name: "Tiny"}}))
	// Alignment is measured in runes, not bytes, so a non-ASCII name does not
	// skew the column.
	test.Eq(t, 3, longestName([]analysis.Function{{Name: "Ünï"}}))
}

func TestIndent(t *testing.T) {
	t.Parallel()

	test.Eq(t, "  ", indent("abc", 5))
	test.Eq(t, "", indent("abc", 3))
}

func TestResolveTarget(t *testing.T) {
	t.Parallel()

	t.Run("expands a directory to everything beneath it", func(t *testing.T) {
		t.Parallel()

		dir, patterns := resolveTarget(fixture("simple"), nil)

		test.Eq(t, fixture("simple"), dir)
		test.SliceEmpty(t, patterns)
	})

	t.Run("passes a package pattern through", func(t *testing.T) {
		t.Parallel()

		dir, patterns := resolveTarget("example.com/mod/...", nil)

		test.Eq(t, ".", dir)
		test.Eq(t, []string{"example.com/mod/..."}, patterns)
	})

	t.Run("prefers explicit arguments", func(t *testing.T) {
		t.Parallel()

		dir, patterns := resolveTarget(".", []string{"./alpha", "./beta"})

		test.Eq(t, ".", dir)
		test.Eq(t, []string{"./alpha", "./beta"}, patterns)
	})
}
