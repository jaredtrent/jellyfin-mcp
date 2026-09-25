VERSION := $(shell cat npm/VERSION | tr -d '[:space:]')
BINARY := jellyfin-mcp
NPM_DIR := npm
BIN_TARGET := $(NPM_DIR)/jellyfin-mcp/bin/$(BINARY)
GO_SRC := $(shell find internal -name '*.go') main.go go.mod go.sum

GO_BUILD_FLAGS := -trimpath -ldflags="-s -w -X github.com/jaredtrent/jellyfin-mcp/internal/server.version=$(VERSION)"

.PHONY: build build-local dist test vet lint check list-binaries version-sync npm-publish \
        clean version-bump

# Release archives as os/arch/name. The names carry no version, so the link
# https://github.com/jaredtrent/jellyfin-mcp/releases/latest/download/<archive>
# that the README's install commands use always reaches the newest release. The
# archives carry no macOS file attributes, which tar on Linux warns about.
DIST_TARGETS := linux/amd64/linux_x64 linux/arm64/linux_arm64 darwin/arm64/macOS_apple-silicon \
        darwin/amd64/macOS_intel windows/amd64/windows_x64

## build: Compile Go binary for linux/amd64 (npm package / MetaMCP target)
build: $(BIN_TARGET)

$(BIN_TARGET): $(GO_SRC)
	mkdir -p $(dir $(BIN_TARGET))
	CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build $(GO_BUILD_FLAGS) -o $(BIN_TARGET) .

## build-local: Compile Go binary for native platform (dev testing)
build-local:
	mkdir -p build
	go build $(GO_BUILD_FLAGS) -o build/$(BINARY) .

## dist: Build the release archives for every platform into dist/, with checksums.txt
dist:
	rm -rf dist && mkdir -p dist
	@for t in $(DIST_TARGETS); do \
		os=$${t%%/*}; rest=$${t#*/}; arch=$${rest%%/*}; name=$${rest#*/}; \
		echo "Building $(BINARY)_$$name"; \
		if [ "$$os" = windows ]; then \
			CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build $(GO_BUILD_FLAGS) -o dist/$(BINARY).exe . && \
			(cd dist && zip -q -X $(BINARY)_$$name.zip $(BINARY).exe && rm $(BINARY).exe) || exit 1; \
		else \
			CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build $(GO_BUILD_FLAGS) -o dist/$(BINARY) . && \
			COPYFILE_DISABLE=1 tar --no-xattrs -czf dist/$(BINARY)_$$name.tar.gz -C dist $(BINARY) && rm dist/$(BINARY) || exit 1; \
		fi; \
	done
	cd dist && shasum -a 256 $(BINARY)_* > checksums.txt
	@echo "Built version $(VERSION):" && cat dist/checksums.txt

## test: Run all tests
test:
	go test ./...

## vet: Static analysis
vet:
	go vet ./...

## lint: Run golangci-lint
lint:
	golangci-lint run ./...

## check: Run vet, lint, and tests together
check: vet lint test

## list-binaries: Show built binaries
list-binaries:
	@echo "NPM binary:"
	@ls -lh $(BIN_TARGET) 2>/dev/null || echo "  (not built; run 'make build')"
	@echo "Local binary:"
	@ls -lh build/$(BINARY) 2>/dev/null || echo "  (not built; run 'make build-local')"

## version-sync: Update package.json to match npm/VERSION
version-sync:
	@echo "Syncing version $(VERSION)..."
	@jq --arg v "$(VERSION)" '.version = $$v' $(NPM_DIR)/jellyfin-mcp/package.json > $(NPM_DIR)/jellyfin-mcp/package.json.tmp && \
		mv $(NPM_DIR)/jellyfin-mcp/package.json.tmp $(NPM_DIR)/jellyfin-mcp/package.json
	@echo "Done. Package at version $(VERSION)."

## npm-publish: Publish the package to npmjs.com
npm-publish:
	@if [ ! -f $(NPM_DIR)/.npmrc ]; then \
		echo "Error: npm/.npmrc not found. Copy npm/.npmrc.template to npm/.npmrc and add your token."; \
		exit 1; \
	fi
	@echo "Publishing @jaredtrent/jellyfin-mcp v$(VERSION)..."
	cd $(NPM_DIR)/jellyfin-mcp && npm publish --userconfig=../.npmrc
	@echo "Published."

## clean: Remove built binaries
clean:
	rm -f $(BIN_TARGET) build/$(BINARY)

## version-bump: Bump to today's date (CalVer YYYY.MMDD.#), incrementing build # if same day
version-bump:
	@current=$$(cat $(NPM_DIR)/VERSION | tr -d '[:space:]'); \
	today_year=$$(date +%Y); \
	today_mmdd=$$(date +%-m%d); \
	cur_year=$$(echo $$current | cut -d. -f1); \
	cur_mmdd=$$(echo $$current | cut -d. -f2); \
	cur_build=$$(echo $$current | cut -d. -f3); \
	if [ "$$today_year" = "$$cur_year" ] && [ "$$today_mmdd" = "$$cur_mmdd" ]; then \
		new="$$today_year.$$today_mmdd.$$((cur_build + 1))"; \
	else \
		new="$$today_year.$$today_mmdd.1"; \
	fi; \
	echo "$$new" > $(NPM_DIR)/VERSION; \
	echo "Version bumped: $$current -> $$new"
	@$(MAKE) version-sync
