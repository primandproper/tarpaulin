package analysis

import (
	"path/filepath"
	"testing"

	platformerrors "github.com/primandproper/platform-go/v10/errors"

	"github.com/shoenig/test"
	"github.com/shoenig/test/must"
)

// excludeRoot is the root the path patterns in these tests are relative to. It
// is absolute because a real one always is, and relativePath resolves both
// sides before comparing them.
var excludeRoot = filepath.FromSlash("/project")

// at renders a module-relative path the way a declaration's position carries
// it: absolute, and separated by the platform's separator.
func at(rel string) string {
	return filepath.Join(excludeRoot, filepath.FromSlash(rel))
}

func TestNewExclusion(t *testing.T) {
	t.Parallel()

	t.Run("configuring nothing withholds nothing", func(t *testing.T) {
		t.Parallel()

		compiled, err := newExclusion(excludeRoot, Exclusions{})
		must.NoError(t, err)
		must.NotNil(t, compiled)

		test.False(t, compiled.excludes(at("main.go"), "Main"))
	})

	t.Run("the zero value withholds nothing", func(t *testing.T) {
		t.Parallel()

		// The collectors hold a *exclusion, and nil is what they are handed
		// when nobody built one.
		var compiled *exclusion

		test.False(t, compiled.excludes(at("main.go"), "Main"))
	})

	t.Run("empty patterns are dropped rather than refused", func(t *testing.T) {
		t.Parallel()

		// What a trailing comma in TARP_EXCLUDE_PATHS produces. An empty
		// pattern that matched everything would be a catastrophe, and one that
		// failed the run would be a papercut; it means nothing instead.
		compiled, err := newExclusion(excludeRoot, Exclusions{Paths: []string{"", "  ", "/"}, Functions: []string{" "}})
		must.NoError(t, err)
		must.NotNil(t, compiled)

		test.False(t, compiled.excludes(at("main.go"), "Main"))
	})

	t.Run("a malformed pattern is refused, by name", func(t *testing.T) {
		t.Parallel()

		for _, exclusions := range []Exclusions{
			{Paths: []string{"internal/[bad"}},
			{Functions: []string{"[bad"}},
		} {
			_, err := newExclusion(excludeRoot, exclusions)

			must.Error(t, err)
			test.ErrorIs(t, err, platformerrors.ErrUnrecognizedInputValue)
			test.StrContains(t, err.Error(), "[bad")
		}
	})
}

func TestExclusionExcludesPath(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		pattern string
		path    string
		want    bool
	}{
		// A bare name reaches any segment at any depth, which is what makes it
		// useful for the two things people write first: a filename and a
		// directory whose contents they never want graded.
		"a bare filename, anywhere":     {pattern: "zz_generated.go", path: "internal/api/zz_generated.go", want: true},
		"a bare directory, anywhere":    {pattern: "mocks", path: "internal/api/mocks/client.go", want: true},
		"a bare name matches literally": {pattern: "mocks", path: "internal/api/mocksy/client.go", want: false},
		"a bare glob":                   {pattern: "*_gen.go", path: "internal/api/thing_gen.go", want: true},

		// A pattern with a separator is measured against the whole path, so it
		// says where as well as what.
		"a rooted subtree":            {pattern: "internal/generated", path: "internal/generated/deep/thing.go", want: true},
		"a rooted subtree, explicit":  {pattern: "internal/generated/**", path: "internal/generated/deep/thing.go", want: true},
		"a rooted subtree elsewhere":  {pattern: "internal/generated", path: "cmd/internal/generated/thing.go", want: false},
		"a single star stops at a /":  {pattern: "internal/*/thing.go", path: "internal/deep/nested/thing.go", want: false},
		"a double star does not":      {pattern: "internal/**/thing.go", path: "internal/deep/nested/thing.go", want: true},
		"a double star matches none":  {pattern: "internal/**/thing.go", path: "internal/thing.go", want: true},
		"a leading double star":       {pattern: "**/*_gen.go", path: "internal/api/thing_gen.go", want: true},
		"a leading double star, root": {pattern: "**/*_gen.go", path: "thing_gen.go", want: true},

		// A leading slash is the only way to say "here, and nowhere else".
		"anchored to the root":      {pattern: "/main.go", path: "main.go", want: true},
		"anchored, found elsewhere": {pattern: "/main.go", path: "cmd/tool/main.go", want: false},

		// A trailing slash is what anybody typing a directory writes, and it
		// means what they meant.
		"a trailing slash":  {pattern: "internal/generated/", path: "internal/generated/thing.go", want: true},
		"a leading ./":      {pattern: "./internal/generated", path: "internal/generated/thing.go", want: true},
		"an unrelated path": {pattern: "internal/generated", path: "internal/analysis/thing.go", want: false},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			compiled, err := newExclusion(excludeRoot, Exclusions{Paths: []string{testCase.pattern}})
			must.NoError(t, err)

			test.Eq(t, testCase.want, compiled.excludesPath(at(testCase.path)),
				test.Sprintf("pattern %q against %q", testCase.pattern, testCase.path))
		})
	}
}

func TestPathPatternMatches(t *testing.T) {
	t.Parallel()

	// The compiled form, on its own: anchored decides whether the pattern is a
	// statement about a whole path or about a name that may appear anywhere in
	// one, and it is the only thing that decides it.
	anchored := pathPattern{segments: []string{"internal", "**", "*_gen.go"}, anchored: true}

	test.True(t, anchored.matches([]string{"internal", "deep", "thing_gen.go"}))
	test.False(t, anchored.matches([]string{"cmd", "internal", "thing_gen.go"}))

	bare := pathPattern{segments: []string{"mocks"}}

	test.True(t, bare.matches([]string{"internal", "mocks", "client.go"}))
	test.False(t, bare.matches([]string{"internal", "api", "client.go"}))
}

func TestMatchSegments(t *testing.T) {
	t.Parallel()

	// A pattern that runs out matches whatever is left, which is the implied
	// trailing **: naming a directory is how anyone expects to exclude the
	// files in it.
	test.True(t, matchSegments([]string{"a"}, []string{"a", "b", "c.go"}))
	test.True(t, matchSegments(nil, []string{"a"}))

	// ** stands for any number of segments, including none.
	test.True(t, matchSegments([]string{"a", "**", "c.go"}, []string{"a", "c.go"}))
	test.True(t, matchSegments([]string{"a", "**", "c.go"}, []string{"a", "b", "c.go"}))
	test.False(t, matchSegments([]string{"a", "**", "c.go"}, []string{"a", "b", "d.go"}))

	// A path that runs out before the pattern does is not a match, because the
	// pattern still had a name to satisfy.
	test.False(t, matchSegments([]string{"a", "b"}, []string{"a"}))
}

func TestMatchGlob(t *testing.T) {
	t.Parallel()

	test.True(t, matchGlob("*_gen.go", "thing_gen.go"))
	test.False(t, matchGlob("*_gen.go", "thing.go"))

	// A malformed pattern matches nothing rather than panicking. Nothing can
	// reach this — every pattern is validated when it is compiled — but the
	// alternative to answering was propagating an error out of a predicate.
	test.False(t, matchGlob("[unterminated", "anything"))
}

func TestCompilePathPattern(t *testing.T) {
	t.Parallel()

	t.Run("a bare name is unanchored", func(t *testing.T) {
		t.Parallel()

		pattern, ok, err := compilePathPattern("mocks")
		must.NoError(t, err)
		must.True(t, ok)

		test.Eq(t, []string{"mocks"}, pattern.segments)
		test.False(t, pattern.anchored)
	})

	t.Run("a separator anchors it", func(t *testing.T) {
		t.Parallel()

		pattern, ok, err := compilePathPattern("./internal/generated/")
		must.NoError(t, err)
		must.True(t, ok)

		test.Eq(t, []string{"internal", "generated"}, pattern.segments)
		test.True(t, pattern.anchored)
	})

	t.Run("a leading slash anchors a bare name", func(t *testing.T) {
		t.Parallel()

		pattern, ok, err := compilePathPattern("/main.go")
		must.NoError(t, err)
		must.True(t, ok)

		test.Eq(t, []string{"main.go"}, pattern.segments)
		test.True(t, pattern.anchored)
	})

	t.Run("an empty pattern compiles to nothing at all", func(t *testing.T) {
		t.Parallel()

		_, ok, err := compilePathPattern("  ")
		must.NoError(t, err)
		test.False(t, ok)
	})

	t.Run("a malformed segment is refused", func(t *testing.T) {
		t.Parallel()

		_, _, err := compilePathPattern("internal/[bad")

		must.Error(t, err)
		test.ErrorIs(t, err, platformerrors.ErrUnrecognizedInputValue)
	})
}

func TestValidatePattern(t *testing.T) {
	t.Parallel()

	must.NoError(t, validatePattern("*_gen.go"))
	must.NoError(t, validatePattern("[abc].go"))

	// An unterminated character class is a typo, and finding out by silently
	// matching nothing is how a config file quietly stops excluding what it
	// says it does.
	err := validatePattern("[bad")

	must.Error(t, err)
	test.ErrorIs(t, err, platformerrors.ErrUnrecognizedInputValue)
	test.StrContains(t, err.Error(), "[bad")
}

func TestExclusionExcludesPathOutsideTheRoot(t *testing.T) {
	t.Parallel()

	// A file that does not live under the root keeps its absolute path, so an
	// anchored pattern cannot reach it — which is the right answer for source
	// that is not part of the project whose config file this is.
	compiled, err := newExclusion(excludeRoot, Exclusions{Paths: []string{"/main.go", "vendored.go"}})
	must.NoError(t, err)

	elsewhere := filepath.Join(filepath.FromSlash("/elsewhere"), "main.go")
	test.False(t, compiled.excludes(elsewhere, "Main"))

	// An unanchored one still can, because it was never a statement about
	// where.
	test.True(t, compiled.excludes(filepath.Join(filepath.FromSlash("/elsewhere"), "vendored.go"), "Main"))
}

func TestExclusionExcludesFunction(t *testing.T) {
	t.Parallel()

	cases := map[string]struct {
		pattern string
		name    string
		want    bool
	}{
		// The bare-name fallback: the methods this is for are named by their
		// method, not by whichever types happen to implement them.
		"a bare name reaches a pointer method": {pattern: "String", name: "(*Thing).String", want: true},
		"a bare name reaches a value method":   {pattern: "String", name: "Thing.String", want: true},
		"a bare name reaches a function":       {pattern: "String", name: "String", want: true},
		"a bare name is not a prefix":          {pattern: "String", name: "Stringify", want: false},
		"a bare name is not a receiver":        {pattern: "Thing", name: "(*Thing).String", want: false},

		"any receiver":       {pattern: "*.MarshalJSON", name: "(*Report).MarshalJSON", want: true},
		"one receiver":       {pattern: "(*Report).MarshalJSON", name: "(*Report).MarshalJSON", want: true},
		"one receiver, miss": {pattern: "(*Report).MarshalJSON", name: "(*Function).MarshalJSON", want: false},
		"a suffix glob":      {pattern: "mock*", name: "mockClient", want: true},
	}

	for name, testCase := range cases {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			compiled, err := newExclusion(excludeRoot, Exclusions{Functions: []string{testCase.pattern}})
			must.NoError(t, err)

			test.Eq(t, testCase.want, compiled.excludesFunction(testCase.name),
				test.Sprintf("pattern %q against %q", testCase.pattern, testCase.name))
		})
	}
}

func TestExclusionExcludesEither(t *testing.T) {
	t.Parallel()

	// The two lists are alternatives, not a conjunction: a declaration is
	// withheld when either has something to say about it.
	compiled, err := newExclusion(excludeRoot, Exclusions{
		Paths:     []string{"internal/generated"},
		Functions: []string{"String"},
	})
	must.NoError(t, err)

	test.True(t, compiled.excludes(at("internal/generated/thing.go"), "Anything"))
	test.True(t, compiled.excludes(at("internal/analysis/thing.go"), "(*Thing).String"))
	test.False(t, compiled.excludes(at("internal/analysis/thing.go"), "Anything"))
}
