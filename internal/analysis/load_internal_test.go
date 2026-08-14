package analysis

import (
	"go/ast"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
	"golang.org/x/tools/go/packages"
)

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

func TestCollectSourceFiles(t *testing.T) {
	t.Parallel()

	t.Run("keys every file by the package that named it", func(t *testing.T) {
		t.Parallel()

		pkgs := []*packages.Package{
			{
				PkgPath:         "example.com/m/pkg",
				GoFiles:         []string{"/src/pkg/b.go", "/src/pkg/a.go"},
				CompiledGoFiles: []string{"/src/pkg/a.go"},
				// Excluded by build constraints, and kept: a profile produced
				// under different tags still names it.
				IgnoredFiles: []string{"/src/pkg/windows.go"},
			},
			{PkgPath: "example.com/m", GoFiles: []string{"/src/main.go"}},
		}

		test.Eq(t, map[string][]string{
			"example.com/m":     {"/src/main.go"},
			"example.com/m/pkg": {"/src/pkg/a.go", "/src/pkg/b.go", "/src/pkg/windows.go"},
		}, collectSourceFiles(pkgs))
	})

	t.Run("de-duplicates across package variants", func(t *testing.T) {
		t.Parallel()

		// A package and its `pkg [pkg.test]` variant share their non-test
		// sources, and both mean the same file on disk.
		pkgs := []*packages.Package{
			{PkgPath: "example.com/m/pkg", GoFiles: []string{"/src/pkg/a.go"}},
			{PkgPath: "example.com/m/pkg", GoFiles: []string{"/src/pkg/a.go", "/src/pkg/a_test.go"}},
		}

		test.Eq(t, map[string][]string{
			"example.com/m/pkg": {"/src/pkg/a.go", "/src/pkg/a_test.go"},
		}, collectSourceFiles(pkgs))
	})

	t.Run("skips a package with no import path", func(t *testing.T) {
		t.Parallel()

		// Nothing can be keyed on a package that does not say what it is.
		test.MapEmpty(t, collectSourceFiles([]*packages.Package{{GoFiles: []string{"/src/a.go"}}}))
	})

	t.Run("nothing loaded", func(t *testing.T) {
		t.Parallel()

		test.MapEmpty(t, collectSourceFiles(nil))
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
