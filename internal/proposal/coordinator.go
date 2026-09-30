package proposal

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Eu-Pedro0ficial/praetor/internal/artifact"
	"github.com/Eu-Pedro0ficial/praetor/internal/audit"
	"github.com/Eu-Pedro0ficial/praetor/internal/authority"
	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"github.com/Eu-Pedro0ficial/praetor/internal/project"
	"github.com/Eu-Pedro0ficial/praetor/internal/source"
)

const OperationProposalWorkspaceCreate = "proposal-workspace-create"

type workspaceAuthority interface {
	authority.OperationStore
	authority.AuditLedger
	authority.ChangeStore
	authority.ArtifactStore
}

type workspaceCoordinator struct {
	workspaces     WorkspacePort
	recovery       WorkspaceReservationPort
	store          workspaceAuthority
	stateDirectory string
	repositoryRoot string
}

type workspaceCreationResult struct {
	SchemaVersion     uint32      `json:"schema_version"`
	WorkspaceId       WorkspaceId `json:"workspace_id"`
	WorkspaceRoot     string      `json:"workspace_root"`
	BaseRevision      string      `json:"base_revision"`
	SourceStateDigest string      `json:"source_state_digest"`
}

func newWorkspaceCoordinator(workspaces WorkspacePort, store workspaceAuthority, stateDirectory, repositoryRoot string) (*workspaceCoordinator, error) {
	recovery, ok := workspaces.(WorkspaceReservationPort)
	if !ok {
		return nil, fmt.Errorf("proposal workspace adapter does not support durable creation recovery")
	}
	if store == nil {
		return nil, fmt.Errorf("durable workspace authority is required")
	}
	if strings.TrimSpace(stateDirectory) == "" {
		return nil, fmt.Errorf("Praetor state directory is required")
	}
	if strings.TrimSpace(repositoryRoot) == "" {
		return nil, fmt.Errorf("canonical repository root is required")
	}
	return &workspaceCoordinator{
		workspaces:     workspaces,
		recovery:       recovery,
		store:          store,
		stateDirectory: stateDirectory,
		repositoryRoot: repositoryRoot,
	}, nil
}

func (coordinator *workspaceCoordinator) create(
	currentChange change.Change,
	request WorkspaceRequest,
	persist func(ProposalWorkspace) (Proposal, error),
) (Proposal, error) {
	operationId, err := authority.GenerateOperationId()
	if err != nil {
		return Proposal{}, err
	}
	workspaceId, err := workspaceIdFromOperation(operationId)
	if err != nil {
		return Proposal{}, err
	}
	request.WorkspaceId = workspaceId
	requestDigest := workspaceCreationRequestDigest(request, currentChange.Revision())
	now := time.Now().UTC()
	operation := authority.Operation{
		Id:               operationId,
		ProjectId:        request.ProjectId,
		ChangeId:         request.ChangeId,
		Kind:             OperationProposalWorkspaceCreate,
		RequestDigest:    requestDigest,
		ExpectedRevision: currentChange.Revision(),
		State:            authority.OperationReserved,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if _, replayed, err := coordinator.store.ReserveOperation(operation); err != nil {
		return Proposal{}, fmt.Errorf("reserve proposal workspace creation: %w", err)
	} else if replayed {
		return Proposal{}, fmt.Errorf("%w: new workspace operation unexpectedly replayed", authority.ErrOperationConflict)
	}

	lock, err := acquireWorkspaceProjectLock(coordinator.stateDirectory, request.ProjectId)
	if err != nil {
		return Proposal{}, err
	}
	defer releaseWorkspaceProjectLock(lock)

	reserved, err := coordinator.store.GetOperation(operationId)
	if err != nil || reserved.State != authority.OperationReserved || reserved.RequestDigest != requestDigest {
		return Proposal{}, errors.Join(authority.ErrOperationConflict, err)
	}
	intentEvent, err := coordinator.auditEvent(EventProposalWorkspaceCreationStarted, request.ProjectId, request.ChangeId, map[string]any{
		"operation_id":        string(operationId),
		"workspace_id":        string(workspaceId),
		"base_revision":       request.BaseRevision,
		"source_state_digest": string(request.SourceStateDigest),
		"disposition":         string(authority.OperationReserved),
	})
	if err != nil {
		return Proposal{}, err
	}
	if err := coordinator.store.AppendAudit(intentEvent); err != nil {
		return Proposal{}, errors.Join(
			fmt.Errorf("record proposal workspace creation intent: %w", err),
			coordinator.failReserved(operation, "creation intent persistence failed", WorkspaceReservationAbsent),
		)
	}

	workspace, err := coordinator.workspaces.Create(request)
	if err != nil {
		return Proposal{}, errors.Join(
			fmt.Errorf("create proposal workspace: %w", err),
			coordinator.compensate(operation, workspaceId, "workspace creation failed"),
		)
	}
	currentProposal, persistError := persist(workspace)
	if persistError != nil {
		removeError := coordinator.workspaces.Remove(workspace)
		if removeError != nil {
			return Proposal{}, errors.Join(persistError, fmt.Errorf("clean failed proposal workspace: %w", removeError))
		}
		return Proposal{}, errors.Join(
			persistError,
			coordinator.failReserved(operation, "workspace authority persistence failed", WorkspaceReservationAbsent),
		)
	}
	encoded, err := encodeWorkspaceCreationResult(workspace)
	if err != nil {
		return currentProposal, err
	}
	if _, err := coordinator.store.CompleteOperation(operationId, requestDigest, encoded, nil); err != nil {
		return currentProposal, fmt.Errorf("complete proposal workspace creation operation: %w", err)
	}
	return currentProposal, nil
}

func (coordinator *workspaceCoordinator) compensate(
	operation authority.Operation,
	workspaceId WorkspaceId,
	reason string,
) error {
	condition, err := coordinator.recovery.ClassifyReservedWorkspace(workspaceId, coordinator.repositoryRoot)
	if err != nil {
		return err
	}
	switch condition {
	case WorkspaceReservationAbsent:
		return coordinator.failReserved(operation, reason, condition)
	case WorkspaceReservationPresent:
		if err := coordinator.recovery.RemoveReservedWorkspace(workspaceId, coordinator.repositoryRoot); err != nil {
			return err
		}
		after, err := coordinator.recovery.ClassifyReservedWorkspace(workspaceId, coordinator.repositoryRoot)
		if err != nil {
			return err
		}
		if after != WorkspaceReservationAbsent {
			return fmt.Errorf("proposal workspace compensation ended in %s", after)
		}
		return coordinator.failReserved(operation, reason, after)
	default:
		return fmt.Errorf("proposal workspace creation is AMBIGUOUS; explicit recovery required")
	}
}

func (coordinator *workspaceCoordinator) recover(operationIdText string) (WorkspaceRecoveryResult, error) {
	operationId := authority.OperationId(strings.TrimSpace(operationIdText))
	operation, err := coordinator.store.GetOperation(operationId)
	if err != nil {
		return WorkspaceRecoveryResult{}, err
	}
	if operation.Kind != OperationProposalWorkspaceCreate {
		return WorkspaceRecoveryResult{}, fmt.Errorf("operation %q is not a proposal workspace creation", operationId)
	}
	workspaceId, err := workspaceIdFromOperation(operationId)
	if err != nil {
		return WorkspaceRecoveryResult{}, err
	}
	lock, err := acquireWorkspaceProjectLock(coordinator.stateDirectory, operation.ProjectId)
	if err != nil {
		return WorkspaceRecoveryResult{}, err
	}
	defer releaseWorkspaceProjectLock(lock)

	operation, err = coordinator.store.GetOperation(operationId)
	if err != nil {
		return WorkspaceRecoveryResult{}, err
	}
	if operation.State == authority.OperationCompleted || operation.State == authority.OperationFailed {
		return WorkspaceRecoveryResult{Condition: WorkspaceReservationAbsent, Outcome: "operation already terminal"}, nil
	}
	exactFoundation, contradictoryFoundation, foundation, err := coordinator.findWorkspaceFoundation(operation, workspaceId)
	if err != nil {
		return WorkspaceRecoveryResult{}, err
	}
	if contradictoryFoundation {
		return WorkspaceRecoveryResult{}, fmt.Errorf("durable workspace foundation contradicts creation reservation")
	}
	condition, err := coordinator.recovery.ClassifyReservedWorkspace(workspaceId, coordinator.repositoryRoot)
	if err != nil {
		return WorkspaceRecoveryResult{}, err
	}
	if condition == WorkspaceReservationAmbiguous {
		return WorkspaceRecoveryResult{}, fmt.Errorf("proposal workspace ownership is AMBIGUOUS; external state was preserved")
	}
	if exactFoundation {
		if condition != WorkspaceReservationPresent {
			return WorkspaceRecoveryResult{}, fmt.Errorf("durable workspace authority exists but external worktree is absent")
		}
		if err := coordinator.recovery.VerifyReservedWorkspace(workspaceId, coordinator.repositoryRoot, foundation.BaseRevision); err != nil {
			return WorkspaceRecoveryResult{}, fmt.Errorf("verify durable workspace external state: %w", err)
		}
		encoded, err := json.Marshal(foundation)
		if err != nil {
			return WorkspaceRecoveryResult{}, err
		}
		event, err := coordinator.recoveryEvent(operation, workspaceId, condition, "durable workspace authority verified; creation operation finalized")
		if err != nil {
			return WorkspaceRecoveryResult{}, err
		}
		if _, err := coordinator.store.CompleteOperation(operation.Id, operation.RequestDigest, encoded, []audit.Event{event}); err != nil {
			return WorkspaceRecoveryResult{}, err
		}
		return WorkspaceRecoveryResult{Condition: condition, Outcome: "durable workspace preserved and operation finalized"}, nil
	}
	if condition == WorkspaceReservationPresent {
		if err := coordinator.recovery.RemoveReservedWorkspace(workspaceId, coordinator.repositoryRoot); err != nil {
			return WorkspaceRecoveryResult{}, err
		}
		condition, err = coordinator.recovery.ClassifyReservedWorkspace(workspaceId, coordinator.repositoryRoot)
		if err != nil {
			return WorkspaceRecoveryResult{}, err
		}
		if condition != WorkspaceReservationAbsent {
			return WorkspaceRecoveryResult{}, fmt.Errorf("workspace cleanup could not prove ABSENT")
		}
	}
	if err := coordinator.failReserved(operation, "orphan workspace creation reconciled", condition); err != nil {
		return WorkspaceRecoveryResult{}, err
	}
	return WorkspaceRecoveryResult{Condition: condition, Outcome: "workspace cleanup proven and operation failed terminally"}, nil
}

func (coordinator *workspaceCoordinator) inspect(operationIdText string) (WorkspaceCreationInspection, error) {
	operationId := authority.OperationId(strings.TrimSpace(operationIdText))
	operation, err := coordinator.store.GetOperation(operationId)
	if err != nil {
		return WorkspaceCreationInspection{}, err
	}
	if operation.Kind != OperationProposalWorkspaceCreate {
		return WorkspaceCreationInspection{}, fmt.Errorf("operation %q is not a proposal workspace creation", operationId)
	}
	workspaceId, err := workspaceIdFromOperation(operationId)
	if err != nil {
		return WorkspaceCreationInspection{}, err
	}
	var exactFoundation, contradictoryFoundation bool
	var foundation workspaceCreationResult
	if operation.State == authority.OperationReserved {
		exactFoundation, contradictoryFoundation, foundation, err = coordinator.findWorkspaceFoundation(operation, workspaceId)
	} else {
		exactFoundation, contradictoryFoundation, foundation, err = coordinator.inspectWorkspaceFoundation(operation, workspaceId)
	}
	if err != nil {
		return WorkspaceCreationInspection{}, err
	}
	result := WorkspaceCreationInspection{WorkspaceId: workspaceId, Authority: WorkspaceAuthorityMissing}
	switch {
	case contradictoryFoundation:
		result.Authority = WorkspaceAuthorityContradictory
	case exactFoundation:
		result.Authority = WorkspaceAuthorityExact
	}
	result.ExternalCondition, err = coordinator.recovery.ClassifyReservedWorkspace(workspaceId, coordinator.repositoryRoot)
	if err != nil {
		return WorkspaceCreationInspection{}, err
	}
	if exactFoundation && result.ExternalCondition == WorkspaceReservationPresent {
		if err := coordinator.recovery.VerifyReservedWorkspace(workspaceId, coordinator.repositoryRoot, foundation.BaseRevision); err == nil {
			result.BaseVerified = true
		} else {
			result.ExternalCondition = WorkspaceReservationAmbiguous
		}
	}
	return result, nil
}

func (coordinator *workspaceCoordinator) findWorkspaceFoundation(
	operation authority.Operation,
	workspaceId WorkspaceId,
) (bool, bool, workspaceCreationResult, error) {
	return coordinator.workspaceFoundation(operation, workspaceId, true)
}

func (coordinator *workspaceCoordinator) inspectWorkspaceFoundation(
	operation authority.Operation,
	workspaceId WorkspaceId,
) (bool, bool, workspaceCreationResult, error) {
	return coordinator.workspaceFoundation(operation, workspaceId, false)
}

func (coordinator *workspaceCoordinator) workspaceFoundation(
	operation authority.Operation,
	workspaceId WorkspaceId,
	requireIsolated bool,
) (bool, bool, workspaceCreationResult, error) {
	current, _, err := coordinator.store.GetChange(operation.ChangeId)
	if err != nil {
		return false, false, workspaceCreationResult{}, err
	}
	bindings, err := coordinator.store.ListBindings(operation.ChangeId)
	if err != nil {
		return false, false, workspaceCreationResult{}, err
	}
	for _, binding := range bindings {
		if binding.Role != "source-snapshot" && binding.Role != "source-snapshot:"+string(workspaceId) {
			continue
		}
		item, err := coordinator.store.GetArtifact(operation.ChangeId, binding.ArtifactId, true)
		if err != nil {
			return false, false, workspaceCreationResult{}, err
		}
		if item.Kind() != artifact.KindSourceSnapshot {
			continue
		}
		payload, err := artifact.DecodeSourceSnapshotPayload(item.Payload())
		if err != nil {
			return false, true, workspaceCreationResult{}, nil
		}
		if payload.WorkspaceId != string(workspaceId) {
			continue
		}
		expectedRoot, rootError := coordinator.recovery.ReservedWorkspaceRoot(workspaceId)
		if rootError != nil {
			return false, false, workspaceCreationResult{}, rootError
		}
		result := workspaceCreationResult{
			SchemaVersion:     1,
			WorkspaceId:       workspaceId,
			WorkspaceRoot:     payload.WorkspaceRoot,
			BaseRevision:      payload.HeadRevision,
			SourceStateDigest: payload.SourceStateDigest,
		}
		request := WorkspaceRequest{
			WorkspaceId:       workspaceId,
			ProjectId:         operation.ProjectId,
			ChangeId:          operation.ChangeId,
			CanonicalRoot:     payload.RepositoryRoot,
			BaseRevision:      payload.HeadRevision,
			SourceStateDigest: sourceDigest(payload.SourceStateDigest),
		}
		exact := (!requireIsolated || current.State() == change.StateIsolated) &&
			payload.RepositoryRoot == coordinator.repositoryRoot &&
			payload.WorkspaceRoot == expectedRoot &&
			workspaceCreationRequestDigest(request, operation.ExpectedRevision) == operation.RequestDigest
		return exact, !exact, result, nil
	}
	return false, false, workspaceCreationResult{}, nil
}

func sourceDigest(value string) source.SourceStateDigest {
	return source.SourceStateDigest(value)
}

func (coordinator *workspaceCoordinator) failReserved(
	operation authority.Operation,
	reason string,
	condition WorkspaceReservationCondition,
) error {
	workspaceId, err := workspaceIdFromOperation(operation.Id)
	if err != nil {
		return err
	}
	event, err := coordinator.recoveryEvent(operation, workspaceId, condition, reason)
	if err != nil {
		return err
	}
	result, err := json.Marshal(map[string]any{
		"schema_version": 1,
		"outcome":        "compensated",
		"workspace_id":   workspaceId,
		"condition":      condition,
		"reason":         reason,
	})
	if err != nil {
		return err
	}
	_, err = coordinator.store.FailOperation(operation.Id, operation.RequestDigest, result, []audit.Event{event})
	return err
}

func (coordinator *workspaceCoordinator) recoveryEvent(
	operation authority.Operation,
	workspaceId WorkspaceId,
	condition WorkspaceReservationCondition,
	outcome string,
) (audit.Event, error) {
	return coordinator.auditEvent(audit.EventOperationRecovered, operation.ProjectId, operation.ChangeId, map[string]any{
		"operation_id":   string(operation.Id),
		"operation_kind": operation.Kind,
		"workspace_id":   string(workspaceId),
		"condition":      string(condition),
		"outcome":        outcome,
	})
}

func (coordinator *workspaceCoordinator) auditEvent(eventType string, projectId project.ProjectId, changeId change.ChangeId, metadata map[string]any) (audit.Event, error) {
	return audit.NewEvent(
		eventType,
		string(projectId),
		string(changeId),
		coordinator.repositoryRoot,
		metadata,
		time.Now().UTC(),
	)
}

func workspaceCreationRequestDigest(request WorkspaceRequest, revision uint64) string {
	values := []string{
		"praetor-proposal-workspace-create-v1",
		string(request.WorkspaceId),
		string(request.ProjectId),
		string(request.ChangeId),
		request.CanonicalRoot,
		request.BaseRevision,
		string(request.SourceStateDigest),
		fmt.Sprintf("%d", revision),
	}
	sum := sha256.Sum256([]byte(strings.Join(values, "\x1f")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func workspaceIdFromOperation(operationId authority.OperationId) (WorkspaceId, error) {
	value := string(operationId)
	if !strings.HasPrefix(value, "op-") {
		return "", fmt.Errorf("invalid workspace creation OperationId %q", operationId)
	}
	workspaceId := WorkspaceId("proposal-" + strings.TrimPrefix(value, "op-"))
	if err := validateWorkspaceId(workspaceId); err != nil {
		return "", err
	}
	return workspaceId, nil
}

func encodeWorkspaceCreationResult(workspace ProposalWorkspace) ([]byte, error) {
	return json.Marshal(workspaceCreationResult{
		SchemaVersion:     1,
		WorkspaceId:       workspace.WorkspaceId(),
		WorkspaceRoot:     workspace.Root(),
		BaseRevision:      workspace.BaseRevision(),
		SourceStateDigest: string(workspace.SourceStateDigest()),
	})
}

func acquireWorkspaceProjectLock(stateDirectory string, projectId project.ProjectId) (*os.File, error) {
	base, err := filepath.Abs(stateDirectory)
	if err != nil {
		return nil, err
	}
	projects := filepath.Join(base, "projects")
	projectDirectory := filepath.Join(projects, string(projectId))
	for _, path := range []string{base, projects, projectDirectory} {
		if info, err := os.Lstat(path); err == nil {
			if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
				return nil, fmt.Errorf("workspace lock directory is unsafe")
			}
		} else if os.IsNotExist(err) {
			if err := os.MkdirAll(path, 0o700); err != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
		if err := os.Chmod(path, 0o700); err != nil {
			return nil, err
		}
	}
	path := filepath.Join(projectDirectory, "proposal-workspace.lock")
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("workspace lock path is not a regular file")
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if info, err := file.Stat(); err != nil || !info.Mode().IsRegular() {
		file.Close()
		return nil, fmt.Errorf("workspace lock path changed to an unsafe file type")
	}
	if err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX); err != nil {
		file.Close()
		return nil, err
	}
	return file, nil
}

func releaseWorkspaceProjectLock(file *os.File) {
	if file == nil {
		return
	}
	_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	_ = file.Close()
}
