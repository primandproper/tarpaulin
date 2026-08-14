package coverage

import (
	"context"
	"maps"
	"path"
	"path/filepath"
	"slices"

	"github.com/primandproper/tarpaulin/internal/analysis"

	platformerrors "github.com/primandproper/platform-go/v10/errors"

	"golang.org/x/tools/cover"
	"golang.org/x/tools/go/packages"
)

// locateMode is all go/packages is asked for here: the name of each package and
// the files it is built from. No syntax, no types — this pass only turns the
// package paths a cover profile names into files on disk, and it runs only for
// the packages the analysis being rendered did not already load.
const locateMode = packages.NeedName | packages.NeedFiles | packages.NeedCompiledGoFiles

// ErrEmptyProfile is returned when a profile parses but describes nothing. An
// empty profile usually means `go test` matched no packages, and rendering a
// blank page for it would hide that.
var ErrEmptyProfile = platformerrors.New("the cover profile describes no files")

// ErrUnresolvedFile is returned when a file named in the profile cannot be
// found on disk. The usual cause is running against a different checkout than
// the one the profile was produced from.
var ErrUnresolvedFile = platformerrors.New("no source file found for a file named in the cover profile")

// source pairs one file of a cover profile with the source it refers to.
type source struct {
	profile *cover.Profile
	path    string
}

// resolveSources finds the file on disk behind each of the profile's entries.
//
// A profile names its files by package path — "example.com/m/pkg/thing.go" —
// which is not a path anybody can open. The analysis that is about to be
// rendered over it already loaded those packages and carries their file lists,
// so the mapping is reindexed rather than re-derived. Only what the report
// cannot account for is loaded again: a stale profile, a package outside the
// analyzed pattern, or no report at all.
func resolveSources(
	ctx context.Context,
	dir string,
	report *analysis.Report,
	profiles []*cover.Profile,
) ([]source, error) {
	index := indexReport(report)

	if missing := missingPackages(index, profiles); len(missing) > 0 {
		located, err := locateFiles(ctx, dir, missing)
		if err != nil {
			return nil, err
		}

		maps.Copy(index, located)
	}

	sources := make([]source, 0, len(profiles))

	for _, profile := range profiles {
		located, ok := index[profile.FileName]
		if !ok {
			return nil, platformerrors.Wrapf(ErrUnresolvedFile, "resolving %s", profile.FileName)
		}

		sources = append(sources, source{profile: profile, path: located})
	}

	return sources, nil
}

// indexReport keys the files the analysis already loaded the way a cover
// profile spells them. A nil report — `cover` renders one, ungraded — indexes
// nothing, which sends every package to the load below.
func indexReport(report *analysis.Report) map[string]string {
	if report == nil {
		return map[string]string{}
	}

	index := make(map[string]string, len(report.Sources))

	for pkgPath, files := range report.Sources {
		for _, file := range files {
			index[profileKey(pkgPath, file)] = file
		}
	}

	return index
}

// missingPackages is every distinct package a profile covers that the index
// cannot already account for, sorted so the load is deterministic. When the
// profile covers exactly what was analyzed — the ordinary case — this is empty
// and nothing is loaded a second time.
func missingPackages(index map[string]string, profiles []*cover.Profile) []string {
	paths := make([]string, 0, len(profiles))

	for _, profile := range profiles {
		if _, ok := index[profile.FileName]; ok {
			continue
		}

		// Profile file names are slash-separated package paths, whatever the
		// host's separator is.
		paths = append(paths, path.Dir(profile.FileName))
	}

	slices.Sort(paths)

	return slices.Compact(paths)
}

// profileKey spells a file the way a cover profile does: the package's import
// path, then the file's base name. Paths from a load carry the host's
// separator; a profile's keys are always slashes, so only the base name is
// shared between the two spellings.
func profileKey(pkgPath, file string) string {
	return pkgPath + "/" + filepath.Base(file)
}

// locateFiles maps "<package path>/<file name>" — exactly how a cover profile
// spells a file — to that file's absolute path.
func locateFiles(ctx context.Context, dir string, paths []string) (map[string]string, error) {
	if len(paths) == 0 {
		return map[string]string{}, nil
	}

	loaded, err := packages.Load(&packages.Config{Context: ctx, Mode: locateMode, Dir: dir}, paths...)
	if err != nil {
		return nil, platformerrors.Wrap(err, "locating the packages named in the cover profile")
	}

	index := make(map[string]string)

	for _, pkg := range loaded {
		if pkg.PkgPath == "" {
			continue
		}

		for _, file := range slices.Concat(pkg.GoFiles, pkg.CompiledGoFiles, pkg.IgnoredFiles) {
			index[profileKey(pkg.PkgPath, file)] = file
		}
	}

	return index, nil
}
