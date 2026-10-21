MODULE   := github.com/klaassen-consulting/jc
VERSION  ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "0.1.0-dev")
LDFLAGS  := -s -w -X '$(MODULE)/internal/version.Number=$(VERSION)'
BIN      := jc
DIST     := dist
PLATFORMS := darwin/amd64 darwin/arm64 linux/amd64 linux/arm64 windows/amd64

.PHONY: build test lint install clean vet integration-test integration-test-readonly clean-dist dist release site verify-site refresh-generated verify-generated

build:
	go build -ldflags "$(LDFLAGS)" -o $(BIN) ./cmd/jc

test:
	go test ./... -count=1

lint: vet
	@echo "lint: go vet passed"

# Fail if anything is unformatted. `gofmt -l` prints offenders and exits 0
# either way, so the test for emptiness is what makes this a gate — an
# earlier attempt at this check used `gofmt -l || echo` and passed always.
fmt-check:
	@out="$$(gofmt -l . | grep -v '^dist/' || true)"; \
		if [ -n "$$out" ]; then \
			echo "these files are not gofmt'd — run 'make fmt':"; \
			echo "$$out"; \
			exit 1; \
		fi

fmt:
	gofmt -w $$(git ls-files '*.go')

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

# The two OpenAPI documents every spec-derived artifact in this repo comes
# from. They are published and stable; the download button in the docs UI
# hands you a `blob:` URL instead, which is a browser-local object valid only
# inside the tab that made it, so it cannot be used from a script.
SPEC_CONSOLE  ?= https://docs.jumpcloud.com/new/console/index.yaml
SPEC_INSIGHTS ?= https://docs.jumpcloud.com/new/api/insights/directory/index.yaml

# Rebuild the spec-derived artifacts in place, from the published specs.
#
# Neither generated file carries a timestamp, deliberately: a date would make
# every regeneration a diff and `verify-generated` below could no longer tell
# "the spec moved" from "someone reran the script".
refresh-generated:
	@tmp=$$(mktemp -d) && \
		curl -fsSL -o $$tmp/console.yaml "$(SPEC_CONSOLE)" && \
		curl -fsSL -o $$tmp/insights.yaml "$(SPEC_INSIGHTS)" && \
		python3 -I scripts/api-coverage.py $$tmp/console.yaml $$tmp/insights.yaml && \
		python3 -I scripts/gen-operation-index.py $$tmp/console.yaml && \
		rm -rf $$tmp

# Fail when a spec-derived artifact has fallen behind the published specs.
#
# This exists because staleness here is silent and has shipped twice. The
# operationId index went 162 ids behind and `jc workflows validate` rejected
# real operations as typos; the coverage scorecard read 100% while five areas
# had no command group. Neither could be caught by a unit test, because each
# artifact is the test's source of truth as well as the code's — it agrees
# with itself by construction. Only the spec can say it is out of date.
#
# NOT wired into the PR gate on purpose: it reaches a third-party site, so a
# docs outage would fail PRs that have nothing to do with the spec. It runs on
# a schedule instead (.github/workflows/spec-drift.yml) and by hand here.
verify-generated:
	@tmp=$$(mktemp -d) && \
		curl -fsSL -o $$tmp/console.yaml "$(SPEC_CONSOLE)" && \
		curl -fsSL -o $$tmp/insights.yaml "$(SPEC_INSIGHTS)" && \
		python3 -I scripts/api-coverage.py $$tmp/console.yaml $$tmp/insights.yaml --out=$$tmp/API_COVERAGE.md >/dev/null && \
		python3 -I scripts/gen-operation-index.py $$tmp/console.yaml --out=$$tmp/operations.json >/dev/null && \
		diff -u docs/API_COVERAGE.md $$tmp/API_COVERAGE.md || { \
			echo "verify-generated: docs/API_COVERAGE.md is stale — run 'make refresh-generated' and commit"; \
			rm -rf $$tmp; exit 1; \
		} && \
		diff -q internal/workflow/operations.json $$tmp/operations.json || { \
			echo "verify-generated: internal/workflow/operations.json is stale — run 'make refresh-generated' and commit"; \
			rm -rf $$tmp; exit 1; \
		} && \
		rm -rf $$tmp && \
		echo "verify-generated: spec-derived artifacts are up to date"
