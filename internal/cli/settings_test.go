package cli

import (
	"testing"

	"github.com/primandproper/tarpaulin/internal/analysis"
	"github.com/primandproper/tarpaulin/internal/config"

	platformerrors "github.com/primandproper/platform-go/v10/errors"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
	"github.com/spf13/cobra"
)

// configured builds an application holding a config file's worth of settings,
// which is what bootstrap would have left behind.
func configured(analyze config.AnalyzeConfig, exclude config.ExcludeConfig) *application {
	cfg := config.New(config.Options{})
	cfg.Analyze = analyze
	cfg.Exclude = exclude

	return &application{config: cfg}
}

// resolve parses args over analyze's real flag set and returns what the
// layering decided, without loading anything: what the file, the flags, and the
// environment settled on is the whole question here, and a package load to find
// out would cost a second per case.
func resolve(t *testing.T, app *application, args ...string) (settings, config.AnalyzeConfig, error) {
	t.Helper()

	opts := &analyzeOptions{}

	cmd := &cobra.Command{Use: "analyze"}
	registerAnalyzeFlags(cmd, opts)

	must.NoError(t, cmd.ParseFlags(args))

	resolved, err := app.resolveSettings(cmd, cmd.Flags().Args(), &opts.targetOptions)
	if err != nil {
		return settings{}, config.AnalyzeConfig{}, err
	}

	gates, err := app.resolveGates(cmd, opts)

	return resolved, gates, err
}

// projectConfig is a config file that disagrees with every default, so that a
// test asserting one of its values cannot pass by accident.
func projectConfig() (config.AnalyzeConfig, config.ExcludeConfig) {
	return config.AnalyzeConfig{
			Package:     fixture("simple"),
			Strictness:  analysis.StrictnessAny.String(),
			Format:      formatJSON.String(),
			MinScore:    70,
			FailOnFound: true,
		}, config.ExcludeConfig{
			Paths:     []string{"internal/generated/**"},
			Functions: []string{"*.MarshalJSON"},
		}
}

func TestResolveSettings(t *testing.T) {
	t.Parallel()

	t.Run("the config file supplies what no flag says", func(t *testing.T) {
		t.Parallel()

		resolved, gates, err := resolve(t, configured(projectConfig()))
		must.NoError(t, err)

		test.Eq(t, fixture("simple"), resolved.dir)
		test.Eq(t, analysis.StrictnessAny, resolved.strictness)
		test.Eq(t, []string{"internal/generated/**"}, resolved.exclude.Paths)
		test.Eq(t, []string{"*.MarshalJSON"}, resolved.exclude.Functions)
		test.Eq(t, formatJSON.String(), gates.Format)
		test.Eq(t, 70, gates.MinScore)
		test.True(t, gates.FailOnFound)
	})

	t.Run("a flag that was typed overrides the config file", func(t *testing.T) {
		t.Parallel()

		resolved, gates, err := resolve(t, configured(projectConfig()),
			"--strictness", "file", "--format", "text", "--min-score", "10")
		must.NoError(t, err)

		test.Eq(t, analysis.StrictnessFile, resolved.strictness)
		test.Eq(t, formatText.String(), gates.Format)
		test.Eq(t, 10, gates.MinScore)

		// Everything nobody typed still comes from the file.
		test.True(t, gates.FailOnFound)
	})

	t.Run("a flag left alone does not override the config file", func(t *testing.T) {
		t.Parallel()

		// The whole mechanism: a flag sitting at its default is
		// indistinguishable from a value until you ask cobra whether anybody
		// typed it. Without Changed, every default would silently outrank the
		// file and the file would do nothing at all.
		resolved, _, err := resolve(t, configured(projectConfig()), "--min-score", "10")
		must.NoError(t, err)

		test.Eq(t, analysis.StrictnessAny, resolved.strictness)
	})

	t.Run("arguments still win over the resolved package", func(t *testing.T) {
		t.Parallel()

		resolved, _, err := resolve(t, configured(projectConfig()), "./cmd/...")
		must.NoError(t, err)

		test.Eq(t, ".", resolved.dir)
		test.Eq(t, []string{"./cmd/..."}, resolved.patterns)
	})

	t.Run("an exclusion flag replaces the configured list", func(t *testing.T) {
		t.Parallel()

		// Replaces rather than appends: appending would leave no spelling for
		// "grade the generated code too, just this once".
		resolved, _, err := resolve(t, configured(projectConfig()),
			"--exclude", "mocks", "--exclude", "*_gen.go", "--exclude-function", "String")
		must.NoError(t, err)

		test.Eq(t, []string{"mocks", "*_gen.go"}, resolved.exclude.Paths)
		test.Eq(t, []string{"String"}, resolved.exclude.Functions)
	})

	t.Run("a strictness the config file got wrong is refused", func(t *testing.T) {
		t.Parallel()

		analyze, exclude := projectConfig()
		analyze.Strictness = "pakcage"

		_, _, err := resolve(t, configured(analyze, exclude))

		must.Error(t, err)
		test.ErrorIs(t, err, platformerrors.ErrUnrecognizedInputValue)
	})

	t.Run("no config file at all is the defaults", func(t *testing.T) {
		t.Parallel()

		// bootstrap has not run, so there is nothing cached. A command reaching
		// for a setting is the worst possible place to learn that.
		resolved, gates, err := resolve(t, &application{})
		must.NoError(t, err)

		test.Eq(t, analysis.StrictnessFile, resolved.strictness)
		test.Eq(t, config.DefaultFormat, gates.Format)
		test.SliceEmpty(t, resolved.exclude.Paths)
	})

	t.Run("what it resolves to is what the analyzer is asked for", func(t *testing.T) {
		t.Parallel()

		// The one case that spells the whole path out rather than going through
		// the helper: from a parsed command line to the Config that Analyze
		// receives, which is the only thing any of this exists to produce.
		app := configured(projectConfig())

		opts := &targetOptions{}
		cmd := &cobra.Command{Use: "analyze"}
		registerTargetFlags(cmd, opts)

		must.NoError(t, cmd.ParseFlags([]string{"--strictness", "package", "--exclude", "mocks"}))

		resolved, err := app.resolveSettings(cmd, cmd.Flags().Args(), opts)
		must.NoError(t, err)

		test.Eq(t, &analysis.Config{
			Dir:        fixture("simple"),
			Exclude:    analysis.Exclusions{Paths: []string{"mocks"}, Functions: []string{"*.MarshalJSON"}},
			Strictness: analysis.StrictnessPackage,
		}, resolved.analysisConfig())
	})
}

// TestResolveSettingsEnvironment uses t.Setenv, which cannot be parallel — and
// neither can its parent.
func TestResolveSettingsEnvironment(t *testing.T) {
	t.Run("the environment overrides a flag that was typed", func(t *testing.T) {
		// The last word, deliberately: a config file is what the project
		// decided and a flag is what this run wants, but the environment is
		// what the machine running it insists on, so CI can override a
		// checked-in file without editing it.
		t.Setenv(config.EnvVarPrefix+"ANALYZE_STRICTNESS", "package")
		t.Setenv(config.EnvVarPrefix+"ANALYZE_MIN_SCORE", "99")
		t.Setenv(config.EnvVarPrefix+"EXCLUDE_FUNCTIONS", "String,Error")

		resolved, gates, err := resolve(t, configured(projectConfig()),
			"--strictness", "file", "--min-score", "10", "--exclude-function", "MarshalJSON")
		must.NoError(t, err)

		test.Eq(t, analysis.StrictnessPackage, resolved.strictness)
		test.Eq(t, 99, gates.MinScore)
		test.Eq(t, []string{"String", "Error"}, resolved.exclude.Functions)
	})
}

func TestDefaultFormatMatchesTheFormatType(t *testing.T) {
	t.Parallel()

	// The config package spells the default format rather than importing this
	// one, which it cannot: format is a CLI concern and config is imported by
	// the CLI. This is the seam where the two would drift.
	test.Eq(t, formatText.String(), config.DefaultFormat)
	test.Eq(t, minimumScore, config.DefaultMinScore)
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
