package repository

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
)

func TestDiscoverRepositoryRoot(t *testing.T) {
	repoDir := t.TempDir()
	if err := initGitRepo(repoDir); err != nil {
		t.Fatalf("init git repo: %v", err)
	}

	ctx, err := Discover(repoDir)
	if err != nil {
		t.Fatalf("Discover returned error for repo root: %v", err)
	}
	if ctx.Root != repoDir {
		t.Fatalf("expected repo root %q, got %q", repoDir, ctx.Root)
	}
}

func TestDiscoverRepositoryFromNestedDirectory(t *testing.T) {
	repoDir := t.TempDir()
	if err := initGitRepo(repoDir); err != nil {
		t.Fatalf("init git repo: %v", err)
	}

	nestedDir := filepath.Join(repoDir, "subdir", "deep")
	if err := os.MkdirAll(nestedDir, 0o755); err != nil {
		t.Fatalf("create nested dir: %v", err)
	}

	ctx, err := Discover(nestedDir)
	if err != nil {
		t.Fatalf("Discover returned error from nested directory: %v", err)
	}
	if ctx.Root != repoDir {
		t.Fatalf("expected repo root %q, got %q", repoDir, ctx.Root)
	}
}

func TestDiscoverOutsideRepository(t *testing.T) {
	outsideDir := t.TempDir()
	if _, err := Discover(outsideDir); err == nil {
		t.Fatal("expected error outside Git repository")
	}
}

func initGitRepo(dir string) error {
	cmd := exec.Command("git", "-C", dir, "init")
	if _, err := cmd.CombinedOutput(); err != nil {
		return err
	}
	return nil
}
