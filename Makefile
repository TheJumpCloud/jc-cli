MODULE   := github.com/klaassen-consulting/jc
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "0.1.0-dev")
LDFLAGS  := -s -w -X '$(MODULE)/internal/version.Number=$(VERSION)'
BIN      := jc
DIST     := dist
PLATFORMS := darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64

.PHONY: build test lint install clean vet integration-test integration-test-readonly clean-dist dist release site verify-site

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

clean: clean-dist
	rm -f $(BIN)
	go clean -cache

clean-dist:
	rm -rf $(DIST)

dist: clean-dist
	@for platform in $(PLATFORMS); do \
		os=$${platform%/*}; \
		arch=$${platform#*/}; \
		name=jc-$${os}-$${arch}; \
		ext=""; \
		if [ "$${os}" = "windows" ]; then ext=".exe"; fi; \
		echo "Building $${name}..."; \
		mkdir -p $(DIST)/$${name}; \
		GOOS=$${os} GOARCH=$${arch} go build -ldflags "$(LDFLAGS)" -o $(DIST)/$${name}/jc$${ext} ./cmd/jc || exit 1; \
	done

release: dist
	@cd $(DIST) && \
	for platform in $(PLATFORMS); do \
		os=$${platform%/*}; \
		arch=$${platform#*/}; \
		name=jc-$${os}-$${arch}; \
		if [ "$${os}" = "windows" ]; then \
			echo "Archiving $${name}.zip..."; \
			zip -q $${name}.zip $${name}/jc.exe; \
		else \
			echo "Archiving $${name}.tar.gz..."; \
			tar czf $${name}.tar.gz $${name}; \
		fi; \
	done && \
	shasum -a 256 *.tar.gz *.zip > checksums.txt && \
	echo "Release archives:" && \
	ls -lh *.tar.gz *.zip checksums.txt

integration-test: build
	@JC=./$(BIN) ./scripts/integration-test.sh

integration-test-readonly: build
	@JC=./$(BIN) ./scripts/integration-test.sh --skip-mutable

# Regenerate docs/site/ artifacts (commands.json, llms.txt, llms-full.txt)
# from the schema manifest. Run after touching internal/schema or any
# command that should appear in the public catalog.
site:
	go run ./cmd/sitegen -out docs/site

# CI gate: fail if docs/site/ is stale relative to the current schema.
# Regenerates into a temp dir and diffs the three generated artifacts
# against the committed copies. Catches "added a new command but forgot
# to run make site."
verify-site:
	@tmp=$$(mktemp -d) && \
		go run ./cmd/sitegen -out $$tmp >/dev/null && \
		for f in commands.json llms.txt llms-full.txt; do \
			diff -u docs/site/$$f $$tmp/$$f || { \
				echo "verify-site: docs/site/$$f is stale — run 'make site' and commit"; \
				rm -rf $$tmp; \
				exit 1; \
			}; \
		done && \
		rm -rf $$tmp && \
		echo "verify-site: docs/site/ is up to date"
