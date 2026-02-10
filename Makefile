.PHONY: build test clean build-linux-amd64

# Build all packages
build:
	go build ./...

# Build binaries for current platform
build-bin:
	mkdir -p build
	go build -o ./build/syslet ./cmd/syslet
	go build -o ./build/syslet-push ./cmd/syslet-push

# Build binaries for linux amd64
build-linux-amd64:
	mkdir -p build
	GOOS=linux GOARCH=amd64 go build -o ./build/syslet-linux-amd64 ./cmd/syslet
	GOOS=linux GOARCH=amd64 go build -o ./build/syslet-push-linux-amd64 ./cmd/syslet-push

# Run tests
test:
	go test ./...


fmt:
	go fmt ./...


vet:
	go vet ./...

# Remove build artifacts
clean:
	rm -rf ./build
