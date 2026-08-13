package genericmethods

import "testing"

func TestBox(t *testing.T) {
	if NewBox(1).Get() != 1 {
		t.Fatal("wrong value")
	}
}
