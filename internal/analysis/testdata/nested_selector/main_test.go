package nestedselector

import "testing"

func TestNestedSelector(t *testing.T) {
	x := NewA()
	x.B.C.Method()
}
