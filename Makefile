GO      ?= go
VERSION ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo dev)
LDFLAGS := -s -w -X main.version=$(VERSION)

.PHONY: build install test vet check run clean

build: ## build ./bin/cairn
	$(GO) build -ldflags "$(LDFLAGS)" -o bin/cairn .

install: ## go install into $GOPATH/bin
	$(GO) install -ldflags "$(LDFLAGS)" .

test:
	$(GO) test ./...

vet:
	$(GO) vet ./...

check: vet test

run: build
	./bin/cairn

clean:
	rm -rf bin
