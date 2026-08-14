package main

import "testing"

func TestReal(t *testing.T) {
	if Real() != "real" {
		t.Fatal("wrong value")
	}
}
