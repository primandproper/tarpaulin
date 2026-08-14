package deferredfunctions

import "testing"

func TestX(t *testing.T) {
	defer func() { X() }()
}
