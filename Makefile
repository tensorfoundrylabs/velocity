PKG := github.com/tensorfoundry/velocity

# ── Tool pins ────────────────────────────────────────────────────────────────
# The gate must reproduce on any machine, so every tool that formats, aligns or
# lints is pinned and enforced: a missing or mismatched tool fails the gate with
# the fix command rather than being skipped. Versions were chosen against the
# go1.26 toolchain that builds them and the go 1.24 module directive (gofumpt
# derives -lang from go.mod, so its output stays 1.24-compatible).
#   golangci-lint  v2.11.4   self-reports via --version
#   betteralign    v0.11.0   no --version; checked via `go version -m`.
#                            Applies with -apply (-fix is inert in this release)
#                            and still exits 3 after fixing, so align tolerates
#                            3 from the apply pass and proves cleanliness with a
#                            read-only pass instead.
#   gofumpt        v0.10.0   self-reports via -version
#   goimports      v0.50.0   golang.org/x/tools v0.50.0; no --version, checked
#                            via `go version -m`
GOLANGCI_LINT_VERSION := v2.11.4
BETTERALIGN_VERSION   := v0.11.0
GOFUMPT_VERSION        := v0.10.0
GOIMPORTS_VERSION      := v0.50.0
BENCHSTAT_VERSION      := v0.0.0-20250106010028-fc9b84ea4b35

GOBIN := $(shell go env GOBIN)
ifeq ($(GOBIN),)
GOBIN := $(shell go env GOPATH)/bin
endif

.PHONY: all clean test test-race test-short test-cover lint lint-fix fmt fmt-check \
        vet align align-check tidy tidy-check verify-tools \
        install-tools check-tools ready ready-tools ci help \
        bench bench-baseline perf-gate \
        bench-compare bench-compare-short

# ── Test ─────────────────────────────────────────────────────────────────────

all: clean tidy test lint vet

test:
	@echo "Running tests..."
	@go test ./... -count=1
	@echo "Tests passed."

test-race:
	@echo "Running tests with race detector..."
	@go test ./... -race -count=1
	@echo "Race tests passed."

test-short:
	@echo "Running short tests..."
	@go test ./... -short -count=1
	@echo "Short tests passed."

test-cover:
	@echo "Running tests with coverage..."
	@go test ./... -coverprofile=coverage.out -count=1
	@go tool cover -func=coverage.out | tail -1
	@echo "Coverage report: coverage.out"

# ── Code Quality ─────────────────────────────────────────────────────────────
# Mutating targets keep their historical names (fmt, align, lint-fix, tidy);
# the -check variants and lint/tidy-check are read-only and are the only things
# `make ready` and `make ci` run. gofumpt and goimports recurse from the repo
# root into the nested benchmarks module; go tool ./... and betteralign package
# patterns do not cross its module boundary, so align/align-check/vet give it an
# explicit pass (its vet/test coverage belongs to the bench-compare flows).

fmt: verify-tools
	@echo "Formatting (rewrites files)..."
	@goimports -w -local $(PKG) .
	@gofumpt -w -extra .
	@echo "Formatting done."

fmt-check: verify-tools
	@echo "Checking formatting..."
	@out=$$(goimports -l -local $(PKG) .) || exit $$?; \
	if [ -n "$$out" ]; then \
		printf "\033[31m  goimports would rewrite:\n%s\033[0m\n" "$$out"; \
		exit 1; \
	fi; \
	out=$$(gofumpt -l -extra .) || exit $$?; \
	if [ -n "$$out" ]; then \
		printf "\033[31m  gofumpt -extra would rewrite:\n%s\033[0m\n" "$$out"; \
		exit 1; \
	fi; \
	echo "Formatting clean."

lint: verify-tools
	@echo "Linting..."
	@golangci-lint config verify && golangci-lint run
	@echo "Linting done."

lint-fix: verify-tools
	@echo "Linting with --fix (rewrites files)..."
	@golangci-lint run --fix
	@echo "Lint fix done."

vet:
	@echo "Vetting..."
	@go vet ./...
	@cd benchmarks && go vet ./...
	@echo "Vetting done."

align: verify-tools
	@echo "Aligning structs (rewrites files)..."
	@rc=0; betteralign -apply ./... || rc=$$?; \
	if [ $$rc -ne 0 ] && [ $$rc -ne 3 ]; then exit $$rc; fi
	@cd benchmarks && { rc=0; betteralign -apply ./... || rc=$$?; \
		if [ $$rc -ne 0 ] && [ $$rc -ne 3 ]; then exit $$rc; fi; }
	@betteralign ./... && cd benchmarks && betteralign ./...
	@echo "Alignment done."

align-check: verify-tools
	@echo "Checking struct alignment..."
	@betteralign ./... && cd benchmarks && betteralign ./...
	@echo "Alignment clean."

tidy:
	@go mod download && go mod tidy
	@cd benchmarks && go mod tidy

tidy-check:
	@echo "Checking module tidiness..."
	@go mod tidy -diff
	@cd benchmarks && go mod tidy -diff
	@echo "Modules tidy."

# verify-tools: hard gate on presence and exact version of every pinned tool.
# gofumpt and golangci-lint self-report; goimports and betteralign have no
# version flag, so their pin is checked against the module metadata that
# `go version -m` embeds in the binary.
verify-tools:
	@set -e; \
	for t in goimports gofumpt betteralign golangci-lint; do \
		if ! command -v $$t >/dev/null 2>&1; then \
			printf "\033[31m  %s not found. Run 'make install-tools'.\033[0m\n" "$$t"; \
			exit 1; \
		fi; \
	done; \
	v=$$(gofumpt -version 2>/dev/null | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' | head -1); \
	if [ "$$v" != "$(GOFUMPT_VERSION)" ]; then \
		printf "\033[31m  gofumpt %s installed, gate pins %s. Run 'make install-tools'.\033[0m\n" "$$v" "$(GOFUMPT_VERSION)"; \
		exit 1; \
	fi; \
	v=$$(golangci-lint --version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1); \
	if [ "$$v" != "$(GOLANGCI_LINT_VERSION:v%=%)" ]; then \
		printf "\033[31m  golangci-lint %s installed, gate pins %s. Run 'make install-tools'.\033[0m\n" "$$v" "$(GOLANGCI_LINT_VERSION)"; \
		exit 1; \
	fi; \
	v=$$(go version -m $$(command -v betteralign) | awk '$$1 == "mod" { print $$3 }'); \
	if [ "$$v" != "$(BETTERALIGN_VERSION)" ]; then \
		printf "\033[31m  betteralign %s installed, gate pins %s. Run 'make install-tools'.\033[0m\n" "$$v" "$(BETTERALIGN_VERSION)"; \
		exit 1; \
	fi; \
	v=$$(go version -m $$(command -v goimports) | awk '$$1 == "mod" { print $$3 }'); \
	if [ "$$v" != "$(GOIMPORTS_VERSION)" ]; then \
		printf "\033[31m  goimports %s installed, gate pins %s. Run 'make install-tools'.\033[0m\n" "$$v" "$(GOIMPORTS_VERSION)"; \
		exit 1; \
	fi; \
	printf "  tools verified: gofumpt %s, golangci-lint %s, betteralign %s, goimports %s\n" \
		"$(GOFUMPT_VERSION)" "$(GOLANGCI_LINT_VERSION)" "$(BETTERALIGN_VERSION)" "$(GOIMPORTS_VERSION)"

# ── Ready (pre-commit quality gate) ──────────────────────────────────────────
# Read-only: every step either checks without writing or runs tests. Run the
# mutating targets (fmt, align, lint-fix) yourself first when normalising.

ready-tools: verify-tools fmt-check align-check lint vet
	@printf "\033[32mCode quality checks passed.\033[0m\n"

ready: verify-tools tidy-check fmt-check align-check lint vet test test-race
	@printf "\033[32mReady for commit.\033[0m\n"

# ── CI ───────────────────────────────────────────────────────────────────────

ci: verify-tools tidy-check fmt-check align-check lint vet test-race test-cover
	@echo "CI pipeline passed."

# ── Tools ────────────────────────────────────────────────────────────────────

install-tools:
	@echo "Installing tools..."
	@go install golang.org/x/tools/cmd/goimports@$(GOIMPORTS_VERSION)
	@go install mvdan.cc/gofumpt@$(GOFUMPT_VERSION)
	@go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	@go install github.com/dkorunic/betteralign/cmd/betteralign@$(BETTERALIGN_VERSION)
	@go install golang.org/x/perf/cmd/benchstat@$(BENCHSTAT_VERSION)
	@echo "Tools installed."

check-tools:
	@echo "Checking tools..."
	@printf "  go:             %s\n" "$$(go version | awk '{print $$3}')"
	@if command -v gofumpt >/dev/null 2>&1; then \
		printf "  gofumpt:        %s\n" "$$(gofumpt -version 2>/dev/null | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' | head -1) (pin: $(GOFUMPT_VERSION))"; \
	else \
		printf "  gofumpt:        \033[31mnot installed\033[0m\n"; \
	fi
	@if command -v golangci-lint >/dev/null 2>&1; then \
		printf "  golangci-lint:  %s\n" "$$(golangci-lint --version 2>/dev/null | grep -oE '[0-9]+\.[0-9]+\.[0-9]+' | head -1) (pin: $(GOLANGCI_LINT_VERSION:v%=%))"; \
	else \
		printf "  golangci-lint:  \033[31mnot installed\033[0m\n"; \
	fi
	@if command -v betteralign >/dev/null 2>&1; then \
		printf "  betteralign:    %s\n" "$$(go version -m $$(command -v betteralign) | awk '$$1 == "mod" { print $$3 }') (pin: $(BETTERALIGN_VERSION))"; \
	else \
		printf "  betteralign:    \033[31mnot installed\033[0m\n"; \
	fi
	@if command -v goimports >/dev/null 2>&1; then \
		printf "  goimports:      %s\n" "$$(go version -m $$(command -v goimports) | awk '$$1 == "mod" { print $$3 }') (pin: $(GOIMPORTS_VERSION))"; \
	else \
		printf "  goimports:      \033[31mnot installed\033[0m\n"; \
	fi
	@if command -v benchstat >/dev/null 2>&1; then \
		printf "  benchstat:      %s (optional, needed for perf-gate)\n" "$$(go version -m $$(command -v benchstat) | awk '$$1 == "mod" { print $$3 }')"; \
	else \
		printf "  benchstat:      \033[33mnot installed (optional, needed for perf-gate)\033[0m\n"; \
	fi

# ── Benchmarks ───────────────────────────────────────────────────────────────

# bench: quick single-run with allocs — use for spot-checking during development.
bench:
	@echo "Running benchmarks..."
	@go test -bench=. -benchmem -count=3 ./...

# bench-baseline: captures count=10 run to docs/bench-baseline.txt.
# Run this after any intentional perf improvement so the gate tracks the new normal.
bench-baseline:
	@echo "Capturing baseline..."
	@mkdir -p docs
	@go test -bench=. -benchmem -count=10 ./... > docs/bench-baseline.txt 2>&1
	@echo "Baseline written to docs/bench-baseline.txt"

# perf-gate: gates on allocation counts vs docs/bench-baseline.txt.
# Allocation counts are deterministic (unlike timing on Windows with short runs),
# so any increase in allocs/op is a definitive regression regardless of count.
# Timing regressions are logged informatively but do not fail the gate here —
# use "make bench-baseline" + manual benchstat for timing verification at release.
# Not run by `make ready` — invoke manually before tagging or when changing hot paths.
perf-gate:
	@if [ ! -f docs/bench-baseline.txt ]; then \
		printf "\033[33m  no baseline found at docs/bench-baseline.txt -- skipping perf gate\033[0m\n"; \
		exit 0; \
	fi
	@echo "Running perf gate (allocation counts)..."
	@go test -bench=. -benchmem -count=5 ./... > /tmp/bench-current.txt 2>&1
	@if command -v benchstat >/dev/null 2>&1; then \
		benchstat -col /pkg docs/bench-baseline.txt /tmp/bench-current.txt > /tmp/bench-delta.txt 2>&1 || \
		benchstat docs/bench-baseline.txt /tmp/bench-current.txt > /tmp/bench-delta.txt 2>&1; \
		if awk '/allocs\/op/ && !/~/ && /\+[0-9]/ { print "ALLOC REGRESSION:", $$0; found=1 } END { exit found+0 }' /tmp/bench-delta.txt; then \
			printf "\033[32m  perf gate passed (zero-alloc paths unchanged)\033[0m\n"; \
		else \
			printf "\033[31m  perf gate FAILED -- allocation count regression detected\033[0m\n"; \
			grep "allocs/op" /tmp/bench-delta.txt; \
			exit 1; \
		fi; \
	else \
		printf "\033[33m  benchstat not installed -- skipping alloc comparison\033[0m\n"; \
		printf "\033[33m  run: go install golang.org/x/perf/cmd/benchstat@$(BENCHSTAT_VERSION)\033[0m\n"; \
	fi

# ── Comparative benchmarks ───────────────────────────────────────────────────

## bench-compare: Run comparative benchmarks against other logging libraries
bench-compare:
	cd benchmarks && go test -bench=. -benchmem -count=3 -benchtime=2s ./...

## bench-compare-short: Quick comparative benchmark (1 iteration)
bench-compare-short:
	cd benchmarks && go test -bench=. -benchmem -count=1 ./...

# ── Cleanup ──────────────────────────────────────────────────────────────────

clean:
	@rm -rf coverage.out
	@go clean -cache -testcache
	@echo "Clean."

# ── Help ─────────────────────────────────────────────────────────────────────

help:
	@echo "TensorFoundry Terminal Velocity Makefile"
	@echo "				tensorfoundry.io"
	@echo ""
	@echo "Test:"
	@echo "  make test                Run tests"
	@echo "  make test-race           Run tests with race detector"
	@echo "  make test-short          Run short tests only"
	@echo "  make test-cover          Run tests with coverage report"
	@echo ""
	@echo "Quality (read-only checks):"
	@echo "  make fmt-check           Verify formatting (goimports + gofumpt -extra)"
	@echo "  make align-check         Verify struct field alignment (betteralign)"
	@echo "  make lint                Run golangci-lint (read-only, version-pinned)"
	@echo "  make vet                 Run go vet (root + benchmarks module)"
	@echo "  make tidy-check          Verify go.mod tidiness (go mod tidy -diff)"
	@echo "  make verify-tools        Fail unless gate tools match pinned versions"
	@echo ""
	@echo "Quality (rewrites files):"
	@echo "  make fmt                 Format code (goimports + gofumpt -extra)"
	@echo "  make align               Apply struct field alignment (betteralign -apply)"
	@echo "  make lint-fix            Run golangci-lint with --fix"
	@echo "  make tidy                Run go mod tidy (root + benchmarks module)"
	@echo ""
	@echo "Ready (pre-commit):"
	@echo "  make ready               Read-only gate: pinned tools, tidy, fmt, align, lint, vet, test-race"
	@echo "  make ready-tools         Quick read-only check: fmt, align, lint, vet (no tests)"
	@echo ""
	@echo "CI:"
	@echo "  make ci                  Full read-only CI pipeline: quality + tests + coverage"
	@echo ""
	@echo "Benchmarks:"
	@echo "  make bench               Quick bench run (count=3) with allocs"
	@echo "  make bench-baseline      Capture count=10 run to docs/bench-baseline.txt"
	@echo "  make perf-gate           Compare allocs vs baseline; fail on any alloc/op regression"
	@echo "  make bench-compare       Compare against zap, zerolog, slog, charmbracelet, pterm"
	@echo "  make bench-compare-short Quick single-run comparison"
	@echo ""
	@echo "Tools:"
	@echo "  make install-tools       Install pinned golangci-lint, betteralign, goimports, gofumpt, benchstat"
	@echo "  make check-tools         Show installed tool versions against pins"
