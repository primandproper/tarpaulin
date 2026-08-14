package analysis

import (
	"testing"

	"github.com/shoenig/test"
)

func TestRenderWarnings(t *testing.T) {
	t.Parallel()

	t.Run("names the declaration, the place, and the directive", func(t *testing.T) {
		t.Parallel()

		rendered := renderWarnings("/src", []warning{
			{file: "/src/pkg/main.go", line: 4, name: "(*Thing).Method"},
			{file: "/src/pkg/main.go", line: 9, name: "Plain"},
		})

		test.Eq(t, []string{
			"pkg/main.go:4: (*Thing).Method carries a bare //tarp:ignore with no reason and was not exempted",
			"pkg/main.go:9: Plain carries a bare //tarp:ignore with no reason and was not exempted",
		}, rendered)
	})

	t.Run("leaves a path outside the analyzed directory alone", func(t *testing.T) {
		t.Parallel()

		rendered := renderWarnings("/src", []warning{{file: "/elsewhere/main.go", line: 4, name: "Plain"}})

		test.SliceLen(t, 1, rendered)
		test.StrHasPrefix(t, "/elsewhere/main.go:4:", rendered[0])
	})

	t.Run("renders nothing as an empty list", func(t *testing.T) {
		t.Parallel()

		// The result is the report's Warnings field verbatim, and a nil slice
		// would encode as null rather than [] — a shape other people's CI parses.
		rendered := renderWarnings("/src", nil)

		test.SliceEmpty(t, rendered)
		test.NotNil(t, rendered)
	})
}
