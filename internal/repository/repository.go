package repository

import (
	"crypto/sha256"
	"encoding/binary"
	"fmt"
	"hash"
	"os"
	"os/exec"
	"path/filepath"
	"strings"

	"github.com/Eu-Pedro0ficial/praetor/internal/project"
	"github.com/Eu-Pedro0ficial/praetor/internal/source"
)

// Context is the minimal repository context required for C01.
type Context struct {
	Root string
}

// Inspect captures the minimal deterministic source state required by M0.3.
// The returned digest is snapshot integrity metadata and never repository or
// Project identity.
func Inspect(projectId project.ProjectId, startPath string) (source.SourceSnapshot, error) {
	if !projectId.IsValid() {
		return source.SourceSnapshot{}, fmt.Errorf("valid ProjectId is required")
	}
	repositoryContext, err := Discover(startPath)
	if err != nil {
		return source.SourceSnapshot{}, err
	}

	headOutput, err := runGit(repositoryContext.Root, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return source.SourceSnapshot{}, fmt.Errorf("capture HEAD revision: %w", err)
	}
	headRevision := strings.TrimSpace(string(headOutput))

	trackedOutput, err := runGit(repositoryContext.Root, "ls-files", "--cached", "-z")
	if err != nil {
		return source.SourceSnapshot{}, fmt.Errorf("capture tracked source inventory: %w", err)
	}
	trackedPaths := splitNullTerminated(trackedOutput)

	statusOutput, err := runGit(
		repositoryContext.Root,
		"status",
		"--porcelain=v1",
		"-z",
		"--untracked-files=all",
		"--no-renames",
	)
	if err != nil {
		return source.SourceSnapshot{}, fmt.Errorf("capture working-tree state: %w", err)
	}
	workingTreeState := source.WorkingTreeClean
	if len(statusOutput) > 0 {
		workingTreeState = source.WorkingTreeDirty
	}

	diffOutput, err := runGit(
		repositoryContext.Root,
		"diff",
		"--binary",
		"--full-index",
		"--no-color",
		"--no-ext-diff",
		"--no-prefix",
		"--no-renames",
		"--no-textconv",
		"--diff-algorithm=myers",
		"HEAD",
		"--",
	)
	if err != nil {
		return source.SourceSnapshot{}, fmt.Errorf("capture tracked source delta: %w", err)
	}
	sourceStateDigest := digestSourceState(
		[]byte(headRevision),
		statusOutput,
		trackedOutput,
		diffOutput,
	)

	snapshot, err := source.NewSourceSnapshot(
		projectId,
		repositoryContext.Root,
		headRevision,
		workingTreeState,
		trackedPaths,
		sourceStateDigest,
	)
	if err != nil {
		return source.SourceSnapshot{}, fmt.Errorf("construct SourceSnapshot: %w", err)
	}
	return snapshot, nil
}

func runGit(repositoryRoot string, arguments ...string) ([]byte, error) {
	commandArguments := append([]string{"-C", repositoryRoot}, arguments...)
	command := exec.Command("git", commandArguments...)
	output, err := command.Output()
	if err == nil {
		return output, nil
	}
	if exitError, ok := err.(*exec.ExitError); ok {
		message := strings.TrimSpace(string(exitError.Stderr))
		if message != "" {
			return nil, fmt.Errorf("git %s: %s", strings.Join(arguments, " "), message)
		}
	}
	return nil, fmt.Errorf("git %s: %w", strings.Join(arguments, " "), err)
}

func splitNullTerminated(output []byte) []string {
	if len(output) == 0 {
		return nil
	}
	parts := strings.Split(string(output), "\x00")
	if parts[len(parts)-1] == "" {
		parts = parts[:len(parts)-1]
	}
	return parts
}

func digestSourceState(components ...[]byte) source.SourceStateDigest {
	digest := sha256.New()
	writeDigestComponent(digest, []byte("praetor-source-state-v1"))
	for _, component := range components {
		writeDigestComponent(digest, component)
	}
	return source.SourceStateDigest(fmt.Sprintf("sha256:%x", digest.Sum(nil)))
}

func writeDigestComponent(writer hash.Hash, component []byte) {
	var length [8]byte
	binary.BigEndian.PutUint64(length[:], uint64(len(component)))
	_, _ = writer.Write(length[:])
	_, _ = writer.Write(component)
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
