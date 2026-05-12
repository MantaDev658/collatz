.PHONY: build run dev test race bench bench-baseline bench-count bench-serial bench-quick bench-large diff esc vet fmt tidy clean install-tools profile

# GOBIN is where `go install` places binaries — may not be in $PATH.
GOBIN := $(shell go env GOPATH)/bin

# Default target
build:
	go build .

run:
	go run .

# Full dev-cycle chain: format → vet → build → race test
# Ordered cheapest-first: fmt and vet catch obvious issues fast;
# build confirms compilation before the slow race test suite runs.
dev: fmt tidy vet build test race
	@echo ""
	@echo "All checks passed."

# Testing
test:
	go test ./...

race:
	go test -race ./...

# Benchmarks
bench:
	go test -bench=. -benchtime=5s -benchmem -run=^$ ./...

# Fast iteration during development (1M range, quick turnaround)
bench-quick:
	go test -bench=. -benchtime=3s -benchmem -run=^$ ./...

# Large-range benchmark showcasing the floor optimization at 1B (bitmap exceeds L3 cache).
# Expect 30+ seconds total.
bench-large:
	go test -bench=BenchmarkVerify1B -benchtime=60s -benchmem -run=^$ ./...

# Run with a single OS thread to measure single-core throughput.
# Parallelism speedup ≈ bench-serial ns/op ÷ bench ns/op (same range).
bench-serial:
	GOMAXPROCS=1 go test -bench=. -benchtime=5s -benchmem ./...

# Save a 10-run sample as the baseline for future comparisons.
bench-baseline:
	go test -bench=. -count=10 -benchmem ./... | tee bench-baseline.txt

# Capture a new 10-run sample (used as input to diff).
bench-count:
	go test -bench=. -count=10 -benchmem ./... | tee bench.txt

# Compare current performance against the saved baseline.
# Requires: make bench-baseline (once) and make install-tools (once).
diff:
	@test -f bench-baseline.txt || { echo "No baseline found — run 'make bench-baseline' first"; exit 1; }
	@test -x $(GOBIN)/benchstat || { echo "benchstat not found — run 'make install-tools' first"; exit 1; }
	go test -bench=. -count=10 -benchmem ./... | tee bench.txt
	$(GOBIN)/benchstat bench-baseline.txt bench.txt

# Install external dev tools into GOBIN (only needs to run once).
# If 'benchstat' is not found on your PATH afterwards, add GOBIN to it:
#   echo 'export PATH="$$PATH:$(go env GOPATH)/bin"' >> ~/.zshrc
install-tools:
	go install golang.org/x/perf/cmd/benchstat@latest
	@echo ""
	@echo "Installed to $(GOBIN)"
	@echo "If 'benchstat' is not on your PATH, add this to ~/.zshrc (or ~/.bashrc):"
	@echo '  export PATH="$$PATH:$(GOBIN)"'

# Show escape analysis for our source files only (filters stdlib noise).
# -m gives verdicts; -m=2 adds verbose flow traces (rarely needed).
# What to look for: "does not escape" on proven/counter in collatz.go.
esc:
	go build -gcflags="-m" . 2>&1 | grep "^\.\/"

# Code quality
vet:
	go vet ./...

fmt:
	gofmt -w .

tidy:
	go mod tidy

# CPU profiling — writes cpu.prof then prints instructions for exploring it
profile:
	go test -bench=BenchmarkVerify100M -benchtime=30s -cpuprofile=cpu.prof -run=^$ ./...
	@echo ""
	@echo "Profile written to cpu.prof. To explore:"
	@echo "  go tool pprof -top cpu.prof"
	@echo "  go tool pprof -http=:6060 cpu.prof"

clean:
	rm -f collatz collatz.test bench.txt bench-baseline.txt *.prof
