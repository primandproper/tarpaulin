package cli

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// simpleProfile is a checked-in cover profile over the analysis corpus' simple
// package, shared with the coverage package's own tests.
var simpleProfile = filepath.Join("..", "coverage", "testdata", "simple.out")

// runCoverCommand executes the cover subcommand as the binary would, and
// returns what it wrote to stdout and stderr.
func runCoverCommand(t *testing.T, args ...string) (stdout, stderr string, err error) {
	t.Helper()

	out, errOut := new(bytes.Buffer), new(bytes.Buffer)

	cmd := (&application{}).newCoverCommand()
	cmd.SetOut(out)
	cmd.SetErr(errOut)
	cmd.SetArgs(args)

	err = cmd.ExecuteContext(t.Context())

	return out.String(), errOut.String(), err
}

func TestCoverCommand(t *testing.T) {
	t.Parallel()

	t.Run("writes the report to stdout", func(t *testing.T) {
		t.Parallel()

		stdout, stderr, err := runCoverCommand(t,
			"--html", simpleProfile, "--package", fixture("simple"))
		must.NoError(t, err)

		test.StrContains(t, stdout, "<title>simple: tarp coverage</title>")
		test.StrContains(t, stdout, `<span class="tarp-indirect" title="ran once; B has no direct test">`)
		test.StrContains(t, stdout, "Grade: 75% (3/4 functions)")
		test.Eq(t, "", stderr)
	})

	t.Run("writes the report to a file", func(t *testing.T) {
		t.Parallel()

		path := filepath.Join(t.TempDir(), "coverage.html")

		stdout, _, err := runCoverCommand(t,
			"--html", simpleProfile, "--package", fixture("simple"), "--output", path)
		must.NoError(t, err)

		test.Eq(t, "", stdout)

		written, err := os.ReadFile(path)
		must.NoError(t, err)
		test.StrContains(t, string(written), "Grade: 75% (3/4 functions)")
	})

	t.Run("honors the strictness dial", func(t *testing.T) {
		t.Parallel()

		stdout, _, err := runCoverCommand(t,
			"--html", simpleProfile, "--package", fixture("simple"), "--strictness", "any")
		must.NoError(t, err)

		// B is untested at every level in this fixture; what the dial proves
		// here is that cover accepts it and renders through the same path.
		test.StrContains(t, stdout, `<span class="tarp-indirect" title="ran once; B has no direct test">`)
	})

	t.Run("requires a profile", func(t *testing.T) {
		t.Parallel()

		_, _, err := runCoverCommand(t, "--package", fixture("simple"))

		must.ErrorIs(t, err, errNoProfile)
	})

	t.Run("rejects an unknown strictness", func(t *testing.T) {
		t.Parallel()

		_, _, err := runCoverCommand(t, "--html", simpleProfile, "--strictness", "nonsense")

		must.Error(t, err)
		test.StrContains(t, err.Error(), "unknown strictness")
	})

	t.Run("reports a package it cannot analyze", func(t *testing.T) {
		t.Parallel()

		_, _, err := runCoverCommand(t, "--html", simpleProfile, "--package", fixture("broken_package"))

		must.Error(t, err)
		test.StrContains(t, err.Error(), "could not be analyzed")
	})

	t.Run("reports a profile it cannot read", func(t *testing.T) {
		t.Parallel()

		_, _, err := runCoverCommand(t,
			"--html", filepath.Join(t.TempDir(), "missing.out"), "--package", fixture("simple"))

		must.Error(t, err)
		test.StrContains(t, err.Error(), "parsing cover profile")
	})

	t.Run("warns about a reasonless ignore directive", func(t *testing.T) {
		t.Parallel()

		stdout, stderr, err := runCoverCommand(t,
			"--html", simpleProfile, "--package", fixture("ignore_directive"))
		must.NoError(t, err)

		// Warnings about the source go to stderr, exactly as they do under
		// analyze. The profile describes a different package than the one
		// analyzed here, so nothing in it is graded — and the report says so in
		// grey rather than guessing.
		test.StrContains(t, stderr, "no reason")
		test.StrContains(t, stdout, `<span class="tarp-ungraded" title=`)
		test.StrNotContains(t, stdout, `<span class="tarp-direct" title=`)
	})
}
