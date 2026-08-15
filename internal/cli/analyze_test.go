package cli

import (
	"bytes"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/primandproper/tarpaulin/internal/analysis"
	"github.com/primandproper/tarpaulin/internal/config"

	"github.com/primandproper/platform-go/v10/encoding"
	platformerrors "github.com/primandproper/platform-go/v10/errors"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
	"github.com/spf13/cobra"
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

func TestRenderText(t *testing.T) {
	t.Parallel()

	t.Run("aligns names into a column measured in runes", func(t *testing.T) {
		t.Parallel()

		out := new(bytes.Buffer)

		must.NoError(t, renderText(out, &analysis.Report{Functions: []analysis.Function{
			{File: "a.go", Name: "Tested", Line: 1, Tested: true},
			{File: "a.go", Name: "Ünïcödé", Line: 3},
			{File: "a.go", Name: "Sh", Line: 5},
		}}))

		// The column is seven wide because the widest name is seven runes, not
		// the eleven bytes it takes to write them.
		test.Eq(t, "Functions without direct unit tests:\n"+
			"in a.go:\n"+
			"\tÜnïcödé on line 3\n"+
			"\t     Sh on line 5\n"+
			"\nGrade: 33% (1/3 functions)\n", out.String())
	})

	t.Run("announces a file once, however many functions it holds", func(t *testing.T) {
		t.Parallel()

		out := new(bytes.Buffer)

		must.NoError(t, renderText(out, &analysis.Report{Functions: []analysis.Function{
			{File: "a.go", Name: "One", Line: 3},
			{File: "a.go", Name: "Two", Line: 5},
			{File: "b.go", Name: "Three", Line: 7},
		}}))

		test.Eq(t, 1, strings.Count(out.String(), "in a.go:"))
		test.Eq(t, 1, strings.Count(out.String(), "in b.go:"))
	})

	t.Run("leaves escape codes out of a destination that is not a terminal", func(t *testing.T) {
		t.Parallel()

		out := new(bytes.Buffer)

		// This is the whole reason the palette is decided from the writer: the
		// file headers and the grade are the two painted runs, and a buffer —
		// a pipe, a file, a CI log — gets neither.
		must.NoError(t, renderText(out, &analysis.Report{Functions: []analysis.Function{
			{File: "a.go", Name: "One", Line: 3},
		}}))

		test.StrNotContains(t, out.String(), "\033[")
	})

	t.Run("surfaces a broken writer", func(t *testing.T) {
		t.Parallel()

		// The report is built in memory and written once, so a failing
		// destination is reported rather than left half-printed.
		test.ErrorIs(t, renderText(brokenWriter{}, &analysis.Report{}), errBrokenWriter)
	})
}

func TestRenderMarkdown(t *testing.T) {
	t.Parallel()

	t.Run("grades each package on its own functions", func(t *testing.T) {
		t.Parallel()

		out := new(bytes.Buffer)

		must.NoError(t, renderMarkdown(out, &analysis.Report{
			Strictness: analysis.StrictnessAny,
			Functions: []analysis.Function{
				{PackagePath: "example.com/m/thirds", File: "t.go", Name: "One", Line: 3, Tested: true},
				{PackagePath: "example.com/m/thirds", File: "t.go", Name: "Two", Line: 5, Tested: true},
				{PackagePath: "example.com/m/thirds", File: "t.go", Name: "Three", Line: 7},
				{PackagePath: "example.com/m/done", File: "d.go", Name: "Four", Line: 3, Tested: true},
			},
		}))

		// Every row is truncated the way the total is, so a package two thirds
		// of the way there never reads as 67% while the module reads as 66%.
		test.Eq(t, "| Package | Score | Tested | Declared |\n"+
			"| --- | ---: | ---: | ---: |\n"+
			"| `example.com/m/done` | 100% | 1 | 1 |\n"+
			"| `example.com/m/thirds` | 66% | 2 | 3 |\n"+
			"| **Total** | **75%** | **3** | **4** |\n"+
			"\nGraded at `any` strictness.\n", out.String())
	})

	t.Run("names the strictness the report was graded at", func(t *testing.T) {
		t.Parallel()

		out := new(bytes.Buffer)

		// A table gets pasted somewhere the command line that produced it is
		// not, so the dial it was measured on travels with it.
		must.NoError(t, renderMarkdown(out, &analysis.Report{Strictness: analysis.StrictnessPackage}))

		test.StrHasSuffix(t, "\nGraded at `package` strictness.\n", out.String())
	})

	t.Run("surfaces a broken writer", func(t *testing.T) {
		t.Parallel()

		test.ErrorIs(t, renderMarkdown(brokenWriter{}, &analysis.Report{}), errBrokenWriter)
	})
}

func TestWriteWarnings(t *testing.T) {
	t.Parallel()

	t.Run("writes one warning per line", func(t *testing.T) {
		t.Parallel()

		out := new(bytes.Buffer)

		must.NoError(t, writeWarnings(out, []string{"first", "second"}))
		test.Eq(t, "first\nsecond\n", out.String())
	})

	t.Run("writes nothing when there is nothing to warn about", func(t *testing.T) {
		t.Parallel()

		out := new(bytes.Buffer)

		must.NoError(t, writeWarnings(out, nil))
		must.NoError(t, writeWarnings(out, []string{}))
		test.Eq(t, "", out.String())
	})

	t.Run("surfaces a broken writer", func(t *testing.T) {
		t.Parallel()

		err := writeWarnings(brokenWriter{}, []string{"first"})

		test.ErrorIs(t, err, errBrokenWriter)
		test.StrContains(t, err.Error(), "writing warnings")
	})
}

func TestResolveGates(t *testing.T) {
	t.Parallel()

	t.Run("a typed flag overrides the config file", func(t *testing.T) {
		t.Parallel()

		app := configured(projectConfig())

		opts := &analyzeOptions{}
		cmd := &cobra.Command{Use: "analyze"}
		registerAnalyzeFlags(cmd, opts)

		must.NoError(t, cmd.ParseFlags([]string{"--min-score", "10"}))

		gates, err := app.resolveGates(cmd, opts)
		must.NoError(t, err)

		test.Eq(t, 10, gates.MinScore)
		// The two nobody typed still come from the file.
		test.Eq(t, formatJSON.String(), gates.Format)
		test.True(t, gates.FailOnFound)
	})

	t.Run("nothing configured is the defaults", func(t *testing.T) {
		t.Parallel()

		opts := &analyzeOptions{}
		cmd := &cobra.Command{Use: "analyze"}
		registerAnalyzeFlags(cmd, opts)

		gates, err := (&application{}).resolveGates(cmd, opts)
		must.NoError(t, err)

		test.Eq(t, config.DefaultFormat, gates.Format)
		test.Eq(t, config.DefaultMinScore, gates.MinScore)
		test.False(t, gates.FailOnFound)
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
