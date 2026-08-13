package subtests

import "testing"

func TestSubtests(t *testing.T) {
	for _, name := range []string{"a", "b"} {
		t.Run(name, func(t *testing.T) {
			if InSubtest() != "subtest" {
				t.Fatal("wrong value")
			}
		})
	}
}
