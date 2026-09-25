package main

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRunRejectsArgvCommandsBeforeRuntimeMutation(t *testing.T) {
	repositoryRoot := t.TempDir()
	t.Chdir(repositoryRoot)
	xdgDataHome := filepath.Join(t.TempDir(), "xdg")
	t.Setenv("XDG_DATA_HOME", xdgDataHome)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(t.TempDir(), "cache"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))

	err := run([]string{"status"})
	if err == nil || !strings.Contains(err.Error(), "interactive shell") {
		t.Fatalf("run(argv) error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(xdgDataHome, "praetor")); !os.IsNotExist(err) {
		t.Fatalf("rejected argv command mutated runtime data: %v", err)
	}
}
