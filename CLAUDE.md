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
reference this function? — and grades the package on the answer. `PRD.md` is the specification and
records why each semantic decision is what it is; read it before changing analyzer behavior.

## Layout

- `cmd/main/main.go` — thin entrypoint: signal-cancellable context → `cli.Execute`.
- `internal/analysis/` — the analyzer. `analysis.go` (the `Analyze` entrypoint and `Config`),
  `load.go` (go/packages loading, variant filtering, diagnostics), `declarations.go` (what is held
  accountable, and the exclusions), `references.go` (what a test body resolves to, the one-hop rule,
  interface dispatch), `strictness.go` (the dial), `report.go` (sorting, score, JSON shape).
- `internal/analysis/testdata/` — the fixture corpus, one directory per case in PRD section 5. See
  Testing below; these are *not* formatted or linted, deliberately.
- `internal/coverage/` — the `cover` renderer. `coverage.go` (the `Render` entrypoint, `Config`, and
  the per-file totals), `sources.go` (turning the package paths a profile names into files on disk),
  `annotate.go` (the boundary walk and the four verdicts), `html.go` (the page template).
  `testdata/simple.out` is a real profile over `analysis/testdata/simple`, pinned to that fixture's
  line and column numbers.
- `cmd/tools/codegen/configs/` — codegen tool behind `make configs`: builds each environment's
  `*config.Config` as a real, typed Go object (`environments.go`), validates it, and renders it to
  `config/<env>.json` via `config.Render`. The checked-in JSON is a projection of these builders — edit
  the Go, never the JSON, then re-run `make configs`.
- `config/` — generated per-environment config files (`localdev.json`, `production.json`); committed so
  they stay reviewable, and loadable at runtime via `--config`.
- `internal/cli/` — cobra root command, the `analyze` and `cover` subcommands, terminal output,
  observability bootstrap + shutdown.
- `internal/config/` — assembles `observability.Config` and builds the pillars (slog logging + noop
  tracing/metrics/profiling by default). See `Config.NewPillars` for the upgrade path to real telemetry.
  Two loaders use `platform-go/v10/config`: `Load` overlays `TARP_`-prefixed environment
  variables on the flag/default-seeded config, and `LoadFromFile` decodes a complete JSON config file
  and then overlays the same environment variables. `Render` goes the other way: it validates typed
  `Config` objects and writes them to disk (see `make configs`).
- `version/` — build metadata (`CommitHash`/`BuildTime`/`CommitTime`), injected via `-ldflags` by
  `scripts/build.sh`.

## Common Commands

```bash
make setup          # Create artifacts dir + download the module cache
make configs        # Render config/<env>.json from the real Go objects in cmd/tools/codegen/configs
make build          # Compile all packages, then build artifacts/tarp with version metadata
make run ARGS="version"   # go run the CLI with arguments
make format         # Format all Go code (imports, field alignment, tag alignment, gofmt)
make lint           # Run golangci-lint (Docker) + shellcheck
make test           # Run tests (race detector, shuffle, failfast); excludes cmd packages
```

Run a single test:
```bash
go test -run TestName ./internal/config/...
```

Linting runs in Docker (`golangci/golangci-lint` image). Formatting runs locally via `go tool` with
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
numbers, `testdata/` is excluded from `scripts/format_golang.sh`, `scripts/format_imports.sh`, and
`scripts/goimports.sh`. Do not remove those exclusions.

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
- `Function` carries `Path` (absolute) and `EndLine` alongside `File` and `Line`, both `json:"-"`.
  They exist for the coverage view: a cover profile names files by package path and blocks by line,
  so joining the two needs a file to open and a range to fall inside. Keep them out of the JSON —
  the wire shape is pinned verbatim in `analysis_test.go`.

## CLI conventions worth knowing

- Observability logs are structured slog written to **stdout**. `version`, `analyze`, and `cover`
  print to stdout and emit nothing at the default `info` level, so `tarp analyze --json` and
  `tarp cover --html` stay machine-parseable. Warnings go to **stderr** for the same reason.
- `cover` renders into memory before writing: a report that fails halfway through should not land on
  disk over the last good one. It opens no browser, on purpose — `--output` or stdout.
- The root command sets `SilenceErrors`; `Execute` prints failures itself so that `--fail-on-found`
  can exit non-zero without stapling an `Error:` line under the report it just printed.
- Color is decided once in `internal/cli/color.go`: off unless stdout is a character device, and off
  regardless when `NO_COLOR` is set or `TERM=dumb`. No dependency, on purpose.
- The `--log-level` / `--service-name` persistent flags default from the `TARP_LOG_LEVEL` and
  `TARP_SERVICE_NAME` environment variables. The `--config` flag (default from
  `TARP_CONFIG_FILEPATH`) points at a JSON config file; when set, `bootstrap` loads it via
  `config.LoadFromFile` instead of the flag/env defaults.
- Configuration is layered: defaults (or a JSON file) < `TARP_`-prefixed environment variables.
  Env vars follow platform-go's nested `envPrefix` tags, e.g.
  `TARP_OBSERVABILITY_LOGGING_LEVEL`. Give new `Config` fields both `envPrefix`/`env` and `json`
  tags so they participate in `Load` and `LoadFromFile`.
- To enable real tracing/metrics/profiling, populate the sub-configs in `internal/config` and call
  `observability.Config.NewPillars`, or swap the noop constructors in `Config.NewPillars`.

## Linting

- ~46 linters enabled via `.golangci.yml` (golangci-lint v2 format).
- Formatters: `gci` and `gofmt` (configured in the `formatters:` section).
- Notable strictness: `errcheck` (with `check-blank` + `check-type-assertions`), `errorlint`,
  `gosec`, `forcetypeassert`, `unconvert`, `unparam`. Many are relaxed for `_test.go` files.
- `depguard` carries platform-go's ban list verbatim (testify, `pkg/errors`, `io/ioutil`,
  `math/rand` v1, `dgrijalva/jwt-go`). Use `shoenig/test` + `shoenig/test/must` for assertions and
  `matryer/moq` for mocks; do not reintroduce testify.
