package analysis_test

import (
	"fmt"
	"go/ast"
	"go/build"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/primandproper/tarpaulin/internal/analysis"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// corpusDir holds one directory per semantic case the analyzer has to get right.
const corpusDir = "testdata"

// wantDirective marks a fixture declaration with the expectation the corpus
// asserts against it. Expectations live next to the code they describe rather
// than in a golden file so they survive line drift and read as documentation:
//
//	//tarp:want excluded                     — never counted at all
//	//tarp:want untested=file,package        — reported at those strictness levels
//
// A declaration with no marker is expected to be counted and tested at every
// level.
const wantDirective = "//tarp:want"

// errorFixtures are exercised by TestAnalyzeDiagnostics instead: they exist to
// prove the tool reports broken input cleanly, so they have no grade.
var errorFixtures = map[string]bool{
	"broken_package": true,
	"broken_test":    true,
	"no_go_files":    true,
	"unparseable":    true,
}

// allStrictness is every position on the dial, so each fixture is asserted at
// all three — the semantics live in the places where they differ.
var allStrictness = []analysis.Strictness{
	analysis.StrictnessFile,
	analysis.StrictnessPackage,
	analysis.StrictnessAny,
}

func TestCorpus(t *testing.T) {
	t.Parallel()

	entries, err := os.ReadDir(corpusDir)
	must.NoError(t, err)

	for _, entry := range entries {
		if !entry.IsDir() || errorFixtures[entry.Name()] {
			continue
		}

		t.Run(entry.Name(), func(t *testing.T) {
			t.Parallel()

			dir := filepath.Join(corpusDir, entry.Name())
			expected := readExpectations(t, dir)

			for _, strictness := range allStrictness {
				t.Run(strictness.String(), func(t *testing.T) {
					t.Parallel()

					report, analyzeErr := analysis.Analyze(t.Context(), analysis.Config{
						Dir:        dir,
						Strictness: strictness,
					})
					must.NoError(t, analyzeErr)

					test.Eq(t, expected.declared, identities(report.Functions),
						test.Sprintf("functions accounted for in %s", dir))
					test.Eq(t, expected.untested[strictness], identities(report.Untested()),
						test.Sprintf("functions reported untested in %s at --strictness=%s", dir, strictness))
					test.Eq(t, expectedScore(len(expected.declared), len(expected.untested[strictness])), report.Score(),
						test.Sprintf("score for %s at --strictness=%s", dir, strictness))
				})
			}
		})
	}
}

// expectations is what a fixture's markers say the report should contain.
type expectations struct {
	untested map[analysis.Strictness][]string
	declared []string
}

// readExpectations derives a fixture's expected report from its own source: the
// declarations are read syntactically, and the markers say which of them are
// exempt or expected to be reported.
func readExpectations(t *testing.T, dir string) expectations {
	t.Helper()

	got := expectations{
		untested: map[analysis.Strictness][]string{},
	}

	for _, strictness := range allStrictness {
		got.untested[strictness] = []string{}
	}

	fset := token.NewFileSet()

	must.NoError(t, filepath.WalkDir(dir, func(path string, entry os.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case entry.IsDir():
			// Vendored code is another module's problem, and the tool must never
			// analyze it.
			if entry.Name() == "vendor" {
				return filepath.SkipDir
			}

			return nil
		case !strings.HasSuffix(path, ".go") || strings.HasSuffix(path, "_test.go"):
			return nil
		}

		// Build-constrained files participate only where they would compile, so
		// the corpus asserts the same thing on every platform.
		matches, err := build.Default.MatchFile(filepath.Dir(path), filepath.Base(path))
		if err != nil || !matches {
			return err
		}

		file, err := parser.ParseFile(fset, path, nil, parser.ParseComments)
		if err != nil {
			return err
		}

		readFileExpectations(fset, dir, file, &got)

		return nil
	}))

	slices.Sort(got.declared)

	for _, names := range got.untested {
		slices.Sort(names)
	}

	return got
}

// readFileExpectations records the expectations declared by one fixture file.
func readFileExpectations(fset *token.FileSet, dir string, file *ast.File, into *expectations) {
	for _, decl := range file.Decls {
		funcDecl, ok := decl.(*ast.FuncDecl)
		if !ok {
			continue
		}

		position := fset.Position(funcDecl.Pos())
		relative, err := filepath.Rel(dir, position.Filename)
		if err != nil {
			relative = position.Filename
		}

		identity := fmt.Sprintf("%s:%d %s", filepath.ToSlash(relative), position.Line, astFuncName(funcDecl))

		excluded, untestedAt := parseWant(funcDecl.Doc)
		if excluded {
			continue
		}

		into.declared = append(into.declared, identity)

		for _, strictness := range untestedAt {
			into.untested[strictness] = append(into.untested[strictness], identity)
		}
	}
}

// parseWant reads the fixture's expectation marker off a declaration's doc
// comment.
func parseWant(doc *ast.CommentGroup) (bool, []analysis.Strictness) {
	if doc == nil {
		return false, nil
	}

	for _, comment := range doc.List {
		if !strings.HasPrefix(comment.Text, wantDirective) {
			continue
		}

		body := strings.TrimSpace(strings.TrimPrefix(comment.Text, wantDirective))
		if body == "excluded" {
			return true, nil
		}

		levels := make([]analysis.Strictness, 0, len(allStrictness))

		for name := range strings.SplitSeq(strings.TrimPrefix(body, "untested="), ",") {
			strictness, err := analysis.ParseStrictness(strings.TrimSpace(name))
			if err != nil {
				panic(fmt.Sprintf("malformed corpus marker %q: %v", comment.Text, err))
			}

			levels = append(levels, strictness)
		}

		return false, levels
	}

	return false, nil
}

// astFuncName renders a declaration's name the way the analyzer does, but from
// syntax alone — an independent second opinion on the analyzer's type-driven
// rendering.
func astFuncName(decl *ast.FuncDecl) string {
	if decl.Recv == nil || len(decl.Recv.List) == 0 {
		return decl.Name.Name
	}

	recv := decl.Recv.List[0].Type

	pointer := false
	if star, ok := recv.(*ast.StarExpr); ok {
		pointer = true
		recv = star.X
	}

	name := typeExprName(recv)
	if pointer {
		return "(*" + name + ")." + decl.Name.Name
	}

	return name + "." + decl.Name.Name
}

// typeExprName renders a receiver type expression, including the type
// parameters of a generic receiver such as Box[T].
func typeExprName(expr ast.Expr) string {
	switch typed := expr.(type) {
	case *ast.Ident:
		return typed.Name
	case *ast.IndexExpr:
		return typeExprName(typed.X) + "[" + typeExprName(typed.Index) + "]"
	case *ast.IndexListExpr:
		params := make([]string, 0, len(typed.Indices))
		for _, index := range typed.Indices {
			params = append(params, typeExprName(index))
		}

		return typeExprName(typed.X) + "[" + strings.Join(params, ", ") + "]"
	default:
		return fmt.Sprintf("%T", expr)
	}
}

// identities renders functions in the corpus's "file:line name" format, so a
// failure names the declaration rather than just a count.
func identities(functions []analysis.Function) []string {
	names := make([]string, 0, len(functions))
	for i := range functions {
		names = append(names, fmt.Sprintf("%s:%d %s", functions[i].File, functions[i].Line, functions[i].Name))
	}

	slices.Sort(names)

	return names
}

// expectedScore mirrors Report.Score for the corpus's own arithmetic.
func expectedScore(declared, untested int) int {
	if declared == 0 {
		return 100
	}

	return (declared - untested) * 100 / declared
}
