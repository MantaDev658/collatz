package main

import (
	"sync"
	"sync/atomic"
	"time"
)

const batchSize = 1024

// paddedCounter pads atomic.Uint64 to a full CPU cache line (64 bytes) to
// prevent false sharing when goroutines update adjacent per-core counters.
type paddedCounter struct {
	atomic.Uint64
	_ [56]byte
}

func setBit(proven []uint64, n uint64) {
	atomic.OrUint64(&proven[n>>6], 1<<(n&63))
}

func isProven(proven []uint64, n uint64) bool {
	return atomic.LoadUint64(&proven[n>>6])>>(n&63)&1 != 0
}

func worker(
	id int,
	maxNumber uint64,
	proven []uint64,
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

		if onProgress != nil {
			onProgress(id, end-start)
		}
	}
}

// Verify checks the Collatz conjecture for every number in [2, maxNumber].
// It spawns numWorkers goroutines and calls onProgress(workerID, delta) after
// each batch. onProgress may be nil.
func Verify(maxNumber uint64, numWorkers int, onProgress func(id int, delta uint64)) time.Duration {
	proven := make([]uint64, maxNumber/64+1)
	setBit(proven, 1)

	var counter atomic.Uint64
	counter.Store(2)

	start := time.Now()

	var wg sync.WaitGroup
	for i := 0; i < numWorkers; i++ {
		wg.Add(1)
		go worker(i, maxNumber, proven, &counter, &wg, onProgress)
	}
	wg.Wait()

	return time.Since(start)
}
