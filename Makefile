MODULE   := github.com/klaassen-consulting/jc
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "0.1.0-dev")
LDFLAGS  := -s -w -X '$(MODULE)/internal/version.Number=$(VERSION)'
BIN      := jc

.PHONY: build test lint install clean vet

build:
	go build -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/jc

test:
	go test ./... -count=1

lint: vet
	@echo "lint: go vet passed"

vet:
	go vet ./...

install:
	go install -ldflags "$(LDFLAGS)" ./cmd/jc

clean:
	rm -f $(BIN)
	go clean -cache
