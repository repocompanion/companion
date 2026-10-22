BINARY := docsync
VERSION ?= dev
LDFLAGS := -s -w -X github.com/bolacha/docsync/internal/cli.Version=$(VERSION)

# `go install` destination. Defaults to $GOBIN, then $GOPATH/bin.
GOBIN ?= $(shell go env GOBIN)
ifeq ($(strip $(GOBIN)),)
GOBIN := $(shell go env GOPATH)/bin
endif

.PHONY: build test vet fmt install clean

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/docsync

install:
	GOBIN="$(GOBIN)" go install -trimpath -ldflags "$(LDFLAGS)" ./cmd/docsync
	@echo "installed $(BINARY) to $(GOBIN)/$(BINARY)"

test:
	go test ./...

vet:
	go vet ./...

fmt:
	gofmt -w cmd internal

clean:
	rm -f $(BINARY)
	rm -rf dist
