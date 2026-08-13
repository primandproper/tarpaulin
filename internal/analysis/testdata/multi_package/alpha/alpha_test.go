package alpha

import "testing"

func TestTested(t *testing.T) {
	if Tested() != "alpha" {
		t.Fatal("wrong value")
	}
}
