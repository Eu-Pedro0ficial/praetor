package repository

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
)

// Context is the minimal repository context required for C01.
type Context struct {
	Root string
}

// Discover resolves the Git repository root from the provided path.
// It intentionally keeps the scope to repository discovery only and does not
// attempt project identity, memory, workflow, or patch lifecycle work.
func Discover(startPath string) (Context, error) {
	if startPath == "" {
		startPath = "."
	}

	absPath, err := filepath.Abs(startPath)
	if err != nil {
		return Context{}, fmt.Errorf("resolve path: %w", err)
	}

	info, err := os.Stat(absPath)
	if err != nil {
		return Context{}, fmt.Errorf("locate repository: %w", err)
	}
	if !info.IsDir() {
		return Context{}, fmt.Errorf("repository path is not a directory: %s", absPath)
	}

	cmd := exec.Command("git", "-C", absPath, "rev-parse", "--show-toplevel")
	output, err := cmd.Output()
	if err != nil {
		if exitErr, ok := err.(*exec.ExitError); ok {
			stderr := strings.TrimSpace(string(exitErr.Stderr))
			if stderr == "" {
				stderr = "not a Git repository"
			}
			return Context{}, fmt.Errorf("not a Git repository: %s", stderr)
		}
		return Context{}, fmt.Errorf("determine repository root: %w", err)
	}

	root := strings.TrimSpace(string(output))
	if root == "" {
		return Context{}, fmt.Errorf("Git returned an empty repository root")
	}

	return Context{Root: root}, nil
}
