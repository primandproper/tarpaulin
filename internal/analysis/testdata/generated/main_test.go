package generated

import "testing"

func TestHandwritten(t *testing.T) {
	if Handwritten() != "handwritten" {
		t.Fatal("wrong value")
	}
}
