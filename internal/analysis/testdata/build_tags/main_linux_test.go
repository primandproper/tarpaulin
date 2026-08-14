//go:build linux

package buildtags

import "testing"

func TestOnLinux(t *testing.T) {
	if OnLinux() != "linux" {
		t.Fatal("wrong value")
	}
}
