package buildtags

import "testing"

func TestAlways(t *testing.T) {
	if Always() != "always" {
		t.Fatal("wrong value")
	}
}
