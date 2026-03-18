.PHONY: all build test test-cover lint fmt vet clean tidy

all: fmt vet test build

build:
	go build -o dicom .

test:
	go test -v -race -timeout 5m ./...

test-cover:
	go test -race -coverprofile=coverage.out -covermode=atomic ./...
	go tool cover -html=coverage.out -o coverage.html
	@echo "Coverage report: coverage.html"

lint:
	@which golangci-lint > /dev/null 2>&1 || { echo "golangci-lint not installed. Install with: go install github.com/golangci/golangci-lint/cmd/golangci-lint@latest"; exit 1; }
	golangci-lint run ./...

fmt:
	gofmt -s -w .

vet:
	go vet ./...

clean:
	rm -f dicom coverage.out coverage.html

tidy:
	go mod tidy
