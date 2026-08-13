package methodvalue

import "testing"

func TestMethodValue(t *testing.T) {
	f := Thing{}.Bound
	if f() != "bound" {
		t.Fatal("wrong value")
	}
}
