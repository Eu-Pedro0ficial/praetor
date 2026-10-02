package main

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Eu-Pedro0ficial/praetor/internal/repository"
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

func TestRunOutsideGitRepositoryReturnsKnownRepositoryError(t *testing.T) {
	outsideDir := t.TempDir()
	t.Chdir(outsideDir)

	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "data"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(t.TempDir(), "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))

	err := run(nil)
	if !errors.Is(err, repository.ErrNotGitRepository) {
		t.Fatalf("run() error = %v, want ErrNotGitRepository", err)
	}
}

func TestFormatStartupErrorExplainsGitRepositoryRequirement(t *testing.T) {
	message := formatStartupError(repository.ErrNotGitRepository)

	for _, expected := range []string{
		"no Git repository found from the current directory.",
		"Praetor must be started inside the Git repository you want it to govern.",
		"cd /path/to/repository",
		"praetor",
	} {
		if !strings.Contains(message, expected) {
			t.Fatalf("formatStartupError() = %q, missing %q", message, expected)
		}
	}

	for _, leaked := range []string{
		"fatal:",
		"not a git repository (or any of the parent directories)",
	} {
		if strings.Contains(strings.ToLower(message), leaked) {
			t.Fatalf("formatStartupError() leaked raw Git detail %q: %q", leaked, message)
		}
	}
}

func TestRunInsideGitRepositoryPassesRepositoryDiscovery(t *testing.T) {
	repositoryRoot := t.TempDir()

	command := exec.Command("git", "-C", repositoryRoot, "init", "--quiet")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}

	t.Chdir(repositoryRoot)

	// An unborn repository is sufficient for this regression: startup must get
	// past repository discovery. A later HEAD-dependent error is acceptable and
	// proves that the non-Git sentinel was not produced.
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "data"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(t.TempDir(), "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))

	err := run(nil)
	if errors.Is(err, repository.ErrNotGitRepository) {
		t.Fatalf("run() rejected a Git repository as non-Git: %v", err)
	}
}
