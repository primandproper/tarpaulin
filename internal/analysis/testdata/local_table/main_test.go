package localtable

import "testing"

func TestTable(t *testing.T) {
	testCases := []struct {
		fn func() string
	}{
		{fn: Tabled},
	}

	for _, tc := range testCases {
		if tc.fn() == "" {
			t.Fatal("empty")
		}
	}
}
