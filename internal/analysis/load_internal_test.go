package analysis

import (
	"go/ast"
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
