package analysis

import (
	"testing"

	"github.com/shoenig/test/must"
)

// benchmarkTargets are the same shapes BenchmarkAnalyze uses, named the same
// way, so the two sets of numbers can be read side by side.
var benchmarkTargets = map[string][]string{
	"one small package":  nil,
	"interface dispatch": nil,
	"this module":        {"./internal/...", "./version/...", "./cmd/..."},
}

// benchmarkDirs pairs each target with the directory it loads from.
var benchmarkDirs = map[string]string{
	"one small package":  "testdata/simple",
	"interface dispatch": "testdata/interface_single_impl",
	"this module":        "../..",
}

// BenchmarkLoadPackages measures the half of an analysis that belongs to the go
// toolchain: `go list`, parsing, and type-checking every variant of every
// package. Subtracting it from BenchmarkAnalyze leaves what this package
// actually spends, which is the number that matters when the RTA callgraph is
// revisited — it has to be paid for out of that share, not out of the total.
func BenchmarkLoadPackages(b *testing.B) {
	for name, patterns := range benchmarkTargets {
		b.Run(name, func(b *testing.B) {
			dir := benchmarkDirs[name]

			for b.Loop() {
				_, _, err := loadPackages(b.Context(), dir, effectivePatterns(patterns))
				if err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// BenchmarkCollect measures the two passes this package owns, over source the
// toolchain has already parsed and type-checked: what is declared, and what the
// tests refer to.
func BenchmarkCollect(b *testing.B) {
	for name, patterns := range benchmarkTargets {
		b.Run(name, func(b *testing.B) {
			pkgs, fset, err := loadPackages(b.Context(), benchmarkDirs[name], effectivePatterns(patterns))
			must.NoError(b, err)

			b.ResetTimer()

			for b.Loop() {
				collectDeclarations(fset, pkgs, nil)
				collectReferences(fset, pkgs)
			}
		})
	}
}

// effectivePatterns applies Analyze's default so the benchmarks load exactly
// what it would.
func effectivePatterns(patterns []string) []string {
	if len(patterns) == 0 {
		return []string{defaultPattern}
	}

	return patterns
}
