package config

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/primandproper/tarpaulin/internal/analysis"

	"github.com/primandproper/platform-go/v10/encoding"
	platformerrors "github.com/primandproper/platform-go/v10/errors"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// writeFile writes contents to a path under dir, creating the parents.
func writeFile(t *testing.T, dir, name, contents string) string {
	t.Helper()

	path := filepath.Join(dir, name)
	must.NoError(t, os.MkdirAll(filepath.Dir(path), 0o750))
	must.NoError(t, os.WriteFile(path, []byte(contents), 0o600))

	return path
}

// module builds a temporary directory that looks like a Go module root, which
// is where discovery stops.
func module(t *testing.T) string {
	t.Helper()

	dir := t.TempDir()
	writeFile(t, dir, goModFile, "module example.com/project\n")

	return dir
}

func TestDiscover(t *testing.T) {
	t.Parallel()

	t.Run("finds the file in the directory itself", func(t *testing.T) {
		t.Parallel()

		dir := module(t)
		want := writeFile(t, dir, FileStem+".yaml", "analyze:\n  strictness: any\n")

		found, err := Discover(dir)
		must.NoError(t, err)
		test.Eq(t, want, found)
	})

	t.Run("walks up to the module root", func(t *testing.T) {
		t.Parallel()

		// The point of walking: `tarp analyze` run from internal/cli has to
		// mean what it means from the root, or the exclusions a project agreed
		// on hold only some of the time.
		dir := module(t)
		want := writeFile(t, dir, FileStem+".json", "{}")
		writeFile(t, dir, filepath.Join("internal", "cli", "root.go"), "package cli\n")

		found, err := Discover(filepath.Join(dir, "internal", "cli"))
		must.NoError(t, err)
		test.Eq(t, want, found)
	})

	t.Run("the nearest one wins", func(t *testing.T) {
		t.Parallel()

		dir := module(t)
		writeFile(t, dir, FileStem+".json", "{}")
		want := writeFile(t, dir, filepath.Join("nested", FileStem+".json"), "{}")

		found, err := Discover(filepath.Join(dir, "nested"))
		must.NoError(t, err)
		test.Eq(t, want, found)
	})

	t.Run("stops at the module root", func(t *testing.T) {
		t.Parallel()

		// Above the module root lies somebody's home directory, and a stray
		// .tarp.yaml there should not silently govern every module beneath it.
		outer := t.TempDir()
		writeFile(t, outer, FileStem+".yaml", "analyze:\n  strictness: any\n")

		inner := filepath.Join(outer, "project")
		writeFile(t, inner, goModFile, "module example.com/project\n")

		found, err := Discover(inner)
		must.NoError(t, err)
		test.Eq(t, "", found)
	})

	t.Run("no config file is not an error", func(t *testing.T) {
		t.Parallel()

		// Running without one is the normal case, and the defaults are a
		// complete configuration.
		found, err := Discover(module(t))
		must.NoError(t, err)
		test.Eq(t, "", found)
	})

	t.Run("two files in one directory is an error", func(t *testing.T) {
		t.Parallel()

		// Picking a winner by extension would mean a project could edit
		// .tarp.yaml for an afternoon while .tarp.json quietly decided
		// everything.
		dir := module(t)
		writeFile(t, dir, FileStem+".yaml", "{}")
		writeFile(t, dir, FileStem+".toml", "")

		_, err := Discover(dir)

		must.ErrorIs(t, err, ErrAmbiguousConfigFile)
		test.StrContains(t, err.Error(), FileStem+".yaml")
		test.StrContains(t, err.Error(), FileStem+".toml")
	})

	t.Run("a directory named like the config file is not one", func(t *testing.T) {
		t.Parallel()

		dir := module(t)
		must.NoError(t, os.MkdirAll(filepath.Join(dir, FileStem+".yaml"), 0o750))

		found, err := Discover(dir)
		must.NoError(t, err)
		test.Eq(t, "", found)
	})
}

func TestLoadFromFileFormats(t *testing.T) {
	t.Parallel()

	// The same configuration in each of the three formats. Every key is spelled
	// identically in all of them, which is the point of tagging the fields for
	// all three rather than leaning on each decoder's fallback.
	cases := map[string]string{
		FileStem + ".yaml": "analyze:\n" +
			"  strictness: package\n" +
			"  minScore: 80\n" +
			"  failOnFound: true\n" +
			"exclude:\n" +
			"  paths:\n" +
			"    - internal/generated/**\n" +
			"  functions:\n" +
			"    - '*.MarshalJSON'\n",
		FileStem + ".json": `{
			"analyze": {"strictness": "package", "minScore": 80, "failOnFound": true},
			"exclude": {"paths": ["internal/generated/**"], "functions": ["*.MarshalJSON"]}
		}`,
		FileStem + ".toml": "[analyze]\n" +
			"strictness = \"package\"\n" +
			"minScore = 80\n" +
			"failOnFound = true\n" +
			"[exclude]\n" +
			"paths = [\"internal/generated/**\"]\n" +
			"functions = [\"*.MarshalJSON\"]\n",
	}

	for name, contents := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			path := writeFile(t, t.TempDir(), name, contents)

			cfg, err := LoadFromFile(context.Background(), path)
			must.NoError(t, err)

			test.Eq(t, analysis.StrictnessPackage.String(), cfg.Analyze.Strictness)
			test.Eq(t, 80, cfg.Analyze.MinScore)
			test.True(t, cfg.Analyze.FailOnFound)
			test.Eq(t, []string{"internal/generated/**"}, cfg.Exclude.Paths)
			test.Eq(t, []string{"*.MarshalJSON"}, cfg.Exclude.Functions)

			// Everything the file declined to mention is still the default.
			test.Eq(t, DefaultPackage, cfg.Analyze.Package)
			test.Eq(t, DefaultFormat, cfg.Analyze.Format)
			test.Eq(t, DefaultServiceName, cfg.Observability.Logging.ServiceName)
		})
	}
}

func TestLoadFromFileErrors(t *testing.T) {
	t.Parallel()

	t.Run("an extension nothing can decode", func(t *testing.T) {
		t.Parallel()

		// Chosen by extension rather than by sniffing the contents, so that a
		// typo in a YAML file is never read as a TOML file that happens to
		// parse.
		path := writeFile(t, t.TempDir(), FileStem+".ini", "[analyze]\n")

		_, err := LoadFromFile(context.Background(), path)

		must.ErrorIs(t, err, platformerrors.ErrUnrecognizedInputValue)
		test.StrContains(t, err.Error(), ".toml")
	})

	t.Run("a file that does not parse", func(t *testing.T) {
		t.Parallel()

		path := writeFile(t, t.TempDir(), FileStem+".yaml", "analyze:\n  strictness: [unterminated\n")

		_, err := LoadFromFile(context.Background(), path)
		must.Error(t, err)
		test.StrContains(t, err.Error(), path)
	})
}

func TestConfigFileIn(t *testing.T) {
	t.Parallel()

	t.Run("reports the one file it finds", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		want := writeFile(t, dir, FileStem+".toml", "")

		found, err := configFileIn(dir)
		must.NoError(t, err)
		test.Eq(t, want, found)
	})

	t.Run("reports nothing for a directory that has none", func(t *testing.T) {
		t.Parallel()

		found, err := configFileIn(t.TempDir())
		must.NoError(t, err)
		test.Eq(t, "", found)
	})

	t.Run("refuses to choose between two", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		writeFile(t, dir, FileStem+".json", "{}")
		writeFile(t, dir, FileStem+".yml", "")

		_, err := configFileIn(dir)
		must.ErrorIs(t, err, ErrAmbiguousConfigFile)
	})
}

func TestIsModuleRoot(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()
	test.False(t, isModuleRoot(dir))

	writeFile(t, dir, goModFile, "module example.com/project\n")
	test.True(t, isModuleRoot(dir))

	// A directory named go.mod is not a module root, and the walk has to keep
	// going rather than stopping at it.
	other := t.TempDir()
	must.NoError(t, os.MkdirAll(filepath.Join(other, goModFile), 0o750))
	test.False(t, isModuleRoot(other))
}

func TestContentTypeOf(t *testing.T) {
	t.Parallel()

	for extension, want := range map[string]encoding.ContentType{
		".json": encoding.ContentTypeJSON,
		".toml": encoding.ContentTypeTOML,
		".yaml": encoding.ContentTypeYAML,
		".yml":  encoding.ContentTypeYAML,
		".YAML": encoding.ContentTypeYAML,
	} {
		contentType, err := contentTypeOf(FileStem + extension)

		must.NoError(t, err, must.Sprintf("extension %q", extension))
		test.Eq(t, want, contentType, test.Sprintf("extension %q", extension))
	}

	_, err := contentTypeOf(FileStem + ".ini")

	must.Error(t, err)
	test.ErrorIs(t, err, platformerrors.ErrUnrecognizedInputValue)
}
