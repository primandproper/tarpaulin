// Package sarif renders an analysis report as a SARIF 2.1.0 document.
//
// SARIF is the interchange format for static analysis findings — the shape that
// SARIF viewers, GitHub code scanning, Jenkins' Warnings NG, SonarQube, and the
// sarif-tools CLI all read. Emitting it is free and unentitled; only uploading
// it into GitHub code scanning is licensed, which is why this is an output
// format rather than a feature of the Action.
//
// It buys two things the report's own JSON cannot. Locations are stated against
// a declared base (%SRCROOT%, the module root), so a consumer resolves paths
// structurally instead of reconstructing them; and every result carries a
// partial fingerprint keyed on the function's name rather than its position, so
// a finding survives the declaration moving down the file.
package sarif

import (
	"io"
	"net/url"
	"path/filepath"
	"strings"

	"github.com/primandproper/tarpaulin/internal/analysis"

	"github.com/primandproper/platform-go/v10/encoding"
	platformerrors "github.com/primandproper/platform-go/v10/errors"
)

// The invariants of the emitted document.
const (
	schemaURI      = "https://json.schemastore.org/sarif-2.1.0.json"
	specVersion    = "2.1.0"
	toolName       = "tarp"
	informationURI = "https://github.com/primandproper/tarpaulin"
	warningLevel   = "warning"
)

// ruleID names the one thing tarp reports. It is the identity a consumer
// filters and dismisses on, so it is a constant rather than anything derived:
// strictness changes what is reported, not what the finding means.
const ruleID = "tarp/untested-function"

// srcRootID is the base every result's URI is relative to. SARIF consumers
// resolve it against their own checkout, which is what makes a document
// produced on a runner readable on a laptop.
const srcRootID = "SRCROOT"

// fingerprintKey is versioned because changing how a fingerprint is computed
// re-opens every finding that was ever dismissed. A new scheme goes under a new
// key rather than silently redefining this one.
const fingerprintKey = "tarpFunction/v1"

// Config selects what to render.
type Config struct {
	// Report is the analysis to describe. Only its untested functions become
	// results: SARIF carries findings, and a function that has a test is not
	// one.
	Report *analysis.Report
	// Version is the tarp build that produced the report, reported as the
	// driver version. Empty omits it.
	Version string
}

// Render writes the SARIF representation of a report to w.
func Render(w io.Writer, cfg Config) error {
	doc := document{
		Schema:  schemaURI,
		Version: specVersion,
		Runs:    []run{buildRun(cfg)},
	}

	encoded, err := encoding.EncodeJSON(doc)
	if err != nil {
		return platformerrors.Wrap(err, "encoding SARIF document")
	}

	if _, err = w.Write(append(encoded, '\n')); err != nil {
		return platformerrors.Wrap(err, "writing SARIF document")
	}

	return nil
}

// buildRun assembles the single run the document carries.
func buildRun(cfg Config) run {
	report := cfg.Report
	untested := report.Untested()

	results := make([]result, 0, len(untested))
	for i := range untested {
		results = append(results, buildResult(report.Root, &untested[i]))
	}

	out := run{
		Tool: tool{Driver: driver{
			Name:           toolName,
			Version:        cfg.Version,
			InformationURI: informationURI,
			Rules:          []rule{untestedFunctionRule()},
		}},
		// The strictness is part of the run's identity, not a detail: the same
		// package graded at `file` and at `any` produces two legitimately
		// different sets of findings, and a consumer tracking both wants them
		// kept apart rather than flip-flopping.
		AutomationDetails: &automationDetails{ID: toolName + "/" + report.Strictness.String()},
		Results:           results,
		Invocations: []invocation{{
			ExecutionSuccessful:        true,
			ToolExecutionNotifications: buildNotifications(report.Warnings),
		}},
		// SARIF has nowhere to put a grade — it carries findings, not scores —
		// so the numbers ride in the property bag, which consumers that do not
		// understand them ignore.
		Properties: &runProperties{
			Strictness: report.Strictness.String(),
			Score:      report.Score(),
			Declared:   report.Declared(),
			Tested:     report.Tested(),
		},
	}

	if base := rootURI(report.Root); base != "" {
		out.OriginalURIBaseIDs = map[string]artifactLocation{srcRootID: {URI: base}}
	}

	return out
}

// buildResult turns one untested function into a SARIF result.
func buildResult(root string, fn *analysis.Function) result {
	return result{
		RuleID:    ruleID,
		RuleIndex: 0,
		Level:     warningLevel,
		Message:   message{Text: fn.Name + " has no direct unit test"},
		Locations: []location{{PhysicalLocation: physicalLocation{
			ArtifactLocation: locate(root, fn),
			Region:           region{StartLine: fn.Line, EndLine: fn.EndLine},
		}}},
		// Keyed on the qualified name rather than the position, which is the
		// whole point: a function that moves down the file is the same finding,
		// and a consumer that dismissed it should not see it again.
		PartialFingerprints: map[string]string{fingerprintKey: qualify(fn)},
	}
}

// qualify names a function uniquely enough to fingerprint it.
//
// The import path rather than the package clause, because a fingerprint that
// collides merges two findings into one: any module of real size holds several
// packages named config, and grading one of them would silently dismiss the
// other. The clause name is the fallback for a package with no path at all.
func qualify(fn *analysis.Function) string {
	if fn.PackagePath != "" {
		return fn.PackagePath + "." + fn.Name
	}

	return fn.Package + "." + fn.Name
}

// buildNotifications carries the analyzer's warnings — a bare ignore directive,
// say — into the run, where a consumer surfaces them as tool output rather than
// as findings about the source.
func buildNotifications(warnings []string) []notification {
	if len(warnings) == 0 {
		return nil
	}

	notifications := make([]notification, 0, len(warnings))
	for _, warning := range warnings {
		notifications = append(notifications, notification{
			Level:   warningLevel,
			Message: message{Text: warning},
		})
	}

	return notifications
}

// untestedFunctionRule describes the one rule, which is what a viewer shows
// when someone asks what the finding means.
func untestedFunctionRule() rule {
	return rule{
		ID:               ruleID,
		Name:             "UntestedFunction",
		ShortDescription: message{Text: "Function has no direct unit test"},
		FullDescription: message{Text: "No TestXxx body references this function. Statement coverage " +
			"cannot tell a function that was tested from one that was merely executed on the way to " +
			"somebody else's assertion; this rule asks the stricter question. It is a different " +
			"question, not a better one: a reference is evidence that a test was written for the " +
			"function, and says nothing about what that test asserts. Read this rule alongside " +
			"coverage rather than instead of it."},
		Help: message{
			Text: "Write a test that references this function directly, or exempt it with a " +
				"//tarp:ignore directive whose reason names the test that does the asserting.",
			Markdown: "Write a test that references this function directly, or exempt it with a " +
				"`//tarp:ignore` directive whose reason names the test that does the asserting. " +
				"See [the README](" + informationURI + ") for the strictness levels.",
		},
		DefaultConfiguration: configuration{Level: warningLevel},
		Properties:           &ruleProperties{Tags: []string{"testing", "maintainability"}},
	}
}

// locate states where a function is, relative to the module root when the file
// sits underneath it.
//
// The fallback matters: Function.File is relative to the *analyzed* directory,
// which is not the root a consumer resolves against, so emitting it under
// %SRCROOT% would produce a URI that points at nothing. Without a root to be
// relative to, the path is emitted bare and unbased — worse, but honest about
// what it is.
func locate(root string, fn *analysis.Function) artifactLocation {
	if root != "" && fn.Path != "" {
		if rel, err := filepath.Rel(root, fn.Path); err == nil && !strings.HasPrefix(rel, "..") {
			return artifactLocation{URI: escapePath(rel), URIBaseID: srcRootID}
		}
	}

	return artifactLocation{URI: escapePath(fn.File)}
}

// escapePath turns a filesystem path into the URI reference SARIF wants:
// forward slashes on every platform, and each segment percent-escaped so a
// directory with a space in it does not produce an invalid URI.
func escapePath(path string) string {
	return (&url.URL{Path: filepath.ToSlash(path)}).EscapedPath()
}

// rootURI renders the module root as the absolute file URI that %SRCROOT%
// resolves to, or empty when there is no root to state.
func rootURI(root string) string {
	if root == "" {
		return ""
	}

	// A Windows root is C:\src\mod, whose URI path has to lead with a slash
	// before the drive letter. Every other platform is already absolute.
	path := filepath.ToSlash(root)
	if !strings.HasPrefix(path, "/") {
		path = "/" + path
	}

	// The trailing slash is what makes it a base to resolve against rather than
	// a file that happens to be a directory.
	if !strings.HasSuffix(path, "/") {
		path += "/"
	}

	return (&url.URL{Scheme: "file", Path: path}).String()
}
