package switchexpressions

import "testing"

func TestSwitch(t *testing.T) {
	switch Tag() {
	case CaseValue():
		t.Log("matched")
	default:
		t.Fatal("unmatched")
	}
}
