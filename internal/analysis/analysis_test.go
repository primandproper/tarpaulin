package analysis_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/primandproper/tarpaulin/internal/analysis"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// simpleReportJSON is the wire contract other tools parse. It is written out in
// full, because a change to any of it is a change other people's CI notices.
const simpleReportJSON = `{
  "strictness": "file",
  "untested": [
    {
      "package": "simple",
      "file": "main.go",
      "name": "B",
      "line": 7
    }
  ],
  "warnings": [],
  "declared": 4,
  "tested": 3,
  "score": 75
}`

func TestAnalyze(t *testing.T) {
	t.Parallel()

	t.Run("reports the function that go test -cover cannot see", func(t *testing.T) {
		t.Parallel()

		// simple is the canonical example: A, B and C are each called by
		// wrapper, and wrapper has a test, so statement coverage reads 100%
		// while B has no test of its own.
		report, err := analysis.Analyze(t.Context(), analysis.Config{Dir: filepath.Join(corpusDir, "simple")})
		must.NoError(t, err)

		mainGo, err := filepath.Abs(filepath.Join(corpusDir, "simple", "main.go"))
		must.NoError(t, err)

		test.Eq(t, 4, report.Declared())
		test.Eq(t, 3, report.Tested())
		test.Eq(t, 75, report.Score())
		test.Eq(t, []analysis.Function{{
			Package: "simple",
			// The import path as well as the clause name: a module of real size
			// holds many packages named the same thing, and only the path tells
			// them apart.
			PackagePath: "github.com/primandproper/tarpaulin/internal/analysis/testdata/simple",
			File:        "main.go",
			Path:        mainGo,
			Name:        "B",
			Line:        7,
			EndLine:     7,
		}}, report.Untested())
	})

	t.Run("records where each declaration ends", func(t *testing.T) {
		t.Parallel()

		// The line range is what lets a consumer holding a line number — a
		// coverage block — ask which function it fell inside.
		report, err := analysis.Analyze(t.Context(), analysis.Config{Dir: filepath.Join(corpusDir, "simple")})
		must.NoError(t, err)

		spans := make(map[string][2]int, report.Declared())
		for _, fn := range report.Functions {
			spans[fn.Name] = [2]int{fn.Line, fn.EndLine}
		}

		test.Eq(t, [2]int{3, 3}, spans["A"])
		test.Eq(t, [2]int{11, 15}, spans["wrapper"])
	})

	t.Run("defaults to the strictest setting", func(t *testing.T) {
		t.Parallel()

		// cross_file's declaration is tested from a sibling file's test, which
		// only the loosened dial accepts. The zero value must be the strict one.
		report, err := analysis.Analyze(t.Context(), analysis.Config{Dir: filepath.Join(corpusDir, "cross_file")})
		must.NoError(t, err)

		must.SliceLen(t, 1, report.Untested())
		test.Eq(t, "Cross", report.Untested()[0].Name)
		test.Eq(t, analysis.StrictnessFile, report.Strictness)
	})

	t.Run("emits stable JSON", func(t *testing.T) {
		t.Parallel()

		report, err := analysis.Analyze(t.Context(), analysis.Config{Dir: filepath.Join(corpusDir, "simple")})
		must.NoError(t, err)

		// Deliberately the standard library rather than platform-go's encoding
		// package: what is under test here is that a Report handed to any
		// ordinary Go consumer encodes to the documented shape.
		encoded, err := json.MarshalIndent(report, "", "  ")
		must.NoError(t, err)

		test.Eq(t, simpleReportJSON, string(encoded))
	})

	t.Run("orders findings by file and line, run after run", func(t *testing.T) {
		t.Parallel()

		// The 2017 implementation ranged a map straight into a template, so the
		// order of its output was whatever Go's map iteration felt like.
		var first []byte

		for range 5 {
			report, err := analysis.Analyze(t.Context(), analysis.Config{Dir: filepath.Join(corpusDir, "ordering")})
			must.NoError(t, err)

			encoded, err := json.Marshal(report)
			must.NoError(t, err)

			if first == nil {
				first = encoded

				continue
			}

			must.True(t, bytes.Equal(first, encoded), must.Sprint("output differed between runs"))
		}

		report, err := analysis.Analyze(t.Context(), analysis.Config{Dir: filepath.Join(corpusDir, "ordering")})
		must.NoError(t, err)

		test.Eq(t, []string{"alpha.go:4 AlphaOne", "alpha.go:7 AlphaTwo", "beta.go:4 BetaOne", "beta.go:7 BetaTwo"},
			identities(report.Untested()))
	})

	t.Run("warns about an ignore directive with no reason", func(t *testing.T) {
		t.Parallel()

		report, err := analysis.Analyze(t.Context(), analysis.Config{Dir: filepath.Join(corpusDir, "ignore_directive")})
		must.NoError(t, err)

		must.SliceLen(t, 1, report.Warnings)
		test.StrContains(t, report.Warnings[0], "Reasonless")
		test.StrContains(t, report.Warnings[0], analysis.IgnoreDirective)
	})
}

func TestAnalyzeDiagnostics(t *testing.T) {
	t.Parallel()

	t.Run("a directory with no Go files", func(t *testing.T) {
		t.Parallel()

		_, err := analysis.Analyze(t.Context(), analysis.Config{Dir: filepath.Join(corpusDir, "no_go_files")})

		must.ErrorIs(t, err, analysis.ErrNoGoFiles)
	})

	t.Run("a directory outside every module", func(t *testing.T) {
		t.Parallel()

		// Packages load in module mode, where the go command's own account of
		// this is "directory prefix . does not contain main module or its
		// selected dependencies" — the symptom, not the cause.
		dir := t.TempDir()
		must.NoError(t, os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n\nfunc A() {}\n"), 0o600))

		_, err := analysis.Analyze(t.Context(), analysis.Config{Dir: dir})

		must.ErrorIs(t, err, analysis.ErrNotInModule)
		test.StrContains(t, err.Error(), "go mod init")
	})

	cases := map[string]string{
		"source that does not parse":        "unparseable",
		"source that does not type check":   "broken_package",
		"a test file that does not compile": "broken_test",
	}

	for name, fixture := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			_, err := analysis.Analyze(t.Context(), analysis.Config{Dir: filepath.Join(corpusDir, fixture)})
			must.Error(t, err)

			// The old implementation parsed with parser.AllErrors and analyzed
			// whatever it got. Refusing is the improvement; refusing legibly is
			// the requirement.
			diagnostic := new(analysis.DiagnosticError)
			must.True(t, errors.As(err, &diagnostic), must.Sprintf("expected a diagnostic error, got %v", err))
			must.SliceNotEmpty(t, diagnostic.Diagnostics)
		})
	}
}
