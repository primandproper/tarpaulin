package testmain

import (
	"os"
	"testing"
)

func TestMain(m *testing.M) {
	Harnessed()
	os.Exit(m.Run())
}

func TestTested(t *testing.T) {
	if Tested() != "tested" {
		t.Fatal("wrong value")
	}
}
