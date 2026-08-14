// Package coverage renders a cover profile as HTML, colored by whether each
// function carries a direct unit test rather than by statement counts alone.
//
// `go tool cover -html` paints a statement green once anything has executed it,
// which is exactly the confusion tarp exists to correct: a function reached only
// on the way to somebody else's assertion looks identical to one with a test of
// its own. This package keeps the familiar layout and splits that green in two —
// green for a function a TestXxx body references directly, yellow for one that
// merely ran. Red still means never executed at all, and code tarp does not
// grade (init, main, generated, ignored) is left grey so the view never claims
// more than the analysis does.
package coverage

import (
	"cmp"
	"context"
	"html/template"
	"io"
	"os"
	"path"
	"slices"

	"github.com/primandproper/tarpaulin/internal/analysis"

	platformerrors "github.com/primandproper/platform-go/v10/errors"

	"golang.org/x/tools/cover"
)

// percent is the multiplier that turns a ratio into a percentage.
const percent = 100

// Config selects the profile to render and the analysis to render it against.
type Config struct {
	// Report is the analysis of the same packages the profile covers. Its
	// verdicts are what separates green from yellow; a file the report says
	// nothing about is rendered, but grey. Its file list also resolves the
	// profile's package-relative names to sources, sparing a second load of
	// packages this one has already read.
	Report *analysis.Report
	// Dir is the directory the profile's package paths are resolved against.
	// Empty means the current working directory.
	Dir string
	// Profile is the path to a cover profile written by `go test -coverprofile`.
	Profile string
}

// Render writes the annotated HTML report for the configured profile to w.
func Render(ctx context.Context, w io.Writer, cfg Config) error {
	profiles, err := cover.ParseProfiles(cfg.Profile)
	if err != nil {
		return platformerrors.Wrapf(err, "parsing cover profile %s", cfg.Profile)
	}

	if len(profiles) == 0 {
		return platformerrors.Wrapf(ErrEmptyProfile, "rendering %s", cfg.Profile)
	}

	dir := cfg.Dir
	if dir == "" {
		dir = "."
	}

	sources, err := resolveSources(ctx, dir, cfg.Report, profiles)
	if err != nil {
		return err
	}

	rendered, err := buildPage(cfg.Report, sources)
	if err != nil {
		return err
	}

	if err = pageTemplate.Execute(w, rendered); err != nil {
		return platformerrors.Wrap(err, "rendering the coverage report")
	}

	return nil
}

// page is what the template renders.
type page struct {
	Package  string
	Files    []pageFile
	Declared int
	Tested   int
	Score    int
}

// pageFile is one source file in the report, already annotated.
type pageFile struct {
	Name     string
	Body     template.HTML
	Coverage float64
	Declared int
	Tested   int
}

// buildPage annotates every file in the profile and totals up the two numbers
// the header carries: the statement coverage the profile measured, and the
// grade tarp gave the same code. The grade covers the files on screen, not the
// whole report — a header describing packages the profile never mentioned would
// be a number the reader cannot check.
func buildPage(report *analysis.Report, sources []source) (*page, error) {
	byFile := functionsByPath(report)

	rendered := &page{Files: make([]pageFile, 0, len(sources))}

	for i := range sources {
		src, err := os.ReadFile(sources[i].path)
		if err != nil {
			return nil, platformerrors.Wrapf(err, "reading %s", sources[i].profile.FileName)
		}

		functions := byFile[sources[i].path]
		tested := testedCount(functions)

		rendered.Files = append(rendered.Files, pageFile{
			Name:     sources[i].profile.FileName,
			Body:     annotate(src, sources[i].profile.Blocks, functions),
			Coverage: percentCovered(sources[i].profile),
			Declared: len(functions),
			Tested:   tested,
		})

		rendered.Declared += len(functions)
		rendered.Tested += tested
	}

	rendered.Package = packageName(rendered.Files)
	rendered.Score = score(rendered.Tested, rendered.Declared)

	return rendered, nil
}

// functionsByPath groups the report by the file each function is declared in,
// sorted by declaration line so a block can be attributed by binary search.
// Files carry absolute paths on both sides of this join: the report's come from
// go/packages, and so do the profile's.
func functionsByPath(report *analysis.Report) map[string][]analysis.Function {
	byFile := make(map[string][]analysis.Function)

	if report == nil {
		return byFile
	}

	for i := range report.Functions {
		declared := report.Functions[i].Path
		byFile[declared] = append(byFile[declared], report.Functions[i])
	}

	for declared := range byFile {
		slices.SortFunc(byFile[declared], func(a, b analysis.Function) int {
			return cmp.Compare(a.Line, b.Line)
		})
	}

	return byFile
}

// testedCount is how many of the functions carry a direct test.
func testedCount(functions []analysis.Function) int {
	tested := 0

	for i := range functions {
		if functions[i].Tested {
			tested++
		}
	}

	return tested
}

// score is the percentage of functions carrying a direct test, truncated the
// way analysis.Report.Score truncates it. Nothing to grade scores 100.
func score(tested, declared int) int {
	if declared == 0 {
		return percent
	}

	return tested * percent / declared
}

// percentCovered is the fraction of the profile's statements that executed,
// which is the number `go tool cover` reports and the one users will compare
// this report against.
func percentCovered(profile *cover.Profile) float64 {
	var total, covered int64

	for i := range profile.Blocks {
		total += int64(profile.Blocks[i].NumStmt)

		if profile.Blocks[i].Count > 0 {
			covered += int64(profile.Blocks[i].NumStmt)
		}
	}

	if total == 0 {
		return 0
	}

	return float64(covered) / float64(total) * percent
}

// packageName names the report after the directory its first file lives in, the
// way `go tool cover` does: it is cheap, needs no parsing, and gets a better
// answer than the package clause would for package main.
func packageName(files []pageFile) string {
	if len(files) == 0 {
		return ""
	}

	// Profile file names are always slash-separated package paths.
	return path.Base(path.Dir(files[0].Name))
}
