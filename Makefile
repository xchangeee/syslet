.PHONY: build test clean

# Build all packages
build:
	go build ./...

# Run tests
test:
	go test ./...

# Remove build artifacts
clean:
	rm -f syslet
