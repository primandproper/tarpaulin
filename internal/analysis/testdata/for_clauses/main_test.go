package forclauses

import "testing"

func TestLoopClauses(t *testing.T) {
	for i := Init(); Cond(i); i = Post(i) {
		_ = i
	}
}
