package inspection

import (
	"errors"
	"sort"

	"github.com/Eu-Pedro0ficial/praetor/internal/audit"
	"github.com/Eu-Pedro0ficial/praetor/internal/authority"
	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"github.com/Eu-Pedro0ficial/praetor/internal/integration"
	"github.com/Eu-Pedro0ficial/praetor/internal/proposal"
)

type RecoveryClassification string

type RecoveryActionability string

type RecoveryActionKind string

const (
	RecoverySafe        RecoveryClassification = "SAFE"
	RecoveryRecoverable RecoveryClassification = "RECOVERABLE"
	RecoveryAmbiguous   RecoveryClassification = "AMBIGUOUS"
	RecoveryCorrupt     RecoveryClassification = "CORRUPT"

	ActionInformational       RecoveryActionability = "informational"
	ActionSafeRetry           RecoveryActionability = "safe-retry"
	ActionOperationRecovery   RecoveryActionability = "explicit-operation-recovery"
	ActionWorkspaceDiscard    RecoveryActionability = "explicit-workspace-discard"
	ActionManualInvestigation RecoveryActionability = "manual-investigation-required"

	RecoveryActionShow      RecoveryActionKind = "show"
	RecoveryActionHistory   RecoveryActionKind = "history"
	RecoveryActionArtifacts RecoveryActionKind = "artifacts"
	RecoveryActionIsolate   RecoveryActionKind = "isolate"
	RecoveryActionImplement RecoveryActionKind = "implement"
	RecoveryActionVerify    RecoveryActionKind = "verify"
	RecoveryActionApprove   RecoveryActionKind = "approve"
	RecoveryActionReject    RecoveryActionKind = "reject"
	RecoveryActionApply     RecoveryActionKind = "apply"
	RecoveryActionClose     RecoveryActionKind = "close"
	RecoveryActionDiscard   RecoveryActionKind = "discard"
	RecoveryActionRecover   RecoveryActionKind = "recover"
)

type RecoveryAction struct {
	Kind        RecoveryActionKind
	ChangeId    change.ChangeId
	OperationId authority.OperationId
}

// RecoveryReport is a read-only projection. It explains existing authority;
// it neither persists derived state nor authorizes a mutation.
type RecoveryReport struct {
	ChangeId           change.ChangeId
	ChangeState        change.ChangeState
	Classification     RecoveryClassification
	Actionability      RecoveryActionability
	Reason             string
	OperationId        authority.OperationId
	OperationKind      string
	OperationState     authority.OperationState
	WorkspaceId        proposal.WorkspaceId
	WorkspaceAuthority proposal.WorkspaceAuthorityCondition
	ExternalState      string
	ProviderAttemptId  string
	ProviderOutcome    string
	AutomaticRetry     bool
	WorkspaceReuse     bool
	DiscardDurable     bool
	NextActions        []RecoveryAction
}

type workspaceCreationInspector interface {
	InspectWorkspaceCreation(string) (proposal.WorkspaceCreationInspection, error)
}

type canonicalRecoveryInspector interface {
	InspectPersisted(authority.OperationId) (integration.ExternalCondition, error)
}

// NewWithRecoveryInspection adds read-only external classification to the
// ordinary durable inspection service.
func NewWithRecoveryInspection(store authority.Store, workspace workspaceCreationInspector, canonical canonicalRecoveryInspector) (*Service, error) {
	service, err := New(store)
	if err != nil {
		return nil, err
	}
	service.workspaceInspector = workspace
	service.canonicalInspector = canonical
	return service, nil
}

func (s *Service) Recovery(id change.ChangeId) (RecoveryReport, error) {
	current, _, err := s.store.GetChange(id)
	if err != nil {
		return RecoveryReport{}, err
	}
	events, err := s.store.AuditHistory(id)
	if err != nil {
		return RecoveryReport{}, err
	}
	operations, err := s.store.ListOperations(id)
	if err != nil {
		return RecoveryReport{}, err
	}
	report := deriveRecovery(current, events, operations)
	return s.addExternalEvidence(report, operations)
}

func (s *Service) RecoveryForOperation(id authority.OperationId) (RecoveryReport, error) {
	operation, err := s.store.GetOperation(id)
	if err != nil {
		return RecoveryReport{}, err
	}
	current, _, err := s.store.GetChange(operation.ChangeId)
	if err != nil {
		return RecoveryReport{}, err
	}
	events, err := s.store.AuditHistory(operation.ChangeId)
	if err != nil {
		return RecoveryReport{}, err
	}
	operations, err := s.store.ListOperations(operation.ChangeId)
	if err != nil {
		return RecoveryReport{}, err
	}
	report := deriveRecovery(current, events, operations)
	// An explicitly addressed operation takes precedence over another one in
	// the Change-level summary and is inspected exactly once.
	report.OperationId = operation.Id
	report.OperationKind = operation.Kind
	report.OperationState = operation.State
	return s.addOperationEvidence(report, operation)
}

func (s *Service) addExternalEvidence(report RecoveryReport, operations []authority.Operation) (RecoveryReport, error) {
	if report.OperationId == "" {
		return report, nil
	}
	for _, operation := range operations {
		if operation.Id == report.OperationId {
			return s.addOperationEvidence(report, operation)
		}
	}
	return report, nil
}

func (s *Service) addOperationEvidence(report RecoveryReport, operation authority.Operation) (RecoveryReport, error) {
	switch operation.Kind {
	case proposal.OperationProposalWorkspaceCreate:
		if s.workspaceInspector == nil {
			return report, nil
		}
		evidence, err := s.workspaceInspector.InspectWorkspaceCreation(string(operation.Id))
		if err != nil {
			if errors.Is(err, authority.ErrCorrupt) {
				return corruptReport(report, "durable workspace authority is corrupt or contradictory"), nil
			}
			return report, err
		}
		report.WorkspaceId = evidence.WorkspaceId
		report.WorkspaceAuthority = evidence.Authority
		report.ExternalState = string(evidence.ExternalCondition)
		return classifyWorkspaceOperation(report, operation, evidence), nil
	case "canonical-git-apply":
		if s.canonicalInspector == nil {
			return report, nil
		}
		condition, err := s.canonicalInspector.InspectPersisted(operation.Id)
		report.ExternalState = string(condition)
		if err != nil {
			if errors.Is(err, authority.ErrCorrupt) || errors.Is(err, authority.ErrOperationConflict) {
				return corruptReport(report, "durable canonical authority contradicts exact Git evidence"), nil
			}
			return report, err
		}
		return classifyCanonicalOperation(report, operation, condition), nil
	default:
		return report, nil
	}
}

func deriveRecovery(current change.Change, events []audit.Event, operations []authority.Operation) RecoveryReport {
	report := healthyRecovery(current)
	providerTerminalDurable := false
	sorted := append([]authority.Operation(nil), operations...)
	sort.SliceStable(sorted, func(i, j int) bool { return sorted[i].UpdatedAt.Before(sorted[j].UpdatedAt) })
	for _, operation := range sorted {
		if operation.Kind == proposal.OperationProposalWorkspaceCreate || operation.Kind == "canonical-git-apply" {
			report.OperationId = operation.Id
			report.OperationKind = operation.Kind
			report.OperationState = operation.State
		}
	}
	for _, event := range events {
		workspaceId := metadataString(event.Metadata, "workspace_id")
		attemptId := metadataString(event.Metadata, "execution_attempt_id")
		switch event.EventType {
		case audit.EventProposalWorkspaceCreated:
			report.DiscardDurable = false
			report.WorkspaceReuse = true
			if workspaceId != "" {
				report.WorkspaceId = proposal.WorkspaceId(workspaceId)
			}
		case audit.EventProviderExecutionStarted:
			report.ProviderAttemptId = attemptId
			providerTerminalDurable = false
			report.ProviderOutcome = "started-without-terminal-outcome"
			report.Classification = RecoveryAmbiguous
			report.Actionability = ActionWorkspaceDiscard
			report.Reason = "provider execution may have occurred and the workspace may contain partial mutations; Git diff alone cannot prove provider completion"
			report.AutomaticRetry = false
			report.WorkspaceReuse = false
			report.NextActions = investigationAndDiscard(current.ChangeId())
		case audit.EventProviderExecutionCompleted:
			if attemptId == "" || report.ProviderAttemptId == "" || attemptId == report.ProviderAttemptId {
				providerTerminalDurable = true
				report.ProviderOutcome = "completed"
				report.Classification = RecoveryRecoverable
				report.Actionability = ActionWorkspaceDiscard
				report.Reason = "provider completion is durable but no accepted patch outcome is durable"
				report.AutomaticRetry = false
				report.WorkspaceReuse = false
				report.NextActions = investigationAndDiscard(current.ChangeId())
			}
		case audit.EventProviderExecutionFailed:
			if attemptId == "" || report.ProviderAttemptId == "" || attemptId == report.ProviderAttemptId {
				providerTerminalDurable = true
				report.ProviderOutcome = "failed"
				report.Classification = RecoveryRecoverable
				report.Actionability = ActionSafeRetry
				report.Reason = "the provider attempt failed and its workspace cannot be reused for a clean attempt"
				report.AutomaticRetry = false
				report.WorkspaceReuse = false
				report.NextActions = []RecoveryAction{{Kind: RecoveryActionImplement, ChangeId: current.ChangeId()}, {Kind: RecoveryActionDiscard, ChangeId: current.ChangeId()}}
			}
		case audit.EventProposalWorkspaceFailed:
			if report.ProviderOutcome != "started-without-terminal-outcome" {
				report.Classification = RecoveryRecoverable
				report.Actionability = ActionSafeRetry
				report.Reason = "the proposal workspace failed and cannot be reused for a clean attempt"
				report.AutomaticRetry = false
				report.WorkspaceReuse = false
				report.NextActions = []RecoveryAction{{Kind: RecoveryActionImplement, ChangeId: current.ChangeId()}, {Kind: RecoveryActionDiscard, ChangeId: current.ChangeId()}}
			}
		case audit.EventPatchRejected:
			if providerTerminalDurable {
				report.ProviderOutcome = "completed-patch-rejected"
				report.Classification = RecoveryRecoverable
				report.Actionability = ActionSafeRetry
				report.Reason = "the provider completed but no valid patch was accepted; a retry requires a fresh workspace"
				report.AutomaticRetry = false
				report.WorkspaceReuse = false
				report.NextActions = []RecoveryAction{{Kind: RecoveryActionImplement, ChangeId: current.ChangeId()}, {Kind: RecoveryActionDiscard, ChangeId: current.ChangeId()}}
			}
		case audit.EventPatchSurfaceValidated:
			if providerTerminalDurable {
				report.ProviderOutcome = "completed-patch-validated"
				report.Classification = RecoverySafe
				report.Actionability = ActionInformational
				report.Reason = "the provider outcome and accepted patch surface are durable"
				report.AutomaticRetry = false
				report.WorkspaceReuse = true
				report.NextActions = []RecoveryAction{{Kind: RecoveryActionVerify, ChangeId: current.ChangeId()}, {Kind: RecoveryActionDiscard, ChangeId: current.ChangeId()}}
			}
		case audit.EventProposalWorkspaceCleanupFailed:
			report.Classification = RecoveryAmbiguous
			report.Actionability = ActionManualInvestigation
			report.Reason = "workspace cleanup did not establish a durable successful outcome"
			report.AutomaticRetry = false
			report.WorkspaceReuse = false
			report.NextActions = investigationActions(current.ChangeId())
		case audit.EventProposalWorkspaceDiscarded:
			report.DiscardDurable = true
			report.Classification = RecoverySafe
			report.Actionability = ActionSafeRetry
			report.Reason = "workspace discard is durably recorded"
			report.AutomaticRetry = false
			report.WorkspaceReuse = false
			report.NextActions = []RecoveryAction{{Kind: RecoveryActionImplement, ChangeId: current.ChangeId()}}
		}
	}
	if report.ProviderAttemptId != "" && !providerTerminalDurable && !report.DiscardDurable {
		report.ProviderOutcome = "started-without-terminal-outcome"
		report.Classification = RecoveryAmbiguous
		report.Actionability = ActionWorkspaceDiscard
		report.Reason = "provider execution may have occurred and the workspace may contain partial mutations; Git diff alone cannot prove provider completion"
		report.AutomaticRetry = false
		report.WorkspaceReuse = false
		report.NextActions = investigationAndDiscard(current.ChangeId())
	}
	if current.State() != change.StateIsolated && (report.ProviderAttemptId == "" || providerTerminalDurable || report.DiscardDurable) {
		healthy := healthyRecovery(current)
		healthy.OperationId = report.OperationId
		healthy.OperationKind = report.OperationKind
		healthy.OperationState = report.OperationState
		healthy.WorkspaceId = report.WorkspaceId
		healthy.ProviderAttemptId = report.ProviderAttemptId
		healthy.ProviderOutcome = report.ProviderOutcome
		healthy.DiscardDurable = report.DiscardDurable
		report = healthy
	}
	for _, operation := range sorted {
		if operation.State == authority.OperationReserved {
			report.OperationId = operation.Id
			report.OperationKind = operation.Kind
			report.OperationState = operation.State
			report.Classification = RecoveryRecoverable
			report.Actionability = ActionOperationRecovery
			report.Reason = "a durable operation was reserved without a terminal outcome"
			report.AutomaticRetry = false
			report.WorkspaceReuse = false
			report.NextActions = []RecoveryAction{{Kind: RecoveryActionRecover, ChangeId: current.ChangeId(), OperationId: operation.Id}}
		}
	}
	return report
}

func healthyRecovery(current change.Change) RecoveryReport {
	report := RecoveryReport{ChangeId: current.ChangeId(), ChangeState: current.State(), Classification: RecoverySafe, Actionability: ActionInformational, Reason: "no incomplete or contradictory recovery authority was found", AutomaticRetry: false, WorkspaceReuse: false}
	switch current.State() {
	case change.StateCreated, change.StatePlanned:
		report.NextActions = []RecoveryAction{{Kind: RecoveryActionIsolate, ChangeId: current.ChangeId()}}
	case change.StateIsolated:
		report.NextActions = []RecoveryAction{{Kind: RecoveryActionImplement, ChangeId: current.ChangeId()}}
	case change.StateValidated:
		report.NextActions = []RecoveryAction{{Kind: RecoveryActionApprove, ChangeId: current.ChangeId()}, {Kind: RecoveryActionReject, ChangeId: current.ChangeId()}}
	case change.StateApproved:
		report.NextActions = []RecoveryAction{{Kind: RecoveryActionApply, ChangeId: current.ChangeId()}}
	case change.StateRejected:
		report.NextActions = []RecoveryAction{{Kind: RecoveryActionClose, ChangeId: current.ChangeId()}}
	case change.StateAuditLocked:
		report.NextActions = investigationActions(current.ChangeId())
	}
	return report
}

func classifyWorkspaceOperation(report RecoveryReport, operation authority.Operation, evidence proposal.WorkspaceCreationInspection) RecoveryReport {
	if evidence.Authority == proposal.WorkspaceAuthorityContradictory {
		return corruptReport(report, "durable workspace foundation contradicts the creation reservation")
	}
	if evidence.ExternalCondition == proposal.WorkspaceReservationAmbiguous {
		return ambiguousReport(report, "workspace ownership or base revision cannot be proven; external state was preserved")
	}
	if operation.State == authority.OperationReserved {
		report.Classification = RecoveryRecoverable
		report.Actionability = ActionOperationRecovery
		report.AutomaticRetry = false
		report.WorkspaceReuse = false
		report.NextActions = []RecoveryAction{{Kind: RecoveryActionRecover, ChangeId: report.ChangeId, OperationId: operation.Id}}
		switch {
		case evidence.Authority == proposal.WorkspaceAuthorityExact && evidence.ExternalCondition == proposal.WorkspaceReservationPresent && evidence.BaseVerified:
			report.Reason = "workspace foundation and exact owned worktree are present; explicit recovery can finalize creation"
		case evidence.Authority == proposal.WorkspaceAuthorityMissing && evidence.ExternalCondition == proposal.WorkspaceReservationPresent:
			report.Reason = "an exact operation-derived worktree exists without durable workspace foundation; explicit recovery can remove it safely"
		case evidence.Authority == proposal.WorkspaceAuthorityMissing && evidence.ExternalCondition == proposal.WorkspaceReservationAbsent:
			report.Reason = "workspace creation was reserved but produced no durable foundation or external worktree; explicit recovery can fail it terminally"
		case evidence.Authority == proposal.WorkspaceAuthorityExact && evidence.ExternalCondition == proposal.WorkspaceReservationAbsent:
			return ambiguousReport(report, "durable workspace authority exists but the external worktree is absent; historical cleanup intent cannot be inferred")
		}
		return report
	}
	if operation.State == authority.OperationCompleted && evidence.Authority == proposal.WorkspaceAuthorityMissing {
		return corruptReport(report, "completed workspace creation has no durable workspace foundation")
	}
	if operation.State == authority.OperationFailed && evidence.ExternalCondition == proposal.WorkspaceReservationPresent {
		return ambiguousReport(report, "a terminal failed workspace operation still has external state; automatic cleanup is not authorized")
	}
	if evidence.Authority == proposal.WorkspaceAuthorityExact && evidence.ExternalCondition == proposal.WorkspaceReservationAbsent && !report.DiscardDurable {
		return ambiguousReport(report, "durable workspace authority remains but the external worktree is absent without durable discard evidence")
	}
	return report
}

func classifyCanonicalOperation(report RecoveryReport, operation authority.Operation, condition integration.ExternalCondition) RecoveryReport {
	if operation.State == authority.OperationCompleted {
		if report.ChangeState != change.StateAuditLocked {
			report.Classification = RecoveryRecoverable
			report.Actionability = ActionOperationRecovery
			report.Reason = "canonical operation is exact POST but terminal Change authority is incomplete"
			report.AutomaticRetry = false
			report.NextActions = []RecoveryAction{{Kind: RecoveryActionRecover, ChangeId: report.ChangeId, OperationId: operation.Id}}
		}
		return report
	}
	if condition == integration.ConditionAmbiguous {
		return ambiguousReport(report, "canonical Git state is neither exact PRE nor exact POST; automatic recovery is blocked")
	}
	report.Classification = RecoveryRecoverable
	report.Actionability = ActionOperationRecovery
	report.AutomaticRetry = false
	report.NextActions = []RecoveryAction{{Kind: RecoveryActionRecover, ChangeId: report.ChangeId, OperationId: operation.Id}}
	if condition == integration.ConditionPRE {
		report.Reason = "canonical operation is still exact PRE; explicit recovery will not replay it"
	} else {
		report.Reason = "canonical operation is exact POST; explicit recovery can finalize durable authority without reapplying it"
	}
	return report
}

func ambiguousReport(report RecoveryReport, reason string) RecoveryReport {
	report.Classification = RecoveryAmbiguous
	report.Actionability = ActionManualInvestigation
	report.Reason = reason
	report.AutomaticRetry = false
	report.WorkspaceReuse = false
	report.NextActions = investigationActions(report.ChangeId)
	return report
}

func corruptReport(report RecoveryReport, reason string) RecoveryReport {
	report.Classification = RecoveryCorrupt
	report.Actionability = ActionManualInvestigation
	report.Reason = reason
	report.AutomaticRetry = false
	report.WorkspaceReuse = false
	report.NextActions = investigationActions(report.ChangeId)
	return report
}

func investigationActions(id change.ChangeId) []RecoveryAction {
	return []RecoveryAction{{Kind: RecoveryActionShow, ChangeId: id}, {Kind: RecoveryActionHistory, ChangeId: id}, {Kind: RecoveryActionArtifacts, ChangeId: id}}
}

func investigationAndDiscard(id change.ChangeId) []RecoveryAction {
	return []RecoveryAction{{Kind: RecoveryActionShow, ChangeId: id}, {Kind: RecoveryActionHistory, ChangeId: id}, {Kind: RecoveryActionDiscard, ChangeId: id}}
}

func metadataString(metadata map[string]any, key string) string {
	if metadata == nil {
		return ""
	}
	value, _ := metadata[key].(string)
	return value
}
