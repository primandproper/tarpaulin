package coverage

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/primandproper/tarpaulin/internal/analysis"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
	"golang.org/x/tools/cover"
)

// writeSource writes one source file for buildPage to read back.
func writeSource(t *testing.T, dir, name, contents string) string {
	t.Helper()

	written := filepath.Join(dir, name)
	must.NoError(t, os.WriteFile(written, []byte(contents), 0o600))

	return written
}

// block is one profile block over a single-line function body, which is the
// only shape the fixtures below need.
func block(line, count int) cover.ProfileBlock {
	return cover.ProfileBlock{StartLine: line, StartCol: 10, EndLine: line, EndCol: 17, NumStmt: 1, Count: count}
}

func TestBuildPage(t *testing.T) {
	t.Parallel()

	// buildPage reads source off disk, so these are real files; the profile
	// blocks are hand-written rather than measured by a toolchain, so the
	// numbers in the header can be checked against arithmetic.
	dir := t.TempDir()
	alpha := writeSource(t, dir, "alpha.go", "package p\n\nfunc A() { a() }\n")
	beta := writeSource(t, dir, "beta.go", "package p\n\nfunc B() { b() }\n\nfunc C() { c() }\n")

	sources := []source{
		{
			profile: &cover.Profile{
				FileName: "example.com/m/pkg/alpha.go",
				Blocks:   []cover.ProfileBlock{block(3, 2)},
			},
			path: alpha,
		},
		{
			profile: &cover.Profile{
				FileName: "example.com/m/pkg/beta.go",
				Blocks:   []cover.ProfileBlock{block(3, 1), block(5, 0)},
			},
			path: beta,
		},
	}

	report := &analysis.Report{Functions: []analysis.Function{
		{Name: "A", Path: alpha, Line: 3, EndLine: 3, Tested: true},
		{Name: "B", Path: beta, Line: 3, EndLine: 3},
		{Name: "C", Path: beta, Line: 5, EndLine: 5},
	}}

	t.Run("carries both numbers for every file and for the page", func(t *testing.T) {
		t.Parallel()

		rendered, err := buildPage(report, sources)
		must.NoError(t, err)

		must.SliceLen(t, 2, rendered.Files)

		// Per file: the statement coverage the profile measured, and the grade
		// tarp gave the functions declared in it.
		test.Eq(t, "example.com/m/pkg/alpha.go", rendered.Files[0].Name)
		test.Eq(t, 100.0, rendered.Files[0].Coverage)
		test.Eq(t, 1, rendered.Files[0].Declared)
		test.Eq(t, 1, rendered.Files[0].Tested)

		test.Eq(t, "example.com/m/pkg/beta.go", rendered.Files[1].Name)
		test.Eq(t, 50.0, rendered.Files[1].Coverage)
		test.Eq(t, 2, rendered.Files[1].Declared)
		test.Eq(t, 0, rendered.Files[1].Tested)

		// And the header totals them, truncated the way every other grade is.
		test.Eq(t, 3, rendered.Declared)
		test.Eq(t, 1, rendered.Tested)
		test.Eq(t, 33, rendered.Score)
		test.Eq(t, "pkg", rendered.Package)
	})

	t.Run("annotates each file against its own functions", func(t *testing.T) {
		t.Parallel()

		rendered, err := buildPage(report, sources)
		must.NoError(t, err)

		test.StrContains(t, string(rendered.Files[0].Body),
			`<span class="tarp-direct" title="ran 2 times; A is directly tested">{ a() }</span>`)
		test.StrContains(t, string(rendered.Files[1].Body),
			`<span class="tarp-indirect" title="ran once; B has no direct test">{ b() }</span>`)
		test.StrContains(t, string(rendered.Files[1].Body),
			`<span class="tarp-uncovered" title="never ran; C has no direct test">{ c() }</span>`)
	})

	t.Run("grades the files on screen rather than the whole report", func(t *testing.T) {
		t.Parallel()

		wider := &analysis.Report{Functions: append(
			[]analysis.Function{{Name: "Elsewhere", Path: filepath.Join(dir, "gamma.go"), Line: 3, EndLine: 3}},
			report.Functions...,
		)}

		rendered, err := buildPage(wider, sources[:1])
		must.NoError(t, err)

		// A header that counted packages the profile never mentioned would be a
		// number the reader has no way to check against the page under it.
		test.Eq(t, 1, rendered.Declared)
		test.Eq(t, 1, rendered.Tested)
		test.Eq(t, 100, rendered.Score)
	})

	t.Run("renders a file the report says nothing about", func(t *testing.T) {
		t.Parallel()

		rendered, err := buildPage(nil, sources[:1])
		must.NoError(t, err)

		must.SliceLen(t, 1, rendered.Files)
		test.Eq(t, 0, rendered.Files[0].Declared)
		// Still the file's real coverage, but no claim about who tested it, and
		// nothing to grade scores 100.
		test.Eq(t, 100.0, rendered.Files[0].Coverage)
		test.Eq(t, 100, rendered.Score)
		test.StrContains(t, string(rendered.Files[0].Body), `<span class="tarp-ungraded" title="ran 2 times; not graded">`)
	})

	t.Run("without a profile to render", func(t *testing.T) {
		t.Parallel()

		rendered, err := buildPage(report, nil)
		must.NoError(t, err)

		test.SliceEmpty(t, rendered.Files)
		test.Eq(t, "", rendered.Package)
		test.Eq(t, 100, rendered.Score)
	})

	t.Run("names the file it could not read", func(t *testing.T) {
		t.Parallel()

		missing := []source{{
			profile: &cover.Profile{FileName: "example.com/m/pkg/gone.go"},
			path:    filepath.Join(dir, "gone.go"),
		}}

		_, err := buildPage(report, missing)

		must.Error(t, err)
		// The profile's spelling, not the path on disk: that is the name the
		// operator can go looking for in the profile they handed over.
		test.StrContains(t, err.Error(), "reading example.com/m/pkg/gone.go")
	})
}

func TestFunctionsByPath(t *testing.T) {
	t.Parallel()

	t.Run("groups by file and sorts by line", func(t *testing.T) {
		t.Parallel()

		byFile := functionsByPath(&analysis.Report{Functions: []analysis.Function{
			{Name: "second", Path: "/src/a.go", Line: 20},
			{Name: "first", Path: "/src/a.go", Line: 10},
			{Name: "other", Path: "/src/b.go", Line: 1},
		}})

		must.MapLen(t, 2, byFile)
		must.SliceLen(t, 2, byFile["/src/a.go"])
		test.Eq(t, "first", byFile["/src/a.go"][0].Name)
		test.Eq(t, "second", byFile["/src/a.go"][1].Name)
		test.Eq(t, "other", byFile["/src/b.go"][0].Name)
	})

	t.Run("without a report", func(t *testing.T) {
		t.Parallel()

		test.MapEmpty(t, functionsByPath(nil))
	})
}

func TestTestedCount(t *testing.T) {
	t.Parallel()

	test.Eq(t, 0, testedCount(nil))
	test.Eq(t, 2, testedCount([]analysis.Function{
		{Tested: true},
		{Tested: false},
		{Tested: true},
	}))
}

func TestScore(t *testing.T) {
	t.Parallel()

	// Truncated, not rounded, so only a genuinely complete file reaches 100 —
	// the same arithmetic analysis.Report.Score uses.
	test.Eq(t, 100, score(0, 0))
	test.Eq(t, 66, score(2, 3))
	test.Eq(t, 75, score(3, 4))
	test.Eq(t, 0, score(0, 5))
}

func TestPercentCovered(t *testing.T) {
	t.Parallel()

	test.Eq(t, 0.0, percentCovered(&cover.Profile{}))
	test.Eq(t, 100.0, percentCovered(&cover.Profile{Blocks: []cover.ProfileBlock{{NumStmt: 3, Count: 1}}}))
	test.Eq(t, 25.0, percentCovered(&cover.Profile{Blocks: []cover.ProfileBlock{
		{NumStmt: 1, Count: 2},
		{NumStmt: 3, Count: 0},
	}}))
}

func TestPackageName(t *testing.T) {
	t.Parallel()

	test.Eq(t, "", packageName(nil))
	test.Eq(t, "simple", packageName([]pageFile{{Name: "example.com/m/internal/simple/main.go"}}))
	test.Eq(t, "m", packageName([]pageFile{{Name: "example.com/m/main.go"}}))
}
