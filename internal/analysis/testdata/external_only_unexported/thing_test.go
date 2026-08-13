package externalonlyunexported_test

import (
	"testing"

	externalonlyunexported "github.com/primandproper/tarpaulin/internal/analysis/testdata/external_only_unexported"
)

// TestExported reaches unexported the only way this file can: through Exported,
// which calls it. That is precisely the execution-without-assertion the tool
// refuses to count as a test, so unexported is still reported — writing
// `externalonlyunexported.unexported()` here would not compile.
func TestExported(t *testing.T) {
	if externalonlyunexported.Exported() != "unexported" {
		t.Fatal("wrong value")
	}
}
