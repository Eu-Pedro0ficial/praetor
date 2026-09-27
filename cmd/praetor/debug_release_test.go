//go:build !praetor_debug

package main

import (
	"strings"
	"testing"
)

func TestReleaseBuildDoesNotExposeDebugMode(t *testing.T) {
	if _, err := parseStartupOptions([]string{"--debug"}); err == nil || !strings.Contains(err.Error(), "unknown startup option") {
		t.Fatalf("release --debug error = %v", err)
	}
	if strings.Contains(startupUsage(), "--debug") {
		t.Fatalf("release help exposes debug mode: %q", startupUsage())
	}
}
