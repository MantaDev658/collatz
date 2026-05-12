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
- **Memoization bitmap** — a 1.25 MB atomic bitset marks every number already proven to converge; chains short-circuit as soon as they hit a proven number, cutting average chain length to 1–2 steps
- **Live TUI** — ANSI progress bars update every 100 ms showing per-core and overall completion, elapsed time, verification rate, and ETA

No external dependencies — standard library only.

## Development

```
make dev            # full cycle: fmt → vet → race test → build (run before committing)
make build          # compile binary only
make run            # go run .
make test           # go test ./...
make race           # go test -race ./...    ← race detector
make bench          # benchmarks, 5s per function
make bench-serial   # GOMAXPROCS=1 (measures single-core throughput for speedup math)
make esc            # escape analysis — see guide below
make vet            # go vet
make fmt            # gofmt -w .
make tidy           # go mod tidy
make clean          # remove binary and bench output files
```

### Reading benchmark output

```
BenchmarkVerify10M-10       337    10617652 ns/op    1254591 B/op    13 allocs/op
BenchmarkVerify1M-10       2804     1290755 ns/op     131912 B/op    13 allocs/op
BenchmarkVerifySerial-10    549     6391031 ns/op     131176 B/op     4 allocs/op
BenchmarkBitmapSetGet-10   1e9        1.779 ns/op          0 B/op     0 allocs/op
```

| Column | Meaning |
|--------|---------|
| `-10` suffix | `GOMAXPROCS` — how many OS threads Go is using |
| `337` | Iterations Go ran to produce a stable sample |
| `ns/op` | Nanoseconds per call (`10617652 ns` ≈ 10.6 ms) |
| `B/op` | Heap bytes allocated per call (does **not** count stack) |
| `allocs/op` | Number of distinct heap allocations per call |

**What the numbers tell you here:**
- `BitmapSetGet: 1.779 ns/op, 0 allocs` — the atomic hot path never touches the heap. Good.
- `Verify10M: 1254591 B/op, 13 allocs` — ~1.25 MB is the proven bitmap allocation; the 13 allocs are the bitmap plus one goroutine stack per core. All unavoidable; the inner loop itself allocates nothing.
- `Verify10M (10.6 ms) vs VerifySerial on 1M (6.4 ms)`: serial on 1/10th the work is 6.4 ms, so serial on 10M would be ~64 ms. Parallel brings that to 10.6 ms → roughly 6× speedup across 10 cores (60% efficiency). The gap from ideal is cache-coherence traffic on the shared bitmap.

### Comparing before/after a change with benchstat

```sh
make install-tools           # one-time: installs benchstat
make bench-baseline          # snapshot current numbers → bench-baseline.txt
# make your change
make diff                    # re-runs benchmarks → bench.txt, then compares
```

Benchstat output looks like:

```
name              old time/op    new time/op    delta
Verify10M-10      10.6ms ± 2%    9.8ms ± 1%    -7.5%  (p=0.001 n=10+10)
BitmapSetGet-10   1.78ns ± 1%   1.75ns ± 0%    -1.7%  (p=0.032 n=10+10)
```

- `±2%` — variance across 10 runs. Above ~5% means the environment is noisy; close other apps and re-run.
- `p=0.001` — statistical confidence. `p < 0.05` means the difference is real, not noise.
- `n=10+10` — 10 samples from each side.

`bench-baseline.txt` and `bench.txt` are gitignored.

### Reading escape analysis output (`make esc`)

`make esc` runs `go build -gcflags="-m"` filtered to only show lines from our own source files. The raw output has three sections:

**collatz.go — the hot path (what actually matters)**
```
./collatz.go:18:6: can inline setBit          ← hot-path function inlined, no call overhead
./collatz.go:22:6: can inline isProven        ← same
./collatz.go:44:56: inlining call to isProven ← compiler actually used the inline
./collatz.go:51:10: inlining call to setBit

./collatz.go:18:13: proven does not escape    ← bitmap slice stays on stack in hot path ✓
./collatz.go:29:2:  proven does not escape    ← same in worker ✓
./collatz.go:30:2:  counter does not escape   ← work counter stays on stack in worker ✓

./collatz.go:64:16: make([]uint64, ...) escapes to heap   ← 1.25 MB bitmap, unavoidable
./collatz.go:67:6:  moved to heap: counter                ← captured by goroutine closure, unavoidable
./collatz.go:72:6:  moved to heap: wg                     ← same
```

The three "does not escape" lines are the green light: the tight inner loop — `setBit`, `isProven`, and the Collatz iteration — never touches the heap. The three heap allocations are all one-time setup costs per `Verify` call.

**tui.go — expected allocations, not a concern**

Every line that calls `fmt.Fprintf` will show its arguments escaping because `fmt.Fprintf` takes a variadic `...interface{}` parameter. The compiler must box each value (an `int`, a `float64`, a `string`) as an `interface{}` on the heap to pass it. This is a fundamental property of `fmt` — not a bug. These allocations happen at most 10 times per second (the TUI refresh rate) and are irrelevant to throughput.

**main.go — one-time setup**

Allocations in `main` run exactly once. Strings passed to `fmt.Printf` escape for the same reason as tui.go. Not worth optimizing.

**When to use `-m=2`**

If you see an unexpected heap escape in the hot path and want to understand *why*, re-run with `go build -gcflags="-m=2" . 2>&1 | grep "^\.\/"`. It prints the full data-flow chain explaining which assignment or call caused the escape.
