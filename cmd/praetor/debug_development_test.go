//go:build praetor_debug

package main

import (
	"strings"
	"testing"
)

func TestDevelopmentBuildExposesDebugMode(t *testing.T) {
	options, err := parseStartupOptions([]string{"--debug"})
	if err != nil {
		t.Fatalf("parseStartupOptions() error = %v", err)
	}
	if !options.debug || options.help {
		t.Fatalf("development options = %#v", options)
	}
	if !strings.Contains(startupUsage(), "--debug") || !strings.Contains(startupUsage(), "XDG STATE") {
		t.Fatalf("development help does not expose debug mode: %q", startupUsage())
	}
}
