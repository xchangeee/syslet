.PHONY: all proto build test clean

all: proto build

# Generate Go code from proto files
proto: clean
	protoc \
		--go_out=. --go_opt=paths=source_relative \
		--go-grpc_out=. --go-grpc_opt=paths=source_relative \
		proto/syslet.proto

# Build binaries
build: proto
	go build ./...

# Run tests
test:
	go test ./...

# Install protoc Go plugins (one-time setup)
tools:
	go install google.golang.org/protobuf/cmd/protoc-gen-go@latest
	go install google.golang.org/grpc/cmd/protoc-gen-go-grpc@latest

# Clean generated files
clean:
	find proto -name '*.pb.go' -delete

# Run the daemon (for development)
run-daemon:
	go run ./cmd/syslet

# Run the CLI
run-cli:
	go run ./cmd/rsctl
