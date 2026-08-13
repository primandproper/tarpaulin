package coverage

import (
	"testing"

	"github.com/primandproper/tarpaulin/internal/analysis"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
	"golang.org/x/tools/cover"
)

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
