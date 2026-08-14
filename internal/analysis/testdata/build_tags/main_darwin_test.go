//go:build darwin

package buildtags

import "testing"

func TestOnDarwin(t *testing.T) {
	if OnDarwin() != "darwin" {
		t.Fatal("wrong value")
	}
}
