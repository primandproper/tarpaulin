package helperonly

import "testing"

func helper(t *testing.T) string {
	t.Helper()

	return ViaHelper()
}

func TestDirect(t *testing.T) {
	if Direct() != "direct" {
		t.Fatal("wrong value")
	}

	if helper(t) == "" {
		t.Fatal("empty")
	}
}
