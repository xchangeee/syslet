.PHONY: deps check fmt test coverage build build-bin clean

deps:
	go mod tidy
	go mod download
	go mod verify

check:
	golangci-lint fmt ./...
	go fix ./...
	go vet ./...
	golangci-lint run ./...

test:
	go test $$(go list -f '{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}{{end}}' ./...)

test-integration:
	go test -tags=integration -count=1 ./test/integration/... -parallel 4

coverage:
	mkdir -p build
	go test -coverpkg=./... -coverprofile=build/unit.out \
		$$(go list -f '{{if or .TestGoFiles .XTestGoFiles}}{{.ImportPath}}{{end}}' ./...)
	go test -tags=integration -coverpkg=./... -coverprofile=build/integration.out -count=1 \
		./test/integration/...
	@printf 'mode: set\n' > build/coverage.out
	@tail -n +2 build/unit.out >> build/coverage.out
	@tail -n +2 build/integration.out >> build/coverage.out
	go tool cover -html=build/coverage.out -o build/coverage.html
	@echo "Coverage report generated: build/coverage.html"

build:
	go build ./...

build-bin:
	mkdir -p build
	GOOS=darwin GOARCH=arm64 go build -o ./build/syslet-darwin-arm64 ./cmd/syslet
	GOOS=linux GOARCH=amd64 go build -o ./build/syslet-linux-amd64 ./cmd/syslet
	GOOS=linux GOARCH=arm64 go build -o ./build/syslet-linux-arm64 ./cmd/syslet

clean:
	rm -rf ./build
