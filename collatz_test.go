package main

import (
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
)

// --- Correctness tests ---

// TestBitmapRoundTrip verifies that setBit and isProven are inverses and that
// setting one bit does not clobber its neighbours.
func TestBitmapRoundTrip(t *testing.T) {
	proven := make([]uint64, 100)

	if isProven(proven, 42) {
		t.Fatal("expected 42 unproven before any setBit")
	}

	setBit(proven, 42)
	if !isProven(proven, 42) {
		t.Fatal("expected 42 proven after setBit")
	}
	if isProven(proven, 41) || isProven(proven, 43) {
		t.Fatal("setBit(42) must not affect neighbouring bits")
	}

	// Word boundaries
	for _, n := range []uint64{0, 63, 64, 127, 128} {
		setBit(proven, n)
		if !isProven(proven, n) {
			t.Fatalf("expected %d proven after setBit", n)
		}
	}
}

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
	want := uint64(max - 1) // numbers 2..max; 1 is pre-proven, not passed through onProgress
	if got != want {
		t.Fatalf("expected %d numbers reported via onProgress, got %d", want, got)
	}
}

// --- Race-condition tests (most valuable when run with -race) ---

// TestBitmapConcurrent hammers setBit and isProven from many goroutines with
// overlapping indices. The race detector catches any non-atomic access.
func TestBitmapConcurrent(t *testing.T) {
	proven := make([]uint64, 1000)
	var wg sync.WaitGroup
	for i := 0; i < 64; i++ {
		n := uint64(i * 7) // spread across multiple words
		wg.Add(1)
		go func() {
			defer wg.Done()
			for j := 0; j < 500; j++ {
				setBit(proven, n)
				_ = isProven(proven, n)
			}
		}()
	}
	wg.Wait()
}

// TestVerifyFloor confirms the global-floor optimization is correct by running
// the same range under several worker counts and checking the progress total.
// A wrong floor would short-circuit chains that haven't converged, causing
// either an incorrect count or an infinite loop.
func TestVerifyFloor(t *testing.T) {
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

// TestVerifyRace is the end-to-end race-detector test. Run with:
//
//	go test -race -run TestVerifyRace ./...
//
// Any unprotected shared state in the bitmap, work counter, or floor will surface here.
func TestVerifyRace(t *testing.T) {
	Verify(500_000, runtime.NumCPU(), nil)
}

// --- Benchmarks ---
//
// b.ResetTimer() is intentionally absent: b.Loop() (Go 1.24+) resets the
// timer automatically before each measured iteration, making it redundant.
// The allocations reported include the proven bitmap (make([]uint64, ...))
// and goroutine stacks — both unavoidable per-call costs. The inner Collatz
// loop itself allocates nothing.
//
// Mnums/s is a custom metric reporting millions of numbers verified per second.
//
// To compare before/after a change, use:
//
//	go test -bench=. -count=10 -benchmem ./... | tee bench-baseline.txt
//	# make your change
//	go test -bench=. -count=10 -benchmem ./... | tee bench.txt
//	benchstat bench-baseline.txt bench.txt

// BenchmarkVerify100M is the primary throughput benchmark (~200ms per iteration).
func BenchmarkVerify100M(b *testing.B) {
	const max = 100_000_000
	b.ReportAllocs()
	for b.Loop() {
		Verify(max, runtime.NumCPU(), nil)
	}
	b.ReportMetric(float64(max)*float64(b.N)/float64(b.Elapsed().Nanoseconds())*1000, "Mnums/s")
}

// BenchmarkVerify1B showcases the floor optimization: the bitmap is 125 MB
// (exceeds L3 cache), so eliminating bitmap lookups via the floor is most
// impactful here. Expect this to run for 30+ seconds.
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

// verifyNoFloor is the pre-floor version of Verify used to benchmark the
// floor optimization's impact. It is intentionally kept in the test file —
// it is not production code, only a measurement baseline.
func verifyNoFloor(maxNumber uint64, numWorkers int) {
	proven := make([]uint64, maxNumber/64+1)
	setBit(proven, 1)
	var counter atomic.Uint64
	counter.Store(2)
	var wg sync.WaitGroup
	for range numWorkers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for {
				start := counter.Add(batchSize) - batchSize
				if start > maxNumber {
					break
				}
				end := min(start+batchSize, maxNumber+1)
				for i := start; i < end; i++ {
					current := i
					for current > 1 && (current > maxNumber || !isProven(proven, current)) {
						if current&1 == 0 {
							current >>= 1
						} else {
							current = (3*current + 1) >> 1
						}
					}
					setBit(proven, i)
				}
			}
		}()
	}
	wg.Wait()
}

// BenchmarkVerify100MNoFloor is the no-floor baseline for BenchmarkVerify100M.
// Compare Mnums/s between the two to quantify the floor optimization's value.
func BenchmarkVerify100MNoFloor(b *testing.B) {
	const max = 100_000_000
	b.ReportAllocs()
	for b.Loop() {
		verifyNoFloor(max, runtime.NumCPU())
	}
	b.ReportMetric(float64(max)*float64(b.N)/float64(b.Elapsed().Nanoseconds())*1000, "Mnums/s")
}

// BenchmarkBitmapSetGet microbenchmarks the atomic hot path in isolation.
func BenchmarkBitmapSetGet(b *testing.B) {
	proven := make([]uint64, 1000)
	b.ReportAllocs()
	for b.Loop() {
		setBit(proven, 42)
		_ = isProven(proven, 42)
	}
}
