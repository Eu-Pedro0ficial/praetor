package proposal

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"github.com/Eu-Pedro0ficial/praetor/internal/project"
	"github.com/Eu-Pedro0ficial/praetor/internal/source"
)

const (
	EventProposalWorkspaceCreated   = "PROPOSAL_WORKSPACE_CREATED"
	EventPatchExtracted             = "PATCH_EXTRACTED"
	EventPatchSurfaceValidated      = "PATCH_SURFACE_VALIDATED"
	EventPatchRejected              = "PATCH_REJECTED"
	EventProposalWorkspaceDiscarded = "PROPOSAL_WORKSPACE_DISCARDED"
)

// WorkspaceRequest is the application-owned request for Git worktree source
// isolation. ChangeId is linkage only and must not control filesystem paths.
type WorkspaceRequest struct {
	ProjectId         project.ProjectId
	ChangeId          change.ChangeId
	CanonicalRoot     string
	BaseRevision      string
	SourceStateDigest source.SourceStateDigest
}

// ExtractedPatch is machine-safe Git adapter output before domain artifact
// construction and ApprovedScope comparison.
type ExtractedPatch struct {
	Content      []byte
	ChangedPaths []string
}

// WorkspacePort is the M0.4 Sandbox Port for source/workspace isolation. It
// does not claim process, network, container, VM, or hostile-code isolation.
type WorkspacePort interface {
	Create(WorkspaceRequest) (ProposalWorkspace, error)
	Remove(ProposalWorkspace) error
}

// PatchPort extracts a Git-native patch and machine-safe changed path set.
type PatchPort interface {
	Extract(ProposalWorkspace) (ExtractedPatch, error)
}

// RepositoryInspector captures canonical source state through the existing
// M0.3 repository boundary.
type RepositoryInspector func(project.ProjectId, string) (source.SourceSnapshot, error)

// LifecycleRecorder appends one M0.4 proposal provenance event.
type LifecycleRecorder func(LifecycleEvent) error

// Clock supplies patch artifact timestamps.
type Clock func() time.Time

// LifecycleEvent carries typed proposal metadata to the audit adapter.
type LifecycleEvent struct {
	EventType   string
	Workspace   ProposalWorkspace
	Artifact    PatchArtifact
	HasArtifact bool
	Validation  source.SurfaceValidationResult
	Disposition string
	Reason      string
}

// WorkspaceMutation is the deliberately narrow, test-controlled M0.4 seam
// for proving that mutation targets an isolated workspace. It is not a command
// runner or provider execution contract.
type WorkspaceMutation func(ProposalWorkspace) error

// CanonicalSourceGuard compares every proposal stage with the approved M0.3
// SourceSnapshot and reports drift without attempting destructive repair.
type CanonicalSourceGuard struct {
	expected  source.SourceSnapshot
	inspector RepositoryInspector
}

// NewCanonicalSourceGuard establishes the clean canonical source invariant.
func NewCanonicalSourceGuard(
	expected source.SourceSnapshot,
	inspector RepositoryInspector,
) (*CanonicalSourceGuard, error) {
	if inspector == nil {
		return nil, fmt.Errorf("canonical source inspector dependency is not configured")
	}
	if expected.WorkingTreeState() != source.WorkingTreeClean {
		return nil, DirtyCanonicalSourceError{RepositoryRoot: expected.RepositoryRoot()}
	}
	return &CanonicalSourceGuard{expected: expected, inspector: inspector}, nil
}

// Verify proves the canonical source still matches the approved snapshot.
func (guard *CanonicalSourceGuard) Verify() error {
	if guard == nil {
		return fmt.Errorf("CanonicalSourceGuard is required")
	}
	actual, err := guard.inspector(guard.expected.ProjectId(), guard.expected.RepositoryRoot())
	if err != nil {
		return fmt.Errorf("inspect canonical source: %w", err)
	}
	if actual.ProjectId() != guard.expected.ProjectId() ||
		actual.RepositoryRoot() != guard.expected.RepositoryRoot() ||
		actual.HeadRevision() != guard.expected.HeadRevision() ||
		actual.WorkingTreeState() != guard.expected.WorkingTreeState() ||
		actual.SourceStateDigest() != guard.expected.SourceStateDigest() {
		return CanonicalSourceDriftError{
			RepositoryRoot: guard.expected.RepositoryRoot(),
			ExpectedHead:   guard.expected.HeadRevision(),
			ActualHead:     actual.HeadRevision(),
			ExpectedDigest: guard.expected.SourceStateDigest(),
			ActualDigest:   actual.SourceStateDigest(),
		}
	}
	return nil
}

// Service coordinates the process-local M0.4 workspace and patch lifecycle
// through explicit source/workspace, patch, audit, and inspection ports.
type Service struct {
	workspaces WorkspacePort
	patches    PatchPort
	inspector  RepositoryInspector
	recorder   LifecycleRecorder
	clock      Clock
}

// New constructs the M0.4 application service.
func New(
	workspaces WorkspacePort,
	patches PatchPort,
	inspector RepositoryInspector,
	recorder LifecycleRecorder,
	clock Clock,
) (*Service, error) {
	if workspaces == nil {
		return nil, fmt.Errorf("proposal workspace dependency is not configured")
	}
	if patches == nil {
		return nil, fmt.Errorf("patch extraction dependency is not configured")
	}
	if inspector == nil {
		return nil, fmt.Errorf("canonical source inspector dependency is not configured")
	}
	if recorder == nil {
		return nil, fmt.Errorf("proposal lifecycle recorder dependency is not configured")
	}
	if clock == nil {
		return nil, fmt.Errorf("proposal clock dependency is not configured")
	}
	return &Service{
		workspaces: workspaces,
		patches:    patches,
		inspector:  inspector,
		recorder:   recorder,
		clock:      clock,
	}, nil
}

// CreateWorkspace establishes a worktree from the exact clean source snapshot
// and proves the canonical source did not change during creation.
func (service *Service) CreateWorkspace(
	currentChange change.Change,
	canonicalSource source.SourceSnapshot,
	approvedScope source.ApprovedScope,
) (Proposal, error) {
	if currentChange.State() != change.StatePlanned {
		return Proposal{}, fmt.Errorf("Change %q must be planned before proposal workspace creation", currentChange.ChangeId())
	}
	if currentChange.ProjectId() != canonicalSource.ProjectId() ||
		currentChange.ProjectId() != approvedScope.ProjectId() ||
		currentChange.ChangeId() != approvedScope.ChangeId() {
		return Proposal{}, fmt.Errorf("proposal Change, Project, SourceSnapshot, and ApprovedScope linkage is inconsistent")
	}
	if canonicalSource.SourceStateDigest() != approvedScope.SourceStateDigest() {
		return Proposal{}, fmt.Errorf("proposal SourceSnapshot and ApprovedScope digest linkage is inconsistent")
	}

	guard, err := NewCanonicalSourceGuard(canonicalSource, service.inspector)
	if err != nil {
		return Proposal{}, err
	}
	if err := guard.Verify(); err != nil {
		return Proposal{}, err
	}

	workspace, err := service.workspaces.Create(WorkspaceRequest{
		ProjectId:         currentChange.ProjectId(),
		ChangeId:          currentChange.ChangeId(),
		CanonicalRoot:     canonicalSource.RepositoryRoot(),
		BaseRevision:      canonicalSource.HeadRevision(),
		SourceStateDigest: canonicalSource.SourceStateDigest(),
	})
	if err != nil {
		return Proposal{}, fmt.Errorf("create proposal workspace: %w", err)
	}
	cleanupOnFailure := func(primary error) error {
		cleanupError := service.workspaces.Remove(workspace)
		if cleanupError != nil {
			return errors.Join(primary, fmt.Errorf("clean failed proposal workspace: %w", cleanupError))
		}
		return primary
	}
	if err := guard.Verify(); err != nil {
		return Proposal{}, cleanupOnFailure(err)
	}

	currentProposal, err := newProposal(workspace, canonicalSource, approvedScope)
	if err != nil {
		return Proposal{}, cleanupOnFailure(err)
	}
	if err := service.recorder(LifecycleEvent{
		EventType:   EventProposalWorkspaceCreated,
		Workspace:   workspace,
		Disposition: string(WorkspaceActive),
	}); err != nil {
		return Proposal{}, cleanupOnFailure(fmt.Errorf("record proposal workspace creation: %w", err))
	}
	if err := guard.Verify(); err != nil {
		return Proposal{}, cleanupOnFailure(err)
	}
	return currentProposal, nil
}

// Mutate applies one controlled callback to the isolated workspace and checks
// canonical source before and after. The callback is not treated as trusted
// hostile-code containment; M0.4 proves only source/workspace isolation.
func (service *Service) Mutate(currentProposal Proposal, mutation WorkspaceMutation) error {
	if mutation == nil {
		return fmt.Errorf("proposal workspace mutation is required")
	}
	if currentProposal.workspace.State() != WorkspaceActive {
		return fmt.Errorf("proposal workspace %q is not active", currentProposal.workspace.WorkspaceId())
	}
	guard, err := NewCanonicalSourceGuard(currentProposal.canonicalSource, service.inspector)
	if err != nil {
		return err
	}
	if err := guard.Verify(); err != nil {
		return err
	}
	mutationError := mutation(currentProposal.workspace)
	guardError := guard.Verify()
	if mutationError != nil {
		return errors.Join(fmt.Errorf("mutate proposal workspace: %w", mutationError), guardError)
	}
	return guardError
}

// ExtractPatch extracts the actual isolated diff, validates its complete path
// set against M0.3 ApprovedScope, and classifies it as retained or rejected.
func (service *Service) ExtractPatch(
	currentProposal Proposal,
) (Proposal, source.SurfaceValidationResult, error) {
	if currentProposal.workspace.State() != WorkspaceActive {
		return currentProposal, source.SurfaceValidationResult{}, fmt.Errorf(
			"proposal workspace %q is not active",
			currentProposal.workspace.WorkspaceId(),
		)
	}
	guard, err := NewCanonicalSourceGuard(currentProposal.canonicalSource, service.inspector)
	if err != nil {
		return currentProposal, source.SurfaceValidationResult{}, err
	}
	if err := guard.Verify(); err != nil {
		return currentProposal, source.SurfaceValidationResult{}, err
	}

	extracted, err := service.patches.Extract(currentProposal.workspace)
	if err != nil {
		return currentProposal, source.SurfaceValidationResult{}, fmt.Errorf("extract proposal patch: %w", err)
	}
	if err := guard.Verify(); err != nil {
		return currentProposal, source.SurfaceValidationResult{}, err
	}

	artifact, err := newPatchArtifact(
		currentProposal.workspace,
		extracted.Content,
		extracted.ChangedPaths,
		service.clock(),
	)
	if err != nil {
		var emptyPatchError EmptyPatchError
		if !errors.As(err, &emptyPatchError) {
			return currentProposal, source.SurfaceValidationResult{}, err
		}
		rejectedWorkspace, transitionError := currentProposal.workspace.transition(WorkspaceRejected)
		if transitionError != nil {
			return currentProposal, source.SurfaceValidationResult{}, transitionError
		}
		rejectedProposal := currentProposal.withWorkspace(rejectedWorkspace)
		if recordError := service.recorder(LifecycleEvent{
			EventType:   EventPatchRejected,
			Workspace:   rejectedWorkspace,
			Disposition: string(WorkspaceRejected),
			Reason:      emptyPatchError.Error(),
		}); recordError != nil {
			return rejectedProposal, source.SurfaceValidationResult{}, fmt.Errorf("record empty patch rejection: %w", recordError)
		}
		if guardError := guard.Verify(); guardError != nil {
			return rejectedProposal, source.SurfaceValidationResult{}, guardError
		}
		return rejectedProposal, source.SurfaceValidationResult{}, emptyPatchError
	}

	withArtifact := currentProposal.withArtifact(artifact)
	if err := service.recorder(LifecycleEvent{
		EventType:   EventPatchExtracted,
		Workspace:   currentProposal.workspace,
		Artifact:    artifact,
		HasArtifact: true,
		Disposition: string(WorkspaceActive),
	}); err != nil {
		return currentProposal, source.SurfaceValidationResult{}, fmt.Errorf("record patch extraction: %w", err)
	}

	validation, validationError := currentProposal.approvedScope.ValidateActualSurface(artifact.ChangedPaths())
	if validationError != nil {
		var surfaceError *source.SurfaceValidationError
		if !errors.As(validationError, &surfaceError) {
			return withArtifact, validation, validationError
		}
		rejectedWorkspace, transitionError := currentProposal.workspace.transition(WorkspaceRejected)
		if transitionError != nil {
			return withArtifact, validation, transitionError
		}
		rejectedProposal := withArtifact.withWorkspace(rejectedWorkspace)
		if err := service.recorder(LifecycleEvent{
			EventType:   EventPatchRejected,
			Workspace:   rejectedWorkspace,
			Artifact:    artifact,
			HasArtifact: true,
			Validation:  validation,
			Disposition: string(WorkspaceRejected),
			Reason:      surfaceError.Error(),
		}); err != nil {
			return rejectedProposal, validation, fmt.Errorf("record patch rejection: %w", err)
		}
		if err := guard.Verify(); err != nil {
			return rejectedProposal, validation, err
		}
		return rejectedProposal, validation, surfaceError
	}

	retainedWorkspace, err := currentProposal.workspace.transition(WorkspaceRetained)
	if err != nil {
		return withArtifact, validation, err
	}
	retainedProposal := withArtifact.withWorkspace(retainedWorkspace)
	if err := service.recorder(LifecycleEvent{
		EventType:   EventPatchSurfaceValidated,
		Workspace:   retainedWorkspace,
		Artifact:    artifact,
		HasArtifact: true,
		Validation:  validation,
		Disposition: "surface-valid",
	}); err != nil {
		return retainedProposal, validation, fmt.Errorf("record patch surface validation: %w", err)
	}
	if err := guard.Verify(); err != nil {
		return retainedProposal, validation, err
	}
	return retainedProposal, validation, nil
}

// Discard removes only the adapter-owned worktree. It never repairs or
// rewrites canonical source, including when external source drift exists.
func (service *Service) Discard(
	currentProposal Proposal,
	reason string,
) (Proposal, error) {
	discardReason := strings.TrimSpace(reason)
	if discardReason == "" {
		return currentProposal, fmt.Errorf("proposal discard reason is required")
	}
	if currentProposal.workspace.State() == WorkspaceCleaned {
		return currentProposal, fmt.Errorf("proposal workspace %q is already cleaned", currentProposal.workspace.WorkspaceId())
	}

	guard, err := NewCanonicalSourceGuard(currentProposal.canonicalSource, service.inspector)
	if err != nil {
		return currentProposal, err
	}
	preCleanupGuardError := guard.Verify()
	if err := service.workspaces.Remove(currentProposal.workspace); err != nil {
		return currentProposal, errors.Join(preCleanupGuardError, fmt.Errorf("remove proposal workspace: %w", err))
	}
	cleanedWorkspace, err := currentProposal.workspace.transition(WorkspaceCleaned)
	if err != nil {
		return currentProposal, errors.Join(preCleanupGuardError, err)
	}
	cleanedProposal := currentProposal.withWorkspace(cleanedWorkspace)
	recordError := service.recorder(LifecycleEvent{
		EventType:   EventProposalWorkspaceDiscarded,
		Workspace:   cleanedWorkspace,
		Disposition: string(WorkspaceCleaned),
		Reason:      discardReason,
	})
	if recordError != nil {
		recordError = fmt.Errorf("record proposal workspace discard: %w", recordError)
	}
	postCleanupGuardError := guard.Verify()
	return cleanedProposal, errors.Join(preCleanupGuardError, recordError, postCleanupGuardError)
}
