package ifinit

import "testing"

func TestIfInit(t *testing.T) {
	if v := Value(); v == "" {
		t.Fatal("empty")
	}
}
