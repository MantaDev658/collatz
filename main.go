package main

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	batchSize = 1024
	barWidth  = 32
	refreshMs = 100
)

func setBit(proven []uint64, n uint64) {
	atomic.OrUint64(&proven[n>>6], 1<<(n&63))
}

func isProven(proven []uint64, n uint64) bool {
	return atomic.LoadUint64(&proven[n>>6])>>(n&63)&1 != 0
}

func bar(pct float64) string {
	filled := int(pct*barWidth/100.0 + 0.5)
	if filled > barWidth {
		filled = barWidth
	}
	if filled < 0 {
		filled = 0
	}
	return "\033[32m" + strings.Repeat("█", filled) + "\033[90m" + strings.Repeat("░", barWidth-filled) + "\033[0m"
}

func humanRate(n float64) string {
	switch {
	case n >= 1_000_000:
		return fmt.Sprintf("%.2fM", n/1_000_000)
	case n >= 1_000:
		return fmt.Sprintf("%.1fK", n/1_000)
	default:
		return fmt.Sprintf("%.0f", n)
	}
}

func renderFrame(numCores int, maxNumber uint64, coreCompleted []atomic.Uint64, totalDone *atomic.Uint64, startTime time.Time, first bool) {
	numDynLines := numCores + 4
	var sb strings.Builder

	if !first {
		fmt.Fprintf(&sb, "\033[%dA", numDynLines)
	}

	fairShare := float64(maxNumber) / float64(numCores)
	sb.WriteByte('\n')

	for i := 0; i < numCores; i++ {
		pct := float64(coreCompleted[i].Load()) / fairShare * 100.0
		if pct > 100 {
			pct = 100
		}
		fmt.Fprintf(&sb, "  \033[1mCore %2d\033[0m  [%s]  %5.1f%%\033[K\n", i+1, bar(pct), pct)
	}

	sb.WriteByte('\n')

	total := totalDone.Load()
	overallPct := float64(total) / float64(maxNumber) * 100.0
	if overallPct > 100 {
		overallPct = 100
	}
	fmt.Fprintf(&sb, "  \033[1mOverall\033[0m  [%s]  %5.1f%%\033[K\n", bar(overallPct), overallPct)

	elapsed := time.Since(startTime)
	elapsedSec := elapsed.Seconds()
	var rateStr, etaStr string
	if elapsedSec > 0 {
		rate := float64(total) / elapsedSec
		rateStr = humanRate(rate) + " /s"
		if total >= maxNumber {
			etaStr = "done"
		} else if rate > 0 {
			etaStr = fmt.Sprintf("%.1fs", float64(maxNumber-total)/rate)
		}
	}
	if rateStr == "" {
		rateStr = "— /s"
	}
	if etaStr == "" {
		etaStr = "—"
	}

	fmt.Fprintf(&sb, "  Elapsed: \033[97m%-9s\033[0m  \033[90m│\033[0m  Rate: \033[97m%-13s\033[0m  \033[90m│\033[0m  ETA: \033[97m%s\033[0m\033[K\n",
		elapsed.Round(time.Millisecond),
		rateStr,
		etaStr,
	)

	os.Stdout.WriteString(sb.String())
}

func worker(
	id int,
	maxNumber uint64,
	proven []uint64,
	counter *atomic.Uint64,
	coreCompleted []atomic.Uint64,
	totalDone *atomic.Uint64,
	wg *sync.WaitGroup,
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

		n := end - start
		coreCompleted[id].Add(n)
		totalDone.Add(n)
	}
}

func main() {
	var maxNumber uint64 = 10_000_000
	numCores := runtime.NumCPU()

	proven := make([]uint64, maxNumber/64+1)
	setBit(proven, 1)

	var counter atomic.Uint64
	counter.Store(2)

	coreCompleted := make([]atomic.Uint64, numCores)
	var totalDone atomic.Uint64

	// Hide cursor; restore on exit
	fmt.Print("\033[?25l")
	defer fmt.Print("\033[?25h")

	// Static header
	const inner = 53
	fmt.Printf("\033[96m┌%s┐\033[0m\n", strings.Repeat("─", inner))
	fmt.Printf("\033[96m│\033[0m \033[1m %-*s\033[0m\033[96m│\033[0m\n", inner-2, "Collatz Conjecture Verifier")
	fmt.Printf("\033[96m│\033[0m   Verifying 1 \033[33m→\033[0m %-*d\033[96m│\033[0m\n", inner-20, maxNumber)
	fmt.Printf("\033[96m└%s┘\033[0m\n", strings.Repeat("─", inner))

	startTime := time.Now()
	renderFrame(numCores, maxNumber, coreCompleted, &totalDone, startTime, true)

	doneCh := make(chan struct{})
	var displayWg sync.WaitGroup
	displayWg.Add(1)
	go func() {
		defer displayWg.Done()
		ticker := time.NewTicker(refreshMs * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				renderFrame(numCores, maxNumber, coreCompleted, &totalDone, startTime, false)
			case <-doneCh:
				return
			}
		}
	}()

	var wg sync.WaitGroup
	for i := 0; i < numCores; i++ {
		wg.Add(1)
		go worker(i, maxNumber, proven, &counter, coreCompleted, &totalDone, &wg)
	}

	wg.Wait()
	close(doneCh)
	displayWg.Wait()

	renderFrame(numCores, maxNumber, coreCompleted, &totalDone, startTime, false)
	duration := time.Since(startTime)
	fmt.Printf("\n  \033[32m\033[1m✓  Verified %d numbers in %s\033[0m\n\n",
		maxNumber, duration.Round(time.Millisecond))
}
