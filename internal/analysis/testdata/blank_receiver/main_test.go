package blankreceiver

import "testing"

func TestBlank(t *testing.T) {
	if (Thing{}).Blank() != "blank" {
		t.Fatal("wrong value")
	}
}
