package main

import (
	"fmt"
	"os"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

const (
	barWidth  = 32
	refreshMs = 100
)

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

func renderFrame(numCores int, maxNumber uint64, coreCompleted []paddedCounter, totalDone *atomic.Uint64, startTime time.Time, first bool) {
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

// RunDisplay starts a background goroutine that redraws the progress TUI every
// refreshMs milliseconds. The returned stop func signals it to exit and waits
// for the final frame to be written before returning.
func RunDisplay(numCores int, maxNumber uint64, coreCompleted []paddedCounter, totalDone *atomic.Uint64, startTime time.Time) func() {
	renderFrame(numCores, maxNumber, coreCompleted, totalDone, startTime, true)

	doneCh := make(chan struct{})
	var wg sync.WaitGroup
	wg.Add(1)
	go func() {
		defer wg.Done()
		ticker := time.NewTicker(refreshMs * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				renderFrame(numCores, maxNumber, coreCompleted, totalDone, startTime, false)
			case <-doneCh:
				return
			}
		}
	}()

	return func() {
		close(doneCh)
		wg.Wait()
	}
}
