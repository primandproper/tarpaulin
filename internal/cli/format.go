package cli

import (
	"fmt"

	platformerrors "github.com/primandproper/platform-go/v10/errors"
)

// format selects how a report is rendered. It is a CLI concern rather than an
// analysis one — analysis.Config has no opinion about output — so it lives here
// alongside the command that reads the flag.
type format uint8

const (
	// formatText is the human-facing summary: the functions missing tests,
	// grouped by file, and the grade.
	formatText format = iota
	// formatJSON is the machine-readable report on stdout.
	formatJSON
	// formatSARIF is the Static Analysis Results Interchange Format, which
	// every SARIF viewer, GitHub code scanning, and the sarif-tools CLI read.
	formatSARIF
	// formatMarkdown is a per-package table, for a report that spans more
	// packages than anybody wants to read one function at a time.
	formatMarkdown
)

// Format names as they appear on the command line.
const (
	formatTextName     = "text"
	formatJSONName     = "json"
	formatSARIFName    = "sarif"
	formatMarkdownName = "markdown"
)

// String implements fmt.Stringer, returning the flag spelling of the format.
func (f format) String() string {
	switch f {
	case formatText:
		return formatTextName
	case formatJSON:
		return formatJSONName
	case formatSARIF:
		return formatSARIFName
	case formatMarkdown:
		return formatMarkdownName
	default:
		return fmt.Sprintf("format(%d)", uint8(f))
	}
}

// parseFormat converts a flag value into a format.
func parseFormat(raw string) (format, error) {
	switch raw {
	case formatTextName:
		return formatText, nil
	case formatJSONName:
		return formatJSON, nil
	case formatSARIFName:
		return formatSARIF, nil
	case formatMarkdownName:
		return formatMarkdown, nil
	default:
		// The platform sentinel is what a caller branches on; the message is
		// what an operator reads. Wrapping carries both, and the human-facing
		// format is what a rejected value falls back to.
		return formatText, platformerrors.Wrapf(
			platformerrors.ErrUnrecognizedInputValue,
			"unknown format %q: expected %s, %s, %s, or %s",
			raw, formatTextName, formatJSONName, formatSARIFName, formatMarkdownName,
		)
	}
}

// resolveFormat reconciles --format with the older --json spelling.
//
// --json is kept because `tarp analyze --json | jq` is muscle memory, and it
// means exactly --format=json. Silently letting one win when they disagree
// would render a format the caller did not ask for, so a contradiction is
// refused rather than resolved.
func resolveFormat(raw string, formatSet, asJSON bool) (format, error) {
	if !asJSON {
		return parseFormat(raw)
	}

	if formatSet && raw != formatJSONName {
		return formatText, platformerrors.Wrapf(
			platformerrors.ErrUnrecognizedInputValue,
			"--json and --format=%s ask for different output: pass only one",
			raw,
		)
	}

	return formatJSON, nil
}
