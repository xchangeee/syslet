.PHONY: deps check fmt test coverage build build-bin build-linux-amd64 clean

deps:
	go mod tidy
	go mod download
	go mod verify

check:
	go vet ./...
	golangci-lint run ./...

fmt:
	go fmt ./...

# Run tests
test:
	go test ./...

# Generate test coverage report
coverage:
	go test -coverprofile=build/coverage.out ./...
	go tool cover -html=build/coverage.out -o build/coverage.html
	@echo "Coverage report generated: build/coverage.html"

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

# Remove build artifacts
clean:
	rm -rf ./build
