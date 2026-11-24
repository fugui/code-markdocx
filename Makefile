BINARY          := code-markdocx-server
FRONTEND_DIR    := frontend
DIST_DIR        := $(FRONTEND_DIR)/dist
NODE_MODULES    := $(FRONTEND_DIR)/node_modules

VERSION         ?= $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
COMMIT          ?= $(shell git rev-parse --short HEAD 2>/dev/null || echo "unknown")
BUILDTIME       ?= $(shell date -u '+%Y-%m-%d %H:%M:%S')
LDFLAGS         := -X 'main.Version=$(VERSION)' -X 'main.CommitID=$(COMMIT)' -X 'main.BuildTime=$(BUILDTIME)' -X 'code-markdocx/config.AppVersion=$(VERSION)'

COMMON_DIR      := ../code-common/frontend/src
COMMON_SRCS     := $(shell find $(COMMON_DIR) -type f 2>/dev/null)

FRONTEND_SRCS   := $(shell find $(FRONTEND_DIR) -type f -not -path "*/node_modules/*" -not -path "*/dist/*" 2>/dev/null) $(COMMON_SRCS)
BACKEND_SRCS    := $(shell find . -type f \( -name "*.go" -o -name "go.mod" -o -name "go.sum" \) -not -path "*/$(FRONTEND_DIR)/*" -not -path "*/.git/*")

.PHONY: all build install frontend backend clean run test lint lint-arch

all: build

lint-arch:
	@echo "Checking architecture boundaries (ArchGuard)..."
	@clean_dist=0; \
	if [ ! -d "$(DIST_DIR)" ]; then \
		mkdir -p "$(DIST_DIR)" && touch "$(DIST_DIR)/.embed_placeholder"; \
		clean_dist=1; \
	fi; \
	cleanup() { if [ "$$clean_dist" = "1" ]; then rm -rf "$(DIST_DIR)"; fi; }; \
	trap cleanup EXIT; \
	if [ -d "pkg" ] && [ -n "$$(find pkg -name '*.go' -print -quit)" ]; then \
		bad_pkg_imports=$$(go list -f '{{.ImportPath}} -> {{.Imports}}' ./pkg/... | grep -E 'code-markdocx/server|gorm\.io/gorm|github\.com/gin-gonic/gin|code-common/backend' || true); \
		if [ -n "$$bad_pkg_imports" ]; then \
			echo "❌ [ArchGuard Failed] pkg/* must not import server/gin/gorm/code-common:"; \
			echo "$$bad_pkg_imports"; exit 1; \
		fi; \
	fi; \
	echo "✅ [ArchGuard Passed] pkg/* pure engine boundaries are clean."

build: $(BINARY)

install: $(NODE_MODULES)

$(NODE_MODULES): $(FRONTEND_DIR)/package.json
	cd $(FRONTEND_DIR) && ( [ -d node_modules ] || npm install --no-package-lock )
	@touch $(NODE_MODULES)

frontend: $(DIST_DIR)

$(DIST_DIR): $(NODE_MODULES) $(FRONTEND_SRCS)
	cd $(FRONTEND_DIR) && npm run build
	@touch $(DIST_DIR)

backend: $(BINARY)

$(BINARY): $(BACKEND_SRCS) $(DIST_DIR)
	CGO_ENABLED=0 go build -ldflags "$(LDFLAGS)" -o $(BINARY) .

clean:
	rm -rf $(DIST_DIR) $(BINARY)

test:
	@clean_dist=0; \
	if [ ! -d "$(DIST_DIR)" ]; then \
		mkdir -p "$(DIST_DIR)" && touch "$(DIST_DIR)/.embed_placeholder"; \
		clean_dist=1; \
	fi; \
	cleanup() { if [ "$$clean_dist" = "1" ]; then rm -rf "$(DIST_DIR)"; fi; }; \
	trap cleanup EXIT; \
	pkgs=$$(go list ./... | grep -v '/$(FRONTEND_DIR)/'); \
	go fmt $$pkgs && go vet $$pkgs && go test -race -v $$pkgs
	@if [ -d "$(NODE_MODULES)" ]; then cd $(FRONTEND_DIR) && npm test; fi

lint: lint-arch $(NODE_MODULES)
	cd $(FRONTEND_DIR) && npm run lint

run: build
	./$(BINARY)
