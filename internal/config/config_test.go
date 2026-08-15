package config

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/primandproper/tarpaulin/internal/analysis"

	"github.com/primandproper/platform-go/v10/observability/logging"
	loggingcfg "github.com/primandproper/platform-go/v10/observability/logging/config"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestNew(t *testing.T) {
	t.Parallel()

	t.Run("defaults are valid and build pillars", func(t *testing.T) {
		t.Parallel()

		cfg := New(Options{})

		test.Eq(t, DefaultServiceName, cfg.Observability.Logging.ServiceName)
		test.Eq(t, loggingcfg.ProviderSlog, cfg.Observability.Logging.Provider)
		test.Eq(t, logging.InfoLevel, cfg.Observability.Logging.Level)

		must.NoError(t, cfg.Validate(context.Background()))

		pillars, err := cfg.Observability.NewPillars(context.Background())
		must.NoError(t, err)
		must.NotNil(t, pillars)
		must.NotNil(t, pillars.Logger)
	})

	t.Run("options override defaults", func(t *testing.T) {
		t.Parallel()

		cfg := New(Options{ServiceName: "custom", LogLevel: "debug"})

		test.Eq(t, "custom", cfg.Observability.Logging.ServiceName)
		test.Eq(t, logging.DebugLevel, cfg.Observability.Logging.Level)
		must.NoError(t, cfg.Validate(context.Background()))
	})
}

// These loader tests mutate the process environment via t.Setenv, which is
// incompatible with t.Parallel, so they run serially by design.

func TestLoad(t *testing.T) {
	t.Run("defaults when no environment variables are set", func(t *testing.T) {
		cfg, err := Load(context.Background(), Options{})
		must.NoError(t, err)

		test.Eq(t, DefaultServiceName, cfg.Observability.Logging.ServiceName)
		test.Eq(t, loggingcfg.ProviderSlog, cfg.Observability.Logging.Provider)
		test.Eq(t, logging.InfoLevel, cfg.Observability.Logging.Level)
	})

	t.Run("environment variables overlay the option defaults", func(t *testing.T) {
		t.Setenv(EnvVarPrefix+"OBSERVABILITY_LOGGING_SERVICE_NAME", "from-env")
		t.Setenv(EnvVarPrefix+"OBSERVABILITY_LOGGING_LEVEL", "error")

		cfg, err := Load(context.Background(), Options{ServiceName: "from-opts", LogLevel: "info"})
		must.NoError(t, err)

		// The env var wins over the option-seeded default.
		test.Eq(t, "from-env", cfg.Observability.Logging.ServiceName)
		test.Eq(t, logging.ErrorLevel, cfg.Observability.Logging.Level)
		// A field with no env var keeps the default set by New.
		test.Eq(t, loggingcfg.ProviderSlog, cfg.Observability.Logging.Provider)
	})
}

func TestLoadFromFile(t *testing.T) {
	t.Run("decodes a complete config file", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.json")
		must.NoError(t, os.WriteFile(path, []byte(
			`{"observability":{"logging":{"provider":"slog","serviceName":"from-file","level":"warn"}}}`,
		), 0o600))

		cfg, err := LoadFromFile(context.Background(), path)
		must.NoError(t, err)

		test.Eq(t, "from-file", cfg.Observability.Logging.ServiceName)
		test.Eq(t, loggingcfg.ProviderSlog, cfg.Observability.Logging.Provider)
		test.Eq(t, logging.WarnLevel, cfg.Observability.Logging.Level)
	})

	t.Run("environment variables overlay the file", func(t *testing.T) {
		t.Setenv(EnvVarPrefix+"OBSERVABILITY_LOGGING_SERVICE_NAME", "env-wins")

		path := filepath.Join(t.TempDir(), "config.json")
		must.NoError(t, os.WriteFile(path, []byte(
			`{"observability":{"logging":{"provider":"slog","serviceName":"from-file"}}}`,
		), 0o600))

		cfg, err := LoadFromFile(context.Background(), path)
		must.NoError(t, err)

		test.Eq(t, "env-wins", cfg.Observability.Logging.ServiceName)
	})

	t.Run("an invalid file fails validation", func(t *testing.T) {
		path := filepath.Join(t.TempDir(), "config.json")
		must.NoError(t, os.WriteFile(path, []byte(
			`{"observability":{"logging":{"provider":"nonsense"}}}`,
		), 0o600))

		_, err := LoadFromFile(context.Background(), path)
		must.Error(t, err)
	})

	t.Run("what a file omits keeps its default", func(t *testing.T) {
		// The file is decoded over a built Config rather than into an empty
		// one, which is what makes a three-line .tarp.yaml a sensible thing to
		// write. A file that mentions only the provider still gets the service
		// name, the log level, and every analyze default the binary would have
		// used on its own.
		path := filepath.Join(t.TempDir(), "config.json")
		must.NoError(t, os.WriteFile(path, []byte(
			`{"observability":{"logging":{"provider":"slog"}}}`,
		), 0o600))

		cfg, err := LoadFromFile(context.Background(), path)
		must.NoError(t, err)

		test.Eq(t, DefaultServiceName, cfg.Observability.Logging.ServiceName)
		test.Eq(t, logging.InfoLevel, cfg.Observability.Logging.Level)
		test.Eq(t, DefaultFormat, cfg.Analyze.Format)
		test.Eq(t, analysis.StrictnessFile.String(), cfg.Analyze.Strictness)
	})

	t.Run("an explicit empty value is still an opt-out", func(t *testing.T) {
		// Overlaying defaults costs the ability to mean "unset" by omission, so
		// the way to turn something off is to name it and give it the empty
		// value. An empty logging provider is the platform's documented opt-out
		// into noop logging, and it has to survive the overlay.
		path := filepath.Join(t.TempDir(), "config.json")
		must.NoError(t, os.WriteFile(path, []byte(
			`{"observability":{"logging":{"provider":""}}}`,
		), 0o600))

		cfg, err := LoadFromFile(context.Background(), path)
		must.NoError(t, err)
		test.Eq(t, "", cfg.Observability.Logging.Provider)
	})

	t.Run("a missing file is an error", func(t *testing.T) {
		_, err := LoadFromFile(context.Background(), filepath.Join(t.TempDir(), "does-not-exist.json"))
		must.Error(t, err)
	})
}

func TestExcludeConfigExclusions(t *testing.T) {
	t.Parallel()

	// The shape the analyzer takes, which is the only reason this type is not
	// analysis.Exclusions itself: that one has no business carrying struct tags
	// for three file formats.
	exclusions := ExcludeConfig{
		Paths:     []string{"internal/generated/**"},
		Functions: []string{"*.MarshalJSON"},
	}.Exclusions()

	test.Eq(t, analysis.Exclusions{
		Paths:     []string{"internal/generated/**"},
		Functions: []string{"*.MarshalJSON"},
	}, exclusions)
}

// TestOverlayEnvironment uses t.Setenv, so neither it nor its subtests can be
// parallel.
func TestOverlayEnvironment(t *testing.T) {
	t.Run("the environment has the last word over an analyze section", func(t *testing.T) {
		t.Setenv(EnvVarPrefix+"ANALYZE_STRICTNESS", "any")
		t.Setenv(EnvVarPrefix+"ANALYZE_MIN_SCORE", "42")

		analyze := AnalyzeConfig{Strictness: "file", MinScore: 10, Format: DefaultFormat}
		must.NoError(t, analyze.OverlayEnvironment())

		test.Eq(t, "any", analyze.Strictness)
		test.Eq(t, 42, analyze.MinScore)
		// A field with no variable set keeps what it was handed, which is what
		// makes this safe to run over a value the flags have already touched.
		test.Eq(t, DefaultFormat, analyze.Format)
	})

	t.Run("a list comes in comma-separated", func(t *testing.T) {
		t.Setenv(EnvVarPrefix+"EXCLUDE_PATHS", "internal/generated/**,**/*_gen.go")

		exclude := ExcludeConfig{Paths: []string{"replaced"}}
		must.NoError(t, exclude.OverlayEnvironment())

		test.Eq(t, []string{"internal/generated/**", "**/*_gen.go"}, exclude.Paths)
	})

	t.Run("a section reads the same variables a whole-config pass would", func(t *testing.T) {
		// overlayEnvironment prepends the application prefix and the section's
		// own, so that TARP_ANALYZE_FORMAT means the same thing whether it was
		// read through Load or through this.
		t.Setenv(EnvVarPrefix+"ANALYZE_FORMAT", "sarif")

		analyze := AnalyzeConfig{Format: DefaultFormat}
		must.NoError(t, overlayEnvironment(&analyze, "ANALYZE_"))

		test.Eq(t, "sarif", analyze.Format)
	})

	t.Run("the whole config reads them too", func(t *testing.T) {
		t.Setenv(EnvVarPrefix+"ANALYZE_FORMAT", "sarif")

		cfg := New(Options{})
		must.NoError(t, applyEnvironmentVariables(cfg))

		test.Eq(t, "sarif", cfg.Analyze.Format)
	})
}

func TestNewPillars(t *testing.T) {
	t.Parallel()

	t.Run("builds all four pillars", func(t *testing.T) {
		t.Parallel()

		pillars, err := New(Options{}).NewPillars(context.Background())
		must.NoError(t, err)
		must.NotNil(t, pillars)

		// All four, because Pillars.Shutdown walks every one of them and the
		// application holds them for the life of the process: a nil here is a
		// panic somewhere far away from this constructor.
		test.NotNil(t, pillars.Logger)
		test.NotNil(t, pillars.TracerProvider)
		test.NotNil(t, pillars.MetricsProvider)
		test.NotNil(t, pillars.Profiler)
	})

	t.Run("reports a logging provider it cannot build", func(t *testing.T) {
		t.Parallel()

		cfg := New(Options{})
		cfg.Observability.Logging.Provider = "nonsense"

		// The three noop pillars cannot fail, so logging is the whole error
		// path — and it has to be returned rather than swallowed into a
		// half-built suite.
		pillars, err := cfg.NewPillars(context.Background())

		must.Error(t, err)
		test.Nil(t, pillars)
	})
}

func TestLevelFromString(t *testing.T) {
	t.Parallel()

	cases := map[string]logging.Level{
		"debug":     logging.DebugLevel,
		"info":      logging.InfoLevel,
		"warn":      logging.WarnLevel,
		"warning":   logging.WarnLevel,
		"error":     logging.ErrorLevel,
		"ERROR":     logging.ErrorLevel,
		"":          logging.InfoLevel,
		"nonsense":  logging.InfoLevel,
		"  debug  ": logging.DebugLevel,
	}

	for input, want := range cases {
		test.Eq(t, want, levelFromString(input), test.Sprintf("input %q", input))
	}
}
