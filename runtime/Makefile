GO_VERSION := $(strip $(shell cat .go-version))
GO ?= go
GOFMT ?= gofmt
PACKAGES ?= ./...
RUN ?= .
TEST_FLAGS ?=
export GOTOOLCHAIN := go$(GO_VERSION)

.PHONY: test test-tdd test-existing test-compile test-inventory vet build fmt check-fmt check check-contracts

# Full suite includes behavior contracts and production composition tests.
test: test-inventory
	$(GO) test $(PACKAGES) -run '$(RUN)' $(TEST_FLAGS)

# Real kernel isolation; ordinary go test does not substitute for this target.
.PHONY: test-sandbox
test-sandbox:
	bash scripts/test-sandbox-linux.sh -count=1 -v

test-tdd: test-inventory
	$(GO) test $(PACKAGES) -run '^TestTDD' $(TEST_FLAGS)

# Explicit baseline selection only; this is not a replacement for the full suite.
test-existing: test-inventory
	$(GO) test $(PACKAGES) -skip '^TestTDD' $(TEST_FLAGS)

test-compile: test-inventory
	$(GO) test $(PACKAGES) -run '^$$'

test-inventory:
	GO='$(GO)' node ../scripts/check-contracts.mjs

vet:
	$(GO) vet ./...

build:
	$(GO) build ./...

fmt:
	$(GOFMT) -w cmd internal tests

check-fmt:
	@test -z "$$($(GOFMT) -l cmd internal tests)" || { $(GOFMT) -l cmd internal tests; exit 1; }

# Full behavior and static verification.
check: check-docs check-fmt test vet build

# Compilation/static/planning checks only, NOT a behavior-test success claim.
check-contracts: check-fmt test-compile test-inventory vet build

.PHONY: docs check-docs
docs:
	node ../scripts/generate-molr-docs.mjs

check-docs:
	node ../scripts/generate-molr-docs.mjs --check
