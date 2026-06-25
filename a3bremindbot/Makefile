.PHONY: test test-v test-store test-race test-all

# Run all tests with cached results.
test:
	go test ./...

# Run all tests with verbose output, no cache.
test-v:
	go test ./... -v -count=1

# Run only store-layer tests.
test-store:
	go test ./internal/store/... -v -count=1

# Run tests with race detector.
test-race:
	go test ./... -race -count=1

# Run all tests verbosely with race detection.
test-all:
	go test ./... -v -race -count=1
