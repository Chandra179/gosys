# CLAUDE.md

This file provides guidance to Claude Code (claude.ai/code) when working with code in this repository.

## What this is

`gosys` (module `gosys`, Go 1.26) is a profile-guided static analyzer for Go memory anti-patterns. Given a heap `.pprof` file and a repo, it maps the hottest allocation sites to their AST and flags a small set of known-risky patterns (see `docs/plan.md` for the original design discussion and product reasoning).

The core idea: pprof data is empirical (it reflects a real captured process), AST pattern matching is a heuristic. Findings are reported as evidence (byte count + matched source + explanation) for a human to judge, never as an auto-fix or a definitive verdict — see the rule set doc comments for the reasoning.

## Commands

```sh
go build ./...
go vet ./...
go test ./...                              # all tests
go test ./internal/pipeline/... -v         # the integration tests (see below)
go test ./internal/pipeline/ -run TestSliceIntoStructField -v   # single test
make dashboard                                                  # run the web UI (ADDR=:8080 by default)
```

There is no separate lint config; `go vet` is the standard check.

## Architecture

Pipeline: `internal/pprofstats` → `internal/astsite` → `internal/rules`, wired together by `internal/pipeline`, exposed via `cmd/dashboard`.

- **`internal/pprofstats`** — parses a heap pprof file (`github.com/google/pprof/profile`), aggregates flat allocation value per `(function, file, line)` from each sample's *leaf* frame (prefers `inuse_space`, falls back to `alloc_space`), returns the top-N hottest sites.
- **`internal/astsite`** — loads a repo with `golang.org/x/tools/go/packages` (`./...` pattern — note this means `testdata/` is excluded from scans by Go convention, same as `go build`), and resolves a pprof `file:line` to the enclosing AST node path via `astutil.PathEnclosingInterval`. Also matches pprof-reported file paths against loaded files with fallbacks (exact → suffix → basename) since pprof embeds whatever path the binary was built with, which may not match the repo's checkout path.
  - **Important gotcha already hit once:** `PathEnclosingInterval`'s returned path holds *ancestors* of the source line's interval. The innermost node is often a statement (e.g. `AssignStmt`), not a nested `CallExpr` on the same line — a rule looking only at ancestors for a `CallExpr` will silently never match. See `findAllocCall` in `internal/rules/rules.go` for the fix (checks ancestors, then walks into the innermost statement's subtree).
- **`internal/rules`** — the anti-pattern rule set. Each `Rule` takes the AST path + site and returns a `*Finding` or `nil`. Rules are deliberately few and narrow (precision over recall): `SliceIntoStructField`, `MapPointerGrowth`, `AllocInLoopWithoutPool`. Adding a rule means adding a `Rule` func and registering it in `All`.
- **`internal/pipeline`** — `Analyze(Config) ([]Result, error)` ties the three together; shared by `cmd/dashboard` and by tests, so this is the entry point to call from new tooling.
- **`cmd/dashboard`** — HTMX+Tailwind web UI. Accepts a heap pprof either as a file upload or fetched live from a target's `net/http/pprof` endpoint (`GET http://<host:port>/debug/pprof/heap`), runs it against a repo path, and renders findings as an interactive package/site treemap plus a per-site list.

## Fixture-validated rules (important pattern to follow)

Every rule has a corresponding fixture package under `testdata/fixtures/<pattern>/` (e.g. `sliceleak`, `mapgrowth`, `loopalloc`) containing a small program that actually exercises the anti-pattern, plus an integration test in `internal/pipeline/pipeline_test.go` that:

1. calls the fixture's `Run(...)`,
2. forces a real GC and captures a real heap profile via `runtime/pprof.WriteHeapProfile`,
3. runs the full pipeline against that captured profile,
4. asserts the expected `Finding.Pattern` shows up.

This is not incidental test scaffolding — it's the project's mechanism for catching rules that look correct on inspection but don't actually fire against real profile data (this already caught one real bug in `findAllocCall`). **New rules should follow this same pattern**: add a fixture, capture a real profile, assert the rule fires — don't just eyeball the AST logic.

## Product direction (see `docs/plan.md`)

`docs/plan.md` also discusses a live runtime diagnostics view (`runtime/metrics` + SSE) with an anti-pattern *simulator*; that was prototyped and then deliberately removed — it didn't visualize real analysis data. The dashboard's live-fetch (`/debug/pprof/heap`) is the real-data alternative that replaced it: pull a genuine snapshot on demand rather than animate synthetic data. If a live/streaming view is revisited, prefer periodic re-fetch over continuous streaming — rule matches are about code patterns, which don't change from moment to moment, so streaming mostly shows GC-cycle noise rather than new information.
