package chainedcalls

import "testing"

func TestChain(t *testing.T) {
	if A().B().C() != "c" {
		t.Fatal("wrong value")
	}
}
