package elsebranch

import "testing"

// The 2017 implementation walked e.Body.List and never e.Else, so a reference
// that only appeared in an else branch was invisible to it.
func TestBranches(t *testing.T) {
	if t.Name() == "" {
		Taken()
	} else if t.Name() == "nope" {
		Taken()
	} else {
		Untaken()
	}
}
