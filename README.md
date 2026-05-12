# collatz

Brute-force parallel verification of the [Collatz conjecture](https://en.wikipedia.org/wiki/Collatz_conjecture) up to 10 million, with a live terminal UI showing per-core progress.

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
