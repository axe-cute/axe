package main

import (
	"os/exec"
	"testing"
)

// TestMainBinary_Builds verifies that the cmd/api binary compiles.
// This is the minimum safety net for the application entry point.
func TestMainBinary_Builds(t *testing.T) {
	cmd := exec.Command("go", "build", "-o", "/dev/null", ".")
	cmd.Dir = "."
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("cmd/api failed to build: %v\n%s", err, string(out))
	}
}
