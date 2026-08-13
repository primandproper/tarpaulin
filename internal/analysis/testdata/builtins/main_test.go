package builtins

import "testing"

// min, max and clear resolve to *types.Builtin rather than *types.Func, so they
// must never be reported — nor may they crash the reference walk.
func TestClamp(t *testing.T) {
	seen := map[int]bool{1: true}
	clear(seen)

	if Clamp(0, 10, max(min(20, 30), 5)) != 10 {
		t.Fatal("wrong clamp")
	}
}
