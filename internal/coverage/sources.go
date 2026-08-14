package coverage

import (
	"context"
	"path"
	"path/filepath"
	"slices"

	platformerrors "github.com/primandproper/platform-go/v10/errors"

	"golang.org/x/tools/cover"
	"golang.org/x/tools/go/packages"
)

// locateMode is all go/packages is asked for here: the name of each package and
// the files it is built from. No syntax, no types — this pass only turns the
// package paths a cover profile names into files on disk.
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
// which is not a path anybody can open. Rather than re-deriving the mapping,
// the packages the profile mentions are loaded (names and file lists only, so
// this stays cheap) and indexed the same way the profile spells them.
func resolveSources(ctx context.Context, dir string, profiles []*cover.Profile) ([]source, error) {
	index, err := locateFiles(ctx, dir, packagePaths(profiles))
	if err != nil {
		return nil, err
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

// packagePaths is every distinct package a profile covers, sorted so the load
// below is deterministic.
func packagePaths(profiles []*cover.Profile) []string {
	paths := make([]string, 0, len(profiles))

	for _, profile := range profiles {
		// Profile file names are slash-separated package paths, whatever the
		// host's separator is.
		paths = append(paths, path.Dir(profile.FileName))
	}

	slices.Sort(paths)

	return slices.Compact(paths)
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

		// Loaded paths carry the host's separator; the profile's keys are always
		// slashes, so only the base name is shared between the two spellings.
		for _, file := range slices.Concat(pkg.GoFiles, pkg.CompiledGoFiles, pkg.IgnoredFiles) {
			index[pkg.PkgPath+"/"+filepath.Base(file)] = file
		}
	}

	return index, nil
}
