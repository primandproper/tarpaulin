package cli

import (
	"os"

	"github.com/primandproper/tarpaulin/internal/analysis"
	"github.com/primandproper/tarpaulin/internal/config"

	"github.com/spf13/cobra"
)

// The flags analyze and cover share. They are named constants because the
// resolution below asks cobra which of them were actually typed, and a typo in
// one of those strings is a flag that silently stops working.
const (
	packageFlag         = "package"
	strictnessFlag      = "strictness"
	excludeFlag         = "exclude"
	excludeFunctionFlag = "exclude-function"
)

// targetOptions holds the flag values that decide what gets analyzed. Both
// analyze and cover load packages the same way, so both embed it.
type targetOptions struct {
	pkg              string
	strictness       string
	excludePaths     []string
	excludeFunctions []string
}

// settings is what one invocation actually runs with, after the config file,
// the flags, and the environment have each had their say.
type settings struct {
	dir        string
	patterns   []string
	exclude    analysis.Exclusions
	strictness analysis.Strictness
}

// analysisConfig renders the settings in the shape the analyzer takes.
func (s *settings) analysisConfig() *analysis.Config {
	return &analysis.Config{
		Dir:        s.dir,
		Patterns:   s.patterns,
		Exclude:    s.exclude,
		Strictness: s.strictness,
	}
}

// registerTargetFlags declares the shared flags on a command.
//
// The defaults come from the config package rather than being spelled here,
// because they are the same defaults a config file overlays and `--help` is
// where most people will read them.
//
//tarp:ignore -- declaration only: flag names, defaults, and help text, asserted through cobra by TestResolveSettings and the command tests that parse them
func registerTargetFlags(cmd *cobra.Command, opts *targetOptions) {
	cmd.Flags().StringVarP(&opts.pkg, packageFlag, "p", config.DefaultPackage,
		"`directory` to analyze (expanded to ./... beneath it), or a go/packages pattern resolved from here; ignored when arguments are given")
	cmd.Flags().StringVarP(&opts.strictness, strictnessFlag, "s", analysis.StrictnessFile.String(),
		"how close a reference must be to count: file, package, or any")
	cmd.Flags().StringArrayVarP(&opts.excludePaths, excludeFlag, "x", nil,
		"`glob` matched against each file's path relative to the module root; repeatable, and replaces the configured list")
	cmd.Flags().StringArrayVarP(&opts.excludeFunctions, excludeFunctionFlag, "X", nil,
		"`glob` matched against declaration names, such as '*.MarshalJSON'; repeatable, and replaces the configured list")
}

// resolveSettings reconciles the config file, the flags, and the environment
// into what this invocation will do.
//
// The order is the one documented on the config package: the file is what the
// project decided, a flag that was actually typed overrides it for this run,
// and the environment overrides both so that CI can always have the last word
// without editing a checked-in file. Cobra's Changed is what makes the middle
// step possible — a flag left alone sits at its default, which is
// indistinguishable from a value until you ask whether anybody typed it.
//
// The exclusion lists replace rather than append. Appending would mean a
// project could never narrow its own configuration from the command line, and
// there would be no spelling for "grade the generated code too, just this once".
func (a *application) resolveSettings(cmd *cobra.Command, args []string, opts *targetOptions) (settings, error) {
	cfg := a.configuration()
	analyze, exclude := cfg.Analyze, cfg.Exclude

	flags := cmd.Flags()

	if flags.Changed(packageFlag) {
		analyze.Package = opts.pkg
	}

	if flags.Changed(strictnessFlag) {
		analyze.Strictness = opts.strictness
	}

	if flags.Changed(excludeFlag) {
		exclude.Paths = opts.excludePaths
	}

	if flags.Changed(excludeFunctionFlag) {
		exclude.Functions = opts.excludeFunctions
	}

	if err := analyze.OverlayEnvironment(); err != nil {
		return settings{}, err
	}

	if err := exclude.OverlayEnvironment(); err != nil {
		return settings{}, err
	}

	strictness, err := analysis.ParseStrictness(analyze.Strictness)
	if err != nil {
		return settings{}, err
	}

	dir, patterns := resolveTarget(analyze.Package, args)

	return settings{
		dir:        dir,
		patterns:   patterns,
		exclude:    exclude.Exclusions(),
		strictness: strictness,
	}, nil
}

// resolveTarget turns the resolved package setting and the arguments into a
// directory and the patterns to load inside it.
//
// Arguments win over --package and are resolved from the working directory, the
// way the go command resolves its own. Failing those, a package value naming an
// existing directory becomes the directory itself, expanded to everything
// beneath it; anything else is handed to go/packages as written, so package
// paths such as example.com/mod/... still work.
func resolveTarget(pkg string, args []string) (dir string, patterns []string) {
	if len(args) > 0 {
		return ".", args
	}

	if info, err := os.Stat(pkg); err == nil && info.IsDir() {
		return pkg, nil
	}

	return ".", []string{pkg}
}
