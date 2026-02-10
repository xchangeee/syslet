.PHONY: build test clean build-linux-amd64 deploy

# Build all packages
build:
	go build ./...

# Build binaries for current platform
build-bin:
	mkdir -p build
	go build -o ./build/syslet ./cmd/syslet
	go build -o ./build/deploy ./cmd/deploy

# Build binaries for linux amd64
build-linux-amd64:
	mkdir -p build
	GOOS=linux GOARCH=amd64 go build -o ./build/syslet-linux-amd64 ./cmd/syslet
	GOOS=linux GOARCH=amd64 go build -o ./build/deploy-linux-amd64 ./cmd/deploy

# Run tests
test:
	go test ./...

# Remove build artifacts
clean:
	rm -rf ./build
