GO ?= go
EXE := $(shell $(GO) env GOEXE)
.PHONY: fmt fmt-check vet test test-race test-integration test-fuzz build bench vuln check
fmt:
	gofmt -w .
fmt-check:
	@files="$$(gofmt -l .)" || exit $$?; test -z "$$files" || { printf '%s\n' "$$files"; exit 1; }
vet:
	$(GO) vet ./...
test:
	$(GO) test ./...
test-race:
	$(GO) test -race ./...
test-integration:
	$(GO) test -count=1 ./test/integration
test-fuzz:
	$(GO) test ./internal/protocol -run '^$$' -fuzz '^FuzzFrame$$' -fuzztime=3s -parallel=2
	$(GO) test ./internal/protocol -run '^$$' -fuzz '^FuzzProtocol$$' -fuzztime=3s -parallel=2
	$(GO) test ./internal/identity -run '^$$' -fuzz '^FuzzIdentity$$' -fuzztime=3s -parallel=2
	$(GO) test ./internal/engine -run '^$$' -fuzz '^FuzzConfig$$' -fuzztime=3s -parallel=2
build:
	$(GO) build -trimpath -o bin/meshlink$(EXE) ./cmd/meshlink
	$(GO) build -trimpath -o bin/meshlink-registry$(EXE) ./cmd/meshlink-registry
bench:
	$(GO) test ./internal/protocol ./internal/engine ./test/integration -run '^$$' -bench . -benchmem
vuln:
	$(GO) run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...
check: fmt-check vet test test-race test-integration build
