# tarp

`tarp` finds Go functions that have no direct unit test.

## The problem

`go test -cover` measures statement coverage, which cannot distinguish a
function that was *tested* from one that was merely *executed*. Take this
package:

```go
package simple

func A() string { return "A" }
func B() string { return "B" }
func C() string { return "C" }

func wrapper() { A(); B(); C() }
```

and these tests:

```go
func TestA(t *testing.T)       { A() }
func TestC(t *testing.T)       { C() }
func TestWrapper(t *testing.T) { wrapper() }
```

`go test -cover` reports **100%**. But `B` has no test of its own — it is only
executed on the way to somebody else's assertion. Delete the `B()` call from
`wrapper` and coverage silently drops, because the coverage was never real.

`tarp` reports what actually happened:

```
$ tarp analyze --package ./simple
Functions without direct unit tests:
in main.go:
	B on line 7

Grade: 75% (3/4 functions)
```

### The opinion this encodes

> Testing primarily asserts behavior. If a behavior is worth codifying into a
> function, it is worth asserting that function's behavior — in a test written
> for it, in a consistent style, in a predictable file location.

The tool's job is to measure against that ideal, not to be satisfiable. Expect a
mediocre grade on a real codebase; that is the point.

### What a reference does not prove

The ideal above is about assertions. The measurement is not: what tarp checks is
that a `TestXxx` body **references** the function. It never looks at what the
test does with it.

So a test that asserts nothing scores as tested — including every one in the
example above, none of which has an assertion in it. So does a test that calls
the function with inputs reaching only its first guard clause:

```go
func TestParse(t *testing.T) { _, _ = Parse(nil) }   // returns on the nil check; graded 1/1
```

tarp and `go test -cover` are wrong in opposite directions. Coverage credits a
function nobody tested, because somebody else's test executed it. tarp credits a
test that checks nothing, because it named the function. Neither subsumes the
other, and neither is a substitute for reading the test:

|                      | `go test -cover` says | `tarp` says |
| -------------------- | --------------------- | ----------- |
| `B` in the example above, executed only by `wrapper`'s test | covered | untested |
| `func TestB(t *testing.T) { B() }`, no assertion | covered | tested |
| A test asserting one branch of ten | partly covered | tested |

A function green in both has a test written for it *and* the statements to show
for it. That is the pair worth running in CI; either alone is a metric with a
known way to be satisfied cheaply.

## Quickstart

Requires **Go 1.26+**. [Docker](https://www.docker.com/) is used for linting and
shellcheck.

```bash
make setup                  # create artifacts/ and download the module cache
make build                  # compile everything, produce artifacts/tarp
./artifacts/tarp analyze --package .
```

## Usage

```
tarp analyze [packages] [--package=.] [--strictness=file|package|any]
                        [--exclude=<glob>] [--exclude-function=<glob>]
                        [--fail-on-found] [--min-score=N]
                        [--format=text|json|sarif|markdown]
tarp cover --html=<profile> [packages] [--package=.] [--output=<file>]
                            [--strictness=file|package|any]
                            [--exclude=<glob>] [--exclude-function=<glob>]
```

| Flag              | Default | Meaning                                                                  |
| ----------------- | ------- | ------------------------------------------------------------------------ |
| `--package`, `-p` | `.`     | A directory (expanded to `./...` beneath it) or a go/packages pattern — see below |
| `--strictness`    | `file`  | How close a reference has to be to count — see below                     |
| `--exclude`, `-x` | —       | Glob matched against each file's path, relative to the module root; repeatable — see below |
| `--exclude-function`, `-X` | — | Glob matched against declaration names, such as `*.MarshalJSON`; repeatable |
| `--fail-on-found` | `false` | Exit non-zero when anything is reported, without printing an error line   |
| `--min-score`, `-m` | `0`   | Exit non-zero when the grade falls below this percentage (0 to 100; `0` never fails) |
| `--format`, `-f`  | `text`  | `text`, `json`, `sarif`, or `markdown` — see below. Warnings always go to stderr |
| `--json`, `-j`    | `false` | Shorthand for `--format=json`                                            |
| `--html`          | —       | `cover` only: the profile from `go test -coverprofile` to render          |
| `--output`, `-o`  | stdout  | `cover` only: write the report to this file instead                       |

Every default in that table can be moved into a `.tarp.yaml` at the project
root, and a flag typed here overrides it — see [Configuration](#configuration).

### Choosing what to analyze

Both subcommands take packages the same way, and there are exactly two rules:
**arguments win over `--package`**, and **a `--package` value that names an
existing directory is analyzed as a directory; anything else is a pattern.**

| Invocation                     | Loaded from | Patterns          |
| ------------------------------ | ----------- | ----------------- |
| `tarp analyze`                 | `.`         | `./...`           |
| `tarp analyze ./cmd/... ./io`  | `.`         | `./cmd/... ./io`  |
| `tarp analyze -p ./internal`   | `./internal`| `./...`           |
| `tarp analyze -p ./cmd/...`    | `.`         | `./cmd/...`       |
| `tarp analyze -p example.com/mod/...` | `.`  | `example.com/mod/...` |

A directory becomes the directory the go command runs in, and everything beneath
it is analyzed — `-p ./internal` and `-p ./internal/...` therefore mean the same
thing. Anything that is not an existing directory is handed to `go/packages` as
written and resolved against the *working* directory, which is what makes module
paths like `example.com/mod/...` work.

The target has to sit inside a Go module. Packages load in module mode, so a
directory with no `go.mod` in it or any parent cannot be listed, and tarp says so
rather than passing the go command's "directory prefix . does not contain main
module" along:

```
$ tarp analyze --package /tmp/scratch
Error: analyzing /tmp/scratch: no go.mod in that directory or any parent, and
packages load in module mode: run `go mod init` there first
```

### Strictness

The dial only ever weakens. The default is the strongest claim the tool can
make.

| Level     | Rule                                                                                     |
| --------- | ---------------------------------------------------------------------------------------- |
| `file`    | `Bar` in `foo.go` must be referenced inside a `TestXxx` body in `foo_test.go` **or** `foo_internal_test.go` |
| `package` | Referenced inside a `TestXxx` body in any `_test.go` in the package                       |
| `any`     | Referenced anywhere in any `_test.go`, test helpers included                              |

`foo_test.go` and `foo_internal_test.go` are **both** accepted at `file`, and
that pairing is load-bearing rather than a convenience: Go forbids
`package foo_test` from referencing unexported identifiers in `package foo`, so
when the external test file is the one that exists, a second internal test file
is the only place an unexported function can be tested from.

A reference counts whether or not it is syntactically a call. Method values,
method expressions, functions passed as arguments, deferred closures,
range-over-func iterators, and generic instantiations are all references,
because the question asked is "what does this identifier resolve to?" rather
than "is this a call?".

That question is answered by resolution alone, which is what makes the dial
cheap and predictable — and what
[a reference does not prove](#what-a-reference-does-not-prove): none of these
levels inspects what the test does after naming the function.

### What is never reported

`init`, `main` in `package main`, anything in a file marked
`// Code generated ... DO NOT EDIT.`, everything declared in a `_test.go`, and
anything carrying the ignore directive:

```go
//tarp:ignore -- talks to a live payment processor; covered by the e2e suite
func Charge() error { ... }
```

The reason is required. A bare `//tarp:ignore` exempts nothing and earns a
warning on stderr, because an escape hatch that costs nothing to use is just a
way to make the score go up.

For a whole tree of them — generated code, mocks, a vendored directory — see
[Configuration](#configuration): `exclude.paths` and `exclude.functions` in
`.tarp.yaml` withhold declarations by pattern, with the same verdict the
directive gives.

### Methods nothing can name

A method a framework reaches by reflection will be reported no matter how well
it is tested, because there is no reference to find. `MarshalJSON` is the
canonical case: the test calls `json.Marshal(value)`, and the string
`MarshalJSON` appears nowhere in it. `String`, `driver.Valuer`, `sort.Interface`,
and any interface satisfied for somebody else's benefit read the same way.

This is the tool being correct, not blind — it reports what is true of the
source. The convention is a directive whose reason names the test that does the
asserting, which is what tarp does to itself in `internal/analysis/report.go`:

```go
//tarp:ignore -- reached by reflection through json.Marshal, so no test can name it; asserted by TestReportMarshalJSON
func (r Report) MarshalJSON() ([]byte, error) { ... }
```

That keeps the exemption auditable: the claim is checkable by opening the named
test, and it goes stale loudly if the test is ever deleted.

#### The errors trio is reported for a different reason

`Is`, `As`, and `Unwrap` are also reported when a test only ever reaches them
through `errors.Is` or `errors.As`, and the reasoning above is *not* why. Their
method set is statically knowable rather than found by reflection, and a test
asserting `errors.Is(err, ErrSentinel)` is not incidentally satisfying an
interface for somebody else's benefit — the truth of that assertion is what the
package's own `Is` returns.

They are reported anyway because the identifier in the source resolves to
`errors.Is`, and getting from there to the method means connecting the *value*
passed in to the type declaring it. That is dataflow, which the analyzer does
not do. The cheap alternative — letting any `errors.Is` in a test absolve every
`Is`/`As`/`Unwrap` in the package — over-credits in exactly the direction
`go test -cover` already does, so the deliberate answer is to report them and
take the directive:

```go
//tarp:ignore -- the errors walk reaches this; asserted by TestExhausted via errors.Is
func (e *ExhaustedError) Is(target error) bool { ... }
```

`internal/analysis/testdata/errors_dispatch` pins this behaviour, and the
decision is written next to the rule in `internal/analysis/references.go`.

### In CI

There are two gates, and both are off by default:

```bash
tarp analyze --package ./... --fail-on-found   # nothing may be reported
tarp analyze --package ./... --min-score 50    # the grade may not fall below 50%
```

`--fail-on-found` exits 1 having already printed the report, with no `Error:`
line stapled underneath it.

`--min-score` is the gate to reach for on an existing codebase, where demanding
a clean report on day one means turning the check off again by Friday. It exits
1 the same way, and adds one line on **stderr** saying what the grade was
measured against — the report says `Grade: 44%`, but only the flag knows that 50
was required:

```
score 44% is below the required minimum of 50%
```

A minimum outside 0–100 is rejected before any package is loaded, so a typo
costs a CI runner nothing and does not become a build that fails forever for no
visible reason. `--fail-on-found` is the strictly stronger gate — a score below
any minimum implies something was reported — so when both are set and both trip,
it wins and the score line is left unsaid.

`--json` gives a stable shape to parse, on stdout, unaffected by either gate:

```json
{
  "strictness": "file",
  "untested": [{"package": "simple", "file": "main.go", "name": "B", "line": 7}],
  "warnings": [],
  "declared": 4,
  "tested": 3,
  "score": 75
}
```

Output is deterministic — sorted by file, then declaration line — and color is
dropped when stdout is not a terminal, when `NO_COLOR` is set, or when
`TERM=dumb`.

### Grading a whole module

The default output lists functions, which stops being readable somewhere around
the second page. `--format markdown` grades one row per package instead:

```bash
tarp analyze --package ../platform-go --format markdown
```

```markdown
| Package | Score | Tested | Declared |
| --- | ---: | ---: | ---: |
| `github.com/primandproper/platform-go/v10/analytics/config` | 84% | 11 | 13 |
| `github.com/primandproper/platform-go/v10/analytics/noop` | 100% | 5 | 5 |
| `github.com/primandproper/platform-go/v10/audit` | 55% | 43 | 77 |
| **Total** | **61%** | **2684** | **4371** |

Graded at `file` strictness.
```

Rows are sorted by import path and always add up to the total. **The import
path is the identity, not the package clause** — platform-go has 48 packages
named `config` and 27 named `noop`, so grouping on the name would collapse 111
of its 281 rows into each other and hide which one needs the work.

Paste it into a pull request, a `$GITHUB_STEP_SUMMARY`, or a README.

### SARIF

```bash
tarp analyze --package ./... --format sarif > tarp.sarif
```

SARIF is the interchange format for static analysis findings — what SARIF
viewers, GitHub code scanning, Jenkins' Warnings NG, SonarQube, and the
`sarif-tools` CLI all read. Each untested function becomes one result under the
rule `tarp/untested-function`.

Emitting it needs no entitlement. Only *uploading* it into GitHub code scanning
does, which is why it is an output format rather than something the Action
presumes. Plenty of consumers never touch GitHub with it:

| Consumer | What you get |
| --- | --- |
| VS Code's SARIF Viewer | Click through findings in the Problems pane |
| `sarif-tools diff` | Baseline a legacy codebase; see only what is new |
| `sarif-fmt` | Readable terminal output |
| Jenkins Warnings NG / SonarQube | Native ingest |
| `actions/upload-artifact` | Just download the file |

Two things it does that the JSON cannot:

- **Locations are stated against a declared base.** `%SRCROOT%` is the module
  root, so a document produced on a runner resolves correctly on a laptop — and
  paths are right even when a subdirectory was analyzed.
- **Findings carry a fingerprint keyed on the function's name, not its
  position**, so a declaration moving down the file is the same finding rather
  than a new one. That is what lets a consumer dismiss something and have it
  stay dismissed.

SARIF carries findings, not scores, so the grade rides in the run's property
bag. `--min-score` is unaffected by the format and remains how a build fails.

### The coverage view

```bash
go test -coverprofile=coverage.out ./...
tarp cover --html=coverage.out --package ./... -o coverage.html
```

`cover` renders the page `go tool cover -html` renders — same layout, same file
picker, same spans in the same places — with the green split in two:

| Color      | Meaning                                                              |
| ---------- | -------------------------------------------------------------------- |
| **red**    | Never ran                                                            |
| **yellow** | Ran, in a function no test names directly                            |
| **green**  | Ran, in a function a `TestXxx` body references                       |
| **grey**   | Ran, in a declaration tarp does not grade (`init`, `main`, generated, ignored) — or in a package that was not analyzed |

Yellow is the whole point: it is the code `go test -cover` calls covered and this
tool calls untested. On the example above, the file reads 100% covered and 3/4
tested, and `B` is the yellow one.

The report goes to stdout unless `--output` names a file, and no browser is
opened. The profile's packages are analyzed exactly as `analyze` analyzes them,
so `--package` and `--strictness` mean the same thing here. The grade in the
header covers the files the profile describes, so it can differ from `tarp
analyze`'s when the profile does not reach every package.

## How it works

`golang.org/x/tools/go/packages` loads each package with `Tests: true`, which
yields four variants: the package itself, the package plus its internal tests,
the external test package, and the synthesized test binary (which is skipped —
it references every `TestXxx` and would otherwise credit every test to itself).

Every identifier in a `TestXxx` body is looked up in `TypesInfo.Uses`; the ones
resolving to a function declared in the package under test are recorded.
Functions are keyed on their **declaration position**, never on `*types.Func`
identity, because the same function has different object pointers in different
variants of its package.

Two bounded extensions to lexical scope:

- **Package-level test tables.** A reference from a `TestXxx` body to a
  package-level var declared in a test file follows one hop into that var's
  initializer. One hop, test files only, var initializers only — following calls
  arbitrarily would quietly collapse `file` mode into `any`.
- **Interface dispatch.** `i.Do()` resolves to `Iface.Do`, since that is the
  static type at the call site. When exactly one named type in the package
  implements the interface, its method gets the credit; when two or more do,
  none of them does, because there is no honest way to say which one ran.

## Common commands

```bash
make format     # imports (gci), field/tag alignment, gofmt -s
make lint       # golangci-lint (Docker) + shellcheck (Docker)
make test       # go test -shuffle -race -vet=all -failfast (excludes cmd)
make bench      # benchmarks; a run is ~99.9% go/packages loading (see CLAUDE.md)
make build      # compile all packages + build the binary with version metadata
make release    # cross-compile the release archives into artifacts/release
```

## GitHub Action

```yaml
- name: Check tarp coverage
  uses: primandproper/tarpaulin@v0.1.0
  with:
    strictness: file
    failure_score_threshold: 50
```

It downloads a prebuilt binary for the runner (verifying its checksum), runs
`tarp analyze --json`, annotates the untested functions inline on the pull
request, writes the report to the job summary, and exits with tarp's own code.

| Input                     | Default | Meaning                                                          |
| ------------------------- | ------- | ---------------------------------------------------------------- |
| `version`                 | pinned  | Which tarp release to run                                         |
| `package`                 | —       | A directory or a go/packages pattern, same two rules as the CLI   |
| `strictness`              | —       | `file`, `package`, or `any`                                       |
| `config`                  | —       | Path to a config file; empty means "find the repository's own"     |
| `failure_score_threshold` | `0`     | Fail when the grade drops below this percentage; `0` never fails  |
| `fail_on_found`           | `false` | Fail when anything at all is reported; strictly stronger          |
| `working-directory`       | `.`     | Where to run tarp from                                            |
| `annotate`                | `true`  | Emit inline annotations                                           |
| `max_annotations`         | `10`    | GitHub renders at most 10 warnings per step; see below            |
| `summary`                 | `true`  | Write the report to the job summary                               |
| `sarif_output`            | —       | Also write a SARIF document to this path; you decide what to do with it |

Outputs `score`, `declared`, `tested`, `untested_count`, and `report` (the path
to the raw JSON), so later steps can use the numbers:

```yaml
- id: tarp
  uses: primandproper/tarpaulin@v0.1.0
- run: echo "graded ${{ steps.tarp.outputs.score }}%"
```

A repository with a [`.tarp.yaml`](#configuration) needs none of these — the
action runs tarp in `working-directory`, and tarp finds the file itself. That is
why the inputs above default to empty rather than to tarp's own defaults: an
input is passed as a flag, a flag outranks the config file, and an action
quietly passing `--strictness file` on every run would mean a project's
`strictness: package` never took effect.

Four things worth knowing:

- **`version` is pinned, not floating.** A stricter analyzer arriving on its own
  would break a consumer's CI on a day they changed nothing.
- **Annotations stay warnings even when the build fails.** Promoting them to
  errors paints every untested function in the Files Changed view, including the
  ones the pull request never touched. The gate is what fails the build.
- **Only the first 10 are annotated**, because that is what GitHub renders per
  step. The rest are not dropped quietly: the count is logged as a notice, and
  the job summary always carries the full list.

### SARIF from the Action

`sarif_output` writes the document and stops there — what happens to it is
yours to decide, because uploading into code scanning needs an entitlement this
action does not assume. Keep it as an artifact:

```yaml
- id: tarp
  uses: primandproper/tarpaulin@v0.1.0
  with:
    sarif_output: tarp.sarif
- uses: actions/upload-artifact@v4
  if: always()
  with:
    name: tarp-sarif
    path: ${{ steps.tarp.outputs.sarif }}
```

Or, if your repository has code scanning — public repos, or private ones with
GitHub Advanced Security — send it to the Security tab instead:

```yaml
- uses: github/codeql-action/upload-sarif@v3
  if: always()
  with:
    sarif_file: ${{ steps.tarp.outputs.sarif }}
```

Both need `if: always()`, or a tripped gate skips the upload on exactly the runs
worth uploading. Asking for SARIF costs a second analysis pass, so it is off by
default.

## Releases

Publishing a GitHub release triggers `.github/workflows/release.yaml`, which
runs the test suite, cross-compiles six targets, and uploads them to the release
it was triggered by:

```
tarp_<tag>_{linux,darwin,windows}_{amd64,arm64}.{tar.gz,zip}
checksums.txt
```

Each archive holds the binary, `LICENSE`, and `README.md`, flat. The tag is used
**verbatim** in the asset name — no leading `v` is stripped — so a consumer can
interpolate a version straight into the URL without reproducing a transformation
here. `checksums.txt` is SHA-256 over exactly the archives that run produced.

`make release` runs the same script locally as a dry run, defaulting `VERSION`
to `git describe`; pass `VERSION=v1.2.0` to pin it. The binary knows which
release it is — `tarp version` reports it, injected at link time alongside the
commit metadata.

## Layout

```
cmd/main/                      # entrypoint: signal-cancellable context -> cli.Execute
internal/analysis/             # the analyzer: load, declarations, references, strictness
internal/analysis/testdata/    # the fixture corpus, one directory per case
internal/coverage/             # the cover profile -> annotated HTML renderer
internal/cli/                  # cobra root command, analyze/cover subcommands, output
internal/config/               # assembles observability.Config and builds the pillars
version/                       # build metadata, injected via -ldflags by scripts/build.sh
scripts/                       # build/format/lint/test/shellcheck helpers
```

## Configuration

Everything above can be a flag. Nobody wants to type it twice, so a project puts
it in a file at its root:

```yaml
# .tarp.yaml
analyze:
  strictness: package
  minScore: 80
  failOnFound: false
  format: text
  package: ./...

exclude:
  paths:
    - internal/generated/**
    - "**/*_gen.go"
    - mocks
  functions:
    - "*.MarshalJSON"
```

`.tarp.yaml`, `.tarp.yml`, `.tarp.json`, and `.tarp.toml` are all read, with the
same keys in each; two of them in one directory is an error rather than a
silent winner. tarp looks for one in the working directory and then upwards,
stopping at the module root — so `tarp analyze` run from `internal/cli` means
what it means from the root, and a stray `.tarp.yaml` in a home directory never
governs a module underneath it. `--config <path>` names one explicitly, in any
of the three formats, and skips the search.

A file says what it wants changed; everything it omits keeps the built-in
default.

### What wins

```
defaults  <  config file  <  flags  <  TARP_ environment variables
```

A config file is what the project decided. A flag is what this run wants
instead — and only a flag somebody actually typed counts, so a default sitting
in `--help` never quietly outranks the file. An environment variable is what the
machine running it insists on, which is how CI overrides a checked-in file
without editing it:

```bash
TARP_ANALYZE_MIN_SCORE=90 tarp analyze     # tighter than .tarp.yaml asked for
TARP_EXCLUDE_PATHS=internal/generated/**,mocks   # lists are comma-separated
```

Every setting has a variable, spelled `TARP_` then the section then the field:
`TARP_ANALYZE_STRICTNESS`, `TARP_ANALYZE_FAIL_ON_FOUND`, `TARP_EXCLUDE_FUNCTIONS`.

### Excluding files and functions

`exclude.paths` are gitignore-shaped globs, matched against each file's path
relative to the module root:

| Pattern                 | Matches                                              |
| ----------------------- | ---------------------------------------------------- |
| `internal/generated/**` | that directory and everything under it               |
| `internal/generated`    | the same thing — naming a directory means its contents |
| `**/*_gen.go`           | any generated file, at any depth                     |
| `mocks`                 | any path segment called `mocks`, anywhere            |
| `zz_generated.go`       | any file with that name, anywhere                    |
| `/main.go`              | that file, in the module root only                   |

A pattern with no `/` matches any single segment, which is what makes a bare
name reach a file or a directory anywhere in the tree; a leading `/` anchors it
to the root. `*` stops at a separator and `**` does not.

`exclude.functions` are globs matched against the name as the report renders it
— `Foo`, `Thing.Method`, `(*Thing).Method` — and against the bare method name on
its own, so `String` reaches `(*Thing).String` without a wildcard. That is the
list for the methods [nothing can name](#methods-nothing-can-name): `String`,
`MarshalJSON`, `Value`, and the rest of the interfaces satisfied for somebody
else's benefit.

An excluded declaration is **withheld, not failed** — exactly like
`//tarp:ignore`, it leaves both sides of the grade rather than counting against
it. The two are for different scales: the directive answers for one declaration
where the reader is already looking, and demands a reason there; a config file
answers for a whole tree at once, where the comment explaining the pattern sits
next to the pattern. Reach for the directive when the reason is about *that*
function.

`--exclude` and `--exclude-function` are the same lists on the command line,
repeatable. They **replace** the configured list rather than adding to it, which
is the only way to say "grade the generated code too, just this once".

### Observability

The CLI also inherits two settings from its platform-go scaffolding:

| Flag             | Environment variable | Default | Values                           |
| ---------------- | -------------------- | ------- | -------------------------------- |
| `--log-level`    | `TARP_LOG_LEVEL`     | `info`  | `debug`, `info`, `warn`, `error` |
| `--service-name` | `TARP_SERVICE_NAME`  | `tarp`  | any string                       |

Logs are structured slog written to stdout, and nothing is emitted at the
default `info` level, so `tarp analyze --json` stays machine-parseable. The same
config file carries an `observability` section for the rest of it; the JSON
files in `config/` are complete examples, generated by `make configs`.

## History

The original `tarp` (2017) was renamed `blanket` (2018) and is now rewritten.
The old implementation hand-rolled type inference across ~150 lines because it
had no type information, and asked "is this a call?" — a question with an
unbounded number of syntactic answers. Everything that made it hard is free
under `go/types`; the fixtures that were expensive to pass in 2017 are kept in
the corpus precisely because they once were.

The one idea carried forward whole is the three-color coverage view, which is
what `tarp cover --html` renders — rebuilt against today's `go tool cover`
output rather than the 2017 fork of it.

## License

[AGPL-3.0](./LICENSE).
