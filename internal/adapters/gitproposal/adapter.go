// Package gitproposal adapts system Git worktrees and diffs to the M0.4
// source/workspace and patch ports. Git worktrees isolate source state; this
// adapter is not a process, network, container, VM, or hostile-code sandbox.
package gitproposal

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/Eu-Pedro0ficial/praetor/internal/proposal"
)

type ownedWorkspace struct {
	ownerRoot     string
	workspaceRoot string
	canonicalRoot string
}

// Adapter owns Git worktrees created beneath one configured temporary root.
// Ownership is explicit and process-local so cleanup cannot target arbitrary
// paths or another adapter's worktree.
type Adapter struct {
	temporaryRoot string
	mutex         sync.Mutex
	owned         map[proposal.WorkspaceId]ownedWorkspace
}

// NewDefault constructs the runtime adapter beneath the operating system
// temporary directory. Creation failures remain explicit at workspace
// creation time.
func NewDefault() *Adapter {
	return &Adapter{
		temporaryRoot: filepath.Clean(os.TempDir()),
		owned:         make(map[proposal.WorkspaceId]ownedWorkspace),
	}
}

// New constructs the local Git proposal adapter. An empty temporaryRoot uses
// the operating system temporary directory.
func New(temporaryRoot string) (*Adapter, error) {
	if strings.TrimSpace(temporaryRoot) == "" {
		temporaryRoot = os.TempDir()
	}
	resolved, err := resolveExistingDirectory(temporaryRoot, "proposal temporary root")
	if err != nil {
		return nil, err
	}
	return &Adapter{
		temporaryRoot: resolved,
		owned:         make(map[proposal.WorkspaceId]ownedWorkspace),
	}, nil
}

func resolveExistingDirectory(value string, label string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("%s is required", label)
	}
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", fmt.Errorf("resolve %s: %w", label, err)
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("resolve %s symlinks: %w", label, err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("inspect %s: %w", label, err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("%s is not a directory: %s", label, resolved)
	}
	return filepath.Clean(resolved), nil
}

// Create adds a detached Git worktree at the exact approved base revision.
func (adapter *Adapter) Create(request proposal.WorkspaceRequest) (proposal.ProposalWorkspace, error) {
	if adapter == nil {
		return proposal.ProposalWorkspace{}, fmt.Errorf("Git proposal adapter is required")
	}
	adapter.mutex.Lock()
	defer adapter.mutex.Unlock()
	temporaryRoot, err := resolveExistingDirectory(adapter.temporaryRoot, "proposal temporary root")
	if err != nil {
		return proposal.ProposalWorkspace{}, err
	}
	canonicalRoot, err := resolveExistingDirectory(request.CanonicalRoot, "canonical repository root")
	if err != nil {
		return proposal.ProposalWorkspace{}, err
	}
	temporaryRootInsideCanonical, err := pathWithin(canonicalRoot, temporaryRoot)
	if err != nil {
		return proposal.ProposalWorkspace{}, err
	}
	if temporaryRootInsideCanonical {
		return proposal.ProposalWorkspace{}, fmt.Errorf("proposal temporary root must be outside canonical repository root")
	}
	adapter.temporaryRoot = temporaryRoot

	workspaceId, err := adapter.newWorkspaceId()
	if err != nil {
		return proposal.ProposalWorkspace{}, err
	}
	ownerRoot, err := os.MkdirTemp(adapter.temporaryRoot, "praetor-proposal-")
	if err != nil {
		return proposal.ProposalWorkspace{}, fmt.Errorf("create proposal owner directory: %w", err)
	}
	workspaceRoot := filepath.Join(ownerRoot, "workspace")
	cleanupOwner := func() {
		_ = os.RemoveAll(ownerRoot)
	}

	if _, err := runGit(
		request.CanonicalRoot,
		"worktree", "add", "--detach", "--", workspaceRoot, request.BaseRevision,
	); err != nil {
		cleanupOwner()
		return proposal.ProposalWorkspace{}, fmt.Errorf("add Git proposal worktree: %w", err)
	}
	cleanupWorktree := func() {
		_, _ = runGit(request.CanonicalRoot, "worktree", "remove", "--force", "--", workspaceRoot)
		cleanupOwner()
	}

	workspace, err := proposal.NewProposalWorkspace(
		workspaceId,
		request.ProjectId,
		request.ChangeId,
		request.CanonicalRoot,
		workspaceRoot,
		request.BaseRevision,
		request.SourceStateDigest,
	)
	if err != nil {
		cleanupWorktree()
		return proposal.ProposalWorkspace{}, err
	}
	adapter.owned[workspaceId] = ownedWorkspace{
		ownerRoot:     ownerRoot,
		workspaceRoot: workspace.Root(),
		canonicalRoot: workspace.CanonicalRoot(),
	}
	return workspace, nil
}

// Extract returns a Git-native binary-capable patch and a NUL-delimited,
// deterministic changed-path inventory. Intent-to-add updates occur only in
// the isolated worktree index so untracked added files appear in the patch.
func (adapter *Adapter) Extract(workspace proposal.ProposalWorkspace) (proposal.ExtractedPatch, error) {
	if adapter == nil {
		return proposal.ExtractedPatch{}, fmt.Errorf("Git proposal adapter is required")
	}
	adapter.mutex.Lock()
	defer adapter.mutex.Unlock()
	if _, err := adapter.validateOwned(workspace); err != nil {
		return proposal.ExtractedPatch{}, err
	}

	untrackedOutput, err := runGit(
		workspace.Root(),
		"ls-files", "--others", "--exclude-standard", "-z", "--",
	)
	if err != nil {
		return proposal.ExtractedPatch{}, fmt.Errorf("list untracked proposal paths: %w", err)
	}
	untrackedPaths := splitNullTerminated(untrackedOutput)
	if len(untrackedPaths) > 0 {
		arguments := []string{"add", "--intent-to-add", "--"}
		arguments = append(arguments, untrackedPaths...)
		if _, err := runGit(workspace.Root(), arguments...); err != nil {
			return proposal.ExtractedPatch{}, fmt.Errorf("prepare added proposal paths: %w", err)
		}
	}

	changedOutput, err := runGit(
		workspace.Root(),
		"-c", "core.quotePath=true",
		"diff", "--name-only", "-z", "--no-renames", workspace.BaseRevision(), "--",
	)
	if err != nil {
		return proposal.ExtractedPatch{}, fmt.Errorf("extract proposal changed paths: %w", err)
	}
	changedPaths := splitNullTerminated(changedOutput)
	sort.Strings(changedPaths)

	patchContent, err := runGit(
		workspace.Root(),
		"-c", "core.quotePath=true",
		"diff",
		"--binary",
		"--full-index",
		"--no-color",
		"--no-ext-diff",
		"--no-renames",
		"--no-textconv",
		"--diff-algorithm=myers",
		workspace.BaseRevision(),
		"--",
	)
	if err != nil {
		return proposal.ExtractedPatch{}, fmt.Errorf("extract Git proposal patch: %w", err)
	}
	return proposal.ExtractedPatch{
		Content:      append([]byte(nil), patchContent...),
		ChangedPaths: append([]string(nil), changedPaths...),
	}, nil
}

// Remove deletes only a worktree recorded as owned by this adapter instance.
func (adapter *Adapter) Remove(workspace proposal.ProposalWorkspace) error {
	if adapter == nil {
		return fmt.Errorf("Git proposal adapter is required")
	}
	adapter.mutex.Lock()
	defer adapter.mutex.Unlock()
	owned, err := adapter.validateOwned(workspace)
	if err != nil {
		return err
	}

	if _, err := runGit(
		owned.canonicalRoot,
		"worktree", "remove", "--force", "--", owned.workspaceRoot,
	); err != nil {
		return fmt.Errorf("remove Git proposal worktree: %w", err)
	}
	if err := os.RemoveAll(owned.ownerRoot); err != nil {
		return fmt.Errorf("remove proposal owner directory: %w", err)
	}
	delete(adapter.owned, workspace.WorkspaceId())
	return nil
}

func (adapter *Adapter) newWorkspaceId() (proposal.WorkspaceId, error) {
	for {
		var randomBytes [16]byte
		if _, err := rand.Read(randomBytes[:]); err != nil {
			return "", fmt.Errorf("generate proposal WorkspaceId: %w", err)
		}
		workspaceId := proposal.WorkspaceId("proposal-" + hex.EncodeToString(randomBytes[:]))
		if _, exists := adapter.owned[workspaceId]; !exists {
			return workspaceId, nil
		}
	}
}

func (adapter *Adapter) validateOwned(workspace proposal.ProposalWorkspace) (ownedWorkspace, error) {
	owned, exists := adapter.owned[workspace.WorkspaceId()]
	if !exists {
		return ownedWorkspace{}, fmt.Errorf("proposal workspace %q is not owned by this adapter", workspace.WorkspaceId())
	}
	if owned.workspaceRoot != workspace.Root() || owned.canonicalRoot != workspace.CanonicalRoot() {
		return ownedWorkspace{}, fmt.Errorf("proposal workspace %q ownership metadata does not match", workspace.WorkspaceId())
	}
	insideTemporaryRoot, err := pathWithin(adapter.temporaryRoot, owned.ownerRoot)
	if err != nil {
		return ownedWorkspace{}, err
	}
	insideOwnerRoot, err := pathWithin(owned.ownerRoot, owned.workspaceRoot)
	if err != nil {
		return ownedWorkspace{}, err
	}
	if !insideTemporaryRoot || !insideOwnerRoot || owned.ownerRoot == adapter.temporaryRoot {
		return ownedWorkspace{}, fmt.Errorf("proposal workspace %q escaped adapter ownership boundary", workspace.WorkspaceId())
	}
	return owned, nil
}

func pathWithin(parent string, candidate string) (bool, error) {
	relative, err := filepath.Rel(parent, candidate)
	if err != nil {
		return false, fmt.Errorf("compare Git proposal ownership boundary: %w", err)
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))), nil
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
			return nil, fmt.Errorf("git %s: %s", formatGitArguments(arguments), strconv.Quote(message))
		}
	}
	return nil, fmt.Errorf("git %s: %w", formatGitArguments(arguments), err)
}

func formatGitArguments(arguments []string) string {
	quoted := make([]string, len(arguments))
	for index, argument := range arguments {
		quoted[index] = strconv.Quote(argument)
	}
	return strings.Join(quoted, " ")
}
