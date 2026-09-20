// Package gitproposal adapts system Git worktrees and diffs to the M0.4
// source/workspace and patch ports. Git worktrees isolate source state; this
// adapter is not a process, network, container, VM, or hostile-code sandbox.
package gitproposal

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"

	"github.com/Eu-Pedro0ficial/praetor/internal/integration"
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

// Reattach safely reclaims an existing adapter-created worktree after a
// process restart. Durable authority supplies its identity and linkage; this
// method independently rechecks the filesystem and Git boundaries before the
// workspace can be extracted or removed by this adapter instance.
func (adapter *Adapter) Reattach(workspace proposal.ProposalWorkspace) error {
	if adapter == nil {
		return fmt.Errorf("Git proposal adapter is required")
	}
	adapter.mutex.Lock()
	defer adapter.mutex.Unlock()
	if _, exists := adapter.owned[workspace.WorkspaceId()]; exists {
		_, err := adapter.validateOwned(workspace)
		return err
	}
	temporaryRoot, err := resolveExistingDirectory(adapter.temporaryRoot, "proposal temporary root")
	if err != nil {
		return err
	}
	workspaceRoot, err := resolveExistingDirectory(workspace.Root(), "proposal workspace root")
	if err != nil {
		return err
	}
	canonicalRoot, err := resolveExistingDirectory(workspace.CanonicalRoot(), "canonical repository root")
	if err != nil {
		return err
	}
	ownerRoot := filepath.Dir(workspaceRoot)
	if filepath.Base(workspaceRoot) != "workspace" || !strings.HasPrefix(filepath.Base(ownerRoot), "praetor-proposal-") || filepath.Dir(ownerRoot) != temporaryRoot {
		return fmt.Errorf("proposal workspace %q is outside the controlled restart layout", workspace.WorkspaceId())
	}
	insideTemporaryRoot, err := pathWithin(temporaryRoot, ownerRoot)
	if err != nil || !insideTemporaryRoot {
		return fmt.Errorf("proposal workspace %q escaped adapter ownership boundary", workspace.WorkspaceId())
	}
	topLevel, err := runGit(workspaceRoot, "rev-parse", "--show-toplevel")
	if err != nil || filepath.Clean(strings.TrimSpace(string(topLevel))) != workspaceRoot {
		return fmt.Errorf("proposal workspace %q is not the expected Git worktree", workspace.WorkspaceId())
	}
	head, err := runGit(workspaceRoot, "rev-parse", "--verify", "HEAD")
	if err != nil || strings.TrimSpace(string(head)) != workspace.BaseRevision() {
		return fmt.Errorf("proposal workspace %q base revision changed", workspace.WorkspaceId())
	}
	worktrees, err := runGit(canonicalRoot, "worktree", "list", "--porcelain")
	if err != nil || !worktreeListContains(worktrees, workspaceRoot) {
		return fmt.Errorf("proposal workspace %q is not registered by the canonical repository", workspace.WorkspaceId())
	}
	adapter.temporaryRoot = temporaryRoot
	adapter.owned[workspace.WorkspaceId()] = ownedWorkspace{ownerRoot: ownerRoot, workspaceRoot: workspaceRoot, canonicalRoot: canonicalRoot}
	return nil
}

func worktreeListContains(output []byte, root string) bool {
	for _, line := range strings.Split(string(output), "\n") {
		if strings.HasPrefix(line, "worktree ") && filepath.Clean(strings.TrimPrefix(line, "worktree ")) == root {
			return true
		}
	}
	return false
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
	return extractPatch(workspace)
}

func extractPatch(workspace proposal.ProposalWorkspace) (proposal.ExtractedPatch, error) {

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

// Preflight proves that canonical source is still the exact clean approved
// base, the index is unchanged, the retained workspace still yields the exact
// artifact, and Git can apply the whole patch without mutation.
func (adapter *Adapter) Preflight(request integration.ApplicationRequest) error {
	if adapter == nil {
		return fmt.Errorf("Git proposal adapter is required")
	}
	adapter.mutex.Lock()
	defer adapter.mutex.Unlock()
	return adapter.preflightLocked(request)
}

// Apply repeats preflight under the adapter lock, invokes default atomic
// working-tree-only git apply, and proves the exact resulting canonical diff.
// It never passes --index, --cached, --3way, --reject, or --unsafe-paths.
func (adapter *Adapter) Apply(request integration.ApplicationRequest) (integration.CanonicalProof, bool, error) {
	if adapter == nil {
		return integration.CanonicalProof{}, false, fmt.Errorf("Git proposal adapter is required")
	}
	adapter.mutex.Lock()
	defer adapter.mutex.Unlock()
	if err := adapter.preflightLocked(request); err != nil {
		return integration.CanonicalProof{}, false, err
	}
	artifact, _ := request.Proposal.PatchArtifact()
	canonicalRoot := request.Proposal.Workspace().CanonicalRoot()
	if _, err := runGitInput(
		canonicalRoot,
		artifact.Content(),
		"apply", "--binary", "--whitespace=nowarn", "-",
	); err != nil {
		// Git apply is whole-patch atomic unless --reject is used. This adapter
		// never uses --reject, so command failure means no adapter mutation.
		return integration.CanonicalProof{}, false, fmt.Errorf("atomically apply canonical patch: %w", err)
	}
	proof, err := adapter.verifyAppliedLocked(request)
	if err != nil {
		return integration.CanonicalProof{}, true, err
	}
	return proof, true, nil
}

// Classify distinguishes the exact approved PRE and POST source conditions.
// Any other observable state is deliberately AMBIGUOUS.
func (adapter *Adapter) Classify(request integration.ApplicationRequest) (integration.ExternalCondition, error) {
	if adapter == nil {
		return integration.ConditionAmbiguous, fmt.Errorf("Git proposal adapter is required")
	}
	adapter.mutex.Lock()
	defer adapter.mutex.Unlock()
	if err := adapter.preflightLocked(request); err == nil {
		return integration.ConditionPRE, nil
	}
	if _, err := adapter.verifyAppliedLocked(request); err == nil {
		return integration.ConditionPOST, nil
	}
	return integration.ConditionAmbiguous, nil
}

// PostProof returns exact deterministic proof without reapplying the patch.
func (adapter *Adapter) PostProof(request integration.ApplicationRequest) (integration.CanonicalProof, error) {
	if adapter == nil {
		return integration.CanonicalProof{}, fmt.Errorf("Git proposal adapter is required")
	}
	adapter.mutex.Lock()
	defer adapter.mutex.Unlock()
	return adapter.verifyAppliedLocked(request)
}

// ClassifyDurable performs restart-safe PRE/POST classification from immutable
// persisted source and patch authority, without a proposal worktree.
func (adapter *Adapter) ClassifyDurable(request integration.RecoveryRequest) (integration.ExternalCondition, error) {
	if adapter == nil {
		return integration.ConditionAmbiguous, fmt.Errorf("Git proposal adapter is required")
	}
	adapter.mutex.Lock()
	defer adapter.mutex.Unlock()
	if err := verifyDurablePre(request); err == nil {
		return integration.ConditionPRE, nil
	}
	if _, err := adapter.verifyDurablePost(request); err == nil {
		return integration.ConditionPOST, nil
	}
	return integration.ConditionAmbiguous, nil
}

func (adapter *Adapter) PostProofDurable(request integration.RecoveryRequest) (integration.CanonicalProof, error) {
	if adapter == nil {
		return integration.CanonicalProof{}, fmt.Errorf("Git proposal adapter is required")
	}
	adapter.mutex.Lock()
	defer adapter.mutex.Unlock()
	return adapter.verifyDurablePost(request)
}

func verifyDurablePre(request integration.RecoveryRequest) error {
	root, err := resolveExistingDirectory(request.RepositoryRoot, "canonical repository root")
	if err != nil {
		return err
	}
	if root != filepath.Clean(request.RepositoryRoot) {
		return fmt.Errorf("canonical repository root linkage changed")
	}
	head, err := runGit(root, "rev-parse", "--verify", "HEAD")
	if err != nil || strings.TrimSpace(string(head)) != request.BaseRevision {
		return fmt.Errorf("canonical HEAD differs from durable PRE authority")
	}
	status, err := runGit(root, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--no-renames")
	if err != nil || len(status) != 0 {
		return fmt.Errorf("canonical working tree differs from durable PRE authority")
	}
	if _, err := runGit(root, "diff", "--cached", "--quiet", "HEAD", "--"); err != nil {
		return fmt.Errorf("canonical index differs from durable PRE authority")
	}
	tracked, err := runGit(root, "ls-files", "--cached", "-z")
	if err != nil || !equalPathLists(splitNullTerminated(tracked), request.TrackedPaths) {
		return fmt.Errorf("canonical tracked inventory differs from durable PRE authority")
	}
	if _, err := runGitInput(root, request.PatchContent, "apply", "--check", "--binary", "--whitespace=nowarn", "-"); err != nil {
		return fmt.Errorf("durable patch is not applicable: %w", err)
	}
	return nil
}

func (adapter *Adapter) verifyDurablePost(request integration.RecoveryRequest) (integration.CanonicalProof, error) {
	root, err := resolveExistingDirectory(request.RepositoryRoot, "canonical repository root")
	if err != nil {
		return integration.CanonicalProof{}, err
	}
	head, err := runGit(root, "rev-parse", "--verify", "HEAD")
	if err != nil || strings.TrimSpace(string(head)) != request.BaseRevision {
		return integration.CanonicalProof{}, fmt.Errorf("canonical HEAD differs from durable POST authority")
	}
	if _, err := runGit(root, "diff", "--cached", "--quiet", "HEAD", "--"); err != nil {
		return integration.CanonicalProof{}, fmt.Errorf("canonical index differs from durable POST authority")
	}
	tracked, err := runGit(root, "ls-files", "--cached", "-z")
	if err != nil || !equalPathLists(splitNullTerminated(tracked), request.TrackedPaths) {
		return integration.CanonicalProof{}, fmt.Errorf("canonical tracked inventory differs from durable POST authority")
	}
	status, err := runGit(root, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--no-renames")
	if err != nil {
		return integration.CanonicalProof{}, err
	}
	changedPaths, err := porcelainPaths(status)
	if err != nil || !equalPathLists(changedPaths, request.ChangedPaths) {
		return integration.CanonicalProof{}, fmt.Errorf("canonical changed paths differ from durable patch authority")
	}
	canonicalPatch, err := extractCanonicalPatch(adapter.temporaryRoot, root, request.BaseRevision, request.ChangedPaths)
	if err != nil {
		return integration.CanonicalProof{}, err
	}
	if !bytes.Equal(canonicalPatch, request.PatchContent) {
		return integration.CanonicalProof{}, fmt.Errorf("canonical diff differs from durable patch authority")
	}
	digest := sha256.Sum256(canonicalPatch)
	actualDigest := fmt.Sprintf("sha256:%x", digest[:])
	if actualDigest != request.PatchDigest {
		return integration.CanonicalProof{}, fmt.Errorf("durable patch digest is inconsistent")
	}
	if _, err := runGitInput(root, request.PatchContent, "apply", "--reverse", "--check", "--binary", "--whitespace=nowarn", "-"); err != nil {
		return integration.CanonicalProof{}, fmt.Errorf("prove durable patch reversibility: %w", err)
	}
	return integration.NewCanonicalProof(request.BaseRevision, actualDigest, changedPaths, true)
}

func (adapter *Adapter) preflightLocked(request integration.ApplicationRequest) error {
	currentProposal := request.Proposal
	workspace := currentProposal.Workspace()
	artifact, hasArtifact := currentProposal.PatchArtifact()
	if workspace.State() != proposal.WorkspaceRetained || !hasArtifact {
		return fmt.Errorf("canonical application requires a retained Proposal and PatchArtifact")
	}
	if _, err := adapter.validateOwned(workspace); err != nil {
		return err
	}
	extracted, err := extractPatch(workspace)
	if err != nil {
		return fmt.Errorf("re-extract retained PatchArtifact: %w", err)
	}
	if !bytes.Equal(extracted.Content, artifact.Content()) ||
		!equalPathLists(extracted.ChangedPaths, artifact.ChangedPaths()) {
		return fmt.Errorf("retained proposal changed after PatchArtifact approval")
	}
	digest := sha256.Sum256(extracted.Content)
	if fmt.Sprintf("sha256:%x", digest[:]) != artifact.PatchDigest() {
		return fmt.Errorf("retained PatchArtifact digest is inconsistent")
	}
	if err := verifyCanonicalBase(currentProposal); err != nil {
		return err
	}
	if _, err := runGitInput(
		workspace.CanonicalRoot(),
		artifact.Content(),
		"apply", "--check", "--binary", "--whitespace=nowarn", "-",
	); err != nil {
		return fmt.Errorf("check canonical patch applicability: %w", err)
	}
	return nil
}

func verifyCanonicalBase(currentProposal proposal.Proposal) error {
	workspace := currentProposal.Workspace()
	expected := currentProposal.CanonicalSource()
	canonicalRoot, err := resolveExistingDirectory(workspace.CanonicalRoot(), "canonical repository root")
	if err != nil {
		return err
	}
	if canonicalRoot != expected.RepositoryRoot() {
		return fmt.Errorf("canonical repository root linkage changed")
	}
	head, err := runGit(canonicalRoot, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return fmt.Errorf("inspect canonical HEAD: %w", err)
	}
	if strings.TrimSpace(string(head)) != workspace.BaseRevision() {
		return fmt.Errorf("canonical HEAD changed after approval")
	}
	status, err := runGit(canonicalRoot, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--no-renames")
	if err != nil {
		return fmt.Errorf("inspect canonical working tree: %w", err)
	}
	if len(status) != 0 {
		return fmt.Errorf("canonical working tree or index changed after approval")
	}
	if _, err := runGit(canonicalRoot, "diff", "--cached", "--quiet", "HEAD", "--"); err != nil {
		return fmt.Errorf("canonical Git index changed after approval: %w", err)
	}
	tracked, err := runGit(canonicalRoot, "ls-files", "--cached", "-z")
	if err != nil {
		return fmt.Errorf("inspect canonical tracked inventory: %w", err)
	}
	expectedPaths := make([]string, len(expected.TrackedPaths()))
	for index, repositoryPath := range expected.TrackedPaths() {
		expectedPaths[index] = string(repositoryPath)
	}
	if !equalPathLists(splitNullTerminated(tracked), expectedPaths) {
		return fmt.Errorf("canonical tracked inventory changed after approval")
	}
	return nil
}

func (adapter *Adapter) verifyAppliedLocked(request integration.ApplicationRequest) (integration.CanonicalProof, error) {
	currentProposal := request.Proposal
	workspace := currentProposal.Workspace()
	artifact, _ := currentProposal.PatchArtifact()
	canonicalRoot := workspace.CanonicalRoot()
	head, err := runGit(canonicalRoot, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return integration.CanonicalProof{}, fmt.Errorf("prove canonical HEAD: %w", err)
	}
	headRevision := strings.TrimSpace(string(head))
	if headRevision != workspace.BaseRevision() {
		return integration.CanonicalProof{}, fmt.Errorf("canonical application unexpectedly changed HEAD")
	}
	if _, err := runGit(canonicalRoot, "diff", "--cached", "--quiet", "HEAD", "--"); err != nil {
		return integration.CanonicalProof{}, fmt.Errorf("canonical application changed Git index: %w", err)
	}
	tracked, err := runGit(canonicalRoot, "ls-files", "--cached", "-z")
	if err != nil {
		return integration.CanonicalProof{}, fmt.Errorf("prove canonical tracked inventory: %w", err)
	}
	expectedTracked := currentProposal.CanonicalSource().TrackedPaths()
	expectedPaths := make([]string, len(expectedTracked))
	for index, repositoryPath := range expectedTracked {
		expectedPaths[index] = string(repositoryPath)
	}
	if !equalPathLists(splitNullTerminated(tracked), expectedPaths) {
		return integration.CanonicalProof{}, fmt.Errorf("canonical application changed tracked inventory")
	}
	status, err := runGit(canonicalRoot, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--no-renames")
	if err != nil {
		return integration.CanonicalProof{}, fmt.Errorf("prove canonical changed paths: %w", err)
	}
	changedPaths, err := porcelainPaths(status)
	if err != nil {
		return integration.CanonicalProof{}, err
	}
	if !equalPathLists(changedPaths, artifact.ChangedPaths()) {
		return integration.CanonicalProof{}, fmt.Errorf("canonical changed paths do not equal approved PatchArtifact paths")
	}
	canonicalPatch, err := extractCanonicalPatch(
		adapter.temporaryRoot,
		canonicalRoot,
		workspace.BaseRevision(),
		artifact.ChangedPaths(),
	)
	if err != nil {
		return integration.CanonicalProof{}, err
	}
	if !bytes.Equal(canonicalPatch, artifact.Content()) {
		return integration.CanonicalProof{}, fmt.Errorf("canonical diff does not equal approved PatchArtifact")
	}
	if _, err := runGitInput(
		canonicalRoot,
		artifact.Content(),
		"apply", "--reverse", "--check", "--binary", "--whitespace=nowarn", "-",
	); err != nil {
		return integration.CanonicalProof{}, fmt.Errorf("prove canonical patch reversibility: %w", err)
	}
	retainedPatch, err := extractPatch(workspace)
	if err != nil {
		return integration.CanonicalProof{}, fmt.Errorf("re-extract proposal after canonical application: %w", err)
	}
	if !bytes.Equal(retainedPatch.Content, artifact.Content()) ||
		!equalPathLists(retainedPatch.ChangedPaths, artifact.ChangedPaths()) {
		return integration.CanonicalProof{}, fmt.Errorf("retained proposal changed during canonical application")
	}
	digest := sha256.Sum256(canonicalPatch)
	return integration.NewCanonicalProof(
		headRevision,
		fmt.Sprintf("sha256:%x", digest[:]),
		changedPaths,
		true,
	)
}

func extractCanonicalPatch(
	temporaryRoot string,
	canonicalRoot string,
	baseRevision string,
	changedPaths []string,
) ([]byte, error) {
	ownerRoot, err := os.MkdirTemp(temporaryRoot, "praetor-proof-index-")
	if err != nil {
		return nil, fmt.Errorf("create canonical proof index directory: %w", err)
	}
	defer func() { _ = os.RemoveAll(ownerRoot) }()
	indexPath := filepath.Join(ownerRoot, "index")
	if _, err := runGitWithIndex(canonicalRoot, indexPath, nil, "read-tree", baseRevision); err != nil {
		return nil, fmt.Errorf("prepare canonical proof index: %w", err)
	}
	trackedOutput, err := runGit(canonicalRoot, "ls-tree", "-r", "--name-only", "-z", baseRevision, "--")
	if err != nil {
		return nil, fmt.Errorf("inspect base paths for canonical proof: %w", err)
	}
	tracked := make(map[string]struct{})
	for _, repositoryPath := range splitNullTerminated(trackedOutput) {
		tracked[repositoryPath] = struct{}{}
	}
	addedPaths := make([]string, 0)
	for _, repositoryPath := range changedPaths {
		if _, existed := tracked[repositoryPath]; !existed {
			addedPaths = append(addedPaths, repositoryPath)
		}
	}
	if len(addedPaths) > 0 {
		arguments := []string{"add", "--intent-to-add", "--"}
		arguments = append(arguments, addedPaths...)
		if _, err := runGitWithIndex(canonicalRoot, indexPath, nil, arguments...); err != nil {
			return nil, fmt.Errorf("prepare added paths in canonical proof index: %w", err)
		}
	}
	patch, err := runGitWithIndex(
		canonicalRoot,
		indexPath,
		nil,
		"-c", "core.quotePath=true",
		"diff", "--binary", "--full-index", "--no-color", "--no-ext-diff",
		"--no-renames", "--no-textconv", "--diff-algorithm=myers",
		baseRevision, "--",
	)
	if err != nil {
		return nil, fmt.Errorf("extract canonical application diff: %w", err)
	}
	return patch, nil
}

func porcelainPaths(output []byte) ([]string, error) {
	entries := splitNullTerminated(output)
	paths := make([]string, 0, len(entries))
	for _, entry := range entries {
		if len(entry) < 4 || entry[2] != ' ' {
			return nil, fmt.Errorf("canonical Git status contained malformed entry")
		}
		paths = append(paths, entry[3:])
	}
	sort.Strings(paths)
	return paths, nil
}

func equalPathLists(left []string, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
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
	return runGitInput(repositoryRoot, nil, arguments...)
}

func runGitInput(repositoryRoot string, input []byte, arguments ...string) ([]byte, error) {
	return runGitWithIndex(repositoryRoot, "", input, arguments...)
}

func runGitWithIndex(repositoryRoot string, indexPath string, input []byte, arguments ...string) ([]byte, error) {
	commandArguments := append([]string{"-C", repositoryRoot}, arguments...)
	command := exec.Command("git", commandArguments...)
	if indexPath != "" {
		environment := make([]string, 0, len(os.Environ())+1)
		for _, value := range os.Environ() {
			if !strings.HasPrefix(value, "GIT_INDEX_FILE=") {
				environment = append(environment, value)
			}
		}
		command.Env = append(environment, "GIT_INDEX_FILE="+indexPath)
	}
	if input != nil {
		command.Stdin = bytes.NewReader(input)
	}
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
