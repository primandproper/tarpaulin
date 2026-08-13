package cli

import (
	"io"
	"os"
	"strconv"
	"strings"
)

// ANSI select-graphic-rendition codes, kept to the handful the report uses.
const (
	ansiReset   = "\033[0m"
	ansiBold    = "1"
	ansiRed     = "31"
	ansiGreen   = "32"
	ansiYellow  = "33"
	ansiBlue    = "34"
	ansiMagenta = "35"
	ansiCyan    = "36"
	ansiWhite   = "37"
)

// palette renders colored output, or plain text when the destination is not an
// interactive terminal or the user asked for no color.
type palette struct {
	enabled bool
}

// newPalette decides once whether output should carry escape codes: NO_COLOR is
// honored regardless of its value (https://no-color.org), TERM=dumb means the
// terminal cannot render them, and a redirected or piped stream should stay
// plain so `tarp analyze | grep` behaves.
func newPalette(w io.Writer) palette {
	if _, disabled := os.LookupEnv("NO_COLOR"); disabled || os.Getenv("TERM") == "dumb" {
		return palette{}
	}

	file, ok := w.(*os.File)
	if !ok {
		return palette{}
	}

	info, err := file.Stat()
	if err != nil {
		return palette{}
	}

	return palette{enabled: info.Mode()&os.ModeCharDevice != 0}
}

// paint wraps text in the given SGR codes when color is enabled.
func (p palette) paint(text string, codes ...string) string {
	if !p.enabled || len(codes) == 0 {
		return text
	}

	var painted strings.Builder

	for _, code := range codes {
		painted.WriteString("\033[" + code + "m")
	}

	return painted.String() + text + ansiReset
}

// grade renders a score as a percentage, colored by decile the way the 2017
// tool did: everything below 60% is red, and only a perfect run is green.
func (p palette) grade(score int) string {
	color := ansiRed

	switch score / 10 {
	case 6:
		color = ansiMagenta
	case 7:
		color = ansiYellow
	case 8:
		color = ansiCyan
	case 9:
		color = ansiBlue
	case 10:
		color = ansiGreen
	}

	return p.paint(strconv.Itoa(score)+"%", color)
}
