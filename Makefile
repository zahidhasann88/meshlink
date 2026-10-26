GO ?= go
EXE := $(shell $(GO) env GOEXE)
FUZZTIME ?= 3s
FUZZ_TARGETS := protocol:FuzzFrame protocol:FuzzProtocol identity:FuzzIdentity engine:FuzzConfig relay:FuzzRelayMessage discovery:FuzzRecordVerify
.PHONY: fmt fmt-check vet test test-race test-integration test-fuzz build bench vuln docker-smoke check
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
	@for t in $(FUZZ_TARGETS); do \
		pkg=$${t%%:*}; name=$${t##*:}; \
		$(GO) test ./internal/$$pkg -run '^$$' -fuzz "^$$name\$$" -fuzztime=$(FUZZTIME) -parallel=2 || exit 1; \
	done
build:
	$(GO) build -trimpath -o bin/ ./cmd/...
bench:
	$(GO) test ./internal/protocol ./test/integration -run '^$$' -bench . -benchmem
vuln:
	$(GO) run golang.org/x/vuln/cmd/govulncheck@v1.9.0 ./...
docker-smoke:
	bash scripts/docker-smoke.sh
check: fmt-check vet test test-race test-integration build
