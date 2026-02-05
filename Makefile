# brew-updater - Makefile

##@ Project Configuration
PROJECT_NAME := brew-updater
VERSION := $(shell git describe --tags --always --dirty 2>/dev/null || echo "dev")
BUILD_TIME := $(shell date -u +"%Y-%m-%dT%H:%M:%SZ")
GO_VERSION := $(shell go version | awk '{print $$3}')

BINARY_NAME := $(PROJECT_NAME)
BUILD_DIR := ./build

HOST_OS := $(shell go env GOOS)
HOST_ARCH := $(shell go env GOARCH)

LDFLAGS := -ldflags "-X main.version=$(VERSION) -X main.buildTime=$(BUILD_TIME) -X main.goVersion=$(GO_VERSION)"

PLATFORMS := \
	linux/amd64 \
	linux/arm64 \
	darwin/amd64 \
	darwin/arm64 \
	windows/amd64

RED := \033[0;31m
GREEN := \033[0;32m
YELLOW := \033[0;33m
BLUE := \033[0;34m
CYAN := \033[0;36m
NC := \033[0m

##@ Dependencies Management

define ensure-go-tool
	@if ! command -v $(1) >/dev/null 2>&1; then \
		echo "$(YELLOW)Installing $(1)...$(NC)"; \
		$(2); \
	fi
endef

define ensure-external-tool
	@if ! command -v $(1) >/dev/null 2>&1; then \
		echo "$(YELLOW)Installing $(1)...$(NC)"; \
		$(2); \
	fi
endef

GOIMPORTS_INSTALL := go install golang.org/x/tools/cmd/goimports@latest
GOLANGCI_LINT_INSTALL := curl -sSfL https://raw.githubusercontent.com/golangci/golangci-lint/master/install.sh | sh -s -- -b $$(go env GOPATH)/bin latest
GOSEC_INSTALL := go install github.com/securego/gosec/v2/cmd/gosec@latest
GORELEASER_INSTALL := go install github.com/goreleaser/goreleaser/v2@latest

.PHONY: install-deps
install-deps: ## Install all development dependencies
	@echo "$(BLUE)Installing development dependencies...$(NC)"
	$(call ensure-go-tool,goimports,$(GOIMPORTS_INSTALL))
	$(call ensure-external-tool,golangci-lint,$(GOLANGCI_LINT_INSTALL))
	$(call ensure-go-tool,gosec,$(GOSEC_INSTALL))
	$(call ensure-go-tool,goreleaser,$(GORELEASER_INSTALL))
	@echo "$(GREEN)All dependencies installed$(NC)"

.PHONY: check-deps
check-deps: ## Check if all required dependencies are installed
	@echo "$(BLUE)Checking development dependencies...$(NC)"
	@missing_deps=""; \
	for tool in goimports golangci-lint gosec goreleaser; do \
		if ! command -v $$tool >/dev/null 2>&1; then \
			missing_deps="$$missing_deps $$tool"; \
		else \
			echo "$(GREEN)✓ $$tool is installed$(NC)"; \
		fi; \
	done; \
	if [ -n "$$missing_deps" ]; then \
		echo "$(RED)✗ Missing dependencies:$$missing_deps$(NC)"; \
		echo "$(YELLOW)Run 'make install-deps' to install missing dependencies$(NC)"; \
		exit 1; \
	else \
		echo "$(GREEN)All dependencies are installed$(NC)"; \
	fi

##@ General
.PHONY: help
help: ## Display available commands
	@awk 'BEGIN {FS = ":.*##"; printf "\nUsage:\n  make \033[36m<target>\033[0m\n"} /^[a-zA-Z_0-9-]+:.*?##/ { printf "  \033[36m%-20s\033[0m %s\n", $$1, $$2 } /^##@/ { printf "\n\033[1m%s\033[0m\n", substr($$0, 5) } ' $(MAKEFILE_LIST)

##@ Development
.PHONY: fmt
fmt: ## Format Go code with goimports
	@echo "$(BLUE)Formatting Go code...$(NC)"
	$(call ensure-go-tool,goimports,$(GOIMPORTS_INSTALL))
	@goimports -w -local $(PROJECT_NAME) .
	@echo "$(GREEN)Code formatting completed$(NC)"

.PHONY: lint
lint: ## Run golangci-lint code analysis
	@echo "$(BLUE)Running code analysis...$(NC)"
	$(call ensure-external-tool,golangci-lint,$(GOLANGCI_LINT_INSTALL))
	@golangci-lint run
	@echo "$(GREEN)Code analysis completed$(NC)"

.PHONY: lint-fix
lint-fix: ## Run golangci-lint with auto-fix
	@echo "$(BLUE)Running code analysis with auto-fix...$(NC)"
	$(call ensure-external-tool,golangci-lint,$(GOLANGCI_LINT_INSTALL))
	@golangci-lint run --fix
	@echo "$(GREEN)Code analysis and fixes completed$(NC)"

.PHONY: sec
sec: ## Run security analysis with gosec
	@echo "$(BLUE)Running security analysis...$(NC)"
	$(call ensure-go-tool,gosec,$(GOSEC_INSTALL))
	@mkdir -p $(BUILD_DIR)
	@gosec -fmt sarif -out $(BUILD_DIR)/gosec-report.sarif -no-fail ./...
	@gosec -fmt text -no-fail ./...
	@echo "$(GREEN)Security analysis completed$(NC)"
	@echo "$(CYAN)Report saved to: $(BUILD_DIR)/gosec-report.sarif$(NC)"

.PHONY: goreleaser-check
goreleaser-check: ## Check GoReleaser configuration
	@echo "$(BLUE)Checking GoReleaser configuration...$(NC)"
	$(call ensure-go-tool,goreleaser,$(GORELEASER_INSTALL))
	@goreleaser check
	@echo "$(GREEN)GoReleaser configuration check completed$(NC)"

##@ Build
.PHONY: build
build: ## Build binary for current platform
	@echo "$(BLUE)Building Go binary for $(HOST_OS)/$(HOST_ARCH)...$(NC)"
	@mkdir -p $(BUILD_DIR)
	@CGO_ENABLED=0 GOOS=$(HOST_OS) GOARCH=$(HOST_ARCH) go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)$(if $(filter windows,$(HOST_OS)),.exe,) ./cmd/brew-updater
	@echo "$(GREEN)Build completed: $(BUILD_DIR)/$(BINARY_NAME)$(if $(filter windows,$(HOST_OS)),.exe,)$(NC)"

.PHONY: build-all
build-all: ## Build binaries for all supported platforms
	@echo "$(BLUE)Building Go binaries for all platforms...$(NC)"
	@mkdir -p $(BUILD_DIR)
	@for platform in $(PLATFORMS); do \
		os=$$(echo $$platform | cut -d'/' -f1); \
		arch=$$(echo $$platform | cut -d'/' -f2); \
		ext=""; \
		if [ "$$os" = "windows" ]; then ext=".exe"; fi; \
		echo "$(CYAN)Building for $$os/$$arch...$(NC)"; \
		CGO_ENABLED=0 GOOS=$$os GOARCH=$$arch go build $(LDFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME)-$$os-$$arch$$ext ./cmd/brew-updater; \
		if [ $$? -eq 0 ]; then \
			echo "$(GREEN)✓ Built $(BUILD_DIR)/$(BINARY_NAME)-$$os-$$arch$$ext$(NC)"; \
		else \
			echo "$(RED)✗ Failed to build for $$os/$$arch$(NC)"; \
		fi; \
	done
	@echo "$(GREEN)All builds completed!$(NC)"

##@ Test
.PHONY: test
test: ## Run unit tests
	@go test ./...
