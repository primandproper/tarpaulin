package coverage

import (
	"path/filepath"
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
	"golang.org/x/tools/cover"
)

func TestPackagePaths(t *testing.T) {
	t.Parallel()

	t.Run("one entry per package, sorted", func(t *testing.T) {
		t.Parallel()

		paths := packagePaths([]*cover.Profile{
			{FileName: "example.com/m/pkg/b.go"},
			{FileName: "example.com/m/a.go"},
			{FileName: "example.com/m/pkg/a.go"},
		})

		test.Eq(t, []string{"example.com/m", "example.com/m/pkg"}, paths)
	})

	t.Run("no profiles", func(t *testing.T) {
		t.Parallel()

		test.SliceEmpty(t, packagePaths(nil))
	})
}

func TestLocateFiles(t *testing.T) {
	t.Parallel()

	const simplePkg = "github.com/primandproper/tarpaulin/internal/analysis/testdata/simple"

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

		profile := &cover.Profile{
			FileName: "github.com/primandproper/tarpaulin/internal/analysis/testdata/simple/main.go",
		}

		sources, err := resolveSources(t.Context(), ".", []*cover.Profile{profile})
		must.NoError(t, err)

		must.SliceLen(t, 1, sources)
		test.Eq(t, profile, sources[0].profile)
		test.Eq(t, "main.go", filepath.Base(sources[0].path))
	})

	t.Run("refuses a file it cannot find", func(t *testing.T) {
		t.Parallel()

		_, err := resolveSources(t.Context(), ".", []*cover.Profile{{FileName: "example.com/gone/thing.go"}})

		must.ErrorIs(t, err, ErrUnresolvedFile)
	})
}
