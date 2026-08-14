package packageleveltable

import "testing"

var testCases = []struct {
	fn func() string
}{
	{fn: Tabled},
}

var unusedCases = []struct {
	fn func() string
}{
	{fn: Orphan},
}

func TestTable(t *testing.T) {
	for _, tc := range testCases {
		if tc.fn() == "" {
			t.Fatal("empty")
		}
	}
}
