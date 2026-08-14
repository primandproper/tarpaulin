package brokentest

import "testing"

func TestFine(t *testing.T) {
	if Fine() != undefinedIdentifier {
		t.Fatal("wrong value")
	}
}
