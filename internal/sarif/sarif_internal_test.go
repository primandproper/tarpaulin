package sarif

import (
	"path/filepath"
	"testing"

	"github.com/primandproper/tarpaulin/internal/analysis"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

func TestLocate(t *testing.T) {
	t.Parallel()

	root := filepath.Join(string(filepath.Separator), "src", "mod")

	t.Run("states a file under the root against the base", func(t *testing.T) {
		t.Parallel()

		located := locate(root, &analysis.Function{
			File: "main.go",
			Path: filepath.Join(root, "internal", "cli", "main.go"),
		})

		test.Eq(t, "internal/cli/main.go", located.URI)
		test.Eq(t, srcRootID, located.URIBaseID)
	})

	t.Run("falls back to the reported path when there is no root", func(t *testing.T) {
		t.Parallel()

		located := locate("", &analysis.Function{File: "main.go", Path: "/elsewhere/main.go"})

		// Emitting a bare path is worse than a based one, but a path that is
		// relative to the analyzed directory is not relative to SRCROOT, and
		// claiming otherwise would point consumers at nothing.
		test.Eq(t, "main.go", located.URI)
		test.Eq(t, "", located.URIBaseID)
	})

	t.Run("falls back when the file sits outside the root", func(t *testing.T) {
		t.Parallel()

		located := locate(root, &analysis.Function{
			File: "main.go",
			Path: filepath.Join(string(filepath.Separator), "elsewhere", "main.go"),
		})

		test.Eq(t, "main.go", located.URI)
		test.Eq(t, "", located.URIBaseID)
	})
}

func TestEscapePath(t *testing.T) {
	t.Parallel()

	test.Eq(t, "internal/cli/main.go", escapePath(filepath.Join("internal", "cli", "main.go")))
	// A directory with a space in it would otherwise produce an invalid URI.
	test.Eq(t, "my%20pkg/main.go", escapePath(filepath.Join("my pkg", "main.go")))
	test.Eq(t, "", escapePath(""))
}

func TestRootURI(t *testing.T) {
	t.Parallel()

	test.Eq(t, "", rootURI(""))
	test.Eq(t, "file:///src/mod/", rootURI("/src/mod"))
	// Already-trailing separators must not double up.
	test.Eq(t, "file:///src/mod/", rootURI("/src/mod/"))
	// A Windows root leads with a drive letter rather than a slash, and the URI
	// needs one. The separator conversion ahead of this is filepath.ToSlash,
	// which is a no-op off Windows — so the input here is already slashed,
	// keeping the assertion about the logic in this function rather than about
	// the GOOS the suite happens to run on.
	test.Eq(t, "file:///C:/src/mod/", rootURI("C:/src/mod"))
}

func TestBuildNotifications(t *testing.T) {
	t.Parallel()

	// Nil rather than an empty slice, so `omitempty` drops the field entirely
	// on the runs that have nothing to say.
	test.Nil(t, buildNotifications(nil))

	notifications := buildNotifications([]string{"first", "second"})

	must.SliceLen(t, 2, notifications)
	test.Eq(t, warningLevel, notifications[0].Level)
	test.Eq(t, "first", notifications[0].Message.Text)
	test.Eq(t, "second", notifications[1].Message.Text)
}

func TestUntestedFunctionRule(t *testing.T) {
	t.Parallel()

	untested := untestedFunctionRule()

	test.Eq(t, ruleID, untested.ID)
	test.Eq(t, warningLevel, untested.DefaultConfiguration.Level)
	// The help is what a viewer shows when somebody asks what to do about a
	// finding, so it has to name the escape hatch as well as the fix.
	test.StrContains(t, untested.Help.Text, "//tarp:ignore")
	test.StrContains(t, untested.Help.Markdown, informationURI)
	must.NotNil(t, untested.Properties)
	test.SliceContains(t, untested.Properties.Tags, "testing")
}

func TestQualify(t *testing.T) {
	t.Parallel()

	// The import path, because a fingerprint that collides merges two findings
	// into one — and platform-go has forty-eight packages named config, whose
	// same-named constructors would otherwise share a fingerprint and silently
	// dismiss each other.
	test.Eq(t, "example.com/m/audit/config.New", qualify(&analysis.Function{
		Package: "config", PackagePath: "example.com/m/audit/config", Name: "New",
	}))
	test.NotEq(t,
		qualify(&analysis.Function{Package: "config", PackagePath: "example.com/m/audit/config", Name: "New"}),
		qualify(&analysis.Function{Package: "config", PackagePath: "example.com/m/analytics/config", Name: "New"}),
	)
	// A package with no path at all still has to fingerprint as something.
	test.Eq(t, "config.New", qualify(&analysis.Function{Package: "config", Name: "New"}))
}

func TestBuildResult(t *testing.T) {
	t.Parallel()

	root := filepath.Join(string(filepath.Separator), "src", "mod")

	built := buildResult(root, &analysis.Function{
		Package: "analysis",
		File:    "references.go",
		Path:    filepath.Join(root, "internal", "analysis", "references.go"),
		Name:    "(*collector).siteFor",
		Line:    116,
		EndLine: 130,
	})

	test.Eq(t, ruleID, built.RuleID)
	test.Eq(t, 0, built.RuleIndex)
	test.Eq(t, "(*collector).siteFor has no direct unit test", built.Message.Text)
	// A method's fingerprint carries its receiver, so two same-named methods on
	// different types are two findings rather than one that flickers.
	test.Eq(t, "analysis.(*collector).siteFor", built.PartialFingerprints[fingerprintKey])
	must.SliceLen(t, 1, built.Locations)
	test.Eq(t, 116, built.Locations[0].PhysicalLocation.Region.StartLine)
}

func TestBuildRun(t *testing.T) {
	t.Parallel()

	built := buildRun(Config{
		Report: &analysis.Report{
			Root:       filepath.Join(string(filepath.Separator), "src", "mod"),
			Strictness: analysis.StrictnessPackage,
		},
		Version: "v9.9.9",
	})

	test.Eq(t, toolName, built.Tool.Driver.Name)
	test.Eq(t, "v9.9.9", built.Tool.Driver.Version)
	must.NotNil(t, built.AutomationDetails)
	test.Eq(t, "tarp/package", built.AutomationDetails.ID)
	must.NotNil(t, built.Properties)
	// A package that declares nothing scores 100, the same as everywhere else.
	test.Eq(t, 100, built.Properties.Score)
	must.MapContainsKey(t, built.OriginalURIBaseIDs, srcRootID)
}
