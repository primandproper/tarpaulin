package analysis_test

import (
	"testing"

	"github.com/primandproper/tarpaulin/internal/analysis"

	platformerrors "github.com/primandproper/platform-go/v10/errors"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestParseStrictness(t *testing.T) {
	t.Parallel()

	cases := map[string]analysis.Strictness{
		"file":    analysis.StrictnessFile,
		"package": analysis.StrictnessPackage,
		"any":     analysis.StrictnessAny,
	}

	for name, expected := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			parsed, err := analysis.ParseStrictness(name)
			must.NoError(t, err)

			test.Eq(t, expected, parsed)
		})
	}

	t.Run("rejects anything else", func(t *testing.T) {
		t.Parallel()

		parsed, err := analysis.ParseStrictness("very")

		must.Error(t, err)
		test.StrContains(t, err.Error(), "unknown strictness")
		// A caller is owed something to branch on, not just prose: the value was
		// supplied and non-empty, it simply is not one of the three.
		test.ErrorIs(t, err, platformerrors.ErrUnrecognizedInputValue)
		// The dial only ever weakens, so a rejected value must not leave the
		// caller holding something looser than the default.
		test.Eq(t, analysis.StrictnessFile, parsed)
	})
}

func TestStrictnessString(t *testing.T) {
	t.Parallel()

	test.Eq(t, "file", analysis.StrictnessFile.String())
	test.Eq(t, "package", analysis.StrictnessPackage.String())
	test.Eq(t, "any", analysis.StrictnessAny.String())
	test.Eq(t, "Strictness(9)", analysis.Strictness(9).String())

	// The zero value is the strictest setting: a Config nobody configured must
	// grade hardest, not softest.
	test.Eq(t, analysis.StrictnessFile, analysis.Strictness(0))
}
