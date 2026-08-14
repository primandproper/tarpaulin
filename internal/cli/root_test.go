package cli

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/primandproper/tarpaulin/internal/config"

	platformerrors "github.com/primandproper/platform-go/v10/errors"
	"github.com/primandproper/platform-go/v10/observability"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestReportExecutionError(t *testing.T) {
	t.Parallel()

	t.Run("prints a real failure", func(t *testing.T) {
		t.Parallel()

		out := new(bytes.Buffer)

		reportExecutionError(out, platformerrors.New("the packages could not be loaded"))

		test.Eq(t, "Error: the packages could not be loaded\n", out.String())
	})

	t.Run("stays quiet when the report itself was the message", func(t *testing.T) {
		t.Parallel()

		out := new(bytes.Buffer)

		// --fail-on-found exits non-zero having already printed the report;
		// an "Error:" line underneath it would be noise in every CI log.
		reportExecutionError(out, errFunctionsFound)

		test.Eq(t, "", out.String())
	})

	t.Run("stays quiet when a score gate tripped", func(t *testing.T) {
		t.Parallel()

		out := new(bytes.Buffer)

		// --min-score has already written its one line to stderr; repeating it
		// as an "Error:" would say the same thing twice.
		reportExecutionError(out, errScoreBelowMinimum)

		test.Eq(t, "", out.String())
	})

	t.Run("stays quiet on success", func(t *testing.T) {
		t.Parallel()

		out := new(bytes.Buffer)

		reportExecutionError(out, nil)

		test.Eq(t, "", out.String())
	})
}

func TestIsGateFailure(t *testing.T) {
	t.Parallel()

	test.True(t, isGateFailure(errFunctionsFound))
	test.True(t, isGateFailure(errScoreBelowMinimum))
	// Wrapped, because the sentinels travel back up through cobra.
	test.True(t, isGateFailure(platformerrors.Wrap(errScoreBelowMinimum, "running analyze")))

	test.False(t, isGateFailure(nil))
	test.False(t, isGateFailure(platformerrors.New("the packages could not be loaded")))
}

func TestEnvOr(t *testing.T) {
	t.Parallel()

	test.Eq(t, "info", envOr("TARP_TEST_UNSET_VARIABLE", "info"))
}

// TestEnvOrPrefersTheEnvironment mutates the environment, so it cannot be
// parallel.
func TestEnvOrPrefersTheEnvironment(t *testing.T) {
	t.Setenv("TARP_TEST_SET_VARIABLE", "debug")

	test.Eq(t, "debug", envOr("TARP_TEST_SET_VARIABLE", "info"))
}

// capture redirects one of the process's standard streams for the duration of
// the test and returns a function yielding what was written to it.
//
// Everywhere else in these tests a bytes.Buffer goes in through cobra's SetOut.
// The lifecycle is the exception: Execute builds its own root command, and the
// observability suite holds os.Stdout from the moment bootstrap constructs it,
// so the only way to read what the binary would have printed is to be the file
// it printed to.
func capture(t *testing.T, stream **os.File) func() string {
	t.Helper()

	read, write, err := os.Pipe()
	must.NoError(t, err)

	original := *stream
	*stream = write

	var (
		captured bytes.Buffer
		copied   = make(chan struct{})
	)

	// Drained concurrently: a pipe holds 64KiB, and a report that filled it
	// would otherwise block the test that asked for it.
	go func() {
		defer close(copied)

		_, _ = io.Copy(&captured, read)
	}()

	stop := sync.OnceValue(func() string {
		*stream = original

		must.NoError(t, write.Close())
		<-copied
		must.NoError(t, read.Close())

		return captured.String()
	})

	t.Cleanup(func() { _ = stop() })

	return stop
}

// setArgs points os.Args at the command line the binary would have been handed.
// Nothing calls SetArgs on the root command Execute builds, so cobra reads the
// process arguments — and reaching the command any other way would be testing a
// different one than the binary runs.
func setArgs(t *testing.T, args ...string) {
	t.Helper()

	original := os.Args
	t.Cleanup(func() { os.Args = original })

	os.Args = append([]string{config.DefaultServiceName}, args...)
}

// TestExecute drives the entrypoint the binary calls, which reads os.Args and
// writes to the process's own streams, so neither it nor its subtests can be
// parallel.
func TestExecute(t *testing.T) {
	t.Run("runs a subcommand end to end", func(t *testing.T) {
		stdout, stderr := capture(t, &os.Stdout), capture(t, &os.Stderr)

		setArgs(t, "version")

		must.NoError(t, Execute(t.Context()))

		out := stdout()
		test.StrContains(t, out, "version:")
		test.StrContains(t, out, runtime.Version())
		test.Eq(t, "", stderr())

		// bootstrap ran at the default level, which is info. Nothing the
		// observability suite has to say belongs on the stdout a subcommand is
		// writing for a machine to read.
		test.StrNotContains(t, out, "observability suite initialized")
	})

	t.Run("reports a failure cobra was told to stay quiet about", func(t *testing.T) {
		stdout, stderr := capture(t, &os.Stdout), capture(t, &os.Stderr)

		setArgs(t, "not-a-subcommand")

		must.Error(t, Execute(t.Context()))

		// The root command silences cobra's own error printing, so this line is
		// Execute's doing — and without it the binary would fail silently.
		test.StrContains(t, stderr(), "Error: unknown command")
		test.Eq(t, "", stdout())
	})

	t.Run("lets a gate exit non-zero without explaining itself twice", func(t *testing.T) {
		stdout, stderr := capture(t, &os.Stdout), capture(t, &os.Stderr)

		setArgs(t, "analyze", "--package", fixture("simple"), "--fail-on-found")

		must.ErrorIs(t, Execute(t.Context()), errFunctionsFound)

		// The report is the message. An "Error:" line underneath it would be
		// noise in every CI log that ever ran this.
		test.StrContains(t, stdout(), "Grade: 75% (3/4 functions)")
		test.Eq(t, "", stderr())
	})
}

func TestBootstrap(t *testing.T) {
	t.Parallel()

	t.Run("builds the suite from the flag defaults", func(t *testing.T) {
		t.Parallel()

		app := &application{}

		must.NoError(t, app.bootstrap(t.Context(), config.Options{ServiceName: "tarp-test"}, ""))

		must.NotNil(t, app.pillars)
		must.NotNil(t, app.pillars.Logger)
		must.NotNil(t, app.pillars.TracerProvider)
		test.NotNil(t, app.logger)
	})

	t.Run("treats a blank config path as no config path", func(t *testing.T) {
		t.Parallel()

		app := &application{}

		// An unset TARP_CONFIG_FILEPATH that somebody exported as empty, or a
		// --config="  " — either way there is no file to load.
		must.NoError(t, app.bootstrap(t.Context(), config.Options{}, "   "))

		must.NotNil(t, app.pillars)
	})

	t.Run("loads the config file it is given", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "config.json")
		must.NoError(t, os.WriteFile(path, []byte(
			`{"observability":{"logging":{"provider":"slog","serviceName":"from-file","level":"warn"}}}`,
		), 0o600))

		app := &application{}

		must.NoError(t, app.bootstrap(t.Context(), config.Options{}, path))

		must.NotNil(t, app.pillars)
	})

	t.Run("reports a config file it cannot read", func(t *testing.T) {
		t.Parallel()

		app := &application{}

		err := app.bootstrap(t.Context(), config.Options{}, filepath.Join(t.TempDir(), "missing.json"))

		must.Error(t, err)
		// Nothing was stood up, which is what makes it safe for Execute to call
		// shutdown on the way out regardless.
		test.Nil(t, app.pillars)
	})

	t.Run("reports a config file that does not validate", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "config.json")
		must.NoError(t, os.WriteFile(path, []byte(
			`{"observability":{"logging":{"provider":"nonsense"}}}`,
		), 0o600))

		app := &application{}

		must.Error(t, app.bootstrap(t.Context(), config.Options{}, path))
		test.Nil(t, app.pillars)
	})
}

// TestBootstrapAppliesTheConfigFile captures the process's stdout, so it cannot
// be parallel.
func TestBootstrapAppliesTheConfigFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	must.NoError(t, os.WriteFile(path, []byte(
		`{"observability":{"logging":{"provider":"slog","serviceName":"from-file","level":"debug"}}}`,
	), 0o600))

	stdout := capture(t, &os.Stdout)

	app := &application{}

	// The options say error, the file says debug. Only the file is supposed to
	// be consulted when there is one.
	must.NoError(t, app.bootstrap(t.Context(), config.Options{
		ServiceName: "from-options",
		LogLevel:    config.LevelError,
	}, path))

	out := stdout()

	// A debug line the options' level would have swallowed, carrying the name
	// the file asked for.
	test.StrContains(t, out, "observability suite initialized")
	test.StrContains(t, out, "from-file")
	test.StrNotContains(t, out, "from-options")
}

// shutdownSpy stands in for a pillar with something to flush, so the context
// shutdown hands its providers can be looked at.
type shutdownSpy struct {
	deadline    time.Time
	err         error
	failWith    error
	called      bool
	hasDeadline bool
}

func (s *shutdownSpy) Start(context.Context) error { return nil }

func (s *shutdownSpy) Shutdown(ctx context.Context) error {
	s.called = true
	s.err = ctx.Err()
	s.deadline, s.hasDeadline = ctx.Deadline()

	return s.failWith
}

func TestShutdown(t *testing.T) {
	t.Parallel()

	t.Run("does nothing when startup never completed", func(t *testing.T) {
		t.Parallel()

		// Execute calls shutdown whether bootstrap got anywhere or not, so a
		// zero application has to survive it.
		(&application{}).shutdown(t.Context())
	})

	t.Run("flushes on its own deadline when the parent is already cancelled", func(t *testing.T) {
		t.Parallel()

		ctx, cancel := context.WithCancel(t.Context())
		cancel()

		spy := &shutdownSpy{}
		app := &application{pillars: &observability.Pillars{Profiler: spy}}

		app.shutdown(ctx)

		must.True(t, spy.called)
		// SIGINT is the usual way to arrive here, so a cancelled parent has to
		// be stripped or nothing would ever be flushed on the way out.
		test.NoError(t, spy.err)
		test.True(t, spy.hasDeadline)
		test.True(t, time.Until(spy.deadline) <= shutdownTimeout)
	})

	t.Run("survives a pillar that fails to flush", func(t *testing.T) {
		t.Parallel()

		spy := &shutdownSpy{failWith: platformerrors.New("the exporter is gone")}
		// No logger, as there would be if pillars were ever set without one:
		// reporting the failure must not become a second failure.
		app := &application{pillars: &observability.Pillars{Profiler: spy}}

		app.shutdown(t.Context())

		must.True(t, spy.called)
	})
}

func TestApplicationLog(t *testing.T) {
	t.Parallel()

	t.Run("is usable before bootstrap", func(t *testing.T) {
		t.Parallel()

		app := &application{}

		must.NotNil(t, app.log())
		// A subcommand that logs before bootstrap gets the noop logger rather
		// than a nil dereference.
		app.log().Info("this goes nowhere")
	})

	t.Run("returns the bootstrapped logger once there is one", func(t *testing.T) {
		t.Parallel()

		app := &application{}
		must.NoError(t, app.bootstrap(t.Context(), config.Options{}, ""))

		test.True(t, app.log() == app.logger)
	})
}
