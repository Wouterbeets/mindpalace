#!/bin/bash

# MindPalace Setup Script
# This script ensures all dependencies are properly set up for building MindPalace

set -e

echo "Setting up MindPalace dependencies..."

# Check if Go is installed
if ! command -v go &> /dev/null; then
    echo "Error: Go is not installed. Please install Go 1.23.5 or later."
    exit 1
fi

# Check if Godot is installed
if ! command -v godot &> /dev/null; then
    echo "Warning: Godot not found. You may need to install it for the UI components."
fi

# Check if cmake is installed
if ! command -v cmake &> /dev/null; then
    echo "Error: CMake is not installed. Please install CMake."
    exit 1
fi

# Install Go dependencies
echo "Installing Go dependencies..."
go mod tidy
go mod download

# Generate templ files
echo "Generating templ files..."
if command -v templ &> /dev/null; then
    templ generate
else
    echo "Warning: templ not found. Install it with: go install github.com/a-h/templ/cmd/templ@latest"
fi

# Build Whisper library
echo "Building Whisper library..."
if ! make whisper; then
    echo "Warning: Whisper build had issues, but continuing..."
fi

# Download model
echo "Downloading Whisper model..."
make download-model

echo "Setup complete! You can now run 'make build' to build MindPalace."