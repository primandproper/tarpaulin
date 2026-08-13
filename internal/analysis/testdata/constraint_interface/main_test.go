package constraintinterface

import "testing"

func TestJoin(t *testing.T) {
	if Join([]Thing{{}, {}}) != "thingthing" {
		t.Fatal("wrong join")
	}
}

func TestThingLabel(t *testing.T) {
	if (Thing{}).Label() != "thing" {
		t.Fatal("wrong label")
	}
}
