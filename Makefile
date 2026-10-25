BINARY := companion
VERSION ?= dev
LDFLAGS := -s -w -X github.com/repocompanion/companion/internal/version.Version=$(VERSION)

# `go install` destination. Defaults to $GOBIN, then $GOPATH/bin.
GOBIN ?= $(shell go env GOBIN)
ifeq ($(strip $(GOBIN)),)
GOBIN := $(shell go env GOPATH)/bin
endif

.PHONY: build test vet fmt install clean

build:
	go build -trimpath -ldflags "$(LDFLAGS)" -o $(BINARY) ./cmd/companion

install:
	GOBIN="$(GOBIN)" go install -trimpath -ldflags "$(LDFLAGS)" ./cmd/companion
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
