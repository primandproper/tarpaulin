package analysis

import (
	"path"
	"strings"

	platformerrors "github.com/primandproper/platform-go/v10/errors"
)

// Exclusions withhold declarations from the report by where they live and by
// what they are called.
//
// They are the project-wide half of the escape hatch. IgnoreDirective answers
// for one declaration at the declaration, and demands a reason there because
// the reader is already looking at the code the reason is about; a whole tree
// of generated files is not an argument worth repeating a thousand times, so
// these answer for it once, from a config file, where the comment explaining
// the pattern sits next to the pattern.
//
// The two are the same verdict either way: an excluded declaration is not
// counted at all, so it leaves the grade rather than counting against it. That
// is also what makes exclusions worth auditing — every pattern here is a
// question the tool has agreed to stop asking.
type Exclusions struct {
	// Paths are gitignore-shaped globs matched against each file's path
	// relative to the module root:
	//
	//	internal/generated/**   a subtree
	//	**/*_gen.go             any generated file, at any depth
	//	mocks                   any path segment called mocks, at any depth
	//	zz_generated.go         any file with that name, at any depth
	//	/main.go                that file, in the module root only
	//
	// A pattern with no slash in it matches any single segment of the path,
	// which is what makes a bare name reach files and directories anywhere in
	// the tree. A pattern with a slash is matched against the whole relative
	// path, where * stops at a separator and ** does not. A leading slash
	// anchors a bare name to the module root; a trailing one is allowed and
	// means nothing, since a pattern always matches everything beneath what it
	// names.
	Paths []string
	// Functions are globs matched against the name of a declaration as it is
	// rendered in the report — Foo, Thing.Method, or (*Thing).Method — and
	// against the bare method name on its own:
	//
	//	String                 String, anywhere, method or function
	//	*.MarshalJSON          MarshalJSON on any receiver
	//	(*Report).MarshalJSON  that method, on that receiver
	//
	// The bare-name fallback is why String reaches (*Thing).String without a
	// wildcard, and it is deliberate: the reflection-reached methods this is
	// for — String, MarshalJSON, Value — are named by their method, not by the
	// types that happen to implement them.
	Functions []string
}

// pathPattern is one compiled path glob. Splitting into segments is what makes
// ** expressible at all: it matches any number of them, which is a question
// about the list, and path.Match only ever answers questions about one.
type pathPattern struct {
	segments []string
	anchored bool
}

// exclusion decides whether a declaration is withheld. It is compiled once per
// analysis, before anything is loaded, so a malformed pattern costs the caller
// nothing rather than a full package load.
type exclusion struct {
	root      string
	paths     []pathPattern
	functions []string
}

// newExclusion compiles the configured patterns against the given root, which
// is what the path patterns are relative to.
func newExclusion(root string, exclusions Exclusions) (*exclusion, error) {
	compiled := &exclusion{root: root}

	for _, raw := range exclusions.Paths {
		pattern, ok, err := compilePathPattern(raw)
		if err != nil {
			return nil, err
		}

		if ok {
			compiled.paths = append(compiled.paths, pattern)
		}
	}

	for _, raw := range exclusions.Functions {
		name := strings.TrimSpace(raw)
		if name == "" {
			continue
		}

		if err := validatePattern(name); err != nil {
			return nil, err
		}

		compiled.functions = append(compiled.functions, name)
	}

	return compiled, nil
}

// excludes reports whether the declaration in filename called name is withheld.
// The nil receiver is the collectors' zero value, and withholds nothing.
func (e *exclusion) excludes(filename, name string) bool {
	if e == nil {
		return false
	}

	return e.excludesPath(filename) || e.excludesFunction(name)
}

// excludesPath reports whether the file's path relative to the root matches any
// path pattern. A file outside the root keeps its absolute path, which anchored
// patterns then cannot match — the correct answer for source that is not part
// of the project the config file belongs to.
func (e *exclusion) excludesPath(filename string) bool {
	if len(e.paths) == 0 {
		return false
	}

	segments := strings.Split(relativePath(e.root, filename), "/")

	for i := range e.paths {
		if e.paths[i].matches(segments) {
			return true
		}
	}

	return false
}

// excludesFunction reports whether the rendered declaration name matches any
// function pattern, either whole or by its bare method name.
func (e *exclusion) excludesFunction(name string) bool {
	bare := name
	if index := strings.LastIndex(name, "."); index >= 0 {
		bare = name[index+1:]
	}

	for _, pattern := range e.functions {
		if matchGlob(pattern, name) || matchGlob(pattern, bare) {
			return true
		}
	}

	return false
}

// matchGlob answers path.Match's question and drops its error, which is
// unreachable here: every pattern was validated when it was compiled, and
// path.Match only ever fails on the pattern.
func matchGlob(pattern, name string) bool {
	matched, err := path.Match(pattern, name)

	return err == nil && matched
}

// matches reports whether the pattern matches the path, given as its segments.
//
// Every pattern matches what it names and everything beneath it, so a trailing
// ** is implied: naming a directory is how anyone expects to exclude the files
// in it, and requiring internal/generated/** where internal/generated was meant
// is a papercut with nothing on the other side of it.
func (p pathPattern) matches(segments []string) bool {
	if p.anchored {
		return matchSegments(p.segments, segments)
	}

	// An unanchored pattern is one segment that may match any of them, so it
	// reaches a name at any depth.
	for _, segment := range segments {
		if matchGlob(p.segments[0], segment) {
			return true
		}
	}

	return false
}

// matchSegments walks the pattern and the path together, letting ** stand for
// any number of segments including none.
func matchSegments(pattern, segments []string) bool {
	if len(pattern) == 0 {
		// The pattern is exhausted, and whatever is left of the path is
		// underneath what it named.
		return true
	}

	if pattern[0] == "**" {
		remaining := pattern[1:]

		// Every suffix of the path, starting with the empty one — that is what
		// "any number of segments, including none" means. The empty suffix is
		// spelled as nil rather than as segments[len(segments):], which is legal
		// but reads like an off-by-one every time somebody meets it.
		if matchSegments(remaining, nil) {
			return true
		}

		for i := range segments {
			//nolint:gosec // G602: i is an index of segments, from ranging over it.
			if matchSegments(remaining, segments[i:]) {
				return true
			}
		}

		return false
	}

	if len(segments) == 0 {
		return false
	}

	if !matchGlob(pattern[0], segments[0]) {
		return false
	}

	return matchSegments(pattern[1:], segments[1:])
}

// compilePathPattern turns one configured path glob into a pattern, reporting
// whether there was anything to compile. An empty pattern is dropped rather
// than refused: it is what a trailing comma in an environment variable or a
// stray list item in YAML produces, and neither is worth failing an analysis
// over.
func compilePathPattern(raw string) (pathPattern, bool, error) {
	trimmed := strings.TrimSpace(raw)
	trimmed = strings.TrimPrefix(trimmed, "./")

	// A leading slash anchors a bare name to the root, which is the only way to
	// say "this file, here" rather than "this name, anywhere".
	anchored := strings.HasPrefix(trimmed, "/")
	trimmed = strings.Trim(trimmed, "/")

	if trimmed == "" {
		return pathPattern{}, false, nil
	}

	segments := strings.Split(trimmed, "/")
	if len(segments) > 1 {
		anchored = true
	}

	for _, segment := range segments {
		if err := validatePattern(segment); err != nil {
			return pathPattern{}, false, err
		}
	}

	return pathPattern{segments: segments, anchored: anchored}, true, nil
}

// validatePattern rejects a glob path.Match cannot parse, naming it. An
// unterminated character class is a typo, and finding out by silently matching
// nothing is how a config file quietly stops excluding what it says it does.
func validatePattern(pattern string) error {
	if _, err := path.Match(pattern, ""); err != nil {
		return platformerrors.Wrapf(
			platformerrors.ErrUnrecognizedInputValue,
			"exclusion pattern %q: %s", pattern, err,
		)
	}

	return nil
}
