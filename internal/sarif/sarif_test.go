package sarif_test

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/primandproper/tarpaulin/internal/analysis"
	"github.com/primandproper/tarpaulin/internal/sarif"

	"github.com/primandproper/platform-go/v10/encoding"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// root is an absolute module root the fixtures below hang off, so the rendered
// URIs are the same on every machine.
var root = filepath.Join(string(filepath.Separator), "src", "mod")

// fixtureReport is one tested function and one untested one, which is the
// smallest report that exercises both "becomes a result" and "does not".
func fixtureReport() *analysis.Report {
	return &analysis.Report{
		Root:       root,
		Strictness: analysis.StrictnessFile,
		Functions: []analysis.Function{
			{
				Package: "simple",
				File:    "main.go",
				Path:    filepath.Join(root, "main.go"),
				Name:    "A",
				Line:    3,
				EndLine: 3,
				Tested:  true,
			},
			{
				Package: "simple",
				File:    "main.go",
				Path:    filepath.Join(root, "main.go"),
				Name:    "B",
				Line:    7,
				EndLine: 9,
			},
		},
	}
}

// renderDocument renders a report and decodes it back into a generic map, which
// is how a consumer that is not this package sees it.
func renderDocument(t *testing.T, cfg sarif.Config) map[string]any {
	t.Helper()

	out := new(bytes.Buffer)
	must.NoError(t, sarif.Render(out, cfg))

	decoded := map[string]any{}
	must.NoError(t, encoding.DecodeJSON(out.Bytes(), &decoded))

	return decoded
}

// number pulls a JSON number out of a decoded document, where every one of them
// arrives as a float64.
func number(t *testing.T, object map[string]any, key string) int {
	t.Helper()

	value, ok := object[key].(float64)
	must.True(t, ok)

	return int(value)
}

// firstRun digs out the single run every tarp document carries.
func firstRun(t *testing.T, doc map[string]any) map[string]any {
	t.Helper()

	runs, ok := doc["runs"].([]any)
	must.True(t, ok)
	must.SliceLen(t, 1, runs)

	run, ok := runs[0].(map[string]any)
	must.True(t, ok)

	return run
}

func TestRender(t *testing.T) {
	t.Parallel()

	t.Run("declares the version consumers dispatch on", func(t *testing.T) {
		t.Parallel()

		// Called here rather than through renderDocument, deliberately: every
		// other case below reaches Render through that helper, and a helper is
		// not a direct test. tarp reports its own package for this, so the fix
		// is the one it asks of everybody else.
		out := new(bytes.Buffer)
		must.NoError(t, sarif.Render(out, sarif.Config{Report: fixtureReport()}))

		doc := map[string]any{}
		must.NoError(t, encoding.DecodeJSON(out.Bytes(), &doc))

		test.Eq(t, "2.1.0", doc["version"])
		test.Eq(t, "https://json.schemastore.org/sarif-2.1.0.json", doc["$schema"])
		// A trailing newline, so the document is a well-formed line on stdout.
		test.StrHasSuffix(t, "\n", out.String())
	})

	t.Run("reports only the untested functions", func(t *testing.T) {
		t.Parallel()

		run := firstRun(t, renderDocument(t, sarif.Config{Report: fixtureReport()}))

		results, ok := run["results"].([]any)
		must.True(t, ok)
		// A tested to an untested is one result, not two: SARIF carries
		// findings, and a function that has a test is not one.
		must.SliceLen(t, 1, results)

		result, ok := results[0].(map[string]any)
		must.True(t, ok)

		test.Eq(t, "tarp/untested-function", result["ruleId"])
		test.Eq(t, "warning", result["level"])

		msg, ok := result["message"].(map[string]any)
		must.True(t, ok)
		test.Eq(t, "B has no direct unit test", msg["text"])
	})

	t.Run("locates a result against the declared base", func(t *testing.T) {
		t.Parallel()

		run := firstRun(t, renderDocument(t, sarif.Config{Report: fixtureReport()}))

		bases, ok := run["originalUriBaseIds"].(map[string]any)
		must.True(t, ok)
		srcRoot, ok := bases["SRCROOT"].(map[string]any)
		must.True(t, ok)
		test.Eq(t, "file:///src/mod/", srcRoot["uri"])

		results, ok := run["results"].([]any)
		must.True(t, ok)
		result, ok := results[0].(map[string]any)
		must.True(t, ok)
		locations, ok := result["locations"].([]any)
		must.True(t, ok)
		physical, ok := locations[0].(map[string]any)["physicalLocation"].(map[string]any)
		must.True(t, ok)

		artifact, ok := physical["artifactLocation"].(map[string]any)
		must.True(t, ok)
		test.Eq(t, "main.go", artifact["uri"])
		test.Eq(t, "SRCROOT", artifact["uriBaseId"])

		region, ok := physical["region"].(map[string]any)
		must.True(t, ok)
		test.Eq(t, 7, number(t, region, "startLine"))
		test.Eq(t, 9, number(t, region, "endLine"))
	})

	t.Run("fingerprints on the name, not the position", func(t *testing.T) {
		t.Parallel()

		report := fixtureReport()
		moved := fixtureReport()
		// The same declaration, forty lines further down the file.
		moved.Functions[1].Line, moved.Functions[1].EndLine = 47, 49

		fingerprint := func(cfg sarif.Config) any {
			run := firstRun(t, renderDocument(t, cfg))
			results, ok := run["results"].([]any)
			must.True(t, ok)
			prints, ok := results[0].(map[string]any)["partialFingerprints"].(map[string]any)
			must.True(t, ok)

			return prints["tarpFunction/v1"]
		}

		// Line drift must not re-open a finding somebody already dismissed.
		test.Eq(t, "simple.B", fingerprint(sarif.Config{Report: report}))
		test.Eq(t, fingerprint(sarif.Config{Report: report}), fingerprint(sarif.Config{Report: moved}))
	})

	t.Run("carries the grade SARIF has no field for", func(t *testing.T) {
		t.Parallel()

		run := firstRun(t, renderDocument(t, sarif.Config{Report: fixtureReport()}))

		properties, ok := run["properties"].(map[string]any)
		must.True(t, ok)

		test.Eq(t, 50, number(t, properties, "score"))
		test.Eq(t, 2, number(t, properties, "declared"))
		test.Eq(t, 1, number(t, properties, "tested"))
		test.Eq(t, "file", properties["strictness"])
	})

	t.Run("keeps runs at different strictness apart", func(t *testing.T) {
		t.Parallel()

		report := fixtureReport()
		report.Strictness = analysis.StrictnessAny

		run := firstRun(t, renderDocument(t, sarif.Config{Report: report}))

		details, ok := run["automationDetails"].(map[string]any)
		must.True(t, ok)
		test.Eq(t, "tarp/any", details["id"])
	})

	t.Run("reports the tool version when it has one", func(t *testing.T) {
		t.Parallel()

		run := firstRun(t, renderDocument(t, sarif.Config{Report: fixtureReport(), Version: "v1.2.0"}))

		driver, ok := run["tool"].(map[string]any)["driver"].(map[string]any)
		must.True(t, ok)

		test.Eq(t, "tarp", driver["name"])
		test.Eq(t, "v1.2.0", driver["version"])

		rules, ok := driver["rules"].([]any)
		must.True(t, ok)
		must.SliceLen(t, 1, rules)
	})

	t.Run("carries analyzer warnings as notifications, not findings", func(t *testing.T) {
		t.Parallel()

		report := fixtureReport()
		report.Warnings = []string{"main.go:2: //tarp:ignore with no reason exempts nothing"}

		run := firstRun(t, renderDocument(t, sarif.Config{Report: report}))

		invocations, ok := run["invocations"].([]any)
		must.True(t, ok)
		invocation, ok := invocations[0].(map[string]any)
		must.True(t, ok)
		successful, ok := invocation["executionSuccessful"].(bool)
		must.True(t, ok)
		test.True(t, successful)

		// A warning is something the tool says about itself, so it must not
		// inflate the finding count.
		notifications, ok := invocation["toolExecutionNotifications"].([]any)
		must.True(t, ok)
		must.SliceLen(t, 1, notifications)

		results, ok := run["results"].([]any)
		must.True(t, ok)
		test.SliceLen(t, 1, results)
	})

	t.Run("renders a clean report as a document with no results", func(t *testing.T) {
		t.Parallel()

		run := firstRun(t, renderDocument(t, sarif.Config{
			Report: &analysis.Report{Root: root, Strictness: analysis.StrictnessFile},
		}))

		// An empty array rather than null: a consumer iterating results should
		// not have to special-case the package that passed.
		results, ok := run["results"].([]any)
		must.True(t, ok)
		test.SliceEmpty(t, results)
	})
}
