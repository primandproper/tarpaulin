package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/primandproper/tarpaulin/internal/analysis"

	"github.com/primandproper/platform-go/v10/encoding"
	platformerrors "github.com/primandproper/platform-go/v10/errors"

	"github.com/spf13/cobra"
)

// errFunctionsFound is returned under --fail-on-found so the process exits
// non-zero. It is printed by nobody: the report itself is the message.
var errFunctionsFound = platformerrors.New("functions without direct unit tests were found")

// analyzeOptions holds the flag values for one invocation.
type analyzeOptions struct {
	pkg         string
	strictness  string
	failOnFound bool
	asJSON      bool
}

// newAnalyzeCommand returns the `analyze` subcommand, which grades a package on
// how many of its functions carry a test of their own.
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
			"directory with no go.mod above it cannot be listed.",
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runAnalyze(cmd, args, opts)
		},
	}

	cmd.Flags().StringVarP(&opts.pkg, "package", "p", ".",
		"`directory` to analyze (expanded to ./... beneath it), or a go/packages pattern resolved from here; ignored when arguments are given")
	cmd.Flags().StringVarP(&opts.strictness, "strictness", "s", analysis.StrictnessFile.String(),
		"how close a reference must be to count: file, package, or any")
	cmd.Flags().BoolVarP(&opts.failOnFound, "fail-on-found", "F", false,
		"exit non-zero when functions without direct tests are found")
	cmd.Flags().BoolVarP(&opts.asJSON, "json", "j", false,
		"render the report as JSON")

	return cmd
}

// runAnalyze performs one analysis and renders it.
func (a *application) runAnalyze(cmd *cobra.Command, args []string, opts *analyzeOptions) error {
	strictness, err := analysis.ParseStrictness(opts.strictness)
	if err != nil {
		return err
	}

	dir, patterns := resolveTarget(opts.pkg, args)

	a.log().WithValues(map[string]any{
		"dir":        dir,
		"patterns":   strings.Join(patterns, " "),
		"strictness": strictness.String(),
	}).Debug("analyzing")

	report, err := analysis.Analyze(cmd.Context(), analysis.Config{
		Dir:        dir,
		Patterns:   patterns,
		Strictness: strictness,
	})
	if err != nil {
		return err
	}

	if err = writeWarnings(cmd.ErrOrStderr(), report.Warnings); err != nil {
		return err
	}

	if err = render(cmd.OutOrStdout(), report, opts.asJSON); err != nil {
		return err
	}

	if opts.failOnFound && len(report.Untested()) > 0 {
		return errFunctionsFound
	}

	return nil
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

// resolveTarget turns the flag and arguments into a directory and the patterns
// to load inside it.
//
// Arguments win over --package and are resolved from the working directory, the
// way the go command resolves its own. Failing those, a --package value naming
// an existing directory becomes the directory itself, expanded to everything
// beneath it; anything else is handed to go/packages as written, so package
// paths such as example.com/mod/... still work.
func resolveTarget(pkg string, args []string) (dir string, patterns []string) {
	if len(args) > 0 {
		return ".", args
	}

	if info, err := os.Stat(pkg); err == nil && info.IsDir() {
		return pkg, nil
	}

	return ".", []string{pkg}
}

// render writes the report as JSON or as the human-facing summary.
func render(w io.Writer, report *analysis.Report, asJSON bool) error {
	if asJSON {
		encoded, err := encoding.EncodeJSON(report)
		if err != nil {
			return platformerrors.Wrap(err, "encoding report")
		}

		_, err = fmt.Fprintf(w, "%s\n", encoded)

		return err
	}

	return renderText(w, report)
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
