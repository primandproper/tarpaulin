package methodexpression

import "testing"

func TestMethodExpression(t *testing.T) {
	g := Thing.Expressed
	if g(Thing{}) != "expressed" {
		t.Fatal("wrong value")
	}
}
