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

// errNoProfile is returned when `annotate` is invoked without a profile to
// render.
var errNoProfile = platformerrors.New("--profile is required: pass the cover profile to annotate")

// annotateOptions holds the flag values for one invocation.
type annotateOptions struct {
	profile string
	output  string
	targetOptions
}

// newAnnotateCommand returns the `annotate` subcommand, which renders a cover
// profile with tarp's verdict layered over it.
//
//tarp:ignore -- declaration only: flags and help text, driven end to end by TestAnnotateCommand through cobra rather than around it
func (a *application) newAnnotateCommand() *cobra.Command {
	opts := &annotateOptions{}

	cmd := &cobra.Command{
		Use:   "annotate --profile=<profile> [packages]",
		Short: "Annotate a cover profile as HTML, colored by which functions have a direct test.",
		Long: "Annotate renders the HTML report `go tool cover -html` renders, with the green\n" +
			"split in two: green for a function a TestXxx body references directly, yellow\n" +
			"for one that merely ran on the way to somebody else's assertion. Red is still\n" +
			"code that never ran, and anything tarp does not grade is left grey.\n\n" +
			"The profile comes from `go test -coverprofile=<profile>`; the packages analyzed\n" +
			"alongside it are chosen exactly as `tarp analyze` chooses them — arguments take\n" +
			"precedence over --package, a --package value naming an existing directory is\n" +
			"expanded to ./... beneath it, and anything else is a go/packages pattern\n" +
			"resolved from the working directory. The project's config file supplies the\n" +
			"defaults for all of them. The report goes to stdout unless --output names a\n" +
			"file.",
		Args: cobra.ArbitraryArgs,
		RunE: func(cmd *cobra.Command, args []string) error {
			return a.runAnnotate(cmd, args, opts)
		},
	}

	// The backticks name the placeholder cobra prints for the flag's argument.
	cmd.Flags().StringVar(&opts.profile, "profile", "",
		"path to the cover `profile` written by go test -coverprofile")
	cmd.Flags().StringVarP(&opts.output, "output", "o", "",
		"write the report to this file instead of stdout")

	registerTargetFlags(cmd, &opts.targetOptions)

	return cmd
}

// runAnnotate analyzes the requested packages and renders the profile against
// them.
func (a *application) runAnnotate(cmd *cobra.Command, args []string, opts *annotateOptions) error {
	profile := strings.TrimSpace(opts.profile)
	if profile == "" {
		return errNoProfile
	}

	resolved, err := a.resolveSettings(cmd, args, &opts.targetOptions)
	if err != nil {
		return err
	}

	a.log().WithValues(map[string]any{
		"dir":        resolved.dir,
		"patterns":   strings.Join(resolved.patterns, " "),
		"strictness": resolved.strictness.String(),
		"profile":    profile,
	}).Debug("rendering coverage")

	report, err := analysis.Analyze(cmd.Context(), resolved.analysisConfig())
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
		Dir:     resolved.dir,
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
