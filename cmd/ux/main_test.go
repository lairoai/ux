package main

import (
	"testing"
)

func TestGetVersion(t *testing.T) {
	origVersion := version
	defer func() { version = origVersion }()

	// Explicit version set (e.g. via -ldflags)
	version = "v1.2.3"
	if got := getVersion(); got != "v1.2.3" {
		t.Errorf("getVersion() = %q, want %q", got, "v1.2.3")
	}

	// Default "dev" without module build info falls back to "dev"
	version = "dev"
	if got := getVersion(); got != "dev" {
		t.Errorf("getVersion() = %q, want %q", got, "dev")
	}

	// Empty version falls back to "dev"
	version = ""
	if got := getVersion(); got != "dev" {
		t.Errorf("getVersion() = %q, want %q", got, "dev")
	}
}
