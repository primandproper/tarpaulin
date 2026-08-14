package internalandexternal

import "testing"

func TestUnexported(t *testing.T) {
	if unexported() != "unexported" {
		t.Fatal("wrong value")
	}
}
