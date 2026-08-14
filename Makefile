# go-dicom Makefile
# Run 'make help' to see all available targets

# ─── Configuration ───────────────────────────────────────────────────────────
BINARY      := dicom
GO          := go
GOFLAGS     := -race
TIMEOUT     := 5m
COVER_OUT   := coverage.out
COVER_HTML  := coverage.html
GOBIN       := $(shell go env GOPATH)/bin

# golangci-lint pinned to the version the lint job in .github/workflows/test.yml
# uses. .golangci.yml is a v2 configuration, and a v1 binary refuses it outright
# rather than degrading, so an unpinned "@latest" install is how a local run ends
# up unable to lint at all.
LINT_MODULE  := github.com/golangci/golangci-lint/v2/cmd/golangci-lint
LINT_VERSION := v2.12.2
LINT         := $(shell which golangci-lint 2>/dev/null || echo $(GOBIN)/golangci-lint)

# Read the version out of main.go rather than repeating it here.
#
# This was a literal, and it said 1.1.0 for the whole of 1.2, 1.3 and 1.4: every
# `make build` stamped -X main.Version=1.1.0 over the correct default in main.go,
# so a locally built binary reported a version three releases old. Released
# binaries were unaffected — build-release.yml stamps the git tag — which is
# exactly why nobody noticed.
#
# version_test.go already guards main.go against network.DefaultImplementationVersionName.
# Deriving from it here means there is no third copy left to drift.
VERSION     := $(shell sed -n 's/^var Version = "\(.*\)"$$/\1/p' main.go)

# Network defaults (overridable: make echoscu ADDR=pacs:11112)
ADDR        ?= 127.0.0.1:11112
AET         ?= GODICOM
AEC         ?= ANY-SCP
PORT        ?= 11112
OUTPUT_DIR  ?= ./received
LEVEL       ?= STUDY

# Default target: show help when running 'make' with no arguments
.DEFAULT_GOAL := help

.PHONY: all build install test test-race test-cover test-network test-integration \
        lint fmt vet clean tidy help \
        echoscu storescu storescp findscu movescu \
        run-echo-server run-store-server

# ─── Core ────────────────────────────────────────────────────────────────────

all: fmt vet test build  ## Run fmt, vet, test, build (CI pipeline)
	@echo ""
	@echo "Done. Run 'make lint' separately if golangci-lint is installed."

build:  ## Build the CLI binary
	$(GO) build -ldflags "-X main.Version=$(VERSION)" -o $(BINARY) .
	@echo "Built: ./$(BINARY)"

install:  ## Install the CLI to $GOPATH/bin
	$(GO) install -ldflags "-X main.Version=$(VERSION)" .

# ─── Testing ─────────────────────────────────────────────────────────────────

test:  ## Run all tests with verbose output
	$(GO) test -v -timeout $(TIMEOUT) ./...

test-race:  ## Run all tests with race detector
	$(GO) test $(GOFLAGS) -timeout $(TIMEOUT) ./...

test-cover:  ## Run tests with coverage report (opens HTML)
	$(GO) test $(GOFLAGS) -coverprofile=$(COVER_OUT) -covermode=atomic -timeout $(TIMEOUT) ./...
	$(GO) tool cover -html=$(COVER_OUT) -o $(COVER_HTML)
	@echo "Coverage report: $(COVER_HTML)"

test-network:  ## Run network package tests only
	$(GO) test -v $(GOFLAGS) -timeout $(TIMEOUT) ./network/...

test-integration:  ## Run network integration tests (real TCP SCP+SCU)
	$(GO) test -v $(GOFLAGS) -timeout $(TIMEOUT) -run TestIntegration ./network/...

test-short:  ## Run tests without race detector (faster)
	$(GO) test -v -timeout $(TIMEOUT) -short ./...

bench:  ## Run benchmarks
	$(GO) test -bench=. -benchmem -timeout $(TIMEOUT) ./...

# ─── Code Quality ────────────────────────────────────────────────────────────

lint:  ## Run golangci-lint (optional, install: make lint prints the command)
	@if [ -x "$(LINT)" ]; then \
		$(LINT) run ./...; \
	else \
		echo "golangci-lint not found — skipping. Install the version CI uses:"; \
		echo "  go install $(LINT_MODULE)@$(LINT_VERSION)"; \
		echo "  Then ensure ~/go/bin is in your PATH"; \
	fi

fmt:  ## Format all Go source files
	gofmt -s -w .

vet:  ## Run go vet
	$(GO) vet ./...

tidy:  ## Tidy go.mod and go.sum
	$(GO) mod tidy

# ─── DICOM Network Commands ─────────────────────────────────────────────────
# These targets build the binary if needed and run network operations.
# Override variables on the command line:
#   make echoscu ADDR=pacs.hospital.com:11112 AEC=PACS
#   make storescu ADDR=pacs:11112 FILES="study/*.dcm"
#   make storescp PORT=4242 OUTPUT_DIR=./data

echoscu: build  ## C-ECHO: ping a DICOM server (ADDR=host:port AEC=title)
	./$(BINARY) echoscu -aet $(AET) -aec $(AEC) $(ADDR)

storescu: build  ## C-STORE: send DICOM files (ADDR=host:port FILES="*.dcm")
	@[ -n "$(FILES)" ] || { echo "Usage: make storescu ADDR=host:port FILES=\"path/to/*.dcm\""; exit 1; }
	./$(BINARY) storescu -aet $(AET) -aec $(AEC) $(ADDR) $(FILES)

storescp: build  ## C-STORE SCP: receive files (PORT=11112 OUTPUT_DIR=./received)
	@mkdir -p $(OUTPUT_DIR)
	./$(BINARY) storescp -aet $(AET) -port $(PORT) -output $(OUTPUT_DIR)

findscu: build  ## C-FIND: query for studies (ADDR=host:port PATIENT_NAME="Smith*")
	./$(BINARY) findscu -aet $(AET) -aec $(AEC) -level $(LEVEL) \
		$(if $(PATIENT_NAME),-patient-name "$(PATIENT_NAME)") \
		$(if $(PATIENT_ID),-patient-id "$(PATIENT_ID)") \
		$(ADDR)

movescu: build  ## C-MOVE: retrieve to destination (ADDR=host:port DEST=AE STUDY_UID=...)
	@[ -n "$(DEST)" ] || { echo "Usage: make movescu ADDR=host:port DEST=MY_SCP STUDY_UID=1.2.3"; exit 1; }
	./$(BINARY) movescu -aet $(AET) -aec $(AEC) -dest $(DEST) -level $(LEVEL) \
		$(if $(STUDY_UID),-study "$(STUDY_UID)") \
		$(if $(SERIES_UID),-series "$(SERIES_UID)") \
		$(ADDR)

# ─── Quick Server Targets ───────────────────────────────────────────────────

run-echo-server: build  ## Start an echo-only verification server (PORT=11112)
	@echo "Starting Echo SCP on port $(PORT)... (Ctrl+C to stop)"
	./$(BINARY) storescp -aet $(AET) -port $(PORT) -output /dev/null

run-store-server: build  ## Start a storage server that saves files (PORT=11112 OUTPUT_DIR=./received)
	@mkdir -p $(OUTPUT_DIR)
	@echo "Starting Store SCP on port $(PORT), saving to $(OUTPUT_DIR)... (Ctrl+C to stop)"
	./$(BINARY) storescp -aet $(AET) -port $(PORT) -output $(OUTPUT_DIR)

# ─── Cleanup ─────────────────────────────────────────────────────────────────

clean:  ## Remove build artifacts
	rm -f $(BINARY) $(COVER_OUT) $(COVER_HTML)
	$(GO) clean -cache -testcache

clean-received:  ## Remove received DICOM files
	rm -rf $(OUTPUT_DIR)

# ─── Help ────────────────────────────────────────────────────────────────────

help:  ## Show this help
	@echo "go-dicom v$(VERSION) — DICOM library and networking toolkit"
	@echo ""
	@echo "Usage: make [target] [VAR=value ...]"
	@echo ""
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | \
		awk 'BEGIN {FS = ":.*?## "}; { \
			if ($$1 ~ /^(all|build|install)$$/) section = "Core"; \
			else if ($$1 ~ /^test/) section = "Testing"; \
			else if ($$1 ~ /^(lint|fmt|vet|tidy|bench)$$/) section = "Quality"; \
			else if ($$1 ~ /^(echoscu|storescu|storescp|findscu|movescu)$$/) section = "Network Commands"; \
			else if ($$1 ~ /^run-/) section = "Servers"; \
			else if ($$1 ~ /^clean/) section = "Cleanup"; \
			else section = "Other"; \
			printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2 \
		}'
	@echo ""
	@echo "Network Variables:"
	@echo "  ADDR          Target address            (default: $(ADDR))"
	@echo "  AET           Calling AE title           (default: $(AET))"
	@echo "  AEC           Called AE title            (default: $(AEC))"
	@echo "  PORT          SCP listen port            (default: $(PORT))"
	@echo "  OUTPUT_DIR    Received files directory   (default: $(OUTPUT_DIR))"
	@echo "  LEVEL         Query retrieve level       (default: $(LEVEL))"
	@echo "  FILES         DICOM files to send        (storescu only)"
	@echo "  DEST          Move destination AE        (movescu only)"
	@echo "  PATIENT_NAME  Patient name filter        (findscu only)"
	@echo "  PATIENT_ID    Patient ID filter          (findscu only)"
	@echo "  STUDY_UID     Study Instance UID         (movescu only)"
	@echo "  SERIES_UID    Series Instance UID        (movescu only)"
	@echo ""
	@echo "Examples:"
	@echo "  make build                                      # Build CLI"
	@echo "  make test-network                                # Test network module"
	@echo "  make test-integration                            # Run integration tests"
	@echo "  make echoscu ADDR=pacs:11112 AEC=PACS            # Ping a PACS"
	@echo "  make storescu ADDR=pacs:11112 FILES=study/*.dcm  # Send files"
	@echo "  make storescp PORT=4242 OUTPUT_DIR=./data        # Receive files"
	@echo "  make findscu ADDR=pacs:11112 PATIENT_NAME=Smith* # Query"
	@echo "  make run-store-server PORT=11112                 # Quick server"
