# Go Diagnostic Dashboard & Codebase Analyzer

I was planning to create a dashboard for Go memory, CPU, and GC analysis using HTMX + Tailwind. How feasible is it? Maybe live tracing too, just like these use cases—we could simulate the error by getting the operation and its related data, then perform it. That's one idea.

Second, from a product-minded perspective: how about we analyze a repository? But if it's a large codebase, it becomes a problem—where to start and what to analyze?

Building both an interactive Go diagnostic simulator and a product-minded codebase analyzer is entirely feasible—and Go's standard library makes the backend remarkably lightweight.

## Status

- **Part 2 (pprof → AST → diagnostic pipeline): v1 built.** CLI (`cmd/analyze`), heap-profile-only, template-based diagnostics (not LLM-narrated — see "Decisions" below). Implements the three anti-patterns listed under "Flagged anti-patterns" below. Every rule is validated against a real captured heap profile via a fixture package + integration test (`internal/pipeline/pipeline_test.go`), not just eyeballed against the AST — see `CLAUDE.md` for the architecture and the one real bug this approach already caught.
- **Part 1 (live HTMX dashboard + simulator): not started.** Package decision made (see below) but no code yet.
- **"Beyond Go" section (cache hierarchy, branch misprediction, etc.): explicitly out of scope for now.** Needs hardware perf counters / compiler-level analysis, a different tech stack from the rest of this plan. Kept here as a long-term vision note, not a near-term target.

## Decisions

These resolve the open questions that were originally at the bottom of this doc.

**Distribution model — not a plugin, not an event listener.** `gosys` is a Go module: a `cmd/` CLI binary built from importable `internal/` packages. It's invoked on demand (`gosys analyze -pprof heap.pprof -repo .`) — from a terminal, from CI, from a cron job — rather than embedded in a target service's runtime or run as a daemon listening for events. If automatic triggering on new profiles/deploys is wanted later, that belongs in a thin orchestration layer *outside* this repo (a CI step, a webhook receiver) that shells out to the CLI — keeps the core tool simple, stateless, and swappable between triggers.

(This is specific to part 2. Part 1, if built, is a different shape: a library the target repository imports and registers a handler from, same pattern as `github.com/arl/statsviz` — see below.)

**Part 1 package choice — stdlib `runtime/metrics`, in-house, no OTel/Prometheus dependency.** OpenTelemetry's SDK is built for cross-service export to a collector backend, which this single-process live dashboard doesn't need. Prometheus `client_golang`'s Go collector already wraps `runtime/metrics`, but its pull/scrape model adds latency versus pushing straight over SSE. [`statsviz`](https://github.com/arl/statsviz) already does close to this exact thing (embed + live runtime/metrics charts) — worth reading for the metric list and GC-pause-histogram handling, not worth depending on, since the differentiated part of this plan is the anti-pattern *simulator*, not metrics collection. A Prometheus `/metrics` endpoint can be bolted on later as an addition, not a v1 dependency.

**False positives — findings are evidence, not verdicts.** The pprof data is empirical (a real captured process); the AST pattern match is a heuristic guess about whether that hot line is actually a problem. Every finding reports the byte count + matched source + explanation for a human to judge, same as a linter warning — the tool never auto-fixes. Rules are kept few and narrow on purpose (precision over recall), and each one ships with a fixture package + integration test that captures a real heap profile and asserts the rule actually fires against it, not just that the AST logic looks right on inspection.

## 1. Live HTMX + Tailwind Diagnostic Dashboard

Go provides built-in low-level telemetry that makes a real-time profiling dashboard straightforward to build without external agents.

### Telemetry Engine (`runtime/metrics`)

Skip raw `runtime.ReadMemStats()` (which triggers stop-the-world pauses). Use `runtime/metrics` to read low-overhead metrics like:

- `/gc/pauses:seconds`
- `/memory/classes/heap/objects:bytes`
- `/sched/goroutines:events`

### Real-Time HTMX UI

Use Server-Sent Events (SSE) with HTMX (`hx-ext="sse"`). Your Go server streams HTML snippets (rendered via `html/template` and styled with Tailwind) over an HTTP/2 or HTTP/1.1 SSE connection every 500ms.

### Interactive Workload Simulator

Expose action endpoints (e.g., `POST /simulate/slice-leak`, `POST /simulate/map-gc-thrash`) that spawn background goroutines executing controlled anti-patterns. The HTMX dashboard live-updates memory RSS, GC pause frequency, and live heap object count while the workload runs.

## 2. Product Strategy for Large Codebases: Where to Analyze?

Scanning a 500k-line codebase for runtime performance issues blindly leads to static analysis noise. A product-minded analyzer handles scale by focusing exclusively on hot paths and high-risk allocation patterns.

| Approach | How it works | Where to start in a large codebase |
| --- | --- | --- |
| Profile-Guided Analysis (Recommended) | Ingest a `heap.pprof` or `cpu.pprof` file from production or CI benchmarks. | Parse the `.pprof` protobuf file first. Use stack frames to locate the top 5% of functions consuming memory or CPU, then run static analysis only on those specific files. |
| AST Entry-Point Tracing | Parse code into a Go AST (`go/ast`, `go/analysis`). | Filter out utility/test packages. Crawl backwards starting exclusively from entry points: HTTP handlers, gRPC methods, Kafka/RabbitMQ consumers, and `for` loops processing streaming input. |
| Anti-Pattern Heuristics | Search for high-risk AST node patterns on those entry paths. | Flag specific code patterns. |

### Flagged anti-patterns

- Buffer slicing (`buf[x:y]`) assigned to struct fields.
- `map[string]*Struct` with high item counts.
- `json.Unmarshal` or `make([]byte)` inside tight loops without `sync.Pool`.

### Product Workflow — v1 (built)

1. **User action:** run the CLI against a captured heap profile + a local repo path: `gosys analyze -pprof heap.pprof -repo ./path -top 20`.
2. **Analysis:** maps the hottest allocation sites from pprof directly to AST nodes in the repo (`internal/pprofstats` → `internal/astsite` → `internal/rules`).
3. **Output:** stdout report per flagged line — byte count, matched pattern, and a templated diagnostic, e.g. "line 42 assigns a slice expression into struct field `buf`... consider copying just the needed bytes instead of re-slicing."

Not built yet: upload UI, GitHub-link ingestion (currently local repo path only, via `go/packages`), CPU-profile support, LLM-narrated diagnostics.

---

Maybe it's not only for Go later, though—it can have many use cases. You know, it all comes back to the fact that a computer's internal language is just the wrapper of it, while it still relates to the computer.

> That insight is the core boundary between language-level coding and true systems engineering: every language runtime, compiler, and garbage collector is just an abstraction wrapper over CPU registers, cache lines, RAM pages, and OS syscalls.

### 1. Memory & CPU Cache Hierarchy

- **Cache Misses & Layout:** Non-contiguous memory traversals (linked lists vs. contiguous arrays), cache-line striding.
- **False Sharing:** Multiple CPU cores repeatedly invalidating shared 64-byte L1/L2 cache lines by writing to adjacent variables.
- **Memory Pinning & Retention:** Holding live references to large allocated memory blocks via small sub-views (Go slices, pre-Java 7 substring, lifetime issues in C++ `string_view`).

### 2. Allocator & Runtime Mechanics

- **Pointer-Graph Traversal Costs:** GC tracing time scaling linearly with pointer density on long-lived heaps (Go pointer maps, Java `HashMap`, C# object graphs).
- **Heap Allocation & Fragmentation:** High-frequency heap allocations causing memory allocator lock contention and heap fragmentation.
- **Reference Leaks:** Leaked execution units (goroutines/threads), unclosed file descriptors, or reference-counting cycles (Rust `Rc`, C++ `std::shared_ptr`).

### 3. CPU Execution & Synchronization

- **Branch Misprediction:** Non-deterministic branching logic inside high-throughput tight loops disrupting CPU instruction pipelines.
- **Lock Contention:** Thread parking, mutex thrashing, and context-switch overhead under high concurrency.
- **Instruction Parallelism:** Missed SIMD/vectorization opportunities due to complex pointer aliasing or non-aligned data layouts.

### 4. Kernel & I/O Subsystems

- **User/Kernel Buffer Copying:** Excessive context switching and memory copying between user-space and kernel-space (lack of zero-copy / `sendfile`).
- **Syscall Thrashing:** Making thousands of small system calls instead of batching I/O operations.