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
                        [--fail-on-found] [--json]
tarp cover --html=<profile> [packages] [--package=.] [--output=<file>]
                            [--strictness=file|package|any]
```

| Flag              | Default | Meaning                                                                  |
| ----------------- | ------- | ------------------------------------------------------------------------ |
| `--package`, `-p` | `.`     | A directory (expanded to `./...` beneath it) or a go/packages pattern — see below |
| `--strictness`    | `file`  | How close a reference has to be to count — see below                     |
| `--fail-on-found` | `false` | Exit non-zero when anything is reported, without printing an error line   |
| `--json`          | `false` | Emit the report as JSON on stdout; warnings still go to stderr           |
| `--html`          | —       | `cover` only: the profile from `go test -coverprofile` to render          |
| `--output`, `-o`  | stdout  | `cover` only: write the report to this file instead                       |

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

### In CI

```bash
tarp analyze --package ./... --fail-on-found
```

`--fail-on-found` exits 1 having already printed the report, with no `Error:`
line stapled underneath it. `--json` gives a stable shape to parse:

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
make build      # compile all packages + build the binary with version metadata
```

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

The CLI inherits two settings from its platform-go scaffolding, via flags or
environment variables:

| Flag             | Environment variable | Default | Values                           |
| ---------------- | -------------------- | ------- | -------------------------------- |
| `--log-level`    | `TARP_LOG_LEVEL`     | `info`  | `debug`, `info`, `warn`, `error` |
| `--service-name` | `TARP_SERVICE_NAME`  | `tarp`  | any string                       |

Logs are structured slog written to stdout, and nothing is emitted at the
default `info` level, so `tarp analyze --json` stays machine-parseable.

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
