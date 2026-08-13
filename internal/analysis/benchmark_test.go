package analysis_test

import (
	"path/filepath"
	"testing"

	"github.com/primandproper/tarpaulin/internal/analysis"

	"github.com/shoenig/test/must"
)

// moduleRootDir is this repository's root, relative to this package. It is the
// only realistic target available to a benchmark that has to run anywhere: a few
// thousand lines across seven packages, with tests, generics, and interfaces in
// it.
const moduleRootDir = "../.."

// BenchmarkAnalyze measures what an analysis costs, because PRD 3.6 spends a
// budget it never measured: the interface heuristic is justified there by an
// assumption that a run finishes in a couple hundred milliseconds inside CI, and
// the RTA callgraph is deferred on the same assumption. Numbers before either is
// revisited.
//
// Almost all of the time is go/packages: `go list` shelling out, then parsing
// and type-checking every variant of every package. Reference collection is one
// pass over syntax that is already in memory. A run is therefore dominated by
// how much source is loaded, not by how strictly it is graded, which is why
// there is no per-strictness case here — the dial is a map lookup at the end.
func BenchmarkAnalyze(b *testing.B) {
	cases := map[string]analysis.Config{
		// The floor: one four-function package, so the number is process
		// overhead plus a single package load.
		"one small package": {Dir: filepath.Join(corpusDir, "simple")},
		// The heuristic under measurement: an interface, its implementations,
		// and the sole-implementer search PRD 3.6 describes.
		"interface dispatch": {Dir: filepath.Join(corpusDir, "interface_single_impl")},
		// The realistic CI shape: this module, tests and all.
		"this module": {Dir: moduleRootDir, Patterns: []string{"./internal/...", "./version/...", "./cmd/..."}},
	}

	for name := range cases {
		cfg := cases[name]

		b.Run(name, func(b *testing.B) {
			// Prove the target loads before timing it: a benchmark quietly
			// measuring an error path would be worse than no benchmark.
			_, err := analysis.Analyze(b.Context(), cfg)
			must.NoError(b, err)

			b.ResetTimer()

			for b.Loop() {
				if _, err = analysis.Analyze(b.Context(), cfg); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
