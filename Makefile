# Makefile for MindPalace project

# Variables
GO = go
GOFLAGS = -v
BINARY_NAME = mindpalace
PLUGIN_DIR = plugins
BUILD_DIR = build
MAIN_SRC = cmd/mindpalace/main.go
PLUGINS = $(wildcard $(PLUGIN_DIR)/*/plugin.go)
PLUGIN_OUTPUTS = $(patsubst $(PLUGIN_DIR)/%/plugin.go,$(PLUGIN_DIR)/%/%.so,$(PLUGINS))

# Allow passing arguments to run
RUN_ARGS ?=

# Default target
.PHONY: all
all: build plugins

# Build the main binary
.PHONY: build
build: world
	@echo "Building MindPalace binary..."
	@mkdir -p $(BUILD_DIR)
	$(GO) build $(GOFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) $(MAIN_SRC)

# Generate templ files (once at root)
.PHONY: templ
templ:
	@echo "Generating templ files..."
	templ generate

# Build all plugins
.PHONY: plugins
plugins: templ $(PLUGIN_OUTPUTS)

# Generic rule for building plugins
$(PLUGIN_DIR)/%/%.so: $(PLUGIN_DIR)/%/plugin.go
	@echo "Building plugin: $@"
	cd $(dir $<) && $(GO) build $(GOFLAGS) -buildmode=plugin -o $(notdir $@) ./*.go

# Run the application with optional arguments
.PHONY: run
run: build plugins
	@echo "Running MindPalace with args: $(RUN_ARGS)"
	./$(BUILD_DIR)/$(BINARY_NAME) $(RUN_ARGS)

# Run with verbose logging
.PHONY: run-verbose
run-verbose: build plugins
	@echo "Running MindPalace in verbose mode..."
	./$(BUILD_DIR)/$(BINARY_NAME) -trace

# Run with debug logging
.PHONY: run-debug
run-debug: build plugins
	@echo "Running MindPalace in debug mode..."
	./$(BUILD_DIR)/$(BINARY_NAME) -debug

# Run in headless mode
.PHONY: run-headless
run-headless: build plugins
	@echo "Running MindPalace in headless mode..."
	./$(BUILD_DIR)/$(BINARY_NAME) -headless

# Clean build artifacts
.PHONY: clean
clean:
	@echo "Cleaning up..."
	rm -rf $(BUILD_DIR)
	find $(PLUGIN_DIR) -name "*.so" -type f -delete

# Install dependencies
.PHONY: deps
deps:
	@echo "Installing dependencies..."
	$(GO) mod tidy
	$(GO) mod download

# Format code
.PHONY: fmt
fmt:
	@echo "Formatting code..."
	$(GO) fmt ./...

# Run tests
.PHONY: test
test:
	@echo "Running tests..."
	$(GO) test ./... -v

# Generate documentation
.PHONY: doc
doc:
	@echo "Generating documentation..."
	$(GO) doc -all ./... > doc.txt

# Check code quality
.PHONY: lint
lint:
	@echo "Running linter..."
	golangci-lint run ./...

# Build everything and create a release package
.PHONY: release
release: clean build plugins
	@echo "Creating release package..."
	@mkdir -p release
	tar -czf release/mindpalace.tar.gz $(BUILD_DIR)/$(BINARY_NAME) $(PLUGIN_OUTPUTS) events.json

# Development targets
.PHONY: dev
dev:
	@echo "Starting development with air..."
	air

.PHONY: dev-verbose
dev-verbose:
	@echo "Starting development with air in verbose mode..."
	air -c .air.toml -- -v

# Build the Godot world binary
.PHONY: world
world:
	@echo "Building Godot world binary..."
	cd world && godot --headless --export-release Linux ./world.x86_64
	@echo "Moving Godot world binary to pkg/world..."
	@mkdir -p pkg/world
	@cp world/world.x86_64 pkg/world/world

# Help target
.PHONY: help
help:
	@echo "Available targets:"
	@echo "  all         : Build everything (default)"
	@echo "  build       : Build the main binary"
	@echo "  templ       : Generate templ files"
	@echo "  plugins     : Build all plugins"
	@echo "  run         : Build and run (use RUN_ARGS='flags' for arguments)"
	@echo "  run-verbose : Run with verbose logging"
	@echo "  run-debug   : Run with debug logging"
	@echo "  clean       : Remove build artifacts"
	@echo "  deps        : Install dependencies"
	@echo "  fmt         : Format code"
	@echo "  test        : Run tests"
	@echo "  doc         : Generate documentation"
	@echo "  lint        : Run linter"
	@echo "  release     : Create a release package"
	@echo "  dev         : Start development mode with air"
	@echo "  dev-verbose : Start development mode in verbose"
	@echo "  world       : Build the Godot world binary and move to pkg/world"
	@echo "  help        : Show this help message"
	@echo ""
	@echo "Example: make run RUN_ARGS='-v --events custom_events.json'"
