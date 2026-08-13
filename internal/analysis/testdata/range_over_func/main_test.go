package rangeoverfunc

import "testing"

func TestSeq(t *testing.T) {
	total := 0
	for x := range Seq {
		total += x
	}

	if total != 3 {
		t.Fatalf("got %d", total)
	}
}
