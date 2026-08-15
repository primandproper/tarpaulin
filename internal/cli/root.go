// Package cli wires the command-line interface together and bootstraps the
// platform-go observability suite that the rest of the application builds on.
//
// The CLI is tarp's single entrypoint: `analyze` reports the functions in a
// package that carry no direct unit test, `cover` renders the same verdict over
// a coverage profile, and new subcommands hang off the root command alongside
// them.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/primandproper/tarpaulin/internal/config"

	platformerrors "github.com/primandproper/platform-go/v10/errors"
	"github.com/primandproper/platform-go/v10/observability"
	"github.com/primandproper/platform-go/v10/observability/logging"

	"github.com/spf13/cobra"
)

// ConfigFilePathEnvVar names the environment variable that seeds the --config
// flag: when it points at a JSON config file, that file is loaded instead of the
// flag/environment defaults.
const ConfigFilePathEnvVar = config.EnvVarPrefix + "CONFIG_FILEPATH"

// shutdownTimeout bounds how long we wait for telemetry to flush on exit.
const shutdownTimeout = 5 * time.Second

// application holds the process-wide dependencies constructed during startup.
// The logger is populated by bootstrap and shared with subcommands, which reach
// it through the application receiver on their RunE closures.
type application struct {
	pillars *observability.Pillars
	config  *config.Config
	logger  logging.Logger
}

// Execute builds the root command, runs it, and tears down the observability
// suite afterwards so buffered telemetry is flushed even when a command fails.
//
// Errors are printed here rather than by cobra so that --fail-on-found can exit
// non-zero without also printing an error: the report it just wrote is the
// message, and CI does not need "Error:" stapled underneath it.
func Execute(ctx context.Context) error {
	app := &application{}

	rootCmd := app.newRootCommand()
	err := rootCmd.ExecuteContext(ctx)

	if reportErr := reportExecutionError(rootCmd.ErrOrStderr(), err); err == nil {
		err = reportErr
	}

	app.shutdown(ctx)

	return err
}

// isGateFailure reports whether err only means "a threshold you asked for was
// not met". Those are not failures to describe: the command has already printed
// everything an operator needs, and an "Error:" line underneath it would be
// noise in every CI log.
func isGateFailure(err error) bool {
	return errors.Is(err, errFunctionsFound) || errors.Is(err, errScoreBelowMinimum)
}

// reportExecutionError prints a failed command's error, except the gate
// failures above. It returns the failure to write, if writing to stderr is
// itself broken.
func reportExecutionError(w io.Writer, err error) error {
	if err == nil || isGateFailure(err) {
		return nil
	}

	if _, writeErr := fmt.Fprintln(w, "Error:", err); writeErr != nil {
		return platformerrors.Wrap(writeErr, "reporting the command's failure to stderr")
	}

	return nil
}

// newRootCommand constructs the cobra root command and registers subcommands.
//
//tarp:ignore -- declaration only: a test here could assert that cobra registered the flags cobra registered, and nothing else; the assembled command is driven end to end by TestExecute
func (a *application) newRootCommand() *cobra.Command {
	var (
		logLevel    string
		serviceName string
		configPath  string
	)

	rootCmd := &cobra.Command{
		Use:   config.DefaultServiceName,
		Short: "Find Go functions that have no direct unit test.",
		Long: "tarp finds functions which lack direct unit tests.\n\n" +
			"`go test -cover` measures statement coverage, which cannot distinguish a function\n" +
			"that was tested from one that was merely executed by somebody else's test. tarp\n" +
			"asks the stricter question, and grades the package on the answer.",
		SilenceUsage:  true,
		SilenceErrors: true,
		PersistentPreRunE: func(cmd *cobra.Command, _ []string) error {
			return a.bootstrap(cmd.Context(), config.Options{ServiceName: serviceName, LogLevel: logLevel}, configPath)
		},
		RunE: func(cmd *cobra.Command, _ []string) error {
			a.log().Info("no subcommand provided; run `tarp help` to see what's available")

			return cmd.Help()
		},
	}

	rootCmd.PersistentFlags().StringVar(&logLevel, "log-level", envOr("TARP_LOG_LEVEL", config.LevelInfo), "log level: debug, info, warn, or error")
	rootCmd.PersistentFlags().StringVar(&serviceName, "service-name", envOr("TARP_SERVICE_NAME", config.DefaultServiceName), "service name reported in telemetry")
	rootCmd.PersistentFlags().StringVar(&configPath, "config", envOr(ConfigFilePathEnvVar, ""),
		"path to a JSON, YAML, or TOML config `file`; when set, it is loaded instead of the project's own .tarp file")

	rootCmd.AddCommand(a.newAnalyzeCommand(), a.newCoverCommand(), a.newVersionCommand())

	return rootCmd
}

// bootstrap assembles configuration and stands up the observability pillars,
// caching both on the application for subcommands to use. When configPath is
// set it loads that file; otherwise it looks for the project's own config file,
// and falls back to the flag/env defaults when there is none. Either way,
// environment variables overlay the result.
func (a *application) bootstrap(ctx context.Context, opts config.Options, configPath string) error {
	path, err := a.configFilePath(configPath)
	if err != nil {
		return err
	}

	var cfg *config.Config

	if path != "" {
		cfg, err = config.LoadFromFile(ctx, path)
	} else {
		cfg, err = config.Load(ctx, opts)
	}
	if err != nil {
		return err
	}

	pillars, err := cfg.NewPillars(ctx)
	if err != nil {
		return err
	}

	a.pillars = pillars
	a.config = cfg
	a.logger = logging.NewNamedLogger(pillars.Logger, cfg.Observability.Logging.ServiceName)

	a.logger.WithValue("configFile", path).Debug("observability suite initialized")

	return nil
}

// configFilePath decides which config file to load: the one the caller named,
// or the project's own, or none.
//
// Discovery is skipped entirely when --config names a file, because the two
// answer different questions. --config is "use this one", which is what a
// deployment mounting a config says; discovery is "use whatever this project
// keeps", which is what everyone else means. Searching after being told where
// to look would only ever surprise somebody.
func (a *application) configFilePath(configPath string) (string, error) {
	if configPath = strings.TrimSpace(configPath); configPath != "" {
		return configPath, nil
	}

	return config.Discover(".")
}

// configuration returns the loaded configuration, or the built-in defaults if
// bootstrap has not run — the same courtesy log() extends, and for the same
// reason: a command reaching for a setting is the worst possible place to learn
// that startup was skipped.
func (a *application) configuration() *config.Config {
	if a.config == nil {
		return config.New(config.Options{})
	}

	return a.config
}

// shutdown flushes and releases the observability pillars. It is safe to call
// when startup never completed. The parent context is typically already
// cancelled (SIGINT/SIGTERM), so we strip cancellation but keep its values and
// bound the flush with our own timeout.
func (a *application) shutdown(ctx context.Context) {
	if a.pillars == nil {
		return
	}

	ctx, cancel := context.WithTimeout(context.WithoutCancel(ctx), shutdownTimeout)
	defer cancel()

	if err := a.pillars.Shutdown(ctx); err != nil {
		// Through log() rather than the field: pillars and logger are only ever
		// set together by bootstrap, but a flush failure is the worst possible
		// moment to learn that assumption was wrong somewhere else.
		a.log().Error("shutting down observability suite", err)
	}
}

// log returns the application logger, or a noop logger if bootstrap has not run.
func (a *application) log() logging.Logger {
	return logging.EnsureLogger(a.logger)
}

// envOr returns the value of the named environment variable, or fallback when
// it is unset or empty.
func envOr(name, fallback string) string {
	if v := os.Getenv(name); v != "" {
		return v
	}

	return fallback
}
