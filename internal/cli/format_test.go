package cli

import (
	"testing"

	platformerrors "github.com/primandproper/platform-go/v10/errors"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestFormatString(t *testing.T) {
	t.Parallel()

	test.Eq(t, "text", formatText.String())
	test.Eq(t, "json", formatJSON.String())
	test.Eq(t, "sarif", formatSARIF.String())
	// A value that cannot come from parseFormat still has to print as
	// something, because it reaches an error message.
	test.Eq(t, "format(9)", format(9).String())
}

func TestParseFormat(t *testing.T) {
	t.Parallel()

	for name, expected := range map[string]format{
		"text":  formatText,
		"json":  formatJSON,
		"sarif": formatSARIF,
	} {
		parsed, err := parseFormat(name)
		must.NoError(t, err)
		test.Eq(t, expected, parsed)
	}

	parsed, err := parseFormat("yaml")

	must.ErrorIs(t, err, platformerrors.ErrUnrecognizedInputValue)
	test.StrContains(t, err.Error(), "unknown format")
	// A rejected value falls back to the human-facing format, the way a
	// rejected strictness falls back to the strictest.
	test.Eq(t, formatText, parsed)
}

func TestResolveFormat(t *testing.T) {
	t.Parallel()

	t.Run("takes --format when --json is absent", func(t *testing.T) {
		t.Parallel()

		resolved, err := resolveFormat("sarif", true, false)
		must.NoError(t, err)
		test.Eq(t, formatSARIF, resolved)
	})

	t.Run("takes --json as the shorthand it is", func(t *testing.T) {
		t.Parallel()

		// Unset --format carries its default, which must not be read as a
		// contradiction of the shorthand.
		resolved, err := resolveFormat("text", false, true)
		must.NoError(t, err)
		test.Eq(t, formatJSON, resolved)
	})

	t.Run("accepts the pair when they agree", func(t *testing.T) {
		t.Parallel()

		resolved, err := resolveFormat("json", true, true)
		must.NoError(t, err)
		test.Eq(t, formatJSON, resolved)
	})

	t.Run("refuses to pick a winner when they disagree", func(t *testing.T) {
		t.Parallel()

		// Rendering either one would be rendering something the caller did not
		// ask for, so neither is chosen.
		_, err := resolveFormat("sarif", true, true)

		must.ErrorIs(t, err, platformerrors.ErrUnrecognizedInputValue)
		test.StrContains(t, err.Error(), "different output")
	})

	t.Run("rejects an unknown format", func(t *testing.T) {
		t.Parallel()

		_, err := resolveFormat("xml", true, false)

		must.ErrorIs(t, err, platformerrors.ErrUnrecognizedInputValue)
	})
}
