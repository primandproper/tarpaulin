package rangeexpression

import "testing"

func TestRangeExpression(t *testing.T) {
	for range Items() {
		t.Log("item")
	}
}
