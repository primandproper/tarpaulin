# PRD status

Where the implementation stands against `PRD.md`, section by section, so a new
session can pick up without re-deriving anything. Last updated 2026-08-13, on
branch `build-tarp-analyzer`.

**Summary:** every piece of PRD scope is built and green — the analyzer, the
corpus, `analyze`, and `cover --html`. **Both §8 deferrals are now built too**:
the GitHub Action (`action.yml`), its release pipeline, and SARIF output
(`internal/sarif`, `--format=sarif`). The three known limitations that were
awaiting a decision have all been settled (§1), and the two follow-ups worth
building — the §3.3 fixture and the §3.6 benchmark — are built (§3). What is
genuinely left is our own grade (§2), two follow-ups deliberately left alone,
and one thing that cannot be tested until a release exists: the action's install
step, whose `version:` default still points at a tag nobody has published.

---

## Decisions taken (do not relitigate)

| Question | Decision | Where it is pinned |
|---|---|---|
| §0 Name | Module `github.com/primandproper/tarpaulin`; every user-visible name is `tarp` — binary, cobra `Use:`, `//tarp:ignore`, `TARP_` | `go.mod`, `Makefile:BINARY_NAME`, `internal/config/config.go` |
| §9.1 `var Foo = func(){}` | **Not** a declaration | `testdata/func_var` |
| §9.2 Ignore directive reason | **Required**; a bare `//tarp:ignore` exempts nothing and warns on stderr | `testdata/ignore_directive`, `TestIgnoreDirective` |
| §9.4 Per-package strictness | Not built; the global flag plus the ignore directive is sufficient | — |
| §9.3 SARIF and the Action's shape | Both settled: composite action shipped, SARIF emitted as an output format but never as the architecture | `action.yml`, `internal/sarif`, §8 below |
| §3.1 (unstated) `TestMain` | A harness, not a test: it credits nothing below `--strictness=any` | `testdata/test_main`, `TestIsTestFunc` |
| Score arithmetic (unstated) | Truncated, not rounded — 2 of 3 is 66% | `TestReportScore` |
| Module requirement (unstated) | Module mode only; a target with no `go.mod` above it is refused by name rather than by the go command's symptom | `ErrNotInModule`, `TestCheckModule`, `TestModuleRoot` |
| `--package` resolution (unstated) | Arguments beat `--package`; an existing directory is a directory (expanded to `./...`), anything else is a pattern from the working directory | `resolveTarget`, `analyze --help`, README "Choosing what to analyze" |
| Reflect-invoked methods (unstated) | Reported like anything else; the convention is an ignore directive whose reason names the asserting test | `report.go:MarshalJSON`, README "Methods nothing can name" |

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

## §5 Corpus — 47 fixture directories, all green

43 graded at all three strictness levels by `TestCorpus`; 4 error-path fixtures
driven by `TestAnalyzeDiagnostics`. Every subsection of §5.1 through §5.8 has
coverage. Adding a case is: create the directory, write the fixture, mark the
expectations — it is picked up automatically.

## §6 CLI surface

| Piece | Status |
|---|---|
| `analyze` with `--package`, `--strictness`, `--fail-on-found`, `--json` | **Done** |
| `analyze --min-score N` (not in the PRD; built for the Action, see §8) | **Done** |
| `analyze --format text\|json\|sarif\|markdown` (`--json` is now its shorthand, see §8) | **Done** |
| `Report.Packages()` per-package grading, and `--format=markdown` over it (not in the PRD) | **Done** |
| Ignore directive | **Done**, reason required |
| `NO_COLOR` / non-TTY handling | **Done** |
| Deterministic ordering | **Done** |
| `cover --html` | **Done** — see below |

### Per-package grading (unstated by the PRD)

`Report.Packages()` groups the report by the package each function was declared
in; `--format=markdown` renders it as a table with a total row. Built for
pointing tarp at a whole module — the function list stops being readable around
the second page, and 282 rows say where the work is.

**Grouping is on the import path, never the package clause name.** This is the
part worth not relitigating: `Function.Package` has always been `pkg.Name`, and
platform-go holds 48 packages named `config`, 27 named `noop`, and 12 named
`migrations`. Grouping on the name would have merged 111 of its 281 rows into
each other. `Function.PackagePath` was added (`json:"-"`, like `Path` and
`EndLine` before it) and is what `Packages()` keys on.

The same bug was live in the SARIF fingerprints, which qualified on
`fn.Package` — every `config.New` across those 48 packages shared one
fingerprint, so dismissing one would have dismissed them all. `sarif.qualify`
now prefers the path.

The grading arithmetic moved into one unexported `score(tested, declared)` so a
package and the report it belongs to cannot disagree about what 2 of 3 is, and
a test asserts the rows sum to the total.

## §7 Repo setup

Done. `rename.sh` ran and removed itself; the app-name pass to `tarp` followed
it. The template was already on `shoenig/test`, so §7's testify swap was a
no-op; `depguard` now carries platform-go's ban list verbatim so it cannot drift
back. Nothing from the old implementation was ported.

## §8 Deferred by the PRD — both are now built

`action.yml` lives at the repository root, so consumers write
`uses: primandproper/tarpaulin@v1` and the action can never skew from the binary
it runs. It is copy-pasteable to a separate `tarp-action` repo unchanged if that
is ever wanted — the download URL names `primandproper/tarpaulin` either way.

Five composite steps: install (OS/arch mapping, checksum-verified download),
run, annotate, summarize, exit. The shape, settled 2026-08-13:

- **A composite action, not Docker** — a container costs a pull per job and pins
  consumers to Linux runners.
- **Workflow-command annotations are the default; SARIF is opt-in.**
  `::warning file=…,line=…` needs no entitlement and no `security-events: write`,
  and `untested[]` already carries `package`, `file`, `line`, `name`. SARIF ships
  alongside it behind `sarif_output` rather than replacing it — see below for
  why that inversion is deliberate.
- **Thresholds belong in `tarp`, not in the action's shell.** Hence
  `--min-score` (§6): CLI users get it too, the exit-code semantics stay one
  decision in one place next to `--fail-on-found`, and the action stays a thin
  shim rather than a pile of `jq` in YAML that nothing tests.

**The release workflow is built** — `.github/workflows/release.yaml` on
`release: [published]`, `scripts/release.sh`, `make release`. Six targets
(linux/darwin/windows × amd64/arm64) cross-compiled with CGO off from one
runner, each archive holding the binary plus `LICENSE` and `README.md`, with a
SHA-256 `checksums.txt` over exactly that run's output. The suite runs before
anything is built, because a tag can be cut from a commit no pull request
gated. Asset names use the tag verbatim, so the action interpolates its
`version:` input without reproducing any transformation. `version.Version` was
added at the same time and is injected from the tag: the action pins a release,
and a bug report cites one, so neither wants a commit hash.

Both known traps are handled. `Function.File` is relative to the *analyzed*
directory, and an annotation GitHub cannot resolve from the repository root is
one it drops without saying so — the run step recomputes the prefix using the
CLI's own two rules and prepends it. The `version:` input is pinned rather than
floating.

Three further decisions taken while building it:

- **Annotations stay `warning` even when a gate fails.** Errors would paint
  every untested function in the Files Changed view, including ones the pull
  request never touched. The exit code fails the build; the annotations inform.
- **`max_annotations` defaults to 10**, which is what GitHub renders per step.
  The truncation is logged as a notice and the summary carries the full list —
  a silent cap would read as "we looked at everything" when it did not.
- **A gate failure and a real failure are distinguished by the report.** The run
  step captures tarp's exit code rather than dying on it, then checks whether
  parseable JSON came out: a gate leaves a complete report behind, a broken
  package or a bad flag does not. Only the latter aborts before annotating.

**Verified by simulating all five steps locally** against real reports — path
rebasing from a subdirectory, the whole-repo case, `--min-score` and
`--fail-on-found` gates, a clean pass, and a broken package. Every `run:` block
passes shellcheck. What is *not* verified is the install step, which cannot run
until a release exists: `version:` still defaults to `v0.1.0` and must be
bumped to whatever the first published tag actually is.

**SARIF is built** — `internal/sarif`, `--format=sarif`, and the action's
`sarif_output` input. The §9.3 open question is closed: **emit it, but not as
the architecture.**

The reasoning, because it inverts what the PRD assumed. §8 expected SARIF to
make the Action a thin `upload-sarif` wrapper. It cannot, for three reasons:

- **Uploading into code scanning is entitled** — free on public repos, GitHub
  Advanced Security on private ones — so a SARIF-only action would be useless
  to most private consumers. *Emitting* SARIF is unentitled, and that is the
  half worth having. (Verify the licensing before leaning on it; it moves.)
- **SARIF carries findings, not scores.** There is no field for a grade, so
  `--min-score` and the gate stay exactly where they are regardless. The score
  rides in the run's property bag.
- **Its dismissal model competes with `//tarp:ignore`.** A click-to-dismiss with
  no reason and no diff is the opposite of a directive that requires one and is
  reviewed. `sarif-tools diff` gives the same baselining without that, which is
  why the format is worth emitting and the Security tab is not worth designing
  around.

What SARIF does buy, and the JSON cannot: locations stated against `%SRCROOT%`
(the module root, so a subdirectory analysis still resolves correctly — this is
the trap the action had to solve with `cd`/`pwd` string surgery), and
`partialFingerprints` keyed on the qualified function name rather than its
position, so a declaration moving down the file is the same finding.

`Report.Root` was added to carry the module root out of the analyzer, `json:"-"`
like `Function.Path` before it and for the same kind of reason.

**Validated against the published SARIF 2.1.0 schema** with `check-jsonschema`
— the simple fixture, a fixture with warnings, and this whole repository.

`--format` replaced the `asJSON` bool that `render` took. Adding `--sarif` as a
second mutually-exclusive bool was the shape that rots; nothing was released
yet, so this was the cheapest it would ever be. `--json` survives as a shorthand
because `tarp analyze --json | jq` is muscle memory, and a `--json` that
contradicts an explicit `--format` is refused rather than silently resolved.

---

# What was built for `cover --html`

`internal/coverage`, plus the `cover` subcommand. The old fork at
`../blanket/output/html/html.go` was **not** reused: the current stdlib
`cmd/cover/html.go` was read first, as the PRD asked, and the page follows it —
same topbar, same file picker, same fragment-driven switch.

- **Four colors, not three.** Red never ran, yellow ran with no direct test,
  green directly tested — and grey for a declaration tarp does not grade (`init`,
  `main`, generated, ignored) or a file in the profile that was not analyzed.
  Painting those green would claim a test nobody asked for, and yellow would
  claim tarp had an opinion it deliberately does not have.
- **The join.** `Function` gained `Path` (absolute) and `EndLine`, both
  `json:"-"` so the pinned wire shape is untouched. A profile names files by
  package path and blocks by line; `sources.go` resolves the former with a cheap
  `NeedName|NeedFiles` load, and `annotate.go` attributes a block to the function
  whose `[Line, EndLine]` contains its start line.
- **The boundary walk** is `x/tools/cover.Boundaries` rewritten to keep the block
  index (the line number is what attributes a run of source) and to guarantee
  balanced spans against a stale profile. It is pinned as byte-identical to
  `go tool cover -html`'s span placement: rendering this repo's own 16-file
  profile through both and normalizing away the class and title attributes
  produces no difference.
- **Verified end to end** on the PRD's opening example: `simple/main.go` reads
  *100.0% covered, 3/4 tested*, and `B` is the yellow one.

# What is left

## 1. Known limitations — all three settled

Kept here with the reasoning rather than deleted: each one was a decision, and
the next person to hit it should find why it went the way it did.

- ~~**`Report.MarshalJSON` reads as untested against ourselves.**~~ **Settled:
  documented as a known shape, and the convention applied to ourselves.** A
  method reached by reflection has no identifier in the test source to find, so
  it is reported however well it is tested — `Stringer`, `driver.Valuer`, and any
  interface satisfied for a framework's benefit all read this way. The tool is
  being correct, so the answer is a directive whose reason names the test that
  does the asserting: `report.go` now carries *"reached by reflection through
  json.Marshal, so no test can name it; asserted by TestReportMarshalJSON"*, and
  the README's "Methods nothing can name" section documents the shape and the
  convention. The exemption stays auditable — open the named test, or notice
  loudly when it is gone.
- ~~**A non-module directory cannot be analyzed.**~~ **Settled: the constraint
  stands, the message explains it.** `loadPackages` consults `checkModule` only
  after a load has already failed — so GOPATH mode under `GO111MODULE=off`, where
  a module-less directory loads fine, is untouched — and returns
  `analysis.ErrNotInModule` when no `go.mod` sits at or above the target:
  *"analyzing /tmp/scratch: no go.mod in that directory or any parent, and
  packages load in module mode: run `go mod init` there first."* Covered by
  `TestCheckModule`, `TestModuleRoot`, and a `TestAnalyzeDiagnostics` case.
- ~~**`--package` with a bare pattern resolves against the working directory.**~~
  **Settled: documented, not changed.** The two rules — arguments win over
  `--package`, an existing directory is a directory and anything else is a
  pattern — are now spelled out in `analyze --help` (with a worked example of
  each shape), in `cover --help`, in both `--package` usage strings, on
  `resolveTarget`, and in the README's "Choosing what to analyze" table.

## 2. Our own grade: 68% (68/100)

Not a defect — the PRD says to expect a mediocre grade and not to soften
defaults. The gap barely moves at looser strictness, so it is
genuinely missing tests rather than tests filed in the wrong place. The 32
untested split: 12 in `analysis`, 12 in `cli`, 3 in `coverage`
(`buildPage`, `writeBoundary`, `writeSourceByte` — each exercised through
`Render` and `annotate`, none named by a test), 3 in `cmd`, 2 in `config`. If it
is worth raising, in order of honesty:

1. **Analyzer internals** (12 functions) — the `*collector` methods,
   `collectDeclarations`, `loadPackages`, `implementingMethod`,
   `recordValueSpec`. `TestCorpus` executes every one of them hundreds of times
   and asserts nothing about any of them in isolation, which is the tool's own
   thesis pointed at itself. Directly testable with the `checkSource` helper
   already in `declarations_internal_test.go`; tests belong in
   `references_internal_test.go`, which is already the right slot.
2. **CLI and HTML rendering** (`renderText`, `writeWarnings`, `buildPage`,
   `writeBoundary`, `writeSourceByte`) — easy, real.
3. **Cobra constructors and lifecycle** (9 functions) — `newRootCommand`,
   `bootstrap`, `shutdown`, `Execute`. Testable but low value; this is where an
   ignore directive with a reason may be the honest answer.
4. **Inherited template code** (5 functions) — `cmd/main.run`, the config
   builders, `envVarOptions`, `NewPillars`.

## 3. Smaller follow-ups — two done, two standing by choice

- ~~No fixture for a package whose only test file is an external one.~~ **Done:
  `testdata/external_only_unexported`.** 47 fixture directories now. The external
  test package cannot legally name an unexported function, so it is reported at
  every level including `any` — no position on the dial can rescue a test that
  would not compile, and the fix is a second internal test file. The fixture
  carries that reasoning in its doc comment, where someone hitting the report
  will find it, and points at `internal_and_external` for the fix.
- ~~No benchmark for the §3.6 latency budget.~~ **Done: `make bench`
  (`scripts/bench.sh`), `BenchmarkAnalyze` plus `BenchmarkLoadPackages` and
  `BenchmarkCollect` to split the total.** On an M4 Max with a warm build cache,
  10 iterations each:

  | Target | `Analyze` | of which loading | of which collecting |
  |---|---|---|---|
  | one small package (`testdata/simple`) | 169 ms | 168 ms | 0.004 ms |
  | interface dispatch (`testdata/interface_single_impl`) | 166 ms | 169 ms | 0.006 ms |
  | this module, 7 packages with tests | 509 ms | 508 ms | 0.69 ms |

  **The budget is spent entirely by the go toolchain.** There is a ~168 ms floor
  that is `go list` shelling out, and everything above it is parsing and
  type-checking; the two passes this repo owns are 0.001%–0.14% of wall clock, and
  the sole-implementer search does not register at all. So §3.6's assumption
  holds, but not for the reason it gives — the heuristic is not cheap *relative
  to* an RTA callgraph, it is invisible next to the load either way. **What this
  says about RTA:** the objection to it is not the graph construction, it is that
  RTA needs whole-program type information — `NeedDeps`/`NeedImports` and every
  dependency type-checked — which lands squarely on the 99.9% side of this table.
  Measure the load mode, not the algorithm, before revisiting.
- `cover` loads packages twice: once to analyze, once (cheaply, `NeedName` and
  file lists only) to resolve the profile's package paths to files. Threading the
  first load's file list out of `analysis` would save the second, at the cost of
  widening that package's surface for one caller. Not worth it yet.
- `internal/coverage/testdata/simple.out` is pinned to the line and column
  numbers in `analysis/testdata/simple/main.go`. Editing that fixture breaks the
  coverage tests loudly, which is intended, but the profile has to be regenerated
  by hand: `go test -covermode=count -coverprofile=... .` inside it. The
  template's `.gitignore` ignores `*.out` and `coverage.*`, which silently
  excluded both that profile and `internal/coverage/coverage.go`; two negations
  now keep them tracked. Any future file named that way needs the same.
