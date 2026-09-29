.PHONY: build test lint fmt check

build:
	go build ./...

# -v lists every test as PASS, FAIL, or SKIP (with its reason).
test:
	go test -race -v ./...

lint:
	go vet ./...
	golangci-lint run ./...

fmt:
	gofmt -w .

# Everything CI runs.
check: build lint test
