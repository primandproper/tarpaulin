package cli

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/primandproper/tarpaulin/internal/analysis"

	"github.com/primandproper/platform-go/v10/encoding"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// fixture locates a corpus package from the CLI package's directory.
func fixture(name string) string {
	return filepath.Join("..", "analysis", "testdata", name)
}

// runAnalyzeCommand executes the analyze subcommand as the binary would, and
// returns what it wrote to stdout and stderr.
func runAnalyzeCommand(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()

	out, errOut := new(bytes.Buffer), new(bytes.Buffer)

	cmd := (&application{}).newAnalyzeCommand()
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

	t.Run("fails on found, without burying the report in an error", func(t *testing.T) {
		t.Parallel()

		stdout, _, err := runAnalyzeCommand(t, "--package", fixture("simple"), "--fail-on-found")

		must.ErrorIs(t, err, errFunctionsFound)
		test.StrContains(t, stdout, "B on line 7")
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
		must.NoError(t, render(out, report, false))

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
		must.NoError(t, render(out, report, true))

		test.StrHasSuffix(t, "\n", out.String())
		must.NoError(t, encoding.DecodeJSON(out.Bytes(), &map[string]any{}))
	})

	t.Run("says nothing but the grade when there is nothing to report", func(t *testing.T) {
		t.Parallel()

		out := new(bytes.Buffer)
		must.NoError(t, render(out, &analysis.Report{}, false))

		test.Eq(t, "Grade: 100% (0/0 functions)\n", out.String())
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
