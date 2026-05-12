package main

import (
	"fmt"
	"runtime"
	"strings"
	"sync/atomic"
	"time"
)

func main() {
	var maxNumber uint64 = 1_000_000_000
	numCores := runtime.NumCPU()

	coreCompleted := make([]paddedCounter, numCores)
	var totalDone atomic.Uint64

	fmt.Print("\033[?25l")
	defer fmt.Print("\033[?25h")

	const inner = 53
	fmt.Printf("\033[96m┌%s┐\033[0m\n", strings.Repeat("─", inner))
	fmt.Printf("\033[96m│\033[0m \033[1m %-*s\033[0m\033[96m│\033[0m\n", inner-2, "Collatz Conjecture Verifier")
	fmt.Printf("\033[96m│\033[0m   Verifying 1 \033[33m→\033[0m %-*d\033[96m│\033[0m\n", inner-17, maxNumber)
	fmt.Printf("\033[96m└%s┘\033[0m\n", strings.Repeat("─", inner))

	startTime := time.Now()
	stop := RunDisplay(numCores, maxNumber, coreCompleted, &totalDone, startTime)

	duration := Verify(maxNumber, numCores, func(id int, delta uint64) {
		coreCompleted[id].Add(delta)
		totalDone.Add(delta)
	})

	stop()
	renderFrame(numCores, maxNumber, coreCompleted, &totalDone, startTime, false)
	fmt.Printf("\n  \033[32m\033[1m✓  Verified %d numbers in %s\033[0m\n\n",
		maxNumber, duration.Round(time.Millisecond))
}
