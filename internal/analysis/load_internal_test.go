package analysis

import (
	"errors"
	"go/ast"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
	"golang.org/x/tools/go/packages"
)

func TestLoadPackages(t *testing.T) {
	t.Parallel()

	simple := filepath.Join("testdata", "simple")

	t.Run("keeps every real variant and drops the synthesized one", func(t *testing.T) {
		t.Parallel()

		pkgs, fset, err := loadPackages(t.Context(), simple, []string{defaultPattern})
		must.NoError(t, err)
		must.NotNil(t, fset)

		// The source variant and the test variant, both under the same import
		// path. The <pkg>.test main the toolchain synthesizes references every
		// TestXxx in the package, so leaving it in would credit every test
		// function to itself.
		must.SliceLen(t, 2, pkgs)

		for _, pkg := range pkgs {
			test.StrHasSuffix(t, "analysis/testdata/simple", pkg.PkgPath)
			test.SliceNotEmpty(t, pkg.Syntax, test.Sprintf("%s was loaded without syntax", pkg.PkgPath))
		}
	})

	t.Run("refuses a directory with no Go files in it", func(t *testing.T) {
		t.Parallel()

		// go/packages happily returns a package for one, which would read as a
		// perfect score.
		_, _, err := loadPackages(t.Context(), filepath.Join("testdata", "no_go_files"), []string{defaultPattern})

		must.ErrorIs(t, err, ErrNoGoFiles)
	})

	t.Run("refuses source that does not type check", func(t *testing.T) {
		t.Parallel()

		// A reference set gathered from source the type checker rejected is not
		// trustworthy, and grading it anyway would be worse than refusing.
		_, _, err := loadPackages(t.Context(), filepath.Join("testdata", "broken_package"), []string{defaultPattern})

		diagnostic := new(DiagnosticError)
		must.True(t, errors.As(err, &diagnostic), must.Sprintf("expected a diagnostic error, got %v", err))
		test.SliceNotEmpty(t, diagnostic.Diagnostics)
	})

	t.Run("names the missing go.mod rather than quoting the go command", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()
		must.NoError(t, os.WriteFile(filepath.Join(dir, "a.go"), []byte("package a\n\nfunc A() {}\n"), 0o600))

		_, _, err := loadPackages(t.Context(), dir, []string{defaultPattern})

		must.ErrorIs(t, err, ErrNotInModule)
	})
}

func TestDiagnosticErrorError(t *testing.T) {
	t.Parallel()

	t.Run("quotes what went wrong", func(t *testing.T) {
		t.Parallel()

		err := &DiagnosticError{Diagnostics: []string{"main.go:4:9: undefined: x"}}

		test.Eq(t, "the requested packages could not be analyzed:\n\tmain.go:4:9: undefined: x", err.Error())
	})

	t.Run("truncates a wholesale failure", func(t *testing.T) {
		t.Parallel()

		// A package that does not compile usually fails in bulk; a screenful is
		// enough to act on.
		diagnostics := make([]string, 0, maxReportedDiagnostics+5)
		for range maxReportedDiagnostics + 5 {
			diagnostics = append(diagnostics, "main.go:1:1: broken")
		}

		message := (&DiagnosticError{Diagnostics: diagnostics}).Error()

		test.Eq(t, maxReportedDiagnostics, strings.Count(message, "main.go:1:1: broken"))
		test.StrContains(t, message, "(and 5 more)")
	})
}

func TestCollectDiagnostics(t *testing.T) {
	t.Parallel()

	t.Run("prefers precise errors over the go command's own output", func(t *testing.T) {
		t.Parallel()

		// The go command reports the same failure twice: once as its stdout
		// quoted wholesale, and once as the type error that caused it.
		pkgs := []*packages.Package{{
			Errors: []packages.Error{
				{Msg: "# example\n./main.go:4:9: undefined: x", Kind: packages.ListError},
				{Pos: "/src/main.go:4:9", Msg: "undefined: x", Kind: packages.TypeError},
			},
		}}

		test.Eq(t, []string{"main.go:4:9: undefined: x"}, collectDiagnostics("/src", pkgs))
	})

	t.Run("falls back to the go command when there is nothing better", func(t *testing.T) {
		t.Parallel()

		pkgs := []*packages.Package{{
			Errors: []packages.Error{{Msg: "no required module provides package example.com/nope", Kind: packages.ListError}},
		}}

		test.Eq(t, []string{"no required module provides package example.com/nope"}, collectDiagnostics("/src", pkgs))
	})

	t.Run("de-duplicates across package variants", func(t *testing.T) {
		t.Parallel()

		// The same file is compiled into the source variant and both test
		// variants, so its errors arrive three times.
		errs := []packages.Error{{Pos: "/src/main.go:4:9", Msg: "undefined: x", Kind: packages.TypeError}}
		pkgs := []*packages.Package{{Errors: errs}, {Errors: errs}, {Errors: errs}}

		test.SliceLen(t, 1, collectDiagnostics("/src", pkgs))
	})

	t.Run("reports nothing for a clean load", func(t *testing.T) {
		t.Parallel()

		test.SliceEmpty(t, collectDiagnostics("/src", []*packages.Package{{}}))
	})
}

func TestCheckModule(t *testing.T) {
	t.Parallel()

	t.Run("names the missing go.mod", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()

		err := checkModule(dir)

		must.ErrorIs(t, err, ErrNotInModule)
		test.StrContains(t, err.Error(), dir)
	})

	t.Run("has no opinion inside a module", func(t *testing.T) {
		t.Parallel()

		// This test file lives in one, so whatever else went wrong, it was not
		// this.
		test.NoError(t, checkModule("."))
	})
}

func TestModuleRoot(t *testing.T) {
	t.Parallel()

	t.Run("finds the nearest go.mod above a directory", func(t *testing.T) {
		t.Parallel()

		root := t.TempDir()
		must.NoError(t, os.WriteFile(filepath.Join(root, goModFile), []byte("module example.com/m\n"), 0o600))

		nested := filepath.Join(root, "a", "b")
		must.NoError(t, os.MkdirAll(nested, 0o750))

		test.Eq(t, root, moduleRoot(nested))
	})

	t.Run("walks to the filesystem root and gives up", func(t *testing.T) {
		t.Parallel()

		test.Eq(t, "", moduleRoot(t.TempDir()))
	})

	t.Run("ignores a go.mod directory", func(t *testing.T) {
		t.Parallel()

		// A directory named go.mod marks nothing; only a file does.
		dir := t.TempDir()
		must.NoError(t, os.Mkdir(filepath.Join(dir, goModFile), 0o750))

		test.Eq(t, "", moduleRoot(dir))
	})
}

func TestRenderPackageError(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		err      *packages.Error
		expected string
	}{
		"with a full position": {
			err:      &packages.Error{Pos: "/src/main.go:4:9", Msg: "undefined: x"},
			expected: "main.go:4:9: undefined: x",
		},
		"with a line only": {
			err:      &packages.Error{Pos: "/src/main.go:4", Msg: "undefined: x"},
			expected: "main.go:4: undefined: x",
		},
		"with no position": {
			err:      &packages.Error{Msg: "build constraints exclude all Go files"},
			expected: "build constraints exclude all Go files",
		},
		"outside the analyzed directory": {
			err:      &packages.Error{Pos: "/elsewhere/main.go:4:9", Msg: "undefined: x"},
			expected: "/elsewhere/main.go:4:9: undefined: x",
		},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			test.Eq(t, testCase.expected, renderPackageError("/src", testCase.err))
		})
	}
}

func TestAnySyntax(t *testing.T) {
	t.Parallel()

	// go/packages happily returns a package for a directory with no Go files in
	// it, which would otherwise read as a perfect score.
	test.False(t, anySyntax([]*packages.Package{{}}))
	test.True(t, anySyntax([]*packages.Package{{}, {Syntax: []*ast.File{{}}}}))
}

func TestAbsolutePath(t *testing.T) {
	t.Parallel()

	t.Run("leaves an absolute path alone", func(t *testing.T) {
		t.Parallel()

		dir := t.TempDir()

		test.Eq(t, dir, absolutePath(dir))
	})

	t.Run("resolves a relative path against the working directory", func(t *testing.T) {
		t.Parallel()

		expected, err := filepath.Abs("testdata")
		must.NoError(t, err)

		test.Eq(t, expected, absolutePath("testdata"))
	})
}

func TestRelativePath(t *testing.T) {
	t.Parallel()

	t.Run("shortens paths inside the analyzed directory", func(t *testing.T) {
		t.Parallel()

		test.Eq(t, "pkg/main.go", relativePath("/src", "/src/pkg/main.go"))
	})

	t.Run("leaves paths outside it alone", func(t *testing.T) {
		t.Parallel()

		test.Eq(t, "/elsewhere/main.go", relativePath("/src", "/elsewhere/main.go"))
	})

	t.Run("resolves a relative directory against the working directory", func(t *testing.T) {
		t.Parallel()

		// Report paths are relative so they read the same on every machine,
		// which means the comparison has to survive a relative Dir.
		absolute, err := filepath.Abs(filepath.Join("testdata", "simple", "main.go"))
		must.NoError(t, err)

		test.Eq(t, "simple/main.go", relativePath("testdata", absolute))
	})
}
