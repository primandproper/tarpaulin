package analysis

import (
	"fmt"

	platformerrors "github.com/primandproper/platform-go/v10/errors"
)

// Strictness selects how close a reference has to be to a declaration before it
// counts as a direct test. The dial only ever weakens: StrictnessFile is the
// default and the strongest claim the tool can make.
type Strictness uint8

const (
	// StrictnessFile requires the reference to live in the declaring file's test
	// slot: Bar declared in foo.go must be referenced from foo_test.go or
	// foo_internal_test.go.
	StrictnessFile Strictness = iota
	// StrictnessPackage accepts a reference from any _test.go in the package.
	StrictnessPackage
	// StrictnessAny accepts a reference anywhere in any _test.go, test helpers
	// included.
	StrictnessAny
)

// Strictness names as they appear on the command line and in JSON output.
const (
	strictnessFileName    = "file"
	strictnessPackageName = "package"
	strictnessAnyName     = "any"
)

// String implements fmt.Stringer, returning the flag spelling of the level.
func (s Strictness) String() string {
	switch s {
	case StrictnessFile:
		return strictnessFileName
	case StrictnessPackage:
		return strictnessPackageName
	case StrictnessAny:
		return strictnessAnyName
	default:
		return fmt.Sprintf("Strictness(%d)", uint8(s))
	}
}

// ParseStrictness converts a flag value into a Strictness.
func ParseStrictness(raw string) (Strictness, error) {
	switch raw {
	case strictnessFileName:
		return StrictnessFile, nil
	case strictnessPackageName:
		return StrictnessPackage, nil
	case strictnessAnyName:
		return StrictnessAny, nil
	default:
		// The platform sentinel is what a caller branches on; the message is
		// what an operator reads. Wrapping carries both, and the strictest
		// setting is what a rejected value falls back to.
		return StrictnessFile, platformerrors.Wrapf(
			platformerrors.ErrUnrecognizedInputValue,
			"unknown strictness %q: expected %s, %s, or %s",
			raw, strictnessFileName, strictnessPackageName, strictnessAnyName,
		)
	}
}
