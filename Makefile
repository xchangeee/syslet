.PHONY: deps check fmt test coverage build build-bin build-linux-amd64 clean

# Update dependencies
deps:
	go mod tidy
	go mod download
	go mod verify

# Run static analysis and linter
check:
	go vet ./...
	golangci-lint run ./...

# Format. code
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

# Build binaries
build-bin:
	mkdir -p build
	GOOS=darwin GOARCH=arm64 go build -o ./build/syslet-darwin-arm64 ./cmd/syslet
	GOOS=linux GOARCH=amd64 go build -o ./build/syslet-linux-amd64 ./cmd/syslet
	GOOS=linux GOARCH=arm64 go build -o ./build/syslet-linux-arm64 ./cmd/syslet
	GOOS=darwin GOARCH=arm64 go build -o ./build/syslet-push-darwin-arm64 ./cmd/syslet-push
	GOOS=linux GOARCH=amd64 go build -o ./build/syslet-push-linux-amd64 ./cmd/syslet-push
	GOOS=linux GOARCH=arm64 go build -o ./build/syslet-push-linux-arm64 ./cmd/syslet-push

# Remove build artifacts
clean:
	rm -rf ./build
