PKG := github.com/tensorfoundrylabs/velocity

# Tool versions (pinned)
GOLANGCI_LINT_VERSION := v2.11.4
BETTERALIGN_VERSION := latest

GOBIN := $(shell go env GOBIN)
ifeq ($(GOBIN),)
GOBIN := $(shell go env GOPATH)/bin
endif

.PHONY: all clean test test-race test-short test-cover lint fmt vet align tidy \
        install-tools check-tools ready ready-tools ci help

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

fmt:
	@echo "Formatting..."
	@goimports -w -local $(PKG) . 2>/dev/null || true
	@go fmt ./...
	@if command -v gofumpt >/dev/null 2>&1; then \
		gofumpt -w -extra .; \
	fi
	@echo "Formatting done."

lint:
	@echo "Linting..."
	@if command -v golangci-lint > /dev/null 2>&1; then \
		INSTALLED=$$(golangci-lint --version 2>/dev/null | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' | head -1); \
		if [ "$$INSTALLED" = "$(GOLANGCI_LINT_VERSION)" ]; then \
			printf "  golangci-lint %s \033[32m(verified)\033[0m\n" "$$INSTALLED"; \
		else \
			printf "  golangci-lint %s [require: %s \033[31m(pinned)\033[0m]\n" "$$INSTALLED" "$(GOLANGCI_LINT_VERSION)"; \
		fi; \
		golangci-lint run --fix; \
	else \
		echo "  golangci-lint not found. Run 'make install-tools'."; \
		exit 1; \
	fi
	@echo "Linting done."

vet:
	@echo "Vetting..."
	@go vet ./...
	@echo "Vetting done."

align:
	@echo "Aligning structs..."
	@if command -v betteralign > /dev/null 2>&1; then \
		betteralign -apply ./...; \
	else \
		echo "  betteralign not found. Run 'make install-tools'."; \
	fi
	@echo "Alignment done."

tidy:
	@go mod download && go mod tidy

# ── Ready (pre-commit quality gate) ─────────────────────────────────────────

ready-tools: fmt align lint vet
	@printf "\033[32mCode quality checks passed.\033[0m\n"

ready: tidy fmt align lint vet test-race
	@printf "\033[32mReady for commit.\033[0m\n"

# ── CI ───────────────────────────────────────────────────────────────────────

ci: tidy fmt align lint vet test-race test-cover
	@echo "CI pipeline passed."

# ── Tools ────────────────────────────────────────────────────────────────────

install-tools:
	@echo "Installing tools..."
	@go install golang.org/x/tools/cmd/goimports@latest
	@go install mvdan.cc/gofumpt@latest
	@go install github.com/golangci/golangci-lint/v2/cmd/golangci-lint@$(GOLANGCI_LINT_VERSION)
	@go install github.com/dkorunic/betteralign/cmd/betteralign@$(BETTERALIGN_VERSION)
	@echo "Tools installed."

check-tools:
	@echo "Checking tools..."
	@printf "  go:             %s\n" "$$(go version | awk '{print $$3}')"
	@if command -v golangci-lint >/dev/null 2>&1; then \
		printf "  golangci-lint:  %s\n" "$$(golangci-lint --version 2>/dev/null | grep -oE 'v[0-9]+\.[0-9]+\.[0-9]+' | head -1)"; \
	else \
		printf "  golangci-lint:  \033[31mnot installed\033[0m\n"; \
	fi
	@if command -v betteralign >/dev/null 2>&1; then \
		printf "  betteralign:    installed\n"; \
	else \
		printf "  betteralign:    \033[31mnot installed\033[0m\n"; \
	fi
	@if command -v gofumpt >/dev/null 2>&1; then \
		printf "  gofumpt:        installed\n"; \
	else \
		printf "  gofumpt:        \033[31mnot installed\033[0m\n"; \
	fi
	@if command -v goimports >/dev/null 2>&1; then \
		printf "  goimports:      installed\n"; \
	else \
		printf "  goimports:      \033[31mnot installed\033[0m\n"; \
	fi

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
	@echo "  make test           Run tests"
	@echo "  make test-race      Run tests with race detector"
	@echo "  make test-short     Run short tests only"
	@echo "  make test-cover     Run tests with coverage report"
	@echo ""
	@echo "Quality:"
	@echo "  make fmt            Format code (goimports + gofumpt)"
	@echo "  make lint           Run golangci-lint (v2, --fix)"
	@echo "  make vet            Run go vet"
	@echo "  make align          Run betteralign (struct field alignment)"
	@echo "  make tidy           Run go mod tidy"
	@echo ""
	@echo "Ready (pre-commit):"
	@echo "  make ready          Full quality gate: tidy, fmt, align, lint, vet, test-race"
	@echo "  make ready-tools    Quick check: fmt, align, lint, vet (no tests)"
	@echo ""
	@echo "CI:"
	@echo "  make ci             Full CI pipeline: quality + tests + coverage"
	@echo ""
	@echo "Tools:"
	@echo "  make install-tools  Install golangci-lint, betteralign, goimports, gofumpt"
	@echo "  make check-tools    Show installed tool versions"
