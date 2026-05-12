# Collatz

Brute-force parallel verification of the [Collatz conjecture](https://en.wikipedia.org/wiki/Collatz_conjecture) up to a given number, with a live terminal UI showing per-core progress.

## What is the Collatz conjecture?

Take any positive integer. If it's even, halve it; if it's odd, multiply by 3 and add 1. The conjecture states this sequence always eventually reaches 1 — no counterexample has ever been found, but no proof exists either.

## Running

```
go run .
```

Or build first:

```
go build .
./collatz
```

## How it works

- **Parallel workers** — one goroutine per CPU core, each claiming work in 1024-number batches via an atomic counter (dynamic scheduling, so no core sits idle)
- **Stateless inductive verification** — each chain runs until it drops strictly below its starting number `i`; by strong induction on the naturals this is sufficient to prove convergence. Workers share nothing except the atomic work counter, so throughput scales linearly with cores at any range
- **`TrailingZeros64` even step** — strips all factors of 2 in a single CPU instruction (TZCNT on x86, CLZ on ARM) instead of one at a time, collapsing runs like 4096 → 1 from 12 steps to 1
- **Live TUI** — ANSI progress bars update every 100 ms showing per-core and overall completion, elapsed time, verification rate, and ETA

No external dependencies — standard library only.

## Development

```
make dev            # full cycle: fmt → vet → build → race test (run before committing)
make build          # compile binary only
make run            # go run .
make test           # go test ./...
make race           # go test -race ./...    ← race detector
make bench          # benchmarks, 5s per function
make bench-quick    # benchmarks, 3s — fast feedback during development
make bench-large    # BenchmarkVerify1B only — large-scale throughput showcase (30s+)
make bench-serial   # GOMAXPROCS=1 (measures single-core throughput for speedup math)
make profile        # CPU profile of BenchmarkVerify100M → cpu.prof
make esc            # escape analysis — see guide below
make vet            # go vet
make fmt            # gofmt -w .
make tidy           # go mod tidy
make clean          # remove binary, bench output, and profile files
```

### Reading benchmark output

```
BenchmarkVerify100M-10     100     53056820 ns/op    1885 Mnums/s    1889 B/op    14 allocs/op
BenchmarkVerify1B-10        10    529075462 ns/op    1890 Mnums/s    1193 B/op    13 allocs/op
BenchmarkVerifySerial-10   165     35870220 ns/op     278 Mnums/s      72 B/op     3 allocs/op
```

| Column | Meaning |
|--------|---------|
| `-10` suffix | `GOMAXPROCS` — how many OS threads Go is using |
| `100` | Iterations Go ran to produce a stable sample |
| `ns/op` | Nanoseconds per call (`53056820 ns` ≈ 53 ms) |
| `Mnums/s` | Custom metric: millions of numbers verified per second |
| `B/op` | Heap bytes allocated per call — goroutine stacks only (does **not** count stack frames) |
| `allocs/op` | Number of distinct heap allocations per call |

**What the numbers tell you here:**
- `Verify100M` and `Verify1B` have nearly identical `Mnums/s` (~1890): the algorithm is compute-bound, not memory-bound. There is no memory wall at 1B because there's no bitmap.
- `B/op` is ~1.9 KB (goroutine stacks), down from 12.5 MB with the old bitmap approach. The inner loop allocates nothing.
- `Mnums/s speedup`: serial is ~279 Mnums/s; parallel is ~1890 Mnums/s → roughly 6.8× across 10 cores (68% efficiency). The gap from ideal is work-stealing overhead and cache-coherence traffic on the shared counter.
- `BenchmarkVerify1B` may take 30+ seconds total. Run it with `make bench-large`.

### Comparing before/after a change with benchstat

```sh
make install-tools           # one-time: installs benchstat
make bench-baseline          # snapshot current numbers → bench-baseline.txt
# make your change
make diff                    # re-runs benchmarks → bench.txt, then compares
```

Benchstat output looks like:

```
name               old time/op    new time/op    delta
Verify100M-10      53.1ms ± 2%    49.8ms ± 1%    -6.2%  (p=0.001 n=10+10)
VerifySerial-10    35.9ms ± 1%    34.1ms ± 0%    -5.0%  (p=0.032 n=10+10)
```

- `±2%` — variance across 10 runs. Above ~5% means the environment is noisy; close other apps and re-run.
- `p=0.001` — statistical confidence. `p < 0.05` means the difference is real, not noise.
- `n=10+10` — 10 samples from each side.

`bench-baseline.txt` and `bench.txt` are gitignored.

### Reading escape analysis output (`make esc`)

`make esc` runs `go build -gcflags="-m"` filtered to only show lines from our own source files. The raw output has three sections:

**collatz.go — the hot path (what actually matters)**
```
./collatz.go:22:2: counter does not escape    ← work counter stays on stack in worker ✓
./collatz.go:24:2: onProgress does not escape ← callback stays on stack in worker ✓

./collatz.go:57:6: moved to heap: counter     ← captured by goroutine closure, unavoidable
./collatz.go:62:6: moved to heap: wg          ← same
```

The two "does not escape" lines are the green light: the inner Collatz loop is pure register arithmetic — `current` never touches the heap, and `TrailingZeros64` compiles to a single instruction. The two heap allocations (`counter` and `wg`) are one-time closure captures per `Verify` call, not per-number costs. There is no bitmap allocation.

**tui.go — expected allocations, not a concern**

Every line that calls `fmt.Fprintf` will show its arguments escaping because `fmt.Fprintf` takes a variadic `...interface{}` parameter. The compiler must box each value (an `int`, a `float64`, a `string`) as an `interface{}` on the heap to pass it. This is a fundamental property of `fmt` — not a bug. These allocations happen at most 10 times per second (the TUI refresh rate) and are irrelevant to throughput.

**main.go — one-time setup**

Allocations in `main` run exactly once. Strings passed to `fmt.Printf` escape for the same reason as tui.go. Not worth optimizing.

**When to use `-m=2`**

If you see an unexpected heap escape in the hot path and want to understand *why*, re-run with `go build -gcflags="-m=2" . 2>&1 | grep "^\.\/"`. It prints the full data-flow chain explaining which assignment or call caused the escape.
