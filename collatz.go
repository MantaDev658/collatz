package main

import (
	"math/bits"
	"sync"
	"sync/atomic"
	"time"
)

const batchSize = 1024

const (
	sieveBits = 20
	sieveSize = 1 << sieveBits
	sieveMask = sieveSize - 1
)

// skip[r] is true when all n ≡ r (mod sieveSize) provably drop below n.
// Soundness: after m steps with the same parity sequence, T^m(n) = A·n + B
// where A,B depend only on the low sieveBits of n. If T^m(r) < r then A < 1
// and B < (1-A)·r, so T^m(n) < n for all n ≡ r (mod sieveSize) with n ≥ r.
var skip [sieveSize]bool

func init() {
	for r := uint64(2); r < sieveSize; r++ {
		current := r
		for range sieveBits * 3 {
			if current < r {
				skip[r] = true
				break
			}
			if current&1 == 0 {
				current >>= bits.TrailingZeros64(current)
			} else {
				current = (3*current + 1) >> 1
			}
		}
	}
}

// paddedCounter pads atomic.Uint64 to a full CPU cache line (64 bytes) to
// prevent false sharing when goroutines update adjacent per-core counters.
type paddedCounter struct {
	atomic.Uint64
	_ [56]byte
}

func worker(
	id int,
	maxNumber uint64,
	counter *atomic.Uint64,
	wg *sync.WaitGroup,
	onProgress func(id int, delta uint64),
) {
	defer wg.Done()
	for {
		start := counter.Add(batchSize) - batchSize
		if start > maxNumber {
			break
		}
		end := min(start+batchSize, maxNumber+1)

		for i := start; i < end; i++ {
			if skip[i&sieveMask] {
				continue
			}
			current := i
			for current >= i {
				if current&1 == 0 {
					current >>= bits.TrailingZeros64(current)
				} else {
					current = (3*current + 1) >> 1
				}
			}
		}

		if onProgress != nil {
			onProgress(id, end-start)
		}
	}
}

// Verify checks the Collatz conjecture for every number in [2, maxNumber].
// It spawns numWorkers goroutines and calls onProgress(workerID, delta) after
// each batch. onProgress may be nil.
func Verify(maxNumber uint64, numWorkers int, onProgress func(id int, delta uint64)) time.Duration {
	var counter atomic.Uint64
	counter.Store(2)

	start := time.Now()

	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go worker(i, maxNumber, &counter, &wg, onProgress)
	}
	wg.Wait()

	return time.Since(start)
}
