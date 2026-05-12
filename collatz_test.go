package main

import (
	"runtime"
	"sync/atomic"
	"testing"
)

// --- Correctness tests ---

// TestVerifySmall checks that small known-converging ranges complete without
// hanging or panicking. Completion is the assertion — any Collatz
// counterexample would cause an infinite loop.
func TestVerifySmall(t *testing.T) {
	Verify(100, 1, nil)
	Verify(1_000, 1, nil)
}

// TestVerifyMultiWorker checks that the result is the same regardless of
// worker count, confirming no data is missed or double-processed.
func TestVerifyMultiWorker(t *testing.T) {
	var counts [3]uint64
	for i, workers := range []int{1, 4, runtime.NumCPU()} {
		var total atomic.Uint64
		Verify(10_000, workers, func(_ int, delta uint64) {
			total.Add(delta)
		})
		counts[i] = total.Load()
	}
	if counts[0] != counts[1] || counts[1] != counts[2] {
		t.Fatalf("worker counts gave different totals: %v", counts)
	}
}

// TestVerifyProgressTotal confirms that the onProgress callback is called for
// every number in [2, maxNumber] exactly once across all workers.
func TestVerifyProgressTotal(t *testing.T) {
	const max = 1_000
	var total atomic.Uint64
	Verify(max, 4, func(_ int, delta uint64) {
		total.Add(delta)
	})
	got := total.Load()
	want := uint64(max - 1) // numbers 2..max; 1 needs no verification
	if got != want {
		t.Fatalf("expected %d numbers reported via onProgress, got %d", want, got)
	}
}

// TestVerifyInductive validates the inductive termination property: stopping
// when a chain drops below its starting number is sufficient for correctness.
// Runs 100K under several worker counts; a wrong termination condition would
// either hang (infinite loop) or produce an incorrect count.
func TestVerifyInductive(t *testing.T) {
	for _, workers := range []int{1, 2, runtime.NumCPU()} {
		var total atomic.Uint64
		Verify(100_000, workers, func(_ int, delta uint64) {
			total.Add(delta)
		})
		want := uint64(100_000 - 1)
		if got := total.Load(); got != want {
			t.Fatalf("workers=%d: want %d reported, got %d", workers, want, got)
		}
	}
}

// --- Race-condition tests (most valuable when run with -race) ---

// TestVerifyRace is the end-to-end race-detector test. Run with:
//
//	go test -race -run TestVerifyRace ./...
//
// The only shared state is the atomic work counter; any unprotected access
// will surface here.
func TestVerifyRace(t *testing.T) {
	Verify(500_000, runtime.NumCPU(), nil)
}

// --- Benchmarks ---
//
// b.ResetTimer() is intentionally absent: b.Loop() (Go 1.24+) resets the
// timer automatically before each measured iteration, making it redundant.
// Workers are stateless — allocations are goroutine stacks only (one per
// worker). The inner Collatz loop itself allocates nothing.
//
// Mnums/s is a custom metric reporting millions of numbers verified per second.
//
// To compare before/after a change, use:
//
//	go test -bench=. -count=10 -benchmem ./... | tee bench-baseline.txt
//	# make your change
//	go test -bench=. -count=10 -benchmem ./... | tee bench.txt
//	benchstat bench-baseline.txt bench.txt

// BenchmarkVerify100M is the primary throughput benchmark.
func BenchmarkVerify100M(b *testing.B) {
	const max = 100_000_000
	b.ReportAllocs()
	for b.Loop() {
		Verify(max, runtime.NumCPU(), nil)
	}
	b.ReportMetric(float64(max)*float64(b.N)/float64(b.Elapsed().Nanoseconds())*1000, "Mnums/s")
}

// BenchmarkVerify1B is the large-scale benchmark. The stateless algorithm
// shines here — no bitmap means no 125 MB memory wall. Expect 30+ seconds.
func BenchmarkVerify1B(b *testing.B) {
	const max = 1_000_000_000
	b.ReportAllocs()
	for b.Loop() {
		Verify(max, runtime.NumCPU(), nil)
	}
	b.ReportMetric(float64(max)*float64(b.N)/float64(b.Elapsed().Nanoseconds())*1000, "Mnums/s")
}

// BenchmarkVerifySerial isolates single-core throughput so you can calculate
// the parallel speedup: BenchmarkVerify100M.ns ÷ (BenchmarkVerifySerial.ns × 10) ≈ numCores at ideal scaling.
func BenchmarkVerifySerial(b *testing.B) {
	const max = 10_000_000
	b.ReportAllocs()
	for b.Loop() {
		Verify(max, 1, nil)
	}
	b.ReportMetric(float64(max)*float64(b.N)/float64(b.Elapsed().Nanoseconds())*1000, "Mnums/s")
}
