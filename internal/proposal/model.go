// Package proposal contains the M0.4 isolated proposal workspace and patch
// artifact model. It models source/workspace isolation, not a hostile-code or
// process security boundary.
package proposal

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"github.com/Eu-Pedro0ficial/praetor/internal/project"
	"github.com/Eu-Pedro0ficial/praetor/internal/source"
)

// WorkspaceId identifies one process-owned proposal workspace. It is not a
// Project identity and is never derived from ChangeId or repository state.
type WorkspaceId string

// WorkspaceState is the small M0.4 operational lifecycle for an isolated
// proposal workspace. It does not replace the Change state machine.
type WorkspaceState string

const (
	WorkspaceActive   WorkspaceState = "active"
	WorkspaceRetained WorkspaceState = "retained"
	WorkspaceRejected WorkspaceState = "rejected"
	WorkspaceCleaned  WorkspaceState = "cleaned"
)

// ProposalWorkspace identifies source/workspace isolation for one Change.
type ProposalWorkspace struct {
	workspaceId       WorkspaceId
	projectId         project.ProjectId
	changeId          change.ChangeId
	canonicalRoot     string
	workspaceRoot     string
	baseRevision      string
	sourceStateDigest source.SourceStateDigest
	state             WorkspaceState
}

// NewProposalWorkspace validates a newly created adapter-owned worktree.
func NewProposalWorkspace(
	workspaceId WorkspaceId,
	projectId project.ProjectId,
	changeId change.ChangeId,
	canonicalRoot string,
	workspaceRoot string,
	baseRevision string,
	sourceStateDigest source.SourceStateDigest,
) (ProposalWorkspace, error) {
	if err := validateWorkspaceId(workspaceId); err != nil {
		return ProposalWorkspace{}, err
	}
	if !projectId.IsValid() {
		return ProposalWorkspace{}, fmt.Errorf("valid ProjectId is required")
	}
	validatedChangeId, err := change.NewChangeId(string(changeId))
	if err != nil {
		return ProposalWorkspace{}, err
	}
	canonical, err := resolveExistingDirectory(canonicalRoot, "canonical repository root")
	if err != nil {
		return ProposalWorkspace{}, err
	}
	isolated, err := resolveExistingDirectory(workspaceRoot, "proposal workspace root")
	if err != nil {
		return ProposalWorkspace{}, err
	}
	insideCanonical, err := isPathWithin(canonical, isolated)
	if err != nil {
		return ProposalWorkspace{}, err
	}
	if insideCanonical {
		return ProposalWorkspace{}, fmt.Errorf("proposal workspace must be outside canonical repository root")
	}
	if strings.TrimSpace(baseRevision) == "" {
		return ProposalWorkspace{}, fmt.Errorf("proposal base revision is required")
	}
	if err := validateSourceStateDigest(sourceStateDigest); err != nil {
		return ProposalWorkspace{}, err
	}

	return ProposalWorkspace{
		workspaceId:       workspaceId,
		projectId:         projectId,
		changeId:          validatedChangeId,
		canonicalRoot:     canonical,
		workspaceRoot:     isolated,
		baseRevision:      strings.TrimSpace(baseRevision),
		sourceStateDigest: sourceStateDigest,
		state:             WorkspaceActive,
	}, nil
}

func validateWorkspaceId(workspaceId WorkspaceId) error {
	value := string(workspaceId)
	const prefix = "proposal-"
	if !strings.HasPrefix(value, prefix) {
		return fmt.Errorf("WorkspaceId must use the proposal prefix")
	}
	encoded := strings.TrimPrefix(value, prefix)
	if len(encoded) != 32 {
		return fmt.Errorf("WorkspaceId must contain 128 bits of random identity")
	}
	if _, err := hex.DecodeString(encoded); err != nil {
		return fmt.Errorf("WorkspaceId contains invalid random identity: %w", err)
	}
	return nil
}

func validateSourceStateDigest(sourceStateDigest source.SourceStateDigest) error {
	value := string(sourceStateDigest)
	if !strings.HasPrefix(value, "sha256:") || len(strings.TrimPrefix(value, "sha256:")) != 64 {
		return fmt.Errorf("valid SourceStateDigest is required")
	}
	if _, err := hex.DecodeString(strings.TrimPrefix(value, "sha256:")); err != nil {
		return fmt.Errorf("SourceStateDigest contains invalid SHA-256 text: %w", err)
	}
	return nil
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
		return "", fmt.Errorf("%s is not a directory", label)
	}
	return filepath.Clean(resolved), nil
}

func isPathWithin(parent string, candidate string) (bool, error) {
	relative, err := filepath.Rel(parent, candidate)
	if err != nil {
		return false, fmt.Errorf("compare proposal workspace boundary: %w", err)
	}
	return relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))), nil
}

// WorkspaceId returns the process-local proposal workspace identity.
func (workspace ProposalWorkspace) WorkspaceId() WorkspaceId { return workspace.workspaceId }

// ProjectId returns the associated logical Project identity.
func (workspace ProposalWorkspace) ProjectId() project.ProjectId { return workspace.projectId }

// ChangeId returns the associated Change identity.
func (workspace ProposalWorkspace) ChangeId() change.ChangeId { return workspace.changeId }

// CanonicalRoot returns operational canonical repository metadata.
func (workspace ProposalWorkspace) CanonicalRoot() string { return workspace.canonicalRoot }

// Root returns the isolated worktree path owned by the workspace adapter.
func (workspace ProposalWorkspace) Root() string { return workspace.workspaceRoot }

// BaseRevision returns the approved source revision used to create the worktree.
func (workspace ProposalWorkspace) BaseRevision() string { return workspace.baseRevision }

// SourceStateDigest returns the approved canonical source-state relationship.
func (workspace ProposalWorkspace) SourceStateDigest() source.SourceStateDigest {
	return workspace.sourceStateDigest
}

// State returns the operational proposal workspace lifecycle state.
func (workspace ProposalWorkspace) State() WorkspaceState { return workspace.state }

func (workspace ProposalWorkspace) transition(resultingState WorkspaceState) (ProposalWorkspace, error) {
	allowed := false
	switch workspace.state {
	case WorkspaceActive:
		allowed = resultingState == WorkspaceRetained || resultingState == WorkspaceRejected || resultingState == WorkspaceCleaned
	case WorkspaceRetained:
		allowed = resultingState == WorkspaceRejected || resultingState == WorkspaceCleaned
	case WorkspaceRejected:
		allowed = resultingState == WorkspaceCleaned
	}
	if !allowed {
		return ProposalWorkspace{}, fmt.Errorf(
			"proposal workspace %q cannot transition from %q to %q",
			workspace.workspaceId,
			workspace.state,
			resultingState,
		)
	}
	workspace.state = resultingState
	return workspace, nil
}

// PatchArtifact is the deterministic reviewable source proposal extracted
// from one isolated worktree.
type PatchArtifact struct {
	workspaceId       WorkspaceId
	projectId         project.ProjectId
	changeId          change.ChangeId
	baseRevision      string
	sourceStateDigest source.SourceStateDigest
	content           []byte
	changedPaths      []string
	diffSummary       string
	patchDigest       string
	createdAt         time.Time
}

func newPatchArtifact(
	workspace ProposalWorkspace,
	content []byte,
	changedPaths []string,
	createdAt time.Time,
) (PatchArtifact, error) {
	if workspace.state != WorkspaceActive {
		return PatchArtifact{}, fmt.Errorf("proposal workspace %q is not active", workspace.workspaceId)
	}
	if len(content) == 0 || len(changedPaths) == 0 {
		return PatchArtifact{}, EmptyPatchError{WorkspaceId: workspace.workspaceId}
	}
	if createdAt.IsZero() {
		return PatchArtifact{}, fmt.Errorf("patch creation timestamp is required")
	}

	unique := make(map[string]struct{}, len(changedPaths))
	for _, changedPath := range changedPaths {
		if changedPath == "" {
			return PatchArtifact{}, fmt.Errorf("patch changed path is empty")
		}
		if !utf8.ValidString(changedPath) {
			return PatchArtifact{}, fmt.Errorf("patch changed path contains invalid UTF-8")
		}
		unique[changedPath] = struct{}{}
	}
	canonicalPaths := make([]string, 0, len(unique))
	for changedPath := range unique {
		canonicalPaths = append(canonicalPaths, changedPath)
	}
	sort.Strings(canonicalPaths)
	digest := sha256.Sum256(content)

	return PatchArtifact{
		workspaceId:       workspace.workspaceId,
		projectId:         workspace.projectId,
		changeId:          workspace.changeId,
		baseRevision:      workspace.baseRevision,
		sourceStateDigest: workspace.sourceStateDigest,
		content:           append([]byte(nil), content...),
		changedPaths:      canonicalPaths,
		diffSummary:       fmt.Sprintf("files=%d bytes=%d", len(canonicalPaths), len(content)),
		patchDigest:       fmt.Sprintf("sha256:%x", digest[:]),
		createdAt:         createdAt.UTC(),
	}, nil
}

func (artifact PatchArtifact) WorkspaceId() WorkspaceId     { return artifact.workspaceId }
func (artifact PatchArtifact) ProjectId() project.ProjectId { return artifact.projectId }
func (artifact PatchArtifact) ChangeId() change.ChangeId    { return artifact.changeId }
func (artifact PatchArtifact) BaseRevision() string         { return artifact.baseRevision }
func (artifact PatchArtifact) SourceStateDigest() source.SourceStateDigest {
	return artifact.sourceStateDigest
}
func (artifact PatchArtifact) Content() []byte { return append([]byte(nil), artifact.content...) }
func (artifact PatchArtifact) ChangedPaths() []string {
	return append([]string(nil), artifact.changedPaths...)
}
func (artifact PatchArtifact) DiffSummary() string  { return artifact.diffSummary }
func (artifact PatchArtifact) PatchDigest() string  { return artifact.patchDigest }
func (artifact PatchArtifact) CreatedAt() time.Time { return artifact.createdAt }

// Proposal retains the linked source boundary, worktree, and optional patch
// artifact for the current process-local lifecycle.
type Proposal struct {
	workspace       ProposalWorkspace
	canonicalSource source.SourceSnapshot
	approvedScope   source.ApprovedScope
	artifact        PatchArtifact
	hasArtifact     bool
}

func newProposal(
	workspace ProposalWorkspace,
	canonicalSource source.SourceSnapshot,
	approvedScope source.ApprovedScope,
) (Proposal, error) {
	if workspace.ProjectId() != canonicalSource.ProjectId() || workspace.ProjectId() != approvedScope.ProjectId() {
		return Proposal{}, fmt.Errorf("proposal ProjectId linkage is inconsistent")
	}
	if workspace.ChangeId() != approvedScope.ChangeId() {
		return Proposal{}, fmt.Errorf("proposal ChangeId linkage is inconsistent")
	}
	if workspace.CanonicalRoot() != canonicalSource.RepositoryRoot() {
		return Proposal{}, fmt.Errorf("proposal canonical repository linkage is inconsistent")
	}
	if workspace.BaseRevision() != canonicalSource.HeadRevision() ||
		workspace.SourceStateDigest() != canonicalSource.SourceStateDigest() ||
		workspace.SourceStateDigest() != approvedScope.SourceStateDigest() {
		return Proposal{}, fmt.Errorf("proposal source-state linkage is inconsistent")
	}
	return Proposal{
		workspace:       workspace,
		canonicalSource: canonicalSource,
		approvedScope:   approvedScope,
	}, nil
}

func (current Proposal) Workspace() ProposalWorkspace           { return current.workspace }
func (current Proposal) CanonicalSource() source.SourceSnapshot { return current.canonicalSource }
func (current Proposal) ApprovedScope() source.ApprovedScope    { return current.approvedScope }
func (current Proposal) PatchArtifact() (PatchArtifact, bool) {
	return current.artifact, current.hasArtifact
}

func (current Proposal) withWorkspace(workspace ProposalWorkspace) Proposal {
	current.workspace = workspace
	return current
}

func (current Proposal) withArtifact(artifact PatchArtifact) Proposal {
	current.artifact = artifact
	current.hasArtifact = true
	return current
}

// RehydrateProposal reconstructs a retained proposal from immutable durable
// source, scope, and patch authority. The referenced worktree must still exist;
// no workspace is created or repaired by hydration.
func RehydrateProposal(workspaceId WorkspaceId, workspaceRoot string, canonicalSource source.SourceSnapshot, approvedScope source.ApprovedScope, patchContent []byte, changedPaths []string, patchDigest string, createdAt time.Time) (Proposal, error) {
	current, err := RehydrateWorkspaceProposal(workspaceId, workspaceRoot, canonicalSource, approvedScope)
	if err != nil {
		return Proposal{}, err
	}
	workspace := current.workspace
	workspace, err = workspace.transition(WorkspaceRetained)
	if err != nil {
		return Proposal{}, err
	}
	current.workspace = workspace
	active := workspace
	active.state = WorkspaceActive
	patch, err := newPatchArtifact(active, patchContent, changedPaths, createdAt)
	if err != nil {
		return Proposal{}, err
	}
	if patch.PatchDigest() != patchDigest {
		return Proposal{}, fmt.Errorf("durable PatchArtifact digest mismatch")
	}
	return current.withArtifact(patch), nil
}

// RehydrateWorkspaceProposal reconstructs an active durable workspace before
// a PatchArtifact has been committed.
func RehydrateWorkspaceProposal(workspaceId WorkspaceId, workspaceRoot string, canonicalSource source.SourceSnapshot, approvedScope source.ApprovedScope) (Proposal, error) {
	workspace, err := NewProposalWorkspace(workspaceId, canonicalSource.ProjectId(), approvedScope.ChangeId(), canonicalSource.RepositoryRoot(), workspaceRoot, canonicalSource.HeadRevision(), canonicalSource.SourceStateDigest())
	if err != nil {
		return Proposal{}, err
	}
	return newProposal(workspace, canonicalSource, approvedScope)
}

// EmptyPatchError identifies an isolated proposal with no extractable source change.
type EmptyPatchError struct{ WorkspaceId WorkspaceId }

func (emptyError EmptyPatchError) Error() string {
	return fmt.Sprintf("proposal workspace %q produced no patch", emptyError.WorkspaceId)
}

// DirtyCanonicalSourceError rejects proposal creation from a dirty repository.
type DirtyCanonicalSourceError struct{ RepositoryRoot string }

func (dirtyError DirtyCanonicalSourceError) Error() string {
	return fmt.Sprintf("canonical repository %q is dirty; commit or otherwise resolve developer changes before creating a proposal workspace", dirtyError.RepositoryRoot)
}

// CanonicalSourceDriftError reports source state changed outside the proposal
// lifecycle. Praetor does not repair or overwrite the changed canonical state.
type CanonicalSourceDriftError struct {
	RepositoryRoot string
	ExpectedHead   string
	ActualHead     string
	ExpectedDigest source.SourceStateDigest
	ActualDigest   source.SourceStateDigest
}

func (driftError CanonicalSourceDriftError) Error() string {
	return fmt.Sprintf(
		"canonical source drift detected in %q: expected HEAD %q and digest %q, found HEAD %q and digest %q",
		driftError.RepositoryRoot,
		driftError.ExpectedHead,
		driftError.ExpectedDigest,
		driftError.ActualHead,
		driftError.ActualDigest,
	)
}
