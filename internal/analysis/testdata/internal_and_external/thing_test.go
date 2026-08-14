package internalandexternal_test

import (
	"testing"

	internalandexternal "github.com/primandproper/tarpaulin/internal/analysis/testdata/internal_and_external"
)

func TestExported(t *testing.T) {
	if internalandexternal.Exported() != "unexported" {
		t.Fatal("wrong value")
	}
}
