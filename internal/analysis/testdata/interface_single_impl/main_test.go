package interfacesingleimpl

import "testing"

func TestDoer(t *testing.T) {
	d := NewDoer()
	if d.Do() != "impl" {
		t.Fatal("wrong value")
	}
}
