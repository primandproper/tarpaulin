package coverage

import (
	"strings"
	"testing"

	"github.com/primandproper/tarpaulin/internal/analysis"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
	"golang.org/x/tools/cover"
)

// twoFunctions is the shape every attribution test needs: one function that
// carries a direct test and one that does not, on known lines.
var twoFunctions = []analysis.Function{
	{Name: "A", Line: 3, EndLine: 5, Tested: true},
	{Name: "B", Line: 7, EndLine: 9, Tested: false},
}

func TestVerdictClass(t *testing.T) {
	t.Parallel()

	test.Eq(t, "tarp-uncovered", verdictUncovered.class())
	test.Eq(t, "tarp-indirect", verdictIndirect.class())
	test.Eq(t, "tarp-direct", verdictDirect.class())
	test.Eq(t, "tarp-ungraded", verdictUngraded.class())
	test.Eq(t, "tarp-ungraded", verdict(200).class())
}

func TestFunctionAt(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		expected string
		line     int
	}{
		"the declaration line itself": {line: 3, expected: "A"},
		"inside the body":             {line: 4, expected: "A"},
		"the closing line":            {line: 5, expected: "A"},
		"the second function":         {line: 8, expected: "B"},
		"between two functions":       {line: 6, expected: ""},
		"above every function":        {line: 1, expected: ""},
		"below every function":        {line: 99, expected: ""},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			found := functionAt(twoFunctions, testCase.line)

			if testCase.expected == "" {
				test.Nil(t, found)

				return
			}

			must.NotNil(t, found)
			test.Eq(t, testCase.expected, found.Name)
		})
	}

	t.Run("no functions at all", func(t *testing.T) {
		t.Parallel()

		test.Nil(t, functionAt(nil, 4))
	})
}

func TestVerdictFor(t *testing.T) {
	t.Parallel()

	tested := analysis.Function{Name: "A", Tested: true}
	untested := analysis.Function{Name: "B"}

	// A statement that never ran is red whatever the analysis concluded: the
	// verdict on the function does not make unexecuted code any better off.
	test.Eq(t, verdictUncovered, verdictFor(cover.ProfileBlock{Count: 0}, &tested))
	test.Eq(t, verdictUncovered, verdictFor(cover.ProfileBlock{Count: 0}, nil))
	test.Eq(t, verdictDirect, verdictFor(cover.ProfileBlock{Count: 1}, &tested))
	test.Eq(t, verdictIndirect, verdictFor(cover.ProfileBlock{Count: 1}, &untested))
	test.Eq(t, verdictUngraded, verdictFor(cover.ProfileBlock{Count: 1}, nil))
}

func TestDescribe(t *testing.T) {
	t.Parallel()

	tested := analysis.Function{Name: "A", Tested: true}
	untested := analysis.Function{Name: "(*Thing).B"}

	test.Eq(t, "never ran; A is directly tested", describe(cover.ProfileBlock{Count: 0}, &tested))
	test.Eq(t, "ran once; A is directly tested", describe(cover.ProfileBlock{Count: 1}, &tested))
	test.Eq(t, "ran 7 times; A is directly tested", describe(cover.ProfileBlock{Count: 7}, &tested))
	test.Eq(t, "ran once; (*Thing).B has no direct test", describe(cover.ProfileBlock{Count: 1}, &untested))
	test.Eq(t, "ran 3 times; not graded", describe(cover.ProfileBlock{Count: 3}, nil))
}

func TestBoundaries(t *testing.T) {
	t.Parallel()

	// Column numbers are 1-based and count bytes, so a block that opens at the
	// brace of `func f() {` on line 2 opens at column 10.
	src := []byte("package p\nfunc f() { return }\n")

	t.Run("locates a block by line and column", func(t *testing.T) {
		t.Parallel()

		found := boundaries(src, []cover.ProfileBlock{{StartLine: 2, StartCol: 10, EndLine: 2, EndCol: 20}})

		must.SliceLen(t, 2, found)
		test.Eq(t, boundary{offset: 19, block: 0, start: true}, found[0])
		test.Eq(t, boundary{offset: 29, block: 0}, found[1])
		test.Eq(t, "{ return }", string(src[found[0].offset:found[1].offset]))
	})

	t.Run("lets one block start where the last one ended", func(t *testing.T) {
		t.Parallel()

		found := boundaries(src, []cover.ProfileBlock{
			{StartLine: 2, StartCol: 10, EndLine: 2, EndCol: 18},
			{StartLine: 2, StartCol: 18, EndLine: 2, EndCol: 20},
		})

		must.SliceLen(t, 4, found)
		test.Eq(t, []int{19, 27, 27, 29}, []int{found[0].offset, found[1].offset, found[2].offset, found[3].offset})
		test.Eq(t, 1, found[2].block)
	})

	t.Run("closes a block that runs past the end of the file", func(t *testing.T) {
		t.Parallel()

		// A profile from an older copy of the source can describe a block the
		// file no longer contains. Leaving its span open would corrupt the whole
		// page rather than the one block it came from.
		found := boundaries(src, []cover.ProfileBlock{{StartLine: 2, StartCol: 10, EndLine: 400, EndCol: 1}})

		must.SliceLen(t, 2, found)
		test.True(t, found[0].start)
		test.False(t, found[1].start)
		test.Eq(t, len(src), found[1].offset)
	})

	t.Run("opens nothing for a block whose start never matches", func(t *testing.T) {
		t.Parallel()

		found := boundaries(src, []cover.ProfileBlock{{StartLine: 2, StartCol: 99, EndLine: 2, EndCol: 28}})

		test.SliceEmpty(t, found)
	})

	t.Run("no blocks", func(t *testing.T) {
		t.Parallel()

		test.SliceEmpty(t, boundaries(src, nil))
	})
}

func TestWriteBoundary(t *testing.T) {
	t.Parallel()

	// One block inside each of twoFunctions, and one that falls between them.
	blocks := []cover.ProfileBlock{
		{StartLine: 4, StartCol: 2, EndLine: 4, EndCol: 10, Count: 2},
		{StartLine: 8, StartCol: 2, EndLine: 8, EndCol: 10, Count: 1},
		{StartLine: 6, StartCol: 1, EndLine: 6, EndCol: 2, Count: 1},
	}

	t.Run("closes a span without consulting anything", func(t *testing.T) {
		t.Parallel()

		var out strings.Builder

		// A closing boundary carries no block index worth trusting, so it must
		// not reach for one.
		writeBoundary(&out, boundary{offset: 12}, nil, nil)

		test.Eq(t, "</span>", out.String())
	})

	t.Run("opens a span colored by the function the block falls inside", func(t *testing.T) {
		t.Parallel()

		var out strings.Builder

		writeBoundary(&out, boundary{offset: 12, block: 0, start: true}, blocks, twoFunctions)

		test.Eq(t, `<span class="tarp-direct" title="ran 2 times; A is directly tested">`, out.String())
	})

	t.Run("distinguishes a function that merely ran", func(t *testing.T) {
		t.Parallel()

		var out strings.Builder

		writeBoundary(&out, boundary{offset: 30, block: 1, start: true}, blocks, twoFunctions)

		test.Eq(t, `<span class="tarp-indirect" title="ran once; B has no direct test">`, out.String())
	})

	t.Run("greys out a block belonging to no graded function", func(t *testing.T) {
		t.Parallel()

		var out strings.Builder

		writeBoundary(&out, boundary{offset: 24, block: 2, start: true}, blocks, twoFunctions)

		test.Eq(t, `<span class="tarp-ungraded" title="ran once; not graded">`, out.String())
	})

	t.Run("escapes the title", func(t *testing.T) {
		t.Parallel()

		var out strings.Builder

		// The title is an attribute value built from a name the analyzer read
		// out of somebody else's source, so it is escaped rather than trusted.
		writeBoundary(&out, boundary{block: 0, start: true}, blocks,
			[]analysis.Function{{Name: `<"a">`, Line: 4, EndLine: 4}})

		test.Eq(t, `<span class="tarp-indirect" title="ran 2 times; &lt;&#34;a&#34;&gt; has no direct test">`, out.String())
	})
}

func TestWriteSourceByte(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		expected string
		b        byte
	}{
		"a less-than":         {b: '<', expected: "&lt;"},
		"a greater-than":      {b: '>', expected: "&gt;"},
		"an ampersand":        {b: '&', expected: "&amp;"},
		"a double quote":      {b: '"', expected: "&#34;"},
		"a single quote":      {b: '\'', expected: "&#39;"},
		"a tab":               {b: '\t', expected: "        "},
		"an ordinary byte":    {b: 'x', expected: "x"},
		"a newline":           {b: '\n', expected: "\n"},
		"a UTF-8 lead byte":   {b: 0xC3, expected: "\xC3"},
		"a continuation byte": {b: 0xA9, expected: "\xA9"},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			var out strings.Builder

			writeSourceByte(&out, testCase.b)

			test.Eq(t, testCase.expected, out.String())
		})
	}

	t.Run("leaves a multi-byte rune intact", func(t *testing.T) {
		t.Parallel()

		var out strings.Builder

		// The walk is byte by byte, so a rune only survives if every byte of it
		// passes through untouched.
		for _, b := range []byte("é") {
			writeSourceByte(&out, b)
		}

		test.Eq(t, "é", out.String())
	})
}

func TestAnnotate(t *testing.T) {
	t.Parallel()

	t.Run("escapes the source it wraps", func(t *testing.T) {
		t.Parallel()

		src := []byte("package p\nfunc f() { _ = \"<a href='x'>\" & 1 }\n")

		rendered := string(annotate(src, nil, nil))

		test.StrContains(t, rendered, "&lt;a href=&#39;x&#39;&gt;")
		test.StrContains(t, rendered, "&amp;")
		test.StrNotContains(t, rendered, "<a href")
	})

	t.Run("renders tabs as spaces", func(t *testing.T) {
		t.Parallel()

		test.StrContains(t, string(annotate([]byte("\tx"), nil, nil)), "        x")
	})

	t.Run("colors each block by the function it falls inside", func(t *testing.T) {
		t.Parallel()

		src := []byte("package p\n\nfunc A() { a() }\n\n\nfunc B() { b() }\n")

		rendered := string(annotate(src, []cover.ProfileBlock{
			{StartLine: 3, StartCol: 10, EndLine: 3, EndCol: 17, Count: 2},
			{StartLine: 6, StartCol: 10, EndLine: 6, EndCol: 17, Count: 1},
		}, []analysis.Function{
			{Name: "A", Line: 3, EndLine: 3, Tested: true},
			{Name: "B", Line: 6, EndLine: 6},
		}))

		test.StrContains(t, rendered, `<span class="tarp-direct" title="ran 2 times; A is directly tested">{ a() }</span>`)
		test.StrContains(t, rendered, `<span class="tarp-indirect" title="ran once; B has no direct test">{ b() }</span>`)
	})
}
