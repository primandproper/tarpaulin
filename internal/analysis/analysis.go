// Package analysis finds functions that carry no direct unit test.
//
// `go test -cover` measures statement coverage, which cannot distinguish a
// function that was tested from one that was merely executed on the way to
// somebody else's assertion. This package asks the stricter question: for each
// function declared in a package, does a TestXxx body actually mention it?
//
// The mechanism is deliberately one question, asked once. For every identifier
// in a test body it consults go/types and keeps the ones that resolve to a
// function declared in the package under test. It never asks "is this a call?",
// which is an unbounded syntactic question — and so method values, method
// expressions, functions passed as arguments, deferred closures, range-over-func
// iterators, and generic instantiations all fall out for free rather than each
// needing a case of its own.
package analysis

import (
	"context"
	"fmt"
)

// defaultPattern analyzes the current directory and everything under it.
const defaultPattern = "./..."

// Config selects what to analyze and how strictly to grade it.
type Config struct {
	// Dir is the directory the patterns are resolved against. Empty means the
	// current working directory.
	Dir string
	// Patterns are go/packages patterns such as "./..." or a package path.
	// Empty means "./...".
	Patterns []string
	// Strictness is how close a reference must be to count. The zero value is
	// the strictest setting, which is the one worth defaulting to.
	Strictness Strictness
}

// Analyze loads the requested packages and reports which of their functions
// have no direct unit test.
func Analyze(ctx context.Context, cfg Config) (*Report, error) {
	dir := cfg.Dir
	if dir == "" {
		dir = "."
	}

	patterns := cfg.Patterns
	if len(patterns) == 0 {
		patterns = []string{defaultPattern}
	}

	pkgs, fset, err := loadPackages(ctx, dir, patterns)
	if err != nil {
		return nil, err
	}

	declarations, warnings := collectDeclarations(fset, pkgs)
	references := collectReferences(fset, pkgs)

	report := &Report{
		Root:       moduleRoot(absolutePath(dir)),
		Strictness: cfg.Strictness,
		Functions:  make([]Function, 0, len(declarations)),
		Warnings:   renderWarnings(dir, warnings),
	}

	for _, decl := range declarations {
		report.Functions = append(report.Functions, Function{
			Package:     decl.pkgName,
			PackagePath: decl.pkgPath,
			File:        relativePath(dir, decl.key.Filename),
			Path:        decl.key.Filename,
			Name:        decl.name,
			Line:        decl.key.Line,
			EndLine:     decl.endLine,
			Tested:      references.satisfies(decl, cfg.Strictness),
		})
	}

	sortFunctions(report.Functions)

	return report, nil
}

// renderWarnings formats warnings with paths relative to the analyzed
// directory.
func renderWarnings(dir string, warnings []warning) []string {
	rendered := make([]string, 0, len(warnings))

	for i := range warnings {
		rendered = append(rendered, fmt.Sprintf(
			"%s:%d: %s carries a bare %s with no reason and was not exempted",
			relativePath(dir, warnings[i].file), warnings[i].line, warnings[i].name, IgnoreDirective,
		))
	}

	return rendered
}
