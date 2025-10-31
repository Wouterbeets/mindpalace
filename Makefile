# Makefile for MindPalace project

# Variables
GO = go
GOFLAGS = -v
CGO_ENABLED = 1
BINARY_NAME = mindpalace
PLUGIN_DIR = plugins
BUILD_DIR = build
MAIN_SRC = cmd/mindpalace/main.go
PLUGIN_NAMES = calendar plugingenerator taskmanager
PLUGIN_OUTPUTS = $(foreach name, $(PLUGIN_NAMES), $(PLUGIN_DIR)/$(name)/$(name).so)
MODELS_DIR = models
WHISPER_MODEL = $(MODELS_DIR)/ggml-base.en.bin

# Allow passing arguments to run
RUN_ARGS ?=

# Default target
.PHONY: all
all: whisper build plugins

# Build the main binary
.PHONY: build
build: setup world
	@echo "Building MindPalace binary..."
	@mkdir -p $(BUILD_DIR)
	PKG_CONFIG_PATH=$(shell pwd)/whisper-cpp/build/lib/pkgconfig:$(PKG_CONFIG_PATH) CGO_LDFLAGS="-Wl,-rpath,$(shell pwd)/whisper-cpp/build/lib" $(GO) build $(GOFLAGS) -o $(BUILD_DIR)/$(BINARY_NAME) $(MAIN_SRC)



# Build all plugins
.PHONY: plugins
plugins: setup $(PLUGIN_OUTPUTS)

# Build plugins
$(PLUGIN_DIR)/calendar/calendar.so: $(PLUGIN_DIR)/calendar/plugin.go
	@echo "Building plugin: $@"
	cd $(dir $<) && $(GO) build $(GOFLAGS) -buildmode=plugin -o calendar.so ./*.go

$(PLUGIN_DIR)/plugingenerator/plugingenerator.so: $(PLUGIN_DIR)/plugingenerator/plugin.go
	@echo "Building plugin: $@"
	cd $(dir $<) && $(GO) build $(GOFLAGS) -buildmode=plugin -o plugingenerator.so ./*.go

$(PLUGIN_DIR)/taskmanager/taskmanager.so: $(PLUGIN_DIR)/taskmanager/plugin.go
	@echo "Building plugin: $@"
	cd $(dir $<) && $(GO) build $(GOFLAGS) -buildmode=plugin -o taskmanager.so ./*.go

# Run the application with optional arguments
.PHONY: run
run: build plugins
	@echo "Running MindPalace with args: $(RUN_ARGS)"
	LD_LIBRARY_PATH=/home/mindpalace/mindpalace/whisper-cpp/build/lib:$LD_LIBRARY_PATH ./$(BUILD_DIR)/$(BINARY_NAME) $(RUN_ARGS)

# Run with verbose logging
.PHONY: run-verbose
run-verbose: build plugins
	@echo "Running MindPalace in verbose mode..."
	LD_LIBRARY_PATH=/home/mindpalace/mindpalace/whisper-cpp/build/lib:$LD_LIBRARY_PATH ./$(BUILD_DIR)/$(BINARY_NAME) -trace

# Run with debug logging
.PHONY: run-debug
run-debug: build plugins
	@echo "Running MindPalace in debug mode..."
	LD_LIBRARY_PATH=/home/mindpalace/mindpalace/whisper-cpp/build/lib:$LD_LIBRARY_PATH ./$(BUILD_DIR)/$(BINARY_NAME) -debug

# Run in headless mode
.PHONY: run-headless
run-headless: build plugins
	@echo "Running MindPalace in headless mode..."
	LD_LIBRARY_PATH=/home/mindpalace/mindpalace/whisper-cpp/build/lib:$LD_LIBRARY_PATH ./$(BUILD_DIR)/$(BINARY_NAME) -headless

# Run for testing with log capture and auto-kill after 10s
.PHONY: run-test
run-test: build plugins
	@echo "Running MindPalace for testing (10s timeout)..."
	@echo "Starting application..."
	@timeout 10s bash -c 'LD_LIBRARY_PATH=/home/mindpalace/mindpalace/whisper-cpp/build/lib:$LD_LIBRARY_PATH ./$(BUILD_DIR)/$(BINARY_NAME) -debug 2>&1 | tee test_run.log' || true
	@echo "Application stopped after 10 seconds. Logs saved to test_run.log"

# Clean build artifacts
.PHONY: clean
clean:
	@echo "Cleaning up..."
	rm -rf $(BUILD_DIR)
	find $(PLUGIN_DIR) -name "*.so" -type f -delete

# Clean everything including dependencies
.PHONY: clean-all
clean-all: clean
	@echo "Cleaning all dependencies..."
	rm -rf whisper-cpp

# Check dependencies and install
.PHONY: setup
setup:
	@echo "Checking dependencies..."
	@if ! command -v go > /dev/null 2>&1; then echo "Error: Go is not installed. Please install Go 1.23.5 or later."; exit 1; fi
	@if ! command -v godot > /dev/null 2>&1; then echo "Warning: Godot not found. You may need to install it for the UI components."; fi
	@if ! command -v cmake > /dev/null 2>&1; then echo "Error: CMake is not installed. Please install CMake."; exit 1; fi
	@echo "Installing Go dependencies..."
	$(GO) mod tidy
	$(GO) mod download
	$(MAKE) whisper
	$(MAKE) download-model

# Format code
.PHONY: fmt
fmt:
	@echo "Formatting code..."
	$(GO) fmt ./...

# Run tests
.PHONY: test
test:
	@echo "Running tests..."
	PKG_CONFIG_PATH=$(shell pwd)/whisper-cpp/build/lib/pkgconfig:$(PKG_CONFIG_PATH) CGO_LDFLAGS="-Wl,-rpath,$(shell pwd)/whisper-cpp/build/lib" LD_LIBRARY_PATH=$(shell pwd)/whisper-cpp/build/lib:$(LD_LIBRARY_PATH) $(GO) test ./... -v

# Generate documentation
.PHONY: doc
doc:
	@echo "Generating documentation..."
	$(GO) doc -all ./... > doc.txt

# Check code quality
.PHONY: lint
lint:
	@echo "Running linter..."
	CGO_ENABLED=1 PKG_CONFIG_PATH=$(shell pwd)/whisper-cpp/build/lib/pkgconfig:$(PKG_CONFIG_PATH) CGO_LDFLAGS="-Wl,-rpath,$(shell pwd)/whisper-cpp/build/lib" golangci-lint run ./...

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

# Download Whisper model
.PHONY: download-model
download-model: $(WHISPER_MODEL)

$(WHISPER_MODEL):
	@echo "Downloading Whisper model..."
	@mkdir -p $(MODELS_DIR)
	curl -L https://huggingface.co/ggerganov/whisper.cpp/resolve/main/ggml-base.en.bin -o $(WHISPER_MODEL)

# Clone Whisper repository
whisper-cpp:
	@echo "Cloning Whisper repository..."
	git clone https://github.com/ggerganov/whisper.cpp.git whisper-cpp

# Build the Whisper library
.PHONY: whisper
whisper: whisper-cpp
	@echo "Building Whisper library..."
	cd whisper-cpp && cmake -B build -DCMAKE_INSTALL_PREFIX=$(shell pwd)/whisper-cpp/build
	cd whisper-cpp && cmake --build build --config Release
	@echo "Installing Whisper library (ignoring bin install errors)..."
	-cd whisper-cpp && cmake --install build || echo "Install completed with warnings (bins may not be installed)"
	@echo "Setting up pkgconfig files..."
	cd whisper-cpp/build/lib/pkgconfig && cp whisper.pc libwhisper.pc && cp whisper.pc libwhisper-linux.pc

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
	@echo ""
	@echo "  plugins     : Build all plugins"
	@echo "  run         : Build and run (use RUN_ARGS='flags' for arguments)"
	@echo "  run-verbose : Run with verbose logging"
	@echo "  run-debug   : Run with debug logging"
	@echo "  run-headless: Run in headless mode"
	@echo "  clean       : Remove build artifacts"
	@echo "  clean-all   : Remove all build artifacts and dependencies"
	@echo "  setup       : Check dependencies and install"
	@echo "  fmt         : Format code"
	@echo "  test        : Run tests"
	@echo "  doc         : Generate documentation"
	@echo "  lint        : Run linter"
	@echo "  release     : Create a release package"
	@echo "  dev         : Start development mode with air"
	@echo "  dev-verbose : Start development mode in verbose"
	@echo "  world       : Build the Godot world binary and move to pkg/world"
	@echo "  whisper     : Clone and build the Whisper library"
	@echo "  help        : Show this help message"
	@echo ""
	@echo "Example: make run RUN_ARGS='-v --events custom_events.json'"
