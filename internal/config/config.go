// Package config assembles the application's configuration, most notably the
// observability settings that the platform-go observability suite consumes.
//
// The configuration is built in Go with sensible, zero-dependency defaults so
// the binary boots out of the box: structured slog logging plus noop tracing,
// metrics, and profiling. Callers may override the service name and log level
// (see Options), which is how the CLI threads its flags and environment into
// the platform configuration.
//
// Two loaders build on those defaults using platform-go's config package:
//
//   - Load overlays environment variables (prefixed with EnvVarPrefix) on top of
//     the defaults, so any field can be tuned without a config file — the
//     twelve-factor path most deployments start with.
//   - LoadFromFile decodes a config file over those same defaults and then
//     overlays the same environment variables. Discover finds that file: a
//     .tarp.yaml, .tarp.yml, .tarp.json, or .tarp.toml at the project root.
//
// Both share envVarOptions, which wires the app's env var prefix and a debug
// hook that logs every value the parser applies.
//
// The result is one layering, and the order of it is the whole design: defaults
// < config file < flags < environment. A config file is what a project decided;
// a flag is what this run wants instead; an environment variable is what the
// machine running it insists on, which is why CI can always have the last word
// without editing anything a project checked in.
package config

import (
	"context"
	"log/slog"
	"strings"

	"github.com/primandproper/tarpaulin/internal/analysis"

	platformconfig "github.com/primandproper/platform-go/v10/config"
	platformerrors "github.com/primandproper/platform-go/v10/errors"
	"github.com/primandproper/platform-go/v10/observability"
	"github.com/primandproper/platform-go/v10/observability/logging"
	loggingcfg "github.com/primandproper/platform-go/v10/observability/logging/config"
	metricsnoop "github.com/primandproper/platform-go/v10/observability/metrics/noop"
	profilingnoop "github.com/primandproper/platform-go/v10/observability/profiling/noop"
	tracingnoop "github.com/primandproper/platform-go/v10/observability/tracing/noop"
)

// DefaultServiceName is the service name reported by the observability suite
// when the caller does not supply one.
const DefaultServiceName = "tarp"

// The defaults for the analyze settings, which are also the defaults cobra
// prints for the flags of the same name — they are defined here so that the
// config file and `tarp analyze --help` cannot come to disagree about what
// happens when neither is given. DefaultFormat is spelled rather than taken
// from the CLI's format type, which lives with the command that reads the flag
// and cannot be imported from here; TestDefaultFormatMatchesTheFormatType keeps
// the two honest.
const (
	DefaultPackage  = "."
	DefaultFormat   = "text"
	DefaultMinScore = 0
)

// EnvVarPrefix is prepended to every environment variable this application
// reads, keeping its configuration in a distinct namespace. For example the
// logging level is read from TARP_OBSERVABILITY_LOGGING_LEVEL: the prefix
// here, then the nested envPrefix tags on Config and the platform sub-configs.
const EnvVarPrefix = "TARP_"

// Log level names accepted by Options.LogLevel (case-insensitive).
const (
	LevelDebug = "debug"
	LevelInfo  = "info"
	LevelWarn  = "warn"
	LevelError = "error"
)

// Config is the application configuration: what a project wants graded and how,
// plus the platform-go observability configuration the binary boots on.
//
// The struct tags let platform-go's config package populate the config from
// environment variables (envPrefix/env) and from a file in any of the three
// formats Discover looks for. Give new fields all four tags so they participate
// in Load, LoadFromFile, and every format equally — json alone works for JSON
// and, case-insensitively, for TOML, but YAML would silently bind nothing for
// any key that is not a single lowercase word.
//
// The analyze and exclude sections are omitzero and their fields are
// omitempty, so that Render never writes out a key nobody set. That matters
// more than tidiness now that LoadFromFile decodes over the defaults: a
// rendered `"strictness": ""` is not an omission, it is an instruction to use
// no strictness at all, and the next load of that file would fail on it.
type Config struct {
	Observability observability.Config `envPrefix:"OBSERVABILITY_" json:"observability"    toml:"observability"     yaml:"observability"`
	Exclude       ExcludeConfig        `envPrefix:"EXCLUDE_"       json:"exclude,omitzero" toml:"exclude,omitempty" yaml:"exclude,omitempty"`
	Analyze       AnalyzeConfig        `envPrefix:"ANALYZE_"       json:"analyze,omitzero" toml:"analyze,omitempty" yaml:"analyze,omitempty"`
}

// AnalyzeConfig holds what `tarp analyze` and `tarp cover` do when nobody
// passes a flag. Every field is the default for the flag of the same name, and
// a flag that was actually typed wins over it — see the CLI's resolve.
//
// None of these carry an envDefault, deliberately: caarlos0/env resets a field
// with one to that default whenever its variable is unset, which would mean a
// config file could never supply a value at all.
type AnalyzeConfig struct {
	// Format is how the report is rendered: text, json, sarif, or markdown.
	Format string `env:"FORMAT" json:"format,omitempty" toml:"format,omitempty" yaml:"format,omitempty"`
	// Package is what to analyze when no argument names something else.
	Package string `env:"PACKAGE" json:"package,omitempty" toml:"package,omitempty" yaml:"package,omitempty"`
	// Strictness is how close a reference must be to count: file, package, or
	// any.
	Strictness string `env:"STRICTNESS" json:"strictness,omitempty" toml:"strictness,omitempty" yaml:"strictness,omitempty"`
	// MinScore is the grade below which the command exits non-zero. Zero never
	// fails.
	MinScore int `env:"MIN_SCORE" json:"minScore,omitempty" toml:"minScore,omitempty" yaml:"minScore,omitempty"`
	// FailOnFound exits non-zero when anything at all is reported.
	FailOnFound bool `env:"FAIL_ON_FOUND" json:"failOnFound,omitempty" toml:"failOnFound,omitempty" yaml:"failOnFound,omitempty"`
}

// ExcludeConfig withholds declarations from every report this project produces.
// It is analysis.Exclusions in the shape a config file writes it; see that type
// for what the patterns mean.
//
// From the environment both are comma-separated, which is caarlos0/env's
// convention for a slice: TARP_EXCLUDE_PATHS=internal/generated/**,**/*_gen.go.
type ExcludeConfig struct {
	// Paths are globs matched against each file's path relative to the module
	// root.
	Paths []string `env:"PATHS" json:"paths,omitempty" toml:"paths,omitempty" yaml:"paths,omitempty"`
	// Functions are globs matched against declaration names.
	Functions []string `env:"FUNCTIONS" json:"functions,omitempty" toml:"functions,omitempty" yaml:"functions,omitempty"`
}

// Exclusions renders the configured exclusions in the shape the analyzer takes.
func (e ExcludeConfig) Exclusions() analysis.Exclusions {
	return analysis.Exclusions{Paths: e.Paths, Functions: e.Functions}
}

// Options tune the values that most deployments care about. Empty fields fall
// back to the built-in defaults.
type Options struct {
	// ServiceName labels telemetry emitted by the service.
	ServiceName string
	// LogLevel is one of "debug", "info", "warn", or "error" (case-insensitive).
	LogLevel string
}

// New builds a Config from the given options, filling in defaults. The result
// validates cleanly and is ready to hand to observability.Config.NewPillars.
func New(opts Options) *Config {
	serviceName := strings.TrimSpace(opts.ServiceName)
	if serviceName == "" {
		serviceName = DefaultServiceName
	}

	cfg := &Config{
		Analyze: AnalyzeConfig{
			Package:    DefaultPackage,
			Strictness: analysis.StrictnessFile.String(),
			Format:     DefaultFormat,
			MinScore:   DefaultMinScore,
			// FailOnFound is left false: a tool that fails a build by default
			// is one nobody installs a second time.
		},
		Observability: observability.Config{
			// slog logging to stdout as structured JSON.
			Logging: loggingcfg.Config{
				Provider:    loggingcfg.ProviderSlog,
				ServiceName: serviceName,
				Level:       levelFromString(opts.LogLevel),
			},
			// Tracing, Metrics, and Profiling are left at their zero values,
			// which the platform resolves to noop providers. Enable them by
			// populating the corresponding sub-config.
		},
	}

	return cfg
}

// envVarOptions returns the platform config options shared by every loader in
// this package: the application's env var prefix and a debug hook that logs each
// value the parser applies. Because the hook uses the standard-library slog
// default logger, these lines appear only when that logger is at debug level.
//
//tarp:ignore -- returns opaque platform-go functional options; there is nothing to assert about them that Load and LoadFromFile's tests do not already assert about their effect
func envVarOptions() []platformconfig.Option {
	return []platformconfig.Option{
		platformconfig.WithPrefix(EnvVarPrefix),
		platformconfig.WithOnSet(func(tag string, value any, isDefault bool) {
			slog.Debug("config value set from environment",
				slog.String("tag", tag),
				slog.Any("value", value),
				slog.Bool("isDefault", isDefault),
			)
		}),
	}
}

// applyEnvironmentVariables overlays the whole environment on an assembled
// config, which is the last thing every loader in this package does.
func applyEnvironmentVariables(cfg *Config) error {
	if err := platformconfig.ApplyEnvironmentVariables(cfg, envVarOptions()...); err != nil {
		return platformerrors.Wrap(err, "applying environment variables")
	}

	return nil
}

// Load builds a Config from the given options and then overlays environment
// variables on top of it. The options (typically the CLI's flags) seed the
// defaults; any TARP_-prefixed environment variable that is set wins over
// them. Fields left unset in the environment keep their default value, so the
// binary still boots with structured slog logging and noop telemetry out of the
// box. The result is validated before it is returned.
func Load(ctx context.Context, opts Options) (*Config, error) {
	cfg := New(opts)

	if err := applyEnvironmentVariables(cfg); err != nil {
		return nil, err
	}

	if err := cfg.Validate(ctx); err != nil {
		return nil, platformerrors.Wrap(err, "validating configuration")
	}

	return cfg, nil
}

// OverlayEnvironment gives the environment the last word over an analyze
// section that a config file and then the flags have already had theirs on.
//
// It exists because the layering is file < flags < environment, and the flags
// only exist at the point cobra parses them — long after Load has run. Rather
// than thread the flag set down here, the CLI merges the file and the flags and
// then hands the result back for one final pass, which is the same pass Load
// would have made and is idempotent with it.
func (a *AnalyzeConfig) OverlayEnvironment() error {
	return overlayEnvironment(a, "ANALYZE_")
}

// OverlayEnvironment does for the exclusions what AnalyzeConfig's does for the
// analyze settings.
func (e *ExcludeConfig) OverlayEnvironment() error {
	return overlayEnvironment(e, "EXCLUDE_")
}

// overlayEnvironment applies the variables for one nested section, whose names
// are the application prefix, then the section's envPrefix, then the field's
// env tag — exactly what a whole-config pass would have read for the same
// field.
func overlayEnvironment(section any, prefix string) error {
	options := append(envVarOptions(), platformconfig.WithPrefix(EnvVarPrefix+prefix))

	return platformconfig.ApplyEnvironmentVariables(section, options...)
}

// Validate confirms the assembled configuration is internally consistent.
func (c *Config) Validate(ctx context.Context) error {
	return c.Observability.ValidateWithContext(ctx)
}

// NewPillars builds the observability pillars for the application.
//
// Logging is configured from Config (structured slog by default), while
// tracing, metrics, and profiling default to noop providers so the binary stays
// quiet and dependency-free out of the box. To enable real telemetry, populate
// the Tracing/Metrics/Profiling sub-configs of c.Observability and call
// c.Observability.NewPillars(ctx) instead — it wires OTel/Cloud providers from
// the same config — or replace the noop constructors below with your own.
func (c *Config) NewPillars(ctx context.Context) (*observability.Pillars, error) {
	logger, err := c.Observability.Logging.NewLogger(ctx)
	if err != nil {
		return nil, err
	}

	return &observability.Pillars{
		Logger:          logger,
		TracerProvider:  tracingnoop.NewTracerProvider(),
		MetricsProvider: metricsnoop.NewMetricsProvider(),
		Profiler:        profilingnoop.NewProvider(),
	}, nil
}

// levelFromString maps a human-friendly level name onto a platform log level,
// defaulting to info for empty or unrecognized input.
func levelFromString(s string) logging.Level {
	switch strings.ToLower(strings.TrimSpace(s)) {
	case LevelDebug:
		return logging.DebugLevel
	case LevelWarn, "warning":
		return logging.WarnLevel
	case LevelError:
		return logging.ErrorLevel
	default:
		return logging.InfoLevel
	}
}
