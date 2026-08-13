package interfacetwoimpls

import "testing"

func TestDispatch(t *testing.T) {
	var d Doer = First{}

	if d.Do() != "first" {
		t.Fatal("wrong value")
	}

	if Dispatch(Second{}) != "second" {
		t.Fatal("wrong value")
	}
}
