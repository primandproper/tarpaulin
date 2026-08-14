package vendored

import "testing"

func TestLocal(t *testing.T) {
	if Local() != "untested" {
		t.Fatal("wrong value")
	}
}
