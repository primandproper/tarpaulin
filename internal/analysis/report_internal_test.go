package analysis

import (
	"testing"

	"github.com/shoenig/test"
)

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
