# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## Project Overview

`github.com/primandproper/tarpaulin` — **tarp**, a tool that finds Go functions with no direct unit
test. Built on the [`platform-go`](https://github.com/primandproper/platform-go) application template
(hence the observability scaffolding). Go 1.26.

The module path is `tarpaulin`; every user-visible name is `tarp` — the binary, the cobra `Use:`, the
`//tarp:ignore` directive, and the `TARP_` env-var prefix.

`go test -cover` measures statement coverage, which cannot distinguish a function that was tested
from one that was merely executed. tarp asks the stricter question — does a `TestXxx` body actually
reference this function? — and grades the package on the answer. The semantic decisions and the
reasoning behind them live in the code comments next to what they govern, and in the fixture corpus;
read the fixture for a rule before changing it.

## Layout

- `cmd/main/main.go` — thin entrypoint: signal-cancellable context → `cli.Execute`.
- `internal/analysis/` — the analyzer. `analysis.go` (the `Analyze` entrypoint and `Config`),
  `load.go` (go/packages loading, variant filtering, diagnostics), `declarations.go` (what is held
  accountable, and the always-exclusions), `exclude.go` (the configured exclusions: `Exclusions`
  and the gitignore-shaped matcher behind it), `references.go` (what a test body resolves to, the
  one-hop rule, interface dispatch), `strictness.go` (the dial), `report.go` (sorting, score, JSON
  shape).
- `internal/analysis/testdata/` — the fixture corpus, one directory per semantic case. See
  Testing below; these are *not* formatted or linted, deliberately.
- `internal/coverage/` — the renderer behind `tarp annotate`. `coverage.go` (the `Render`
  entrypoint, `Config`, and the per-file totals), `sources.go` (turning the package paths a profile names into files on disk,
  from the analysis' own file list where it can and a `NeedName`-only load where it cannot),
  `annotate.go` (the boundary walk and the four verdicts), `html.go` (the page template).
  `testdata/simple.out` is a real profile over `analysis/testdata/simple`, pinned to that fixture's
  line and column numbers.
- `cmd/tools/codegen/configs/` — codegen tool behind `make configs`: builds each environment's
  `*config.Config` as a real, typed Go object (`environments.go`), validates it, and renders it to
  `config/<env>.json` via `config.Render`. The checked-in JSON is a projection of these builders — edit
  the Go, never the JSON, then re-run `make configs`.
- `config/` — generated per-environment config files (`localdev.json`, `production.json`); committed so
  they stay reviewable, and loadable at runtime via `--config`.
- `internal/cli/` — cobra root command, the `analyze` and `annotate` subcommands, terminal output,
  observability bootstrap + shutdown. `settings.go` holds the flags both subcommands share and
  `resolveSettings`, which reconciles the config file, the flags, and the environment into what one
  invocation actually runs with.
- `internal/config/` — the whole configuration: the `analyze` defaults, the `exclude` lists, and the
  `observability.Config` the pillars are built from (slog logging + noop tracing/metrics/profiling by
  default). See `Config.NewPillars` for the upgrade path to real telemetry. `Load` overlays
  `TARP_`-prefixed environment variables on the flag/default-seeded config; `file.go` holds
  `Discover` (the walk up to the module root looking for `.tarp.{yaml,yml,json,toml}`) and
  `LoadFromFile`, which decodes one of those over the built defaults and then overlays the same
  environment variables. `Render` goes the other way: it validates typed `Config` objects and writes
  them to disk (see `make configs`).
- `version/` — build metadata (`Version`/`CommitHash`/`BuildTime`/`CommitTime`), injected via
  `-ldflags` by `scripts/build.sh`. `Version` is the release tag, which is what the GitHub Action
  pins and what a bug report cites.
- `internal/sarif/` — the SARIF 2.1.0 renderer behind `analyze --format=sarif`.
- `action.yml` + `.github/workflows/release.yaml` + `scripts/release.sh` — the composite GitHub
  Action and the release pipeline that builds the binaries it downloads.

## Common Commands

```bash
make setup          # Create artifacts dir + download the module cache
make configs        # Render config/<env>.json from the real Go objects in cmd/tools/codegen/configs
make build          # Compile all packages, then build artifacts/tarp with version metadata
make run ARGS="version"   # go run the CLI with arguments
make format         # Format all Go code (imports, field alignment, tag alignment, gofmt)
make lint           # Run golangci-lint (Docker) + shellcheck
make test           # Run tests (race detector, shuffle, failfast); excludes cmd packages
make bench          # Run benchmarks (no race, no shuffle); BENCH_ARGS="-count 3" to pass flags
```

Run a single test:
```bash
go test -run TestName ./internal/config/...
```

Linting runs in Docker (`golangci/golangci-lint` image), against a copy of the tree rather
than a bind mount — see Linting below. Formatting runs locally via `go tool` with
`gci`, `goimports`, `fieldalignment`, `tagalign`, and `gofmt` (declared in the `tool` block of go.mod).

This repository does **not** vendor dependencies (platform-go's dependency tree is large); builds and
tests run against the module cache. Vendoring targets (`make vendor` / `make revendor`) exist for
consumers who want them.

`make format` skips `testdata/` — see Testing. The Go-wildcard-driven targets (`test`, `lint`,
`go fix`, fieldalignment, tagalign) skip it for free, because `./...` never matches a `testdata`
directory.

## Import Ordering

Import ordering uses `gci` with four sections, separated by blank lines:

1. Standard library
2. `github.com/primandproper/tarpaulin` (this module)
3. `github.com/primandproper` (org-level packages, including platform-go)
4. Everything else (third-party)

The Makefile `THIS` variable must be the full module path (`github.com/primandproper/tarpaulin`)
because `format_imports.sh` runs `dirname` on it to derive the org-level prefix.

## Testing

- Tests use `shoenig/test`: `test` for non-fatal assertions, `must` for fatal ones. Both take
  `(t, expected, actual)` and annotate failures via `test.Sprintf` / `must.Sprintf` settings rather
  than `...f` variants.
- Tests call `t.Parallel()` by default. A test that calls `t.Setenv` or `t.Chdir` cannot be parallel
  *and neither can its parent*, so those live in their own top-level test functions.
- `make test` excludes `cmd` packages, so keep testable logic in `internal/` and `version/`.
- Test command: `CGO_ENABLED=1 go test -shuffle=on -race -vet=all -failfast`.
- Benchmarks live in `internal/analysis/benchmark_test.go` (end to end) and
  `benchmark_internal_test.go` (the load/collect split). They exist to keep the latency assumption
  honest: an analysis is ~99.9% go/packages loading, so a change that adds work to the *load mode*
  is the one to measure, not one that adds a pass over syntax already in memory.

### What an analysis actually costs

`make bench`. On an M4 Max with a warm build cache, 10 iterations each:

| Target | `Analyze` | of which loading | of which collecting |
|---|---|---|---|
| one small package (`testdata/simple`) | 169 ms | 168 ms | 0.004 ms |
| interface dispatch (`testdata/interface_single_impl`) | 166 ms | 169 ms | 0.006 ms |
| this module, 7 packages with tests | 509 ms | 508 ms | 0.69 ms |

**The budget is spent entirely by the go toolchain.** There is a ~168 ms floor that is `go list`
shelling out, and everything above it is parsing and type-checking. The two passes this repo owns
are 0.001%–0.14% of wall clock, and the sole-implementer search does not register at all.

**What this says about the deferred RTA callgraph:** the objection to it is not graph construction.
RTA needs whole-program type information — `NeedDeps`/`NeedImports` and every dependency
type-checked — which lands squarely on the 99.9% side of that table. Measure the load mode, not the
algorithm, before revisiting. `soleImplementation` in `references.go` is the cheap heuristic RTA
would replace, and `testdata/interface_two_impls` documents the case where it withholds credit.

### The corpus

`internal/analysis/testdata/<case>/` is one ordinary Go package per case. `TestCorpus` runs every
one of them at all three strictness levels; the semantics live in the levels where they differ.

Expectations are **markers in the fixture source**, not golden files, so they survive line drift and
read as documentation next to the code they describe:

```go
//tarp:want excluded                  // never counted at all (init, main, generated, ignored)
//tarp:want untested=file,package     // reported at those levels, credited at `any`
```

An unmarked declaration is expected to be counted and tested at every level. The harness derives the
expected declaration set syntactically (`go/parser` plus `build.Default.MatchFile` for build tags),
which keeps it independent of the analyzer it is checking.

Adding a case: create the directory, write the fixture, mark the expectations, and it is picked up
automatically. Error-path fixtures (`unparseable`, `no_go_files`, `broken_package`, `broken_test`)
are listed in `errorFixtures` and exercised by `TestAnalyzeDiagnostics` instead.

Because these fixtures are deliberately unformatted, deliberately broken, or pinned to exact line
numbers, `testdata/` must stay out of every formatter — `unparseable/main.go` does not parse, on
purpose, and `gofmt -l` walks the filesystem rather than Go's package wildcard, so it descends into
`testdata/` unless told not to.

**`scripts/go_files.sh` is the one place that decides which files the formatters see**, and
`scripts/format_golang.sh`, `scripts/format_imports.sh`, `scripts/goimports.sh`, and the `gofmt`
check in `.github/workflows/formatting.yaml` all take their list from it. It asks the Go toolchain
(`go list -e -f '{{.Dir}}' ./...`, then `find -maxdepth 1`) rather than writing out an exclusion
list, so `vendor/`, `testdata/`, and any `_`- or `.`-prefixed directory are skipped for the same
reason `go test ./...` skips them. That list used to be hand-written in each of those four places
and had already drifted (`*/vendor/*` in the scripts, `./vendor/*` in the workflow).

Two things about it are load-bearing. It **fails loudly rather than emitting an empty list** — an
out-of-sync `vendor/modules.txt` makes `go list` exit non-zero, and a formatter that quietly formats
nothing (or a CI check that quietly checks nothing) is worse than a stop. And its callers read it
**through a file, not `< <(...)`**, because process substitution discards the exit status of what it
runs, which is exactly how that empty list would have gone unnoticed.

### The coverage profile fixture

`internal/coverage/testdata/simple.out` is a real profile pinned to the exact line *and column*
numbers in `analysis/testdata/simple/main.go`. Editing that fixture breaks the coverage tests
loudly, which is intended — but the profile then has to be regenerated by hand:

```bash
cd internal/analysis/testdata/simple && go test -covermode=count -coverprofile=/path/to/simple.out .
```

The template's `.gitignore` ignores `*.out` and `coverage.*`, which silently excluded both that
profile and `internal/coverage/coverage.go` from git. Two negations now keep them tracked. **Any
future file named that way needs the same** — check `git status --ignored` if a new file mysteriously
never shows up.

## Reach for platform-go first

This repo is built on platform-go, and its packages are the default answer before the standard
library or a new dependency. Two apply throughout:

- **`platform-go/v10/encoding`** turns values into bytes. Prefer it to calling `json.Marshal`
  directly, so the content type stays one decision made in one place — `encoding.EncodeJSON` /
  `encoding.DecodeJSON` for a fixed wire format, `Encode`/`Decode` with a `ContentType` when it is
  configurable. It returns exactly what `json.Marshal` returns, with no trailing newline.
- **`platform-go/v10/errors`** constructs and wraps: `platformerrors.New`, `Wrap`, `Wrapf`, `Join`,
  and the platform sentinels (`ErrUnrecognizedInputValue` is the right one for a rejected enum or
  flag value — wrap it with the offending input). Import it as `platformerrors`; `errors.Is`,
  `errors.As`, and `errors.Unwrap` stay standard library and work on these values. Do not reach for
  `fmt.Errorf("...: %w", err)`.

There are exactly two deliberate exceptions in the tree, both commented at the call site:
`internal/config/render.go` uses `json.MarshalIndent` because the rendered config files are
committed to be read and `encoding` deliberately does not alter its marshaler's output (so it has no
indentation to offer); and `report_test.go` / `analysis_test.go` use `encoding/json` because what
they pin is that a `Report` satisfies `json.Marshaler` for any ordinary consumer.

Before adding a helper, check whether platform-go already has the package — `pointer`, `numbers`,
`files`, `random`, `identifiers`, `retry`, `panicking`, `reflection`, and `testutils` all exist, and
the list at the module root is worth a look.

## Analyzer conventions worth knowing

- **Key functions on declaration position, never on `*types.Func` identity.** The same function has
  different object pointers in `pkg`, `pkg [pkg.test]`, and `pkg_test [pkg.test]`; comparing pointers
  across variants produces silently wrong answers.
- **Skip the synthesized `<pkg>.test` main package.** It references every `TestXxx`, so leaving it in
  credits every test function to itself.
- **Never ask "is this a call?"** Ask what an identifier resolves to. That single choice is why
  method values, method expressions, defers, range-over-func, and generics need no special cases; any
  new feature that needs one is a sign the question drifted.
- **Normalize generics through `types.Func.Origin()`** so instantiations collapse to the generic.
- Sort everything that reaches output (`report.go`), and prefer precise diagnostics over the go
  command's own stdout (`load.go`).
- **Exclusions are withheld, not failed.** A declaration matching `Config.Exclude` leaves the report
  entirely, exactly as `//tarp:ignore` does, so it drops out of both sides of the grade. The
  patterns are compiled before anything is loaded (`newExclusion` in `analysis.go`), so a typo in a
  config file costs no package load, and path patterns are resolved against the **module root** so
  one file at the root means the same thing from every directory beneath it.
- `Function` carries `Path` (absolute) and `EndLine` alongside `File` and `Line`, both `json:"-"`.
  They exist for the coverage view: a cover profile names files by package path and blocks by line,
  so joining the two needs a file to open and a range to fall inside. Keep them out of the JSON —
  the wire shape is pinned verbatim in `analysis_test.go`.
- `Report.Sources` carries the load's own file list, keyed by import path, for the same reader: a
  load is ~99.9% of an analysis, so the thing worth handing a caller is the load it would otherwise
  repeat. `annotate` resolves the profile's package-relative names against it and loads only what
  the report cannot account for. It is one map because `Report` is passed by value to its own methods
  (`MarshalJSON` has to be reachable from a value) and gocritic holds that value to a size.

## CLI conventions worth knowing

- Observability logs are structured slog written to **stdout**. `version`, `analyze`, and `annotate`
  print to stdout and emit nothing at the default `info` level, so `tarp analyze --json` and
  `tarp annotate --profile` stay machine-parseable. Warnings go to **stderr** for the same reason.
- `annotate` renders into memory before writing: a report that fails halfway through should not
  land on disk over the last good one. It opens no browser, on purpose — `--output` or stdout.
- The root command sets `SilenceErrors`; `Execute` prints failures itself so that `--fail-on-found`
  can exit non-zero without stapling an `Error:` line under the report it just printed.
- Color is decided once in `internal/cli/color.go`: off unless stdout is a character device, and off
  regardless when `NO_COLOR` is set or `TERM=dumb`. No dependency, on purpose.
- The `--log-level` / `--service-name` persistent flags default from the `TARP_LOG_LEVEL` and
  `TARP_SERVICE_NAME` environment variables. The `--config` flag (default from
  `TARP_CONFIG_FILEPATH`) points at a JSON, YAML, or TOML config file; when set, `bootstrap` loads
  it and skips discovery, because "use this one" and "use whatever this project keeps" are different
  questions.
- **Configuration is layered: defaults < config file < flags < `TARP_`-prefixed environment
  variables.** A config file is what the project decided, a flag is what this run wants instead, and
  an environment variable is what the machine running it insists on — which is how CI overrides a
  checked-in file without editing it. The middle step works because cobra's `Flags().Changed` is
  what distinguishes a typed flag from one sitting at its default; without it every default would
  outrank the file and the file would do nothing. `resolveSettings` (and `resolveGates` for the
  analyze-only settings) is the one place this happens.
- Env vars follow platform-go's nested `envPrefix` tags, e.g. `TARP_OBSERVABILITY_LOGGING_LEVEL`
  and `TARP_ANALYZE_MIN_SCORE`. Give new `Config` fields `envPrefix`/`env` **and all three of
  `json`, `toml`, and `yaml`** so they participate in every loader equally: a `json` tag alone works
  for JSON and, case-insensitively, for TOML, but yaml.v3 falls back to the lower-cased field name,
  so `minScore` would silently bind nothing. Do not give them `envDefault` — caarlos0/env resets a
  field carrying one whenever its variable is unset, which would mean a config file could never
  supply that value at all.
- `LoadFromFile` decodes over an already-built `Config` rather than into an empty one, so a file
  says what it wants changed and keeps the default for everything else. The cost is that omission no
  longer means "unset": to turn something off, name it and give it the empty value.
- To enable real tracing/metrics/profiling, populate the sub-configs in `internal/config` and call
  `observability.Config.NewPillars`, or swap the noop constructors in `Config.NewPillars`.

## Where this repo points `//tarp:ignore` at itself

tarp grades itself, so the escape hatch had to be aimed inwards at some point. The line it is
drawn on: **a function whose whole body is a declaration is ignored with a reason naming what
drives it; a function with a branch, a guard, or a failure mode gets a test that names it.**

Ignored, therefore: the cobra constructors (`newRootCommand`, `newAnalyzeCommand`,
`newAnnotateCommand`, `newVersionCommand`) and the flag registrations they call
(`registerTargetFlags`, `registerAnalyzeFlags`), which declare flags and help text and are driven
end to end through cobra by the command tests; `cmd/main.run` and the config builders in
`cmd/tools/codegen/configs`, which sit in `cmd` packages `make test` excludes by design; and
`envVarOptions`, which returns opaque platform-go options whose effect `Load` and `LoadFromFile`
already assert.

Tested, therefore: `Execute`, `bootstrap`, `shutdown`, `(*application).log`, and
`Config.NewPillars`. `TestExecute` runs the real entrypoint over `os.Args` and the process's own
streams — see the `capture` helper in `root_test.go`, which is the only place in these tests that
cannot be handed a `bytes.Buffer`.

A new ignore needs a reason of the same kind: what asserts this instead, or why nothing can. "It
is hard to test" is not one.

## Linting

- ~46 linters enabled via `.golangci.yml` (golangci-lint v2 format).
- Formatters: `gci` and `gofmt` (configured in the `formatters:` section).
- Notable strictness: `errcheck` (with `check-blank` + `check-type-assertions`), `errorlint`,
  `gosec`, `forcetypeassert`, `unconvert`, `unparam`. Many are relaxed for `_test.go` files.
- `depguard` carries platform-go's ban list verbatim (testify, `pkg/errors`, `io/ioutil`,
  `math/rand` v1, `dgrijalva/jwt-go`). Use `shoenig/test` + `shoenig/test/must` for assertions and
  `matryer/moq` for mocks; do not reintroduce testify.
- **`make lint` copies the tree into the container rather than bind-mounting it.** Linting a
  Docker Desktop bind mount in place does not finish inside golangci-lint's own 30-minute
  timeout; the same run against a copy takes ~190s. It is not the module cache (a cold
  `go mod download` in the container is 12s), not the build cache (a cold native run and a
  warm one are both ~390s), not the linter version, and not the volume of I/O (the whole
  tree walks over the mount in ~130ms). `scripts/golang_lint.sh` carries the measurements.
  The cost is that `--fix` cannot work — `make format` is where this repo rewrites files.
- Killing `make lint` does not stop the container it started, which is how an interrupted run
  leaves a linter competing with the next one. The script names its container and traps, so
  the orphan is cleaned up; if lint is ever mysteriously slow, `docker ps` first.
