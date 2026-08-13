# PRD status

Where the implementation stands against `PRD.md`, section by section, so a new
session can pick up without re-deriving anything. Last updated 2026-08-13, at
commit `a454baf` on branch `build-tarp-analyzer`.

**Summary:** the analyzer, the corpus, and `analyze` are done and green. `cover
--html` is the one piece of PRD scope that is not built. The GitHub Action and
SARIF were deferred by the PRD itself.

---

## Decisions taken (do not relitigate)

| Question | Decision | Where it is pinned |
|---|---|---|
| §0 Name | Module `github.com/primandproper/tarpaulin`; every user-visible name is `tarp` — binary, cobra `Use:`, `//tarp:ignore`, `TARP_` | `go.mod`, `Makefile:BINARY_NAME`, `internal/config/config.go` |
| §9.1 `var Foo = func(){}` | **Not** a declaration | `testdata/func_var` |
| §9.2 Ignore directive reason | **Required**; a bare `//tarp:ignore` exempts nothing and warns on stderr | `testdata/ignore_directive`, `TestIgnoreDirective` |
| §9.4 Per-package strictness | Not built; the global flag plus the ignore directive is sufficient | — |
| §3.1 (unstated) `TestMain` | A harness, not a test: it credits nothing below `--strictness=any` | `testdata/test_main`, `TestIsTestFunc` |
| Score arithmetic (unstated) | Truncated, not rounded — 2 of 3 is 66% | `TestReportScore` |

## Deviations from the plan, deliberate

- **Expectations are markers, not golden files.** `//tarp:want excluded` and
  `//tarp:want untested=file,package` live in the fixture source. They survive
  line drift and read as documentation; the harness derives the expected
  declaration set syntactically (`go/parser` + `build.Default.MatchFile`), which
  keeps it independent of the analyzer it checks. The one golden that matters —
  the JSON wire shape — is pinned verbatim in `analysis_test.go`.
- **Build-tag fixtures are platform-agnostic.** `build.Default.MatchFile` in the
  harness rather than a hardcoded GOOS, so the corpus asserts the same thing on
  a darwin laptop and an Ubuntu runner.

---

## §1–§2 Problem and scope

Done. The tool reproduces the PRD's opening example exactly — `B on line 7`,
`Grade: 75% (3/4 functions)` — and the same numbers come out of the 2017 repo's
own `example_packages/simple` fixture.

Benchmark and fuzz coverage remain rejected, as specified.

## §3 Core semantics — all implemented

| Rule | Status | Where |
|---|---|---|
| §3.1 `TestXxx` only; not `Example`/`Benchmark`/`Fuzz` | Done | `references.go:isTestFunc` |
| §3.2 Three-position strictness dial, strictest default | Done | `strictness.go`, `refIndex.satisfies` |
| §3.3 Test slot is `{foo_test.go, foo_internal_test.go}` | Done | `collector.siteFor`, `testdata/internal_and_external` |
| §3.4 References count, not just calls | Done | `collector.walk` via `TypesInfo.Uses` |
| §3.5 One hop into package-level test-table vars | Done | `collector.walkTestBody`, `packageLevelVars` |
| §3.6 Interface dispatch, single-implementer heuristic | Done (rung 1) | `soleImplementation` |
| §3.7 Never-reported declarations | Done | `isNeverReported`, `ast.IsGenerated`, `ignoreDirective` |

**Rung 2 (RTA callgraph) was not built and no fixture demands it.** The PRD makes
it conditional on the heuristic proving insufficient; `testdata/interface_two_impls`
documents the case where credit is withheld, and nothing in the corpus needs more.

## §4 Implementation — all implemented

Loading (§4.1) with the four variants and the synthesized `<pkg>.test` package
skipped; position-keyed identity (§4.2); `TypesInfo.Uses` reference collection
with `Origin()` normalization (§4.3); clean diagnostics rather than panics
(§4.4); fully sorted output (§4.5).

## §5 Corpus — 46 fixture directories, all green

42 graded at all three strictness levels by `TestCorpus`; 4 error-path fixtures
driven by `TestAnalyzeDiagnostics`. Every subsection of §5.1 through §5.8 has
coverage. Adding a case is: create the directory, write the fixture, mark the
expectations — it is picked up automatically.

## §6 CLI surface

| Piece | Status |
|---|---|
| `analyze` with `--package`, `--strictness`, `--fail-on-found`, `--json` | **Done** |
| Ignore directive | **Done**, reason required |
| `NO_COLOR` / non-TTY handling | **Done** |
| Deterministic ordering | **Done** |
| **`cover --html`** | **NOT BUILT** — see below |

## §7 Repo setup

Done. `rename.sh` ran and removed itself; the app-name pass to `tarp` followed
it. The template was already on `shoenig/test`, so §7's testify swap was a
no-op; `depguard` now carries platform-go's ban list verbatim so it cannot drift
back. Nothing from the old implementation was ported.

## §8 Deferred by the PRD

GitHub Action and SARIF. Untouched, as intended. SARIF is still the open design
question that should be settled before the Action is built.

---

# What is left

## 1. `tarp cover --html` — the only unbuilt PRD scope

The genuinely novel part of the 2017 tool, and the reason `Report` carries
`Functions []Function` with a `Tested bool` rather than just a list of failures:
the three-color render needs the verdict on *every* function, not only the
missing ones.

- Renders a cover profile with **red** untested, **yellow** covered but no direct
  test, **green** directly tested.
- The old fork lives at `../blanket/output/html/html.go` — a copy of
  `cmd/cover/html.go` circa 2017. **Check the current stdlib `go tool cover`
  output before reusing it**; the PRD says so explicitly and eight years have
  passed.
- Inputs: `golang.org/x/tools/cover.ParseProfiles` for the profile, plus an
  `analysis.Report` over the same packages. Joining them needs a per-function
  line range, which the analyzer does not currently record — `Function` carries
  the declaration line only. The old `BlanketFunc` kept `DeclPos`/`LBracePos`/
  `RBracePos`; expect to add an end position to `Function`.

## 2. Known limitations worth a decision

- **`Report.MarshalJSON` reads as untested against ourselves.** It is called
  through `json.Marshal`, and a reflect-driven call is invisible to any static
  analysis — there is no `MarshalJSON` identifier in the test source to find.
  This is the tool being correct, not broken. Same will be true for any
  `Stringer`, `driver.Valuer`, or interface satisfied for a framework's benefit.
  Worth documenting in the README as a known shape, and possibly worth an
  ignore-directive convention.
- **A non-module directory cannot be analyzed.** `../blanket` has no `go.mod`, so
  go/packages refuses it in module mode: *"directory prefix . does not contain
  main module."* Fine for any modern repo; the error message could name the
  cause more helpfully.
- **`--package` with a bare pattern resolves against the working directory.**
  `resolveTarget` treats an existing directory as `Dir` and anything else as a
  pattern from `.`. Reasonable, but undocumented in `--help` beyond one line.

## 3. Our own grade: 56% (36/64)

Not a defect — the PRD says to expect a mediocre grade and not to soften
defaults. The gap barely moves at looser strictness (56/56/57%), so it is
genuinely missing tests rather than tests filed in the wrong place. If it is
worth raising, in order of honesty:

1. **Analyzer internals** (12 functions) — the `*collector` methods,
   `collectDeclarations`, `loadPackages`, `implementingMethod`,
   `recordValueSpec`. `TestCorpus` executes every one of them hundreds of times
   and asserts nothing about any of them in isolation, which is the tool's own
   thesis pointed at itself. Directly testable with the `checkSource` helper
   already in `declarations_internal_test.go`; tests belong in
   `references_internal_test.go`, which is already the right slot.
2. **CLI rendering** (`renderText`, `writeWarnings`) — easy, real.
3. **Cobra constructors and lifecycle** (9 functions) — `newRootCommand`,
   `bootstrap`, `shutdown`, `Execute`. Testable but low value; this is where an
   ignore directive with a reason may be the honest answer.
4. **Inherited template code** (5 functions) — `cmd/main.run`, the config
   builders, `envVarOptions`, `NewPillars`.

## 4. Smaller follow-ups

- `README.md` documents `cover` as not yet ported; update when it lands.
- The corpus has no fixture for a package whose *only* test file is an external
  `foo_test.go` referencing an unexported function — impossible to write in Go,
  which is precisely §3.3's point, but a comment fixture asserting the reported
  outcome would document the reasoning where someone will find it.
- No benchmark exists for the "couple hundred milliseconds inside CI" budget the
  PRD assumes in §3.6. Worth one before deciding anything about RTA.
