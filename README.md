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
- **Stateless inductive verification** — each chain runs until it drops strictly below its starting number `i`; by strong induction on the naturals, if every sequence in [2, N] drops below its start, all of them reach 1. Workers share nothing except the work counter, so throughput scales linearly with cores at any range and there is no shared bitmap
- **`TrailingZeros64` even step** — strips all factors of 2 in a single CPU instruction (TZCNT on x86, CLZ on ARM) instead of one at a time, collapsing runs like 4096 → 1 from 12 iterations to 1
- **Residue sieve** — at startup a 1 MB lookup table marks every residue class mod 2²⁰ that is provably convergent. After m Collatz steps the sequence value is a linear function of n: `T^m(n) = A·n + B` where A and B depend only on the low 20 bits of n. If A < 1 and the representative r (0 ≤ r < 2²⁰) drops below itself in those steps, then every n ≡ r (mod 2²⁰) is also provably convergent — the inner loop can be skipped entirely. **99.89% of all residue classes resolve within 60 steps**, so the inner loop runs for only ~0.11% of numbers
- **Live TUI** — ANSI progress bars update every 100 ms showing per-core and overall completion, elapsed time, verification rate, and ETA

## Practical range

At ~15,000 Mnums/s on a 10-core machine, verification time scales as:

| Target | Numbers | Estimated time |
|--------|---------|----------------|
| 2⁴¹ | 2.2 × 10¹² | ~4 minutes |
| 2⁴⁸ | 2.8 × 10¹⁴ | ~5 hours |
| 2⁵⁵ | 3.6 × 10¹⁶ | ~27 days |
| 2⁵⁸ | 2.9 × 10¹⁷ | ~220 days |

**2⁵⁸ is the recommended upper bound for consumer hardware.** The Collatz sequence for `n` can reach intermediate values significantly larger than `n` before converging. The combined step `(3n+1)/2` overflows `uint64` when `n > 2^62.5`. For starting values up to 2⁵⁸, empirical evidence (the conjecture has been externally verified beyond 2⁶⁸ using 64-bit arithmetic) confirms that intermediate values stay within `uint64` range. Going above 2⁵⁸ without switching to 128-bit arithmetic is unsafe.

To change the target, edit `exp` in `main.go`:

```go
var base uint64 = 2
var exp  uint64 = 41   // ← change this; 41 ≈ 4 min, 58 ≈ 220 days at 15K Mnums/s
```

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
BenchmarkVerify100M-10    889     6608512 ns/op    15132 Mnums/s    745 B/op    12 allocs/op
BenchmarkVerify1B-10        ?           ? ns/op        ? Mnums/s      ? B/op     ? allocs/op
BenchmarkVerifySerial-10    ?           ? ns/op        ? Mnums/s      ? B/op     ? allocs/op
```

*(Run `make bench` to get current numbers for your machine.)*

| Column | Meaning |
|--------|---------|
| `-10` suffix | `GOMAXPROCS` — how many OS threads Go is using |
| `889` | Iterations Go ran to produce a stable sample |
| `ns/op` | Nanoseconds per call (`6608512 ns` ≈ 6.6 ms for 100M numbers) |
| `Mnums/s` | Custom metric: millions of numbers verified per second |
| `B/op` | Heap bytes allocated per call — goroutine stacks only (does **not** count stack) |
| `allocs/op` | Number of distinct heap allocations per call |

**What the numbers tell you here:**
- `Verify100M: 15,132 Mnums/s` — ~16× faster than the pre-sieve baseline (917 Mnums/s). The sieve skips 99.89% of numbers, eliminating the inner loop for all but 0.11% of the range.
- `B/op` is ~745 bytes (goroutine stacks only), down from 12.5 MB with the original bitmap approach. The inner loop allocates nothing.
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
./collatz.go:XX:2: counter does not escape    ← work counter stays on stack in worker ✓
./collatz.go:XX:2: onProgress does not escape ← callback stays on stack in worker ✓

./collatz.go:XX:6: moved to heap: counter     ← captured by goroutine closure, unavoidable
./collatz.go:XX:6: moved to heap: wg          ← same
```

The two "does not escape" lines are the green light: the inner Collatz loop is pure register arithmetic — `current` never touches the heap, and `TrailingZeros64` compiles to a single instruction. The sieve lookup `skip[i&sieveMask]` is a read-only access into the global `skip` array (BSS segment) — no allocation, no indirection. The two heap allocations (`counter` and `wg`) are one-time closure captures per `Verify` call, not per-number costs.

**tui.go — expected allocations, not a concern**

Every line that calls `fmt.Fprintf` will show its arguments escaping because `fmt.Fprintf` takes a variadic `...interface{}` parameter. The compiler must box each value (an `int`, a `float64`, a `string`) as an `interface{}` on the heap to pass it. This is a fundamental property of `fmt` — not a bug. These allocations happen at most 10 times per second (the TUI refresh rate) and are irrelevant to throughput.

**main.go — one-time setup**

Allocations in `main` run exactly once. Strings passed to `fmt.Printf` escape for the same reason as tui.go. Not worth optimizing.

**When to use `-m=2`**

If you see an unexpected heap escape in the hot path and want to understand *why*, re-run with `go build -gcflags="-m=2" . 2>&1 | grep "^\.\/"`. It prints the full data-flow chain explaining which assignment or call caused the escape.
