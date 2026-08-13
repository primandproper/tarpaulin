package cli

import (
	"bytes"
	"os"
	"strings"

	"github.com/primandproper/tarpaulin/internal/analysis"
	"github.com/primandproper/tarpaulin/internal/coverage"

	platformerrors "github.com/primandproper/platform-go/v10/errors"

	"github.com/spf13/cobra"
)

// reportFileMode is the permission the written report carries. It is a report
// about somebody's source, written for whoever asked for it.
const reportFileMode = 0o600

// errNoProfile is returned when `cover` is invoked without a profile to render.
var errNoProfile = platformerrors.New("--html is required: pass the cover profile to render")

// coverOptions holds the flag values for one invocation.
type coverOptions struct {
	profile    string
	output     string
	pkg        string
	strictness string
}

// newCoverCommand returns the `cover` subcommand, which renders a cover profile
// with tarp's verdict layered over it.
func (a *application) newCoverCommand() *cobra.Command {
	opts := &coverOptions{}

	cmd := &cobra.Command{
		Use:   "cover --html=<profile> [packages]",
		Short: "Render a cover profile as HTML, colored by which functions have a direct test.",
		Long: "Cover renders the HTML report `go tool cover -html` renders, with the green\n" +
			"split in two: green for a function a TestXxx body references directly, yellow\n" +
			"for one that merely ran on the way to somebody else's assertion. Red is still\n" +
			"code that never ran, and anything tarp does not grade is left grey.\n\n" +
			"The profile comes from `go test -coverprofile=<profile>`; the packages analyzed\n" +
			"alongside it are chosen exactly as `tarp analyze` chooses them — arguments take\n" +
			"precedence over --package, a --package value naming an existing directory is\n" +
			"expanded to ./... beneath it, and anything else is a go/packages pattern\n" +
			"resolved from the working directory. The report goes to stdout unless --output\n" +
			"names a file.",
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runCover(cmd, args, opts)
		},
	}

	// The backticks name the placeholder cobra prints for the flag's argument.
	cmd.Flags().StringVar(&opts.profile, "html", "",
		"path to the cover `profile` written by go test -coverprofile")
	cmd.Flags().StringVarP(&opts.output, "output", "o", "",
		"write the report to this file instead of stdout")
	cmd.Flags().StringVarP(&opts.pkg, "package", "p", ".",
		"`directory` to analyze (expanded to ./... beneath it), or a go/packages pattern resolved from here; ignored when arguments are given")
	cmd.Flags().StringVarP(&opts.strictness, "strictness", "s", analysis.StrictnessFile.String(),
		"how close a reference must be to count: file, package, or any")

	return cmd
}

// runCover analyzes the requested packages and renders the profile against
// them.
func (a *application) runCover(cmd *cobra.Command, args []string, opts *coverOptions) error {
	profile := strings.TrimSpace(opts.profile)
	if profile == "" {
		return errNoProfile
	}

	strictness, err := analysis.ParseStrictness(opts.strictness)
	if err != nil {
		return err
	}

	dir, patterns := resolveTarget(opts.pkg, args)

	a.log().WithValues(map[string]any{
		"dir":        dir,
		"patterns":   strings.Join(patterns, " "),
		"strictness": strictness.String(),
		"profile":    profile,
	}).Debug("rendering coverage")

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

	// Render into memory first: a report that fails halfway through is not
	// worth writing anywhere, least of all over the last good one.
	var rendered bytes.Buffer

	if err = coverage.Render(cmd.Context(), &rendered, coverage.Config{
		Report:  report,
		Dir:     dir,
		Profile: profile,
	}); err != nil {
		return err
	}

	if output := strings.TrimSpace(opts.output); output != "" {
		if err = os.WriteFile(output, rendered.Bytes(), reportFileMode); err != nil {
			return platformerrors.Wrapf(err, "writing the report to %s", output)
		}

		return nil
	}

	if _, err = cmd.OutOrStdout().Write(rendered.Bytes()); err != nil {
		return platformerrors.Wrap(err, "writing the report")
	}

	return nil
}
