# Claude Instructions

This project uses a Makefile. Key targets:

- `make proto` — Generate Go code from proto files (run after any `.proto` changes)
- `make build` — Generate proto + build all packages
- `make test` — Run all tests
- `make tools` — Install protoc Go plugins (one-time setup)
- `make clean` — Remove generated proto files

Always run `make proto` before `make build` or `make test` if proto files have changed. **Do NOT run `go build` or `go run` directly.**

To see source files from a dependency, or to answer questions about a dependency, run `go mod download -json MODULE` and use the returned `Dir` path to read the files.

Use `go doc foo.Bar` or `go doc -all foo` to read documentation for packages, types, functions, etc.

Use `go run .` or `go run ./cmd/foo` instead of `go build` to run programs, to avoid leaving behind build artifacts.
