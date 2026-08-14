package crossfile

import "testing"

func TestLocal(t *testing.T) {
	if Local() != "local" {
		t.Fatal("wrong value")
	}
}

func TestCross(t *testing.T) {
	if Cross() != "cross" {
		t.Fatal("wrong value")
	}
}
