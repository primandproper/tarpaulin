package cli

import (
	"bytes"
	"os"
	"strings"
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestNewPalette(t *testing.T) {
	t.Parallel()

	t.Run("stays plain when output is not a terminal", func(t *testing.T) {
		t.Parallel()

		// The common case in CI, and the one that keeps `tarp analyze | grep`
		// and `> report.txt` free of escape codes.
		test.False(t, newPalette(new(bytes.Buffer)).enabled)
	})
}

// TestNewPaletteHonorsNoColor and its neighbor below mutate the environment,
// so they cannot run in parallel with anything.
func TestNewPaletteHonorsNoColor(t *testing.T) {
	// NO_COLOR is honored whatever its value, per https://no-color.org.
	t.Setenv("NO_COLOR", "")

	device, err := os.Open(os.DevNull)
	must.NoError(t, err)

	t.Cleanup(func() { _ = device.Close() })

	test.False(t, newPalette(device).enabled)
}

func TestNewPaletteHonorsDumbTerminal(t *testing.T) {
	t.Setenv("TERM", "dumb")

	test.False(t, newPalette(new(bytes.Buffer)).enabled)
}

func TestPalettePaint(t *testing.T) {
	t.Parallel()

	t.Run("passes text through when disabled", func(t *testing.T) {
		t.Parallel()

		test.Eq(t, "main.go", palette{}.paint("main.go", ansiBold, ansiWhite))
	})

	t.Run("wraps text when enabled", func(t *testing.T) {
		t.Parallel()

		painted := palette{enabled: true}.paint("main.go", ansiWhite)

		test.True(t, strings.HasPrefix(painted, "\033["+ansiWhite+"m"))
		test.True(t, strings.HasSuffix(painted, ansiReset))
		test.StrContains(t, painted, "main.go")
	})
}

func TestPaletteGrade(t *testing.T) {
	t.Parallel()

	t.Run("renders a bare percentage when disabled", func(t *testing.T) {
		t.Parallel()

		test.Eq(t, "75%", palette{}.grade(75))
	})

	t.Run("colors by decile", func(t *testing.T) {
		t.Parallel()

		colors := palette{enabled: true}

		test.StrContains(t, colors.grade(100), ansiGreen)
		test.StrContains(t, colors.grade(95), ansiBlue)
		test.StrContains(t, colors.grade(85), ansiCyan)
		test.StrContains(t, colors.grade(75), ansiYellow)
		test.StrContains(t, colors.grade(65), ansiMagenta)
		test.StrContains(t, colors.grade(11), ansiRed)
	})
}
