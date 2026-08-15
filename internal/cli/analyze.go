package cli

import (
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/primandproper/tarpaulin/internal/analysis"
	"github.com/primandproper/tarpaulin/internal/config"
	"github.com/primandproper/tarpaulin/internal/sarif"
	"github.com/primandproper/tarpaulin/version"

	"github.com/primandproper/platform-go/v10/encoding"
	platformerrors "github.com/primandproper/platform-go/v10/errors"

	"github.com/spf13/cobra"
)

// The gate sentinels are returned when a threshold the caller set was not met,
// so the process exits non-zero. Neither is printed as an error: by the time
// either is returned the operator has already been told everything — the report
// itself for --fail-on-found, and the line on stderr below for --min-score.
var (
	errFunctionsFound    = platformerrors.New("functions without direct unit tests were found")
	errScoreBelowMinimum = platformerrors.New("the score is below the required minimum")
)

// The bounds of --min-score, which is a percentage like the score it is
// compared against.
const (
	minimumScore = 0
	maximumScore = 100
)

// The flags only analyze carries. Like the shared ones, they are named because
// resolution asks cobra which of them were typed.
const (
	formatFlag      = "format"
	minScoreFlag    = "min-score"
	failOnFoundFlag = "fail-on-found"
)

// analyzeOptions holds the flag values for one invocation.
type analyzeOptions struct {
	format string
	targetOptions
	minScore    int
	failOnFound bool
	asJSON      bool
}

// newAnalyzeCommand returns the `analyze` subcommand, which grades a package on
// how many of its functions carry a test of their own.
//
//tarp:ignore -- declaration only: flags and help text, driven end to end by TestAnalyzeCommand through cobra rather than around it
func (a *application) newAnalyzeCommand() *cobra.Command {
	opts := &analyzeOptions{}

	cmd := &cobra.Command{
		Use:   "analyze [packages]",
		Short: "Report the functions in a package that have no direct unit test.",
		Long: "Analyze loads a package with its tests and reports every function that no\n" +
			"TestXxx body references.\n\n" +
			"What gets analyzed:\n" +
			"  tarp analyze                 the current directory and everything beneath it\n" +
			"  tarp analyze ./cmd/... ./io  the patterns given, resolved from here\n" +
			"  tarp analyze -p ./internal   that directory, expanded to ./... beneath it\n" +
			"  tarp analyze -p ./cmd/...    a pattern, resolved from here\n\n" +
			"Arguments take precedence over --package. A --package value naming an existing\n" +
			"directory is loaded as that directory, expanded to ./... beneath it; anything\n" +
			"else is handed to go/packages as written and resolved against the working\n" +
			"directory, so package paths such as example.com/mod/... work too.\n\n" +
			"The target must sit inside a Go module: packages load in module mode, and a\n" +
			"directory with no go.mod above it cannot be listed.\n\n" +
			"Defaults come from the project's config file when there is one — a\n" +
			".tarp.yaml, .tarp.yml, .tarp.json, or .tarp.toml at the module root — and\n" +
			"a flag typed here overrides it for this run.\n\n" +
			"Failing the build:\n" +
			"  --fail-on-found    exit non-zero if anything at all is reported\n" +
			"  --min-score 50     exit non-zero if the grade falls below 50%\n\n" +
			"Both are off by default, and either can be combined with any --format.\n" +
			"Neither prints an \"Error:\" line: the report, and the one line --min-score\n" +
			"writes to stderr, are the whole message.\n\n" +
			"Output:\n" +
			"  --format text   the human-facing summary (the default)\n" +
			"  --format json   the report as JSON; --json is shorthand for this\n" +
			"  --format sarif  a SARIF 2.1.0 document, for viewers and code scanning\n\n" +
			"Warnings go to stderr in every format, so stdout stays parseable.",
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runAnalyze(cmd, args, opts)
		},
	}

	registerAnalyzeFlags(cmd, opts)

	return cmd
}

// registerAnalyzeFlags declares analyze's flags: the shared ones that decide
// what is loaded, and the ones only this command carries.
//
// It is separate from the command so that resolution can be tested over the
// real flag set rather than a copy of it — the layering below asks cobra which
// flags were typed, and a test that registered its own would be checking a
// second set that could drift from this one.
//
//tarp:ignore -- declaration only: flag names, defaults, and help text, asserted through cobra by TestResolveSettings and TestAnalyzeCommand, which parse the set it declares
func registerAnalyzeFlags(cmd *cobra.Command, opts *analyzeOptions) {
	registerTargetFlags(cmd, &opts.targetOptions)

	cmd.Flags().BoolVarP(&opts.failOnFound, failOnFoundFlag, "F", false,
		"exit non-zero when functions without direct tests are found")
	cmd.Flags().IntVarP(&opts.minScore, minScoreFlag, "m", config.DefaultMinScore,
		"exit non-zero when the grade falls below this `percentage` (0 to 100; 0 never fails)")
	cmd.Flags().StringVarP(&opts.format, formatFlag, "f", config.DefaultFormat,
		"how to render the report: text, json, or sarif")
	cmd.Flags().BoolVarP(&opts.asJSON, "json", "j", false,
		"shorthand for --format=json")
}

// runAnalyze performs one analysis and renders it.
func (a *application) runAnalyze(cmd *cobra.Command, args []string, opts *analyzeOptions) error {
	resolved, err := a.resolveSettings(cmd, args, &opts.targetOptions)
	if err != nil {
		return err
	}

	gates, err := a.resolveGates(cmd, opts)
	if err != nil {
		return err
	}

	rendering, err := resolveFormat(gates.Format, cmd.Flags().Changed(formatFlag), opts.asJSON)
	if err != nil {
		return err
	}

	// Every setting is checked before anything is loaded: a typo in a threshold
	// or a format should cost a runner no time at all, let alone a full package
	// load.
	if err = checkMinScore(gates.MinScore); err != nil {
		return err
	}

	a.log().WithValues(map[string]any{
		"dir":        resolved.dir,
		"patterns":   strings.Join(resolved.patterns, " "),
		"strictness": resolved.strictness.String(),
	}).Debug("analyzing")

	report, err := analysis.Analyze(cmd.Context(), resolved.analysisConfig())
	if err != nil {
		return err
	}

	if err = writeWarnings(cmd.ErrOrStderr(), report.Warnings); err != nil {
		return err
	}

	if err = render(cmd.OutOrStdout(), report, rendering); err != nil {
		return err
	}

	// --fail-on-found is the strictly stronger gate: a score below any minimum
	// implies something was reported, so when both are set and both trip, the
	// score line underneath would be saying the same thing twice.
	if gates.FailOnFound && len(report.Untested()) > 0 {
		return errFunctionsFound
	}

	return checkScore(cmd.ErrOrStderr(), report.Score(), gates.MinScore)
}

// resolveGates reconciles the analyze-only settings — how the report is
// rendered and what fails the build — the same way resolveSettings reconciles
// the shared ones.
func (a *application) resolveGates(cmd *cobra.Command, opts *analyzeOptions) (config.AnalyzeConfig, error) {
	analyze := a.configuration().Analyze

	flags := cmd.Flags()

	if flags.Changed(formatFlag) {
		analyze.Format = opts.format
	}

	if flags.Changed(minScoreFlag) {
		analyze.MinScore = opts.minScore
	}

	if flags.Changed(failOnFoundFlag) {
		analyze.FailOnFound = opts.failOnFound
	}

	if err := analyze.OverlayEnvironment(); err != nil {
		return config.AnalyzeConfig{}, err
	}

	return analyze, nil
}

// checkMinScore rejects a --min-score outside the range a score can occupy.
// Without it, --min-score 150 would be a build that fails forever for a reason
// nothing on screen explains.
func checkMinScore(minimum int) error {
	if minimum < minimumScore || minimum > maximumScore {
		// The platform sentinel is what a caller branches on; the message is
		// what an operator reads. Wrapping carries both.
		return platformerrors.Wrapf(
			platformerrors.ErrUnrecognizedInputValue,
			"minimum score %d out of range: expected %d to %d",
			minimum, minimumScore, maximumScore,
		)
	}

	return nil
}

// checkScore applies the --min-score gate. The default minimum is 0 and a score
// is never negative, so an unset flag can never trip this.
//
// The explanation goes to stderr rather than stdout, for the same reason
// warnings do: --json output has to stay parseable. The report says what the
// grade is, but only the flag knows what it needed to be, so unlike
// --fail-on-found this gate has something left to say.
func checkScore(w io.Writer, score, minimum int) error {
	if score >= minimum {
		return nil
	}

	if _, err := fmt.Fprintf(w, "score %d%% is below the required minimum of %d%%\n", score, minimum); err != nil {
		return platformerrors.Wrap(err, "reporting the score gate")
	}

	return errScoreBelowMinimum
}

// writeWarnings reports anything the analyzer noticed about the source itself.
// They go to stderr so that --json output on stdout stays parseable.
func writeWarnings(w io.Writer, warnings []string) error {
	for _, warning := range warnings {
		if _, err := fmt.Fprintln(w, warning); err != nil {
			return platformerrors.Wrap(err, "writing warnings")
		}
	}

	return nil
}

// render writes the report in the requested format.
func render(w io.Writer, report *analysis.Report, rendering format) error {
	switch rendering {
	case formatJSON:
		encoded, err := encoding.EncodeJSON(report)
		if err != nil {
			return platformerrors.Wrap(err, "encoding report")
		}

		_, err = fmt.Fprintf(w, "%s\n", encoded)

		return err
	case formatSARIF:
		return sarif.Render(w, sarif.Config{Report: report, Version: version.Version})
	case formatMarkdown:
		return renderMarkdown(w, report)
	case formatText:
		return renderText(w, report)
	default:
		// parseFormat is the only way to get a format, and it rejects anything
		// else — so this is unreachable rather than a case worth handling.
		return platformerrors.Wrapf(platformerrors.ErrUnrecognizedInputValue, "rendering format %s", rendering)
	}
}

// renderText writes the report the way the 2017 tool did: the functions that
// are missing tests, grouped by file, and then the grade.
func renderText(w io.Writer, report *analysis.Report) error {
	colors := newPalette(w)
	untested := report.Untested()

	var out strings.Builder

	if len(untested) > 0 {
		out.WriteString("Functions without direct unit tests:\n")

		width := longestName(untested)
		file := ""

		for i := range untested {
			fn := &untested[i]

			if fn.File != file {
				file = fn.File

				fmt.Fprintf(&out, "in %s:\n", colors.paint(file, ansiBold, ansiWhite))
			}

			fmt.Fprintf(&out, "\t%s%s on line %d\n", indent(fn.Name, width), fn.Name, fn.Line)
		}

		out.WriteString("\n")
	}

	fmt.Fprintf(&out, "Grade: %s (%d/%d functions)\n",
		colors.grade(report.Score()), report.Tested(), report.Declared())

	_, err := io.WriteString(w, out.String())

	return err
}

// renderMarkdown writes the grade as a table with one row per package.
//
// This is the shape a report takes when it spans a module rather than a
// package: nobody reads two thousand functions one line at a time, but a
// hundred packages ranked by grade says where the work is. No color is
// involved — markdown is read after it is pasted somewhere, not in the terminal
// that produced it.
func renderMarkdown(w io.Writer, report *analysis.Report) error {
	var out strings.Builder

	// Numbers are right-aligned, names are not: the columns being compared
	// down the page are the ones that should line up.
	out.WriteString("| Package | Score | Tested | Declared |\n")
	out.WriteString("| --- | ---: | ---: | ---: |\n")

	packages := report.Packages()
	for i := range packages {
		pkg := &packages[i]

		fmt.Fprintf(&out, "| `%s` | %d%% | %d | %d |\n", pkg.Path, pkg.Score(), pkg.Tested, pkg.Declared)
	}

	// The total is a row rather than a sentence underneath, so the table stays
	// one thing to sort, paste, or diff.
	fmt.Fprintf(&out, "| **Total** | **%d%%** | **%d** | **%d** |\n",
		report.Score(), report.Tested(), report.Declared())

	fmt.Fprintf(&out, "\nGraded at `%s` strictness.\n", report.Strictness)

	_, err := io.WriteString(w, out.String())

	return err
}

// longestName measures the widest function name so the report can right-align
// them into a column.
func longestName(functions []analysis.Function) int {
	widest := 0

	for i := range functions {
		if width := utf8.RuneCountInString(functions[i].Name); width > widest {
			widest = width
		}
	}

	return widest
}

// indent returns the padding that right-aligns name in a column of the given
// width.
func indent(name string, width int) string {
	return strings.Repeat(" ", width-utf8.RuneCountInString(name))
}
