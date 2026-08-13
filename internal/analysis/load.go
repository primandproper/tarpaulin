package analysis

import (
	"context"
	"fmt"
	"go/token"
	"path/filepath"
	"slices"
	"strings"

	platformerrors "github.com/primandproper/platform-go/v10/errors"

	"golang.org/x/tools/go/packages"
)

// loadMode is the minimum go/packages needs to answer "what does this
// identifier resolve to?" — the question the whole analysis is built on.
const loadMode = packages.NeedName |
	packages.NeedFiles |
	packages.NeedCompiledGoFiles |
	packages.NeedSyntax |
	packages.NeedTypes |
	packages.NeedTypesInfo

// syntheticTestSuffix marks the test binary's main package, which the toolchain
// synthesizes and which references every TestXxx in the package. Analyzing it
// would credit every test function to itself and poison the reference set
// (PRD 4.1).
const syntheticTestSuffix = ".test"

// maxReportedDiagnostics bounds how much of a broken build we quote back. A
// package that does not compile usually fails in bulk, and a screenful is
// enough to act on.
const maxReportedDiagnostics = 10

// ErrNoGoFiles is returned when the requested patterns match no Go source at
// all — an empty directory reads as a perfect score otherwise, which would be a
// lie.
var ErrNoGoFiles = platformerrors.New("no Go files found")

// DiagnosticError reports that the analyzed source could not be loaded and
// type-checked. The old implementation parsed with parser.AllErrors and happily
// analyzed broken source; go/types refuses, which is an improvement, so long as
// the refusal is legible.
type DiagnosticError struct {
	Diagnostics []string
}

// Error implements the error interface.
func (e *DiagnosticError) Error() string {
	var sb strings.Builder

	sb.WriteString("the requested packages could not be analyzed:")

	shown := e.Diagnostics
	if len(shown) > maxReportedDiagnostics {
		shown = shown[:maxReportedDiagnostics]
	}

	for _, diagnostic := range shown {
		sb.WriteString("\n\t")
		sb.WriteString(diagnostic)
	}

	if remaining := len(e.Diagnostics) - len(shown); remaining > 0 {
		fmt.Fprintf(&sb, "\n\t(and %d more)", remaining)
	}

	return sb.String()
}

// loadPackages loads every variant of the requested packages, dropping the
// synthesized test binary. It returns an error when anything failed to compile:
// a reference set gathered from source that does not type-check is not
// trustworthy, and silently grading it would be worse than refusing.
func loadPackages(ctx context.Context, dir string, patterns []string) ([]*packages.Package, *token.FileSet, error) {
	fset := token.NewFileSet()

	cfg := &packages.Config{
		Context: ctx,
		Mode:    loadMode,
		Dir:     dir,
		Fset:    fset,
		Tests:   true,
	}

	loaded, err := packages.Load(cfg, patterns...)
	if err != nil {
		return nil, nil, platformerrors.Wrap(err, "loading packages")
	}

	kept := make([]*packages.Package, 0, len(loaded))

	for _, pkg := range loaded {
		if strings.HasSuffix(pkg.PkgPath, syntheticTestSuffix) && pkg.Name == "main" {
			continue
		}

		kept = append(kept, pkg)
	}

	if diagnostics := collectDiagnostics(dir, kept); len(diagnostics) > 0 {
		return nil, nil, &DiagnosticError{Diagnostics: diagnostics}
	}

	if !anySyntax(kept) {
		return nil, nil, platformerrors.Wrapf(ErrNoGoFiles, "analyzing %s", dir)
	}

	return kept, fset, nil
}

// collectDiagnostics renders every load, parse, and type error as a sorted,
// de-duplicated list with paths relative to the analyzed directory.
//
// The go command reports the same failure twice: once as its own stdout, quoted
// wholesale into a ListError, and once as the parse or type error that caused
// it. The precise ones are kept when there are any, because "main.go:4:9:
// undefined: x" is what the user can act on.
func collectDiagnostics(dir string, pkgs []*packages.Package) []string {
	seen := make(map[string]struct{})
	precise := make([]string, 0)
	fallback := make([]string, 0)

	for _, pkg := range pkgs {
		for i := range pkg.Errors {
			pkgErr := &pkg.Errors[i]

			rendered := renderPackageError(dir, pkgErr)
			if _, ok := seen[rendered]; ok {
				continue
			}

			seen[rendered] = struct{}{}

			if pkgErr.Kind == packages.ListError {
				fallback = append(fallback, rendered)

				continue
			}

			precise = append(precise, rendered)
		}
	}

	diagnostics := precise
	if len(diagnostics) == 0 {
		diagnostics = fallback
	}

	slices.Sort(diagnostics)

	return diagnostics
}

// renderPackageError formats one packages.Error, shortening its position to a
// path relative to the analyzed directory when it points inside it.
func renderPackageError(dir string, pkgErr *packages.Error) string {
	if pkgErr.Pos == "" {
		return pkgErr.Msg
	}

	// packages.Error.Pos is "file:line:col" (or "file:line"), already formatted.
	parts := strings.SplitN(pkgErr.Pos, ":", 2)
	position := relativePath(dir, parts[0])

	if len(parts) == 2 {
		position += ":" + parts[1]
	}

	return position + ": " + pkgErr.Msg
}

// anySyntax reports whether anything was actually parsed. go/packages happily
// returns a package for a directory with no Go files in it.
func anySyntax(pkgs []*packages.Package) bool {
	return slices.ContainsFunc(pkgs, func(pkg *packages.Package) bool {
		return len(pkg.Syntax) > 0
	})
}

// relativePath renders path relative to dir when it lives underneath it, and
// unchanged otherwise. Both are resolved to absolute paths first so that
// symlinked temp directories (macOS /tmp) do not defeat the comparison.
func relativePath(dir, path string) string {
	absDir, err := filepath.Abs(dir)
	if err != nil {
		return path
	}

	absPath, err := filepath.Abs(path)
	if err != nil {
		return path
	}

	rel, err := filepath.Rel(absDir, absPath)
	if err != nil || strings.HasPrefix(rel, "..") {
		return path
	}

	return filepath.ToSlash(rel)
}
