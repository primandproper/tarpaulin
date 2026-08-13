# PRD: a Go tool for finding functions without direct unit tests

> **Status:** ready to build. Every semantic decision below is **locked** unless
> explicitly marked OPEN. Do not relitigate them — they were argued out already,
> and the rationale is recorded so you can implement confidently rather than
> re-deriving.

## 0. Name

The original project was called `tarp` (2017), renamed to `blanket` (Jan 2018).
It may be resurrected under **either** name — the owner leans `tarp`. Both are
covering-things puns.

**Pick up the name from the module path in `go.mod` and use it consistently.**
This document says "the tool". Anywhere a name is user-visible, derive it:

- binary name and cobra `Use:`
- the ignore directive — `//tarp:ignore` or `//blanket:ignore`
- env var prefix (`TARP_` / `BLANKET_`), per template-go's `rename.sh` convention

## 1. Problem

`go test -cover` measures statement coverage, which cannot distinguish a
function that was *tested* from one that was merely *executed*.

```go
package simple

func A() string { return "A" }
func B() string { return "B" }
func C() string { return "C" }

func wrapper() { A(); B(); C() }
```

```go
func TestA(t *testing.T)       { A() }
func TestC(t *testing.T)       { C() }
func TestWrapper(t *testing.T) { wrapper() }
```

`go test -cover` reports **100%**. But `B` has no test of its own — it is only
executed incidentally through `TestWrapper`. Delete the `B()` call from
`wrapper` and coverage silently drops, because the coverage was never real.

The tool reports:

```
Functions without direct unit tests:
in example_packages/simple/main.go:
    B on line 7

Grade: 75% (3/4 functions)
```

### The opinion this encodes

> Testing primarily asserts behavior. If a behavior is worth codifying into a
> function, it is worth asserting that function's behavior — in a test written
> for it, in a consistent style, in a predictable file location.

The owner explicitly does **not** fully live up to this and expects to accept a
mediocre grade. That is fine and intended. The tool's job is to *measure*
against the strict ideal, not to be satisfiable. Do not soften defaults to make
scores look better.

## 2. Scope

**In scope for v1:** the analyzer, a cobra CLI, JSON + human output, a strictness
dial, an ignore directive, and a comprehensive test corpus.

**Explicitly deferred:** the GitHub Action and SARIF output (§8).

**Explicitly rejected:** benchmark and fuzz coverage checks. Rationale: every
function should be tested; *not* every function should be benchmarked. There is
no way to detect which functions deserve a benchmark, and any mechanism that
requires the user to declare it is worthless — if they remember to configure it,
they'd have remembered to write the benchmark.

## 3. Core semantics — LOCKED

### 3.1 What counts as a test

**`TestXxx` functions only.**

`ExampleXxx` does **not** count. An `ExampleFoo` with no `TestFoo` should fail
the check. `BenchmarkXxx` and `FuzzXxx` do not count (see §2).

### 3.2 What counts as "tested" — the strictness dial

Three positions. **Default is the strictest.** The dial only ever weakens.

| Level | Flag | Rule |
|---|---|---|
| **file** | `--strictness=file` *(default)* | `Bar` declared in `foo.go` must be referenced inside a `TestXxx` body in **`foo.go`'s test slot** (§3.3) |
| **package** | `--strictness=package` | referenced inside a `TestXxx` body in any `_test.go` in the package |
| **any** | `--strictness=any` | referenced anywhere in any `_test.go`, including helpers |

At `file` and `package`, a reference from a test *helper* does **not** count —
only references lexically inside a `TestXxx` body (plus §3.5).

### 3.3 The test slot for a file

The test slot for `foo.go` is the pair:

```
{ foo_test.go, foo_internal_test.go }
```

**Both** are accepted. This is not a convenience — it is load-bearing.

Go forbids `package foo_test` from referencing unexported identifiers in
`package foo`. If a project's `foo_test.go` declares `package foo_test`, the
*only* way to test an unexported function is a second, internal test file. A
rule that accepted only `foo_test.go` would report those functions, and then
refuse to credit the correct fix. Accepting the pair absorbs the intricacy
without weakening strictness.

This composes correctly with build-tagged files for free: `foo_linux.go` →
`foo_linux_test.go` is already Go convention.

**Do not attempt to merge external test packages into the package under test**
by rewriting the package clause in memory. It was considered and rejected. It
breaks on self-imports (the external test imports the package and qualifies its
references), on redeclaration collisions, and fatally on **import cycles** —
which are the entire reason external test packages exist. And it would gain
nothing: if the test file is external, the user's source *contains no reference*
to the unexported function, because Go forbids writing one. You cannot discover
a reference that does not exist.

### 3.4 References count, not just calls

A function is "referenced" if its identifier is *used* — not only if it is
syntactically called. All of these count:

```go
RunIt(Foo)             // passed as a value
f := x.Method          // method value
g := Type.Method       // method expression
for x := range Seq {}  // range-over-func: invokes Seq with no CallExpr
Map[int, string](v)    // generic instantiation
```

**This is the single most important implementation insight.** The 2017 version
was hard to write because it kept asking *"is this a call?"*, which requires
enumerating every syntactic form a call can take — an unbounded question. Ask
instead: **"what does this identifier resolve to?"** See §4.

### 3.5 The one-hop rule for package-level test tables

Lexical scope is the definition, extended by **exactly one hop into the
initializers of package-level vars declared in test files**.

```go
// package level in foo_test.go — a reference to Foo lives here, not in the Test body
var testCases = []struct{ fn func() }{{fn: Foo}}

func TestTable(t *testing.T) {
    for _, tc := range testCases { tc.fn() }
}
```

When scanning a `TestXxx` body you encounter an identifier resolving to a
package-level var declared in a `_test.go`, follow into that var's initializer
and collect references there too.

Bounded deliberately: **one hop, test files only, var initializers only.** Not
arbitrary call-following — that would quietly collapse `file` mode into `any`.

### 3.6 Interface dispatch

`var i Iface = &Impl{}; i.Do()` resolves to `Iface.Do`, because that is the
static type at the call site. `Impl.Do` gets no credit.

The owner wants implementations credited where it is knowable. Ladder it:

1. **Single-implementer heuristic (build this).** Walk the package's named
   types, test with `types.Implements`. Exactly one implements the interface →
   credit its method. Two or more → credit none, and be able to explain why.
2. **RTA callgraph** (`go/callgraph/rta` over SSA) — only if corpus fixtures
   prove the heuristic insufficient.

**Do not reach for whole-program pointer analysis.** Building SSA blows the
runtime budget for a tool meant to finish in a couple hundred milliseconds
inside CI, and it buys precision on a case that good Go style already avoids:
platform-go's own convention is *"provider packages return their own concrete
type — never the interface it satisfies."* When a constructor returns the
concrete type, `i := NewThing()` gives `i` a static type of `*Impl` and
`i.Do()` resolves through plain `Uses` with zero extra machinery.

### 3.7 Declarations that are never reported

- `init()`
- `main()` in `package main`
- functions in files marked `// Code generated ... DO NOT EDIT.` — non-optional;
  without it every protobuf-bearing repo scores near zero and the tool is unusable
- anything carrying the ignore directive (§6)
- test functions themselves, and anything declared in `_test.go`

OPEN: whether `var Foo = func(){}` at package level counts as a declaration.
Suggest **no** for v1 (it is a var, not a func decl); add a fixture pinning
whichever way you decide.

## 4. Implementation

### 4.1 Load

`golang.org/x/tools/go/packages` with `Tests: true`, `Mode` including
`NeedName | NeedSyntax | NeedTypes | NeedTypesInfo | NeedFiles | NeedCompiledGoFiles`.

**This returns four variants per package.** Verified empirically:

```
probe/foo                        name=foo       source only
probe/foo [probe/foo.test]       name=foo       source + internal tests
probe/foo_test [probe/foo.test]  name=foo_test  external tests
probe/foo.test                   name=main      synthesized test main — SKIP THIS
```

The fourth is generated by the toolchain and references every `TestXxx`. If you
do not skip it, every test function pollutes the reference set.

### 4.2 Function identity — position, never pointer

**`*types.Func` identity does NOT survive across variants.** Verified: the same
`unexported` declared at `foo.go:4` yields *different* `*types.Func` pointers
when referenced from `probe/foo` versus `probe/foo [probe/foo.test]`.

Key every function on its **declaration position** —
`Fset.Position(obj.Pos())` — and union references across variants by that key.
Comparing object pointers across variants produces silently wrong answers.

### 4.3 Collecting references

For each `TestXxx` body (and each §3.5 initializer), `ast.Inspect` for every
`*ast.Ident`; look it up in `TypesInfo.Uses`; keep it if the result is a
`*types.Func` whose declaration position is in the package under test.

That is the whole mechanism. It never asks what a call looks like, so it handles
method values, method expressions, function arguments, range-over-func, and
generic instantiation without special cases.

Normalize generics through `types.Func.Origin()` so instantiations collapse to
the generic they came from.

### 4.4 Errors

The old version used `parser.AllErrors` and happily analyzed broken source.
`go/types` will fail instead — that is an improvement. Surface it as a clean
diagnostic, never a panic. Handle: a package that does not compile; a package
where only the *test* file fails to compile; a directory with no Go files;
vendored deps (never analyzed).

### 4.5 Determinism

The old version ranged a `map[string][]BlanketFunc` straight into a template, so
output order was nondeterministic. **Sort all output** — by file, then by
declaration line. Add a fixture that asserts stable ordering across runs.

## 5. Test corpus — build this first

The owner wants **TDD**: write the whole corpus as fully-failing tests, then make
them pass one at a time. Structure each as a `testdata/` package plus a golden
expectation. Expectations must be stated for **all three** strictness levels
wherever they differ — that is where the semantics actually live.

### 5.1 Salvaged fixtures

Four fixtures were recovered from the old repo's deleted history (they predate
the reorg at `78c3047`) and are reproduced here because that repo is being
discarded. Each is a case that was hard in 2017 and is free under §4.3 — which
makes them good regression tests precisely because they were once expensive.
All four should pass at every strictness level.

<details>
<summary><code>deeply_nested</code> — call buried in nested loops</summary>

```go
package deeply_nested

func X() string { return "X" }
```
```go
package deeply_nested

import "testing"

func TestX(t *testing.T) {
	for range [10]struct{}{} {
		for range [5]struct{}{} {
			for range [1]struct{}{} {
				X()
			}
		}
	}
}
```
</details>

<details>
<summary><code>table_driven_tests</code> — call inside a table loop</summary>

```go
package tabledriven

func X() string { return "X" }
```
```go
package tabledriven

import (
	"testing"
	"time"
)

func TestX(t *testing.T) {
	testCases := [3]struct{ runAt time.Time }{}
	for range testCases {
		X()
	}
}
```
</details>

<details>
<summary><code>function_literals</code> — closure and IIFE</summary>

```go
package functionliterals

func X() string { return "X" }
```
```go
package functionliterals

import "testing"

func TestX(t *testing.T) {
	f := func() { X() }
	f()
}

func TestXAgain(t *testing.T) {
	func() { X() }()
}
```
</details>

<details>
<summary><code>deferred_functions</code> — call inside a defer</summary>

```go
package deferredfunctions

func X() string { return "X" }
```
```go
package deferredfunctions

import "testing"

func TestX(t *testing.T) {
	defer func() { X() }()
}
```
</details>

Also salvaged: an **unparseable** package (`funk main()` with mismatched
delimiters) and a **directory containing no Go files** — both error-path
fixtures for §4.4.

### 5.2 Regressions from the 2017 implementation

These were genuine bugs. They go in first as proof the rewrite fixed something
real. All are free under §4.3, but pin them anyway.

Call in an `else` branch (the old `parseStmt` walked `e.Body.List` and never
`e.Else`) · call in a bare nested block · call in `for` init/post/cond · call in
`if` init · call in a `switch` tag or case expression · call in a range
expression · nested selector `x.A.B.C.Method()` (the old README's documented
"Known Issue") · nondeterministic output ordering (§4.5).

### 5.3 Language that postdates 2018

Range-over-func iterators — `for x := range Seq` invokes `Seq` with no
`CallExpr` · generic instantiation, explicit and inferred · generic methods on
generic types · constraint interfaces (their methods are not real functions) ·
`min`/`max`/`clear` builtins must never be reported.

### 5.4 Dispatch and identity

Interface dispatch, single implementer (§3.6) · interface dispatch, two
implementers (credit none) · embedded-struct method promotion · method value ·
method expression · pointer vs. value receiver · two types with the same method
name · func stored in a package-level var · chained calls `A().B().C()`.

### 5.5 Test-file shapes

External test package `package foo_test` · `foo_test.go` external **plus**
`foo_internal_test.go` internal (§3.3 — the critical one) · both `foo` and
`foo_test` packages present · table-driven with `t.Run` closures · **package-level
test table** (§3.5) and the same table declared inside the function — pin both
sides of the boundary · `TestMain` · calls inside `t.Cleanup`.

### 5.6 Declarations

`init()` · `main()` · build-tagged functions (`//go:build linux` evaluated on a
darwin runner) · generated files · `var Foo = func(){}` (§3.7 OPEN) · blank
receiver · same function declared in one file, tested from another (must FAIL at
`file`, PASS at `package`).

### 5.7 Module reality

Vendored deps not analyzed · `./...` across many packages · internal packages ·
non-compiling package · non-compiling test file only.

### 5.8 CLI contract

Stable JSON shape · exit code under `--fail-on-found` · `NO_COLOR` and non-TTY ·
the ignore directive (§6) · deterministic ordering.

## 6. CLI surface

Keep the 2017 command shape — it was fine:

```
<tool> analyze [--package=.] [--strictness=file|package|any]
               [--fail-on-found] [--json]
<tool> cover --html=coverage.out
```

`cover` renders a cover profile to HTML with three colors: **red** untested,
**yellow** covered but no direct test, **green** directly tested. This is the
genuinely novel part of the original and worth preserving. Check the current
stdlib `go tool cover` HTML output before reusing the old fork of it.

**Ignore directive.** A comment directive above a declaration exempts it:

```go
//<tool>:ignore  -- reason
func Whatever() {}
```

This is the escape hatch that decides whether the tool is adoptable on a real
codebase. Treat it as required, not optional. Consider requiring a reason string.

## 7. Repo setup

**Base:** clone `github.com/primandproper/template-go` and run its `rename.sh`
with the new module path. The template is already on **platform-go v10**
(`dd5185e`) and ships the cobra CLI, observability bootstrap, `Makefile`,
`scripts/`, `.golangci.yml`, and GitHub Actions.

**One thing the template gets wrong for this project:** it still uses
`stretchr/testify` (`go.mod:8`, `CLAUDE.md:72`), which **platform-go bans
outright** via `depguard`. Swap it:

- `github.com/shoenig/test` (package `test`) for non-fatal assertions
- `github.com/shoenig/test/must` for fatal assertions
- `matryer/moq` for mocks — see any platform-go `<pkg>/mock/doc.go` for the
  `//go:generate` pattern
- copy platform-go's `depguard` block from its `.golangci.yml` verbatim so it
  cannot drift back
- update the new repo's `CLAUDE.md` to match

Note `test.Eq` uses `google/go-cmp` internally, so you get cmp-quality diffs
without writing `cmp.Diff` by hand. Helper argument order is **flipped** relative
to testify: `test.SliceLen(t, n, slice)`.

Tests call `t.Parallel()` by default.

**Do not port the old implementation.** `github.com/verygoodsoftwarenotvirus/blanket`
is reference material only. Its `analysis/analyze.go` hand-rolls type inference
across ~150 lines (`nameToTypeMap`, `helperFunctionReturnMap`, `parseUnaryExpr`,
`parseCompositeLit`) purely because it had no type information; all of it is
obsolete under §4. Its ~2,700 lines of tests assert on private methods and
private state, so none survive the rewrite. Carry over only: the README's
problem statement, the `example_packages/` fixtures, the three-color HTML idea,
and the §5.1 salvaged fixtures reproduced above.

## 8. Deferred

**GitHub Action.** The goal is that any Go library can invoke this in CI. Ship a
prebuilt binary or container image so consumers never pay `go install` against
platform-go's large module graph.

**SARIF output** is the open design question and should be settled before the
Action is built: emitting SARIF makes the Action a thin wrapper around
`upload-sarif` and gets native inline PR annotations for free, versus parsing
our own output in a composite action.

## 9. Open questions

1. §3.7 — does `var Foo = func(){}` count as a declaration? (suggest no)
2. §6 — should the ignore directive require a reason string? (suggest yes)
3. §8 — SARIF or not, and the Action's shape.
4. Should `--strictness` also be settable per-package via config file, or is the
   global flag plus the ignore directive sufficient? (suggest sufficient)
