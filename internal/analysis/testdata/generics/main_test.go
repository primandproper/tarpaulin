package generics

import (
	"strconv"
	"testing"
)

func TestMap(t *testing.T) {
	// explicit instantiation
	got := Map[int, string]([]int{1, 2}, strconv.Itoa)
	if len(got) != 2 {
		t.Fatal("wrong length")
	}
}

func TestIdentity(t *testing.T) {
	// inferred instantiation
	if Identity(42) != 42 {
		t.Fatal("not identity")
	}
}
