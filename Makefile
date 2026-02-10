.PHONY: build test clean build-linux-amd64 deploy

# Build all packages
build:
	go build ./...

# Build binaries for current platform
build-bin:
	go build -o syslet ./cmd/syslet
	go build -o deploy ./cmd/deploy

# Build binaries for linux amd64
build-linux-amd64:
	GOOS=linux GOARCH=amd64 go build -o syslet-linux-amd64 ./cmd/syslet
	GOOS=linux GOARCH=amd64 go build -o deploy-linux-amd64 ./cmd/deploy

# Run tests
test:
	go test ./...

# Remove build artifacts
clean:
	rm -f syslet deploy syslet-linux-amd64 deploy-linux-amd64
