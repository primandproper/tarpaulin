package externaltestpackage_test

import (
	"testing"

	externaltestpackage "github.com/primandproper/tarpaulin/internal/analysis/testdata/external_test_package"
)

func TestExported(t *testing.T) {
	if externaltestpackage.Exported() != "exported" {
		t.Fatal("wrong value")
	}
}
