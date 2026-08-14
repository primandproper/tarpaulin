package coverage

import (
	"path/filepath"
	"testing"

	"github.com/primandproper/tarpaulin/internal/analysis"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
	"golang.org/x/tools/cover"
)

// simplePkg is the import path of the analysis corpus' canonical fixture, which
// is the package the checked-in profile covers.
const simplePkg = "github.com/primandproper/tarpaulin/internal/analysis/testdata/simple"

// unloadableDir is a directory no package load can succeed in. Passing it
// asserts the negative the report's file list exists for: that resolving a
// profile the analysis already covered loads nothing at all.
func unloadableDir(t *testing.T) string {
	t.Helper()

	return filepath.Join(t.TempDir(), "no-such-directory")
}

// simpleReport is an analysis whose file list names the given paths as the
// fixture's, without running one.
func simpleReport(paths ...string) *analysis.Report {
	return &analysis.Report{Sources: map[string][]string{simplePkg: paths}}
}

func TestProfileKey(t *testing.T) {
	t.Parallel()

	// The two spellings a file arrives in: a loaded path carrying the host's
	// separator, and the slash-separated package path a profile names.
	located := filepath.FromSlash("/abs/wherever/thing.go")

	test.Eq(t, "example.com/m/pkg/thing.go", profileKey("example.com/m/pkg", located))
}

func TestIndexReport(t *testing.T) {
	t.Parallel()

	t.Run("keys the loaded files the way a profile spells them", func(t *testing.T) {
		t.Parallel()

		located := filepath.FromSlash("/src/simple/main.go")

		index := indexReport(simpleReport(located))

		test.Eq(t, map[string]string{simplePkg + "/main.go": located}, index)
	})

	t.Run("no report", func(t *testing.T) {
		t.Parallel()

		test.MapEmpty(t, indexReport(nil))
	})
}

func TestMissingPackages(t *testing.T) {
	t.Parallel()

	t.Run("one entry per package, sorted", func(t *testing.T) {
		t.Parallel()

		paths := missingPackages(nil, []*cover.Profile{
			{FileName: "example.com/m/pkg/b.go"},
			{FileName: "example.com/m/a.go"},
			{FileName: "example.com/m/pkg/a.go"},
		})

		test.Eq(t, []string{"example.com/m", "example.com/m/pkg"}, paths)
	})

	t.Run("skips what the index already accounts for", func(t *testing.T) {
		t.Parallel()

		index := map[string]string{"example.com/m/pkg/a.go": "/src/pkg/a.go"}

		paths := missingPackages(index, []*cover.Profile{
			{FileName: "example.com/m/pkg/a.go"},
			{FileName: "example.com/m/other/a.go"},
		})

		test.Eq(t, []string{"example.com/m/other"}, paths)
	})

	t.Run("a fully indexed profile loads nothing", func(t *testing.T) {
		t.Parallel()

		index := map[string]string{"example.com/m/pkg/a.go": "/src/pkg/a.go"}

		test.SliceEmpty(t, missingPackages(index, []*cover.Profile{{FileName: "example.com/m/pkg/a.go"}}))
	})

	t.Run("no profiles", func(t *testing.T) {
		t.Parallel()

		test.SliceEmpty(t, missingPackages(nil, nil))
	})
}

func TestLocateFiles(t *testing.T) {
	t.Parallel()

	t.Run("keys files the way a cover profile spells them", func(t *testing.T) {
		t.Parallel()

		index, err := locateFiles(t.Context(), ".", []string{simplePkg})
		must.NoError(t, err)

		located, ok := index[simplePkg+"/main.go"]
		must.True(t, ok)
		test.True(t, filepath.IsAbs(located))
		test.Eq(t, "main.go", filepath.Base(located))
	})

	t.Run("nothing to locate", func(t *testing.T) {
		t.Parallel()

		index, err := locateFiles(t.Context(), ".", nil)
		must.NoError(t, err)
		test.MapEmpty(t, index)
	})
}

func TestResolveSources(t *testing.T) {
	t.Parallel()

	t.Run("pairs each profile with its source", func(t *testing.T) {
		t.Parallel()

		profile := &cover.Profile{FileName: simplePkg + "/main.go"}

		sources, err := resolveSources(t.Context(), ".", nil, []*cover.Profile{profile})
		must.NoError(t, err)

		must.SliceLen(t, 1, sources)
		test.Eq(t, profile, sources[0].profile)
		test.Eq(t, "main.go", filepath.Base(sources[0].path))
	})

	t.Run("resolves from the report without loading again", func(t *testing.T) {
		t.Parallel()

		located := filepath.FromSlash("/src/simple/main.go")
		profile := &cover.Profile{FileName: simplePkg + "/main.go"}

		// The directory would fail any load, so arriving at an answer at all is
		// the proof that the report's file list was enough.
		sources, err := resolveSources(t.Context(), unloadableDir(t), simpleReport(located), []*cover.Profile{profile})
		must.NoError(t, err)

		must.SliceLen(t, 1, sources)
		test.Eq(t, located, sources[0].path)
	})

	t.Run("loads what the report does not cover", func(t *testing.T) {
		t.Parallel()

		// A report that named some other file entirely: the fixture still has to
		// be found, and only the load can find it.
		report := simpleReport(filepath.FromSlash("/src/elsewhere/other.go"))
		profile := &cover.Profile{FileName: simplePkg + "/main.go"}

		sources, err := resolveSources(t.Context(), ".", report, []*cover.Profile{profile})
		must.NoError(t, err)

		must.SliceLen(t, 1, sources)
		test.True(t, filepath.IsAbs(sources[0].path))
		test.Eq(t, "main.go", filepath.Base(sources[0].path))
	})

	t.Run("refuses a file it cannot find", func(t *testing.T) {
		t.Parallel()

		_, err := resolveSources(t.Context(), ".", nil, []*cover.Profile{{FileName: "example.com/gone/thing.go"}})

		must.ErrorIs(t, err, ErrUnresolvedFile)
	})
}
