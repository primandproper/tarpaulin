package coverage_test

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/primandproper/tarpaulin/internal/analysis"
	"github.com/primandproper/tarpaulin/internal/coverage"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// simpleProfile is a real `go test -covermode=count` profile over the analysis
// corpus' `simple` package: the PRD's opening example, where every statement
// runs and one function still has no test of its own. It is checked in rather
// than generated so the test needs no toolchain of its own — the cost is that
// editing simple/main.go shifts the line and column numbers in it out from
// under the assertions here, which is what those assertions are for.
const simpleProfile = "testdata/simple.out"

// simpleDir is the package that profile covers.
var simpleDir = filepath.Join("..", "analysis", "testdata", "simple")

// renderSimple analyzes the simple fixture and renders the profile against it.
func renderSimple(t *testing.T) string {
	t.Helper()

	report, err := analysis.Analyze(t.Context(), analysis.Config{Dir: simpleDir})
	must.NoError(t, err)

	var out strings.Builder

	must.NoError(t, coverage.Render(t.Context(), &out, coverage.Config{
		Report:  report,
		Dir:     simpleDir,
		Profile: simpleProfile,
	}))

	return out.String()
}

// writeProfile writes a profile the test built for itself.
func writeProfile(t *testing.T, contents string) string {
	t.Helper()

	path := filepath.Join(t.TempDir(), "profile.out")
	must.NoError(t, os.WriteFile(path, []byte(contents), 0o600))

	return path
}

func TestRender(t *testing.T) {
	t.Parallel()

	t.Run("splits covered code by whether a test names the function", func(t *testing.T) {
		t.Parallel()

		rendered := renderSimple(t)

		// The whole point of the report: A and C are green, B ran on wrapper's
		// coattails and is yellow, and `go tool cover` calls all three the same.
		test.StrContains(t, rendered, `<span class="tarp-direct" title="ran 2 times; A is directly tested">{ return &#34;A&#34; }</span>`)
		test.StrContains(t, rendered, `<span class="tarp-indirect" title="ran once; B has no direct test">{ return &#34;B&#34; }</span>`)
		test.StrContains(t, rendered, `<span class="tarp-direct" title="ran 2 times; C is directly tested">{ return &#34;C&#34; }</span>`)
	})

	t.Run("reports both numbers in the header", func(t *testing.T) {
		t.Parallel()

		rendered := renderSimple(t)

		test.StrContains(t, rendered, "Grade: 75% (3/4 functions)")
		test.StrContains(t, rendered,
			`<option value="file0">github.com/primandproper/tarpaulin/internal/analysis/testdata/simple/main.go (100.0% covered, 3/4 tested)</option>`)
		test.StrContains(t, rendered, "<title>simple: tarp coverage</title>")
	})

	t.Run("balances every span it opens", func(t *testing.T) {
		t.Parallel()

		rendered := renderSimple(t)

		test.Eq(t, strings.Count(rendered, "<span"), strings.Count(rendered, "</span>"))
	})

	t.Run("grades nothing without a report", func(t *testing.T) {
		t.Parallel()

		var out strings.Builder

		must.NoError(t, coverage.Render(t.Context(), &out, coverage.Config{
			Dir:     simpleDir,
			Profile: simpleProfile,
		}))

		rendered := out.String()

		// No verdicts to render means no claims: every block that ran is grey,
		// and a report with nothing to grade scores 100.
		test.StrContains(t, rendered, `<span class="tarp-ungraded" title="ran once; not graded">`)
		test.StrNotContains(t, rendered, `<span class="tarp-direct" title=`)
		test.StrContains(t, rendered, "Grade: 100% (0/0 functions)")
	})

	t.Run("rejects a profile it cannot read", func(t *testing.T) {
		t.Parallel()

		var out strings.Builder

		err := coverage.Render(t.Context(), &out, coverage.Config{Profile: "testdata/nonexistent.out"})

		must.Error(t, err)
		test.StrContains(t, err.Error(), "parsing cover profile testdata/nonexistent.out")
	})

	t.Run("rejects a profile that describes nothing", func(t *testing.T) {
		t.Parallel()

		var out strings.Builder

		err := coverage.Render(t.Context(), &out, coverage.Config{Profile: writeProfile(t, "mode: set\n")})

		must.ErrorIs(t, err, coverage.ErrEmptyProfile)
	})

	t.Run("says so when the profile names source it cannot find", func(t *testing.T) {
		t.Parallel()

		stale := writeProfile(t, "mode: set\nexample.com/gone/away/thing.go:1.1,2.2 1 1\n")

		var out strings.Builder

		err := coverage.Render(t.Context(), &out, coverage.Config{Dir: simpleDir, Profile: stale})

		must.ErrorIs(t, err, coverage.ErrUnresolvedFile)
		test.StrContains(t, err.Error(), "example.com/gone/away/thing.go")
	})
}
