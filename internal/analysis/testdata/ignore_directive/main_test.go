package ignoredirective

import "testing"

func TestFine(t *testing.T) {
	if Fine() != "fine" {
		t.Fatal("wrong value")
	}
}
