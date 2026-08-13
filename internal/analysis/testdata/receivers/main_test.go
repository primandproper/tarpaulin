package receivers

import "testing"

func TestPings(t *testing.T) {
	if (Val{}).Ping() != "val" {
		t.Fatal("wrong value")
	}

	if (&Ptr{}).Ping() != "ptr" {
		t.Fatal("wrong value")
	}
}
