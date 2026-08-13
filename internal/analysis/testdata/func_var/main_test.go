package funcvar

import "testing"

func TestDeclared(t *testing.T) {
	if Declared() != "declared" {
		t.Fatal("wrong value")
	}
}
