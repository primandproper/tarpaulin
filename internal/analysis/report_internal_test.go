package analysis

import (
	"testing"

	"github.com/shoenig/test"
)

func TestScore(t *testing.T) {
	t.Parallel()

	// The one place the grading arithmetic lives, so a package and the report
	// it belongs to can never disagree about what 2 of 3 is.
	test.Eq(t, 66, score(2, 3))
	test.Eq(t, 0, score(0, 4))
	test.Eq(t, 100, score(4, 4))
	// Nothing to grade is perfect, not a division by zero.
	test.Eq(t, 100, score(0, 0))
}

func TestSortFunctions(t *testing.T) {
	t.Parallel()

	// The 2017 implementation ranged a map straight into its template, so its
	// output order was whatever Go's map iteration felt like that run.
	functions := []Function{
		{File: "beta.go", Name: "Second", Line: 7},
		{File: "alpha.go", Name: "Zebra", Line: 9},
		{File: "beta.go", Name: "First", Line: 3},
		{File: "alpha.go", Name: "Beta", Line: 3},
		{File: "alpha.go", Name: "Alpha", Line: 3},
	}

	sortFunctions(functions)

	test.Eq(t, []Function{
		{File: "alpha.go", Name: "Alpha", Line: 3},
		{File: "alpha.go", Name: "Beta", Line: 3},
		{File: "alpha.go", Name: "Zebra", Line: 9},
		{File: "beta.go", Name: "First", Line: 3},
		{File: "beta.go", Name: "Second", Line: 7},
	}, functions)
}
