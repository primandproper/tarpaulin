package gamma

import "testing"

func TestTested(t *testing.T) {
	if Tested() != "gamma" {
		t.Fatal("wrong value")
	}
}
