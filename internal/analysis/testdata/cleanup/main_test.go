package cleanup

import "testing"

func TestCleanup(t *testing.T) {
	t.Cleanup(func() {
		Torn()
	})
}
