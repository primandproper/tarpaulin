package embeddedpromotion

import "testing"

func TestPromotedMethod(t *testing.T) {
	if (Outer{}).Shared() != "shared" {
		t.Fatal("wrong value")
	}
}
