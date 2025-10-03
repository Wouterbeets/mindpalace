// Package world provides access to the embedded world.x86_64 binary.
package world

import (
	_ "embed"
	"os"
	"os/exec"
)

//go:embed world
var binary []byte

// Binary returns the raw bytes of the embedded world.x86_64 executable.
func Binary() []byte {
	return binary
}

// ExtractToTemp writes the binary to a temporary file and returns its path.
// The caller is responsible for cleaning up the file.
func ExtractToTemp() (string, error) {
	tmpfile, err := os.CreateTemp("", "world_*.x86_64")
	if err != nil {
		return "", err
	}
	defer tmpfile.Close()

	if _, err := tmpfile.Write(binary); err != nil {
		return "", err
	}

	if err := os.Chmod(tmpfile.Name(), 0755); err != nil {
		return "", err
	}

	return tmpfile.Name(), nil
}

// Run executes the binary with the given arguments, extracting it to a temp file first.
// It waits for the process to complete and returns the output and any error.
func Run(args ...string) (string, error) {
	tmpPath, err := ExtractToTemp()
	if err != nil {
		return "", err
	}
	defer os.Remove(tmpPath) // Clean up after execution

	cmd := exec.Command(tmpPath, args...)
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), err
	}
	return string(output), nil
}

// RunInDir executes the binary in the specified directory with the given arguments.
func RunInDir(dir string, args ...string) (string, error) {
	tmpPath, err := ExtractToTemp()
	if err != nil {
		return "", err
	}
	defer os.Remove(tmpPath)

	cmd := exec.Command(tmpPath, args...)
	cmd.Dir = dir
	output, err := cmd.CombinedOutput()
	if err != nil {
		return string(output), err
	}
	return string(output), nil
}
