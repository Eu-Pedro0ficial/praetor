package proposal

import (
	"bytes"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"github.com/Eu-Pedro0ficial/praetor/internal/project"
	"github.com/Eu-Pedro0ficial/praetor/internal/source"
)

const (
	EventProposalWorkspaceCreationStarted = "PROPOSAL_WORKSPACE_CREATION_STARTED"
	EventProposalWorkspaceCreated         = "PROPOSAL_WORKSPACE_CREATED"
	EventPatchExtracted                   = "PATCH_EXTRACTED"
	EventPatchSurfaceValidated            = "PATCH_SURFACE_VALIDATED"
	EventPatchRejected                    = "PATCH_REJECTED"
	EventProposalWorkspaceFailed          = "PROPOSAL_WORKSPACE_FAILED"
	EventProposalWorkspaceCleanupFailed   = "PROPOSAL_WORKSPACE_CLEANUP_FAILED"
	EventProposalWorkspaceDiscarded       = "PROPOSAL_WORKSPACE_DISCARDED"
)

// WorkspaceRequest is the application-owned request for Git worktree source
// isolation. ChangeId is linkage only and must not control filesystem paths.
type WorkspaceRequest struct {
	WorkspaceId       WorkspaceId
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

type WorkspaceReservationCondition string

const (
	WorkspaceReservationAbsent    WorkspaceReservationCondition = "ABSENT"
	WorkspaceReservationPresent   WorkspaceReservationCondition = "PRESENT"
	WorkspaceReservationAmbiguous WorkspaceReservationCondition = "AMBIGUOUS"
)

// WorkspaceReservationPort proves and compensates only the deterministic
// workspace path derived from a durable creation reservation.
type WorkspaceReservationPort interface {
	ReservedWorkspaceRoot(WorkspaceId) (string, error)
	VerifyReservedWorkspace(WorkspaceId, string, string) error
	ClassifyReservedWorkspace(WorkspaceId, string) (WorkspaceReservationCondition, error)
	RemoveReservedWorkspace(WorkspaceId, string) error
}

type WorkspacePersistence func(Proposal) error

type WorkspaceRecoveryResult struct {
	Condition WorkspaceReservationCondition
	Outcome   string
}

// WorkspaceReattacher is the optional restart capability implemented by a
// workspace adapter that can safely reclaim its own durable local workspace.
type WorkspaceReattacher interface {
	Reattach(ProposalWorkspace) error
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
	EventType          string
	Workspace          ProposalWorkspace
	Artifact           PatchArtifact
	HasArtifact        bool
	Validation         source.SurfaceValidationResult
	Disposition        string
	Reason             string
	ExecutionAttemptId string
	ApprovedScope      source.ApprovedScope
	HasApprovedScope   bool
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
	workspaces  WorkspacePort
	patches     PatchPort
	inspector   RepositoryInspector
	recorder    LifecycleRecorder
	clock       Clock
	coordinator *workspaceCoordinator
}

// Reattach re-establishes adapter ownership for a durable workspace after a
// process restart. It never creates, repairs, or relocates the workspace.
func (service *Service) Reattach(current Proposal) error {
	if service == nil || service.workspaces == nil {
		return fmt.Errorf("proposal workspace service is required")
	}
	reattacher, ok := service.workspaces.(WorkspaceReattacher)
	if !ok {
		return fmt.Errorf("proposal workspace adapter does not support durable reattachment")
	}
	return reattacher.Reattach(current.Workspace())
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

// NewDurable adds the reservation, exclusion, and explicit recovery boundary
// required around external ProposalWorkspace creation.
func NewDurable(
	workspaces WorkspacePort,
	patches PatchPort,
	inspector RepositoryInspector,
	recorder LifecycleRecorder,
	clock Clock,
	store workspaceAuthority,
	stateDirectory string,
	repositoryRoot string,
) (*Service, error) {
	service, err := New(workspaces, patches, inspector, recorder, clock)
	if err != nil {
		return nil, err
	}
	coordinator, err := newWorkspaceCoordinator(workspaces, store, stateDirectory, repositoryRoot)
	if err != nil {
		return nil, err
	}
	service.coordinator = coordinator
	return service, nil
}

// CreateWorkspace establishes a worktree from the exact clean source snapshot
// and proves the canonical source did not change during creation.
func (service *Service) CreateWorkspace(
	currentChange change.Change,
	canonicalSource source.SourceSnapshot,
	approvedScope source.ApprovedScope,
) (Proposal, error) {
	return service.createWorkspace(currentChange, canonicalSource, approvedScope, change.StatePlanned, nil)
}

func (service *Service) CreateWorkspacePersisted(
	currentChange change.Change,
	canonicalSource source.SourceSnapshot,
	approvedScope source.ApprovedScope,
	persist WorkspacePersistence,
) (Proposal, error) {
	return service.createWorkspace(currentChange, canonicalSource, approvedScope, change.StatePlanned, persist)
}

func (service *Service) CreateReplacementWorkspace(
	currentChange change.Change,
	canonicalSource source.SourceSnapshot,
	approvedScope source.ApprovedScope,
) (Proposal, error) {
	return service.createWorkspace(currentChange, canonicalSource, approvedScope, change.StateIsolated, nil)
}

func (service *Service) CreateReplacementWorkspacePersisted(
	currentChange change.Change,
	canonicalSource source.SourceSnapshot,
	approvedScope source.ApprovedScope,
	persist WorkspacePersistence,
) (Proposal, error) {
	return service.createWorkspace(currentChange, canonicalSource, approvedScope, change.StateIsolated, persist)
}

func (service *Service) createWorkspace(
	currentChange change.Change,
	canonicalSource source.SourceSnapshot,
	approvedScope source.ApprovedScope,
	requiredState change.ChangeState,
	persist WorkspacePersistence,
) (Proposal, error) {
	if currentChange.State() != requiredState {
		return Proposal{}, fmt.Errorf("Change %q must be %s before proposal workspace creation", currentChange.ChangeId(), requiredState)
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

	request := WorkspaceRequest{
		ProjectId:         currentChange.ProjectId(),
		ChangeId:          currentChange.ChangeId(),
		CanonicalRoot:     canonicalSource.RepositoryRoot(),
		BaseRevision:      canonicalSource.HeadRevision(),
		SourceStateDigest: canonicalSource.SourceStateDigest(),
	}
	if service.coordinator != nil && persist != nil {
		return service.coordinator.create(currentChange, request, func(workspace ProposalWorkspace) (Proposal, error) {
			return service.establishWorkspaceAuthority(workspace, canonicalSource, approvedScope, guard, persist)
		})
	}
	workspace, err := service.workspaces.Create(request)
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

	currentProposal, err := service.establishWorkspaceAuthority(workspace, canonicalSource, approvedScope, guard, persist)
	if err != nil {
		return Proposal{}, cleanupOnFailure(err)
	}
	return currentProposal, nil
}

func (service *Service) establishWorkspaceAuthority(
	workspace ProposalWorkspace,
	canonicalSource source.SourceSnapshot,
	approvedScope source.ApprovedScope,
	guard *CanonicalSourceGuard,
	persist WorkspacePersistence,
) (Proposal, error) {
	currentProposal, err := newProposal(workspace, canonicalSource, approvedScope)
	if err != nil {
		return Proposal{}, err
	}
	if err := service.recorder(LifecycleEvent{
		EventType:   EventProposalWorkspaceCreated,
		Workspace:   workspace,
		Disposition: string(WorkspaceActive),
	}); err != nil {
		return Proposal{}, fmt.Errorf("record proposal workspace creation: %w", err)
	}
	if err := guard.Verify(); err != nil {
		return Proposal{}, err
	}
	if persist != nil {
		if err := persist(currentProposal); err != nil {
			return Proposal{}, err
		}
	}
	return currentProposal, nil
}

// RecoverWorkspaceCreation explicitly reconciles one durable workspace-creation
// operation without replaying creation or trusting an ambiguous path.
func (service *Service) RecoverWorkspaceCreation(operationId string) (WorkspaceRecoveryResult, error) {
	if service == nil || service.coordinator == nil {
		return WorkspaceRecoveryResult{}, fmt.Errorf("durable proposal workspace recovery is not configured")
	}
	return service.coordinator.recover(operationId)
}

// FailWorkspace marks a workspace unsafe for another provider attempt while
// retaining it for explicit cleanup and forensic inspection.
func (service *Service) FailWorkspace(currentProposal Proposal, reason, executionAttemptId string) (Proposal, error) {
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return currentProposal, fmt.Errorf("proposal workspace failure reason is required")
	}
	if currentProposal.workspace.State() == WorkspaceFailed {
		return currentProposal, nil
	}
	failedWorkspace, err := currentProposal.workspace.transition(WorkspaceFailed)
	if err != nil {
		return currentProposal, err
	}
	failedProposal := currentProposal.withWorkspace(failedWorkspace)
	if err := service.recorder(LifecycleEvent{
		EventType:          EventProposalWorkspaceFailed,
		Workspace:          failedWorkspace,
		Disposition:        string(WorkspaceFailed),
		Reason:             reason,
		ExecutionAttemptId: executionAttemptId,
	}); err != nil {
		return failedProposal, fmt.Errorf("record proposal workspace failure: %w", err)
	}
	return failedProposal, nil
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

// InspectChangedPaths obtains Git-authoritative workspace paths without
// creating a PatchArtifact or changing proposal state. M0.5 uses this only to
// record bounded partial-mutation provenance after provider failure.
func (service *Service) InspectChangedPaths(currentProposal Proposal) ([]string, error) {
	if currentProposal.workspace.State() != WorkspaceActive {
		return nil, fmt.Errorf(
			"proposal workspace %q is not active",
			currentProposal.workspace.WorkspaceId(),
		)
	}
	guard, err := NewCanonicalSourceGuard(currentProposal.canonicalSource, service.inspector)
	if err != nil {
		return nil, err
	}
	if err := guard.Verify(); err != nil {
		return nil, err
	}
	extracted, err := service.patches.Extract(currentProposal.workspace)
	if err != nil {
		return nil, fmt.Errorf("inspect proposal changes: %w", err)
	}
	if err := guard.Verify(); err != nil {
		return nil, err
	}
	return append([]string(nil), extracted.ChangedPaths...), nil
}

// VerifyIntegrity proves a retained proposal still represents the exact
// surface-valid PatchArtifact and unchanged canonical source captured by M0.4.
// It does not create a new artifact or mutate proposal state.
func (service *Service) VerifyIntegrity(currentProposal Proposal) error {
	if currentProposal.workspace.State() != WorkspaceRetained {
		return fmt.Errorf(
			"proposal workspace %q must be retained for verification",
			currentProposal.workspace.WorkspaceId(),
		)
	}
	artifact, hasArtifact := currentProposal.PatchArtifact()
	if !hasArtifact {
		return fmt.Errorf("retained proposal has no PatchArtifact")
	}
	if artifact.WorkspaceId() != currentProposal.workspace.WorkspaceId() ||
		artifact.ProjectId() != currentProposal.workspace.ProjectId() ||
		artifact.ChangeId() != currentProposal.workspace.ChangeId() ||
		artifact.BaseRevision() != currentProposal.workspace.BaseRevision() ||
		artifact.SourceStateDigest() != currentProposal.workspace.SourceStateDigest() {
		return fmt.Errorf("retained PatchArtifact linkage is inconsistent")
	}

	guard, err := NewCanonicalSourceGuard(currentProposal.canonicalSource, service.inspector)
	if err != nil {
		return err
	}
	if err := guard.Verify(); err != nil {
		return err
	}
	extracted, err := service.patches.Extract(currentProposal.workspace)
	if err != nil {
		return fmt.Errorf("re-extract retained proposal patch: %w", err)
	}
	if !bytes.Equal(extracted.Content, artifact.Content()) ||
		!equalStrings(extracted.ChangedPaths, artifact.ChangedPaths()) {
		return fmt.Errorf("retained proposal changed after PatchArtifact creation")
	}
	validation, validationError := currentProposal.approvedScope.ValidateActualSurface(extracted.ChangedPaths)
	if validationError != nil || !validation.Allowed() {
		return fmt.Errorf("retained proposal no longer satisfies ApprovedScope: %w", validationError)
	}
	if err := guard.Verify(); err != nil {
		return err
	}
	return nil
}

func equalStrings(left []string, right []string) bool {
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

// ExtractPatch extracts the actual isolated diff, validates its complete path
// set against M0.3 ApprovedScope, and classifies it as retained or rejected.
func (service *Service) ExtractPatch(
	currentProposal Proposal,
) (Proposal, source.SurfaceValidationResult, error) {
	return service.extractPatch(currentProposal, "")
}

// ExtractPatchForExecution reuses the M0.4 extraction and classification
// lifecycle while linking its audit events to one provider execution attempt.
func (service *Service) ExtractPatchForExecution(
	currentProposal Proposal,
	executionAttemptId string,
) (Proposal, source.SurfaceValidationResult, error) {
	if strings.TrimSpace(executionAttemptId) == "" {
		return currentProposal, source.SurfaceValidationResult{}, fmt.Errorf("ExecutionAttemptId is required")
	}
	return service.extractPatch(currentProposal, executionAttemptId)
}

func (service *Service) extractPatch(
	currentProposal Proposal,
	executionAttemptId string,
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
			EventType:          EventPatchRejected,
			Workspace:          rejectedWorkspace,
			Disposition:        string(WorkspaceRejected),
			Reason:             emptyPatchError.Error(),
			ExecutionAttemptId: executionAttemptId,
			ApprovedScope:      currentProposal.approvedScope,
			HasApprovedScope:   true,
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
		EventType:          EventPatchExtracted,
		Workspace:          currentProposal.workspace,
		Artifact:           artifact,
		HasArtifact:        true,
		Disposition:        string(WorkspaceActive),
		ExecutionAttemptId: executionAttemptId,
		ApprovedScope:      currentProposal.approvedScope,
		HasApprovedScope:   true,
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
			EventType:          EventPatchRejected,
			Workspace:          rejectedWorkspace,
			Artifact:           artifact,
			HasArtifact:        true,
			Validation:         validation,
			Disposition:        string(WorkspaceRejected),
			Reason:             surfaceError.Error(),
			ExecutionAttemptId: executionAttemptId,
			ApprovedScope:      currentProposal.approvedScope,
			HasApprovedScope:   true,
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
		EventType:          EventPatchSurfaceValidated,
		Workspace:          retainedWorkspace,
		Artifact:           artifact,
		HasArtifact:        true,
		Validation:         validation,
		Disposition:        "surface-valid",
		ExecutionAttemptId: executionAttemptId,
		ApprovedScope:      currentProposal.approvedScope,
		HasApprovedScope:   true,
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
		failedWorkspace, transitionError := currentProposal.workspace.transition(WorkspaceCleanupFailed)
		if transitionError != nil {
			return currentProposal, errors.Join(preCleanupGuardError, fmt.Errorf("remove proposal workspace: %w", err), transitionError)
		}
		failedProposal := currentProposal.withWorkspace(failedWorkspace)
		recordError := service.recorder(LifecycleEvent{
			EventType:   EventProposalWorkspaceCleanupFailed,
			Workspace:   failedWorkspace,
			Disposition: string(WorkspaceCleanupFailed),
			Reason:      "proposal workspace removal failed",
		})
		return failedProposal, errors.Join(preCleanupGuardError, fmt.Errorf("remove proposal workspace: %w", err), recordError)
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

// CleanupClosed removes only the adapter-owned workspace after terminal
// closure or a truthfully reported canonical mutation. Unlike Discard, it does
// not require the original clean canonical snapshot because an M0.8
// application may intentionally have made the canonical working tree dirty.
func (service *Service) CleanupClosed(
	currentProposal Proposal,
	reason string,
) (Proposal, error) {
	cleanupReason := strings.TrimSpace(reason)
	if cleanupReason == "" {
		return currentProposal, fmt.Errorf("closed proposal cleanup reason is required")
	}
	if currentProposal.workspace.State() == WorkspaceCleaned {
		return currentProposal, fmt.Errorf("proposal workspace %q is already cleaned", currentProposal.workspace.WorkspaceId())
	}
	if currentProposal.workspace.State() != WorkspaceRetained &&
		currentProposal.workspace.State() != WorkspaceRejected &&
		currentProposal.workspace.State() != WorkspaceFailed &&
		currentProposal.workspace.State() != WorkspaceCleanupFailed {
		return currentProposal, fmt.Errorf(
			"proposal workspace %q must be retained or rejected for terminal cleanup",
			currentProposal.workspace.WorkspaceId(),
		)
	}
	if err := service.workspaces.Remove(currentProposal.workspace); err != nil {
		failedWorkspace, transitionError := currentProposal.workspace.transition(WorkspaceCleanupFailed)
		if transitionError != nil {
			return currentProposal, errors.Join(fmt.Errorf("remove closed proposal workspace: %w", err), transitionError)
		}
		failedProposal := currentProposal.withWorkspace(failedWorkspace)
		recordError := service.recorder(LifecycleEvent{
			EventType:   EventProposalWorkspaceCleanupFailed,
			Workspace:   failedWorkspace,
			Disposition: string(WorkspaceCleanupFailed),
			Reason:      "closed proposal workspace removal failed",
		})
		return failedProposal, errors.Join(fmt.Errorf("remove closed proposal workspace: %w", err), recordError)
	}
	cleanedWorkspace, err := currentProposal.workspace.transition(WorkspaceCleaned)
	if err != nil {
		return currentProposal, err
	}
	cleanedProposal := currentProposal.withWorkspace(cleanedWorkspace)
	if err := service.recorder(LifecycleEvent{
		EventType:   EventProposalWorkspaceDiscarded,
		Workspace:   cleanedWorkspace,
		Disposition: string(WorkspaceCleaned),
		Reason:      cleanupReason,
	}); err != nil {
		return cleanedProposal, fmt.Errorf("record closed proposal workspace cleanup: %w", err)
	}
	return cleanedProposal, nil
}
