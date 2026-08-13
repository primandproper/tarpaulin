package cli

import (
	"bytes"
	"testing"

	platformerrors "github.com/primandproper/platform-go/v10/errors"

	"github.com/shoenig/test"
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

	t.Run("stays quiet on success", func(t *testing.T) {
		t.Parallel()

		out := new(bytes.Buffer)

		reportExecutionError(out, nil)

		test.Eq(t, "", out.String())
	})
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
