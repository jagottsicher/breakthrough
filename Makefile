GOLANGCI_LINT ?= golangci-lint
GO_CALLVIS ?= go-callvis

.PHONY: help fmt vet lint test generate check pre-task post-task

help:
	@echo "fmt        - fail if any file needs gofmt"
	@echo "vet        - go vet ./..."
	@echo "lint       - golangci-lint run ./..."
	@echo "test       - go test ./... -race -cover"
	@echo "generate   - regenerate docs/code-index.json and docs/images/callgraph.svg"
	@echo "check      - fmt, vet, lint, test, generate, then fail on any resulting drift"
	@echo "pre-task   - show branch/status, run baseline tests"
	@echo "post-task  - alias for check, run once a change is ready to commit"

fmt:
	@unformatted="$$(gofmt -l .)"; \
	if [ -n "$$unformatted" ]; then \
		echo "gofmt needs to run on:"; \
		echo "$$unformatted"; \
		exit 1; \
	fi

vet:
	go vet ./...

lint:
	$(GOLANGCI_LINT) run ./...

test:
	go test ./... -race -cover

generate:
	go run ./cmd/gen-context-index -root . -output docs/code-index.json
	$(GO_CALLVIS) -focus github.com/jagottsicher/breakthrough/cmd/breakthrough \
		-file docs/images/callgraph \
		-rankdir TB \
		./...

check: fmt vet lint test generate
	@if ! git diff --exit-code -- docs/code-index.json docs/images/callgraph.svg; then \
		echo "docs/code-index.json or docs/images/callgraph.svg is out of date - commit the regenerated file(s)"; \
		exit 1; \
	fi
	git diff --check

pre-task:
	@echo "branch: $$(git branch --show-current)"
	@git status --short
	$(MAKE) test

post-task: check
