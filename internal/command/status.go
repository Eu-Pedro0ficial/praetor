package command

import (
	"context"
	"fmt"

	"github.com/Eu-Pedro0ficial/praetor/internal/aiprovider"
	"github.com/Eu-Pedro0ficial/praetor/internal/audit"
	"github.com/Eu-Pedro0ficial/praetor/internal/authority"
	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"github.com/Eu-Pedro0ficial/praetor/internal/proposal"
)

// StatusSnapshot is the single presentation-neutral projection consumed by
// both the status command and the terminal sidebar.
type StatusSnapshot struct {
	Project                 string
	Repository              string
	GitRepository           string
	Change                  string
	Proposal                string
	ProposalBase            string
	ProviderAdapter         string
	ProviderVendor          string
	ProviderModel           string
	ProviderSelectionSource string
	ProviderReadiness       string
	Verification            string
	VerificationAttempt     string
	HumanDecision           string
	HumanActor              string
	Recovery                string
	RecoveryClassification  string
	RecoveryReason          string
	LastOperation           string
	OperationOutcome        string
	CanonicalSource         string
	WorkspaceDisposition    string
	SafeNextActions         string
	Session                 string
}

// StatusSnapshot returns one coherent view of retained session state.
func (session *Session) StatusSnapshot() StatusSnapshot {
	snapshot := StatusSnapshot{
		Project:       "none",
		Repository:    "none",
		GitRepository: "true",
		Change:        "none",
		Proposal:      "none",
		Verification:  "none",
		HumanDecision: "none",
		Recovery:      "clean",
		Session:       "active",
		ProviderModel: "provider default",
	}
	if session == nil {
		snapshot.GitRepository = "false"
		snapshot.Session = "none"
		return snapshot
	}
	registration := session.Registration()
	snapshot.Project = string(registration.ProjectId)
	snapshot.Repository = registration.RepositoryRoot
	if currentChange, ok := session.CurrentChange(); ok {
		snapshot.Change = string(currentChange.ChangeId()) + " (" + string(currentChange.State()) + ")"
	}
	if currentProposal, ok := session.CurrentProposal(); ok {
		workspace := currentProposal.Workspace()
		snapshot.Proposal = string(workspace.WorkspaceId()) + " (" + string(workspace.State()) + ")"
		snapshot.ProposalBase = workspace.BaseRevision()
	}
	selection := session.ProviderSelection()
	snapshot.ProviderAdapter = string(selection.ProviderIdentifier())
	if descriptor, ok := session.SelectedProviderDescriptor(); ok {
		snapshot.ProviderVendor = descriptor.Vendor()
	}
	if model, ok := selection.ModelIdentifier(); ok {
		snapshot.ProviderModel = string(model)
	}
	provenance := session.ProviderSelectionProvenance()
	snapshot.ProviderSelectionSource = fmt.Sprintf(
		"provider=%s; model=%s",
		provenance.ProviderSource(),
		provenance.ModelSource(),
	)
	_, readiness, readinessError := session.InspectProviderReadiness(
		context.Background(),
		aiprovider.ReadinessDiscovery,
	)
	if readinessError != nil {
		snapshot.ProviderReadiness = "indeterminate"
	} else {
		snapshot.ProviderReadiness = string(readiness.Disposition())
	}
	if result, ok := session.LastVerification(); ok {
		snapshot.VerificationAttempt = string(result.AttemptId())
		snapshot.Verification = "FAIL"
		if result.Passed() {
			snapshot.Verification = "PASS"
		}
	}
	if decision, ok := session.LastDecision(); ok {
		snapshot.HumanDecision = string(decision.Kind())
		snapshot.HumanActor = string(decision.Actor())
	}
	if current, ok := session.CurrentChange(); ok {
		snapshot.CanonicalSource = "not modified by proposal operations"
		switch current.State() {
		case change.StatePlanned:
			snapshot.LastOperation = "change isolate"
			snapshot.OperationOutcome = "isolation incomplete"
			snapshot.WorkspaceDisposition = "none"
			snapshot.SafeNextActions = "change isolate"
			snapshot.Recovery = "retryable"
		case change.StateIsolated:
			if currentProposal, hasProposal := session.CurrentProposal(); hasProposal {
				switch currentProposal.Workspace().State() {
				case proposal.WorkspaceActive:
					snapshot.LastOperation = "change isolate"
					snapshot.OperationOutcome = "ready for implementation"
					snapshot.WorkspaceDisposition = "active"
					snapshot.SafeNextActions = "change implement; change discard"
				case proposal.WorkspaceFailed:
					snapshot.LastOperation = "change implement"
					snapshot.OperationOutcome = "provider or patch extraction failed"
					snapshot.WorkspaceDisposition = "retained failed; cleaned before retry"
					snapshot.SafeNextActions = "change implement; change discard; change diagnose"
					snapshot.Recovery = "retryable"
				case proposal.WorkspaceCleanupFailed:
					snapshot.LastOperation = "workspace cleanup"
					snapshot.OperationOutcome = "cleanup failed"
					snapshot.WorkspaceDisposition = "retained; cleanup not proven"
					snapshot.SafeNextActions = "change implement; change discard; change diagnose"
					snapshot.Recovery = "action required"
				case proposal.WorkspaceRejected:
					snapshot.LastOperation = "change implement"
					snapshot.OperationOutcome = "no valid patch was accepted"
					snapshot.WorkspaceDisposition = "retained rejected; cleaned before retry"
					snapshot.SafeNextActions = "change implement; change discard; change diagnose"
					snapshot.Recovery = "retryable"
				case proposal.WorkspaceRetained:
					snapshot.WorkspaceDisposition = "retained"
					if result, verified := session.LastVerification(); verified && !result.Passed() {
						snapshot.LastOperation = "change verify"
						snapshot.OperationOutcome = "verification failed"
						snapshot.SafeNextActions = "change verify; change discard; change diagnose"
						snapshot.Recovery = "retryable"
					} else {
						snapshot.LastOperation = "change implement"
						snapshot.OperationOutcome = "valid proposal retained"
						snapshot.SafeNextActions = "change verify; change discard"
					}
				}
			} else if _, _, hasFoundation := session.proposalFoundation(); hasFoundation {
				snapshot.LastOperation = "workspace cleanup"
				snapshot.OperationOutcome = "proposal workspace cleaned"
				snapshot.WorkspaceDisposition = "cleaned"
				snapshot.SafeNextActions = "change implement; change diagnose"
				snapshot.Recovery = "retryable"
			}
		case change.StateRejected:
			snapshot.LastOperation = "change discard or human rejection"
			snapshot.OperationOutcome = "rejected"
			if _, hasProposal := session.CurrentProposal(); hasProposal {
				snapshot.WorkspaceDisposition = "retained; cleanup required before closure"
				snapshot.SafeNextActions = "change discard; change close; change diagnose"
				snapshot.Recovery = "action required"
			} else {
				snapshot.WorkspaceDisposition = "cleaned or unavailable"
				snapshot.SafeNextActions = "change close; change diagnose"
				snapshot.Recovery = "closable"
			}
		case change.StateAuditLocked:
			snapshot.LastOperation = "change close or change apply"
			snapshot.OperationOutcome = "terminal"
			snapshot.WorkspaceDisposition = "cleaned"
			snapshot.SafeNextActions = "change show; change history"
			snapshot.Recovery = "terminal"
			if currentProposal, hasProposal := session.CurrentProposal(); hasProposal {
				snapshot.LastOperation = "workspace cleanup"
				snapshot.OperationOutcome = "terminal closure completed; workspace cleanup incomplete"
				snapshot.WorkspaceDisposition = string(currentProposal.Workspace().State())
				snapshot.SafeNextActions = "change discard; change diagnose"
				snapshot.Recovery = "action required"
			}
		}
	}
	if current, ok := session.CurrentChange(); ok && session.durableInspection != nil {
		if detail, err := session.durableInspection.InspectChange(current.ChangeId()); err == nil {
			applyLifecycleAuditRecovery(&snapshot, current, detail.Audit)
		}
		if recovery, err := session.durableInspection.Recovery(current.ChangeId()); err == nil {
			snapshot.RecoveryClassification = string(recovery.Classification)
			snapshot.RecoveryReason = recovery.Reason
		} else {
			snapshot.RecoveryClassification = "UNAVAILABLE"
			snapshot.RecoveryReason = "recovery evidence could not be inspected safely"
		}
	}
	if session.durableAuthority != nil {
		operations, err := session.durableAuthority.ListIncompleteOperations()
		if err != nil {
			snapshot.Recovery = "unavailable"
		} else {
			if session.durableInspection == nil {
				if len(operations) > 0 {
					snapshot.Recovery = fmt.Sprintf("%d incomplete", len(operations))
				}
				return snapshot
			}
			blockers := 0
			changes, listError := session.durableAuthority.ListChanges()
			if listError != nil {
				snapshot.Recovery = "unavailable"
				return snapshot
			}
			for _, current := range changes {
				diagnoses, diagnosisError := session.durableInspection.Diagnose(current.ChangeId())
				if diagnosisError != nil {
					snapshot.Recovery = "unavailable"
					return snapshot
				}
				for _, diagnosis := range diagnoses {
					if diagnosis.Condition != authority.RecoveryClean && diagnosis.Condition != authority.RecoveryIncomplete {
						blockers++
					}
				}
			}
			switch {
			case blockers > 0 && len(operations) > 0:
				snapshot.Recovery = fmt.Sprintf("%d incomplete, %d blocker(s)", len(operations), blockers)
			case blockers > 0:
				snapshot.Recovery = fmt.Sprintf("%d blocker(s)", blockers)
			case len(operations) > 0:
				snapshot.Recovery = fmt.Sprintf("%d incomplete", len(operations))
			}
		}
	}
	if snapshot.LastOperation == "" {
		snapshot.LastOperation = "none"
	}
	if snapshot.OperationOutcome == "" {
		snapshot.OperationOutcome = "none"
	}
	if snapshot.CanonicalSource == "" {
		snapshot.CanonicalSource = "unchanged"
	}
	if snapshot.WorkspaceDisposition == "" {
		snapshot.WorkspaceDisposition = "none"
	}
	if snapshot.SafeNextActions == "" {
		snapshot.SafeNextActions = "none"
	}
	if snapshot.RecoveryClassification == "" {
		snapshot.RecoveryClassification = "SAFE"
		snapshot.RecoveryReason = "no active Change recovery blocker"
	}
	return snapshot
}

func applyLifecycleAuditRecovery(snapshot *StatusSnapshot, current change.Change, events []audit.Event) {
	if snapshot == nil || current.State() != change.StateIsolated {
		return
	}
	for index := len(events) - 1; index >= 0; index-- {
		event := events[index]
		switch event.EventType {
		case audit.EventProposalWorkspaceCleanupFailed:
			snapshot.LastOperation = "workspace cleanup"
			snapshot.OperationOutcome = "cleanup failed"
			snapshot.WorkspaceDisposition = "retained; cleanup not proven"
			snapshot.SafeNextActions = "change implement; change discard; change diagnose"
			snapshot.Recovery = "action required"
			return
		case audit.EventProposalWorkspaceDiscarded:
			snapshot.LastOperation = "workspace cleanup"
			snapshot.OperationOutcome = "proposal workspace cleaned"
			snapshot.WorkspaceDisposition = "cleaned"
			snapshot.SafeNextActions = "change implement; change diagnose"
			snapshot.Recovery = "retryable"
			return
		case audit.EventPatchRejected:
			snapshot.LastOperation = "change implement"
			snapshot.OperationOutcome = "no valid patch was accepted"
			snapshot.WorkspaceDisposition = "retained rejected; cleaned before retry"
			snapshot.SafeNextActions = "change implement; change discard; change diagnose"
			snapshot.Recovery = "retryable"
			return
		case audit.EventPatchSurfaceValidated:
			snapshot.LastOperation = "change implement"
			snapshot.OperationOutcome = "valid proposal retained"
			snapshot.WorkspaceDisposition = "retained"
			snapshot.SafeNextActions = "change verify; change discard"
			return
		case audit.EventProviderExecutionFailed:
			snapshot.LastOperation = "change implement"
			if event.Metadata["failure_stage"] == "pre-invocation-guard" {
				snapshot.OperationOutcome = "canonical source guard failed before provider invocation"
				snapshot.WorkspaceDisposition = "active; provider did not run"
				snapshot.SafeNextActions = "change discard; change diagnose"
				snapshot.Recovery = "action required"
			} else {
				snapshot.OperationOutcome = "provider failed before a valid proposal"
				snapshot.WorkspaceDisposition = "retained failed; cleaned before retry"
				snapshot.SafeNextActions = "change implement; change discard; change diagnose"
				snapshot.Recovery = "retryable"
			}
			return
		case audit.EventProviderExecutionCompleted:
			snapshot.LastOperation = "change implement"
			snapshot.OperationOutcome = "provider completed; proposal outcome is incomplete"
			snapshot.WorkspaceDisposition = "recovery disposition unknown"
			snapshot.SafeNextActions = "change diagnose; change discard"
			snapshot.Recovery = "action required"
			return
		case audit.EventProviderExecutionStarted:
			snapshot.LastOperation = "change implement"
			snapshot.OperationOutcome = "provider attempt has no durable outcome"
			snapshot.WorkspaceDisposition = "recovery disposition unknown"
			snapshot.SafeNextActions = "change diagnose; change discard"
			snapshot.Recovery = "action required"
			return
		case audit.EventVerificationFailed:
			snapshot.LastOperation = "change verify"
			snapshot.OperationOutcome = "verification failed"
			snapshot.WorkspaceDisposition = "retained"
			snapshot.SafeNextActions = "change verify; change discard; change diagnose"
			snapshot.Recovery = "retryable"
			return
		}
	}
}

func durableLifecycleStatus(current change.Change, events []audit.Event) StatusSnapshot {
	snapshot := StatusSnapshot{
		CanonicalSource:      "not modified by proposal operations",
		WorkspaceDisposition: "none",
		SafeNextActions:      "change show; change history",
		Recovery:             "clean",
	}
	switch current.State() {
	case change.StateCreated, change.StatePlanned:
		snapshot.LastOperation = "change isolate"
		snapshot.OperationOutcome = "isolation incomplete"
		snapshot.SafeNextActions = "change isolate"
		snapshot.Recovery = "retryable"
	case change.StateIsolated:
		snapshot.LastOperation = "change isolate"
		snapshot.OperationOutcome = "isolated"
		snapshot.WorkspaceDisposition = "unknown; inspect lifecycle evidence"
		snapshot.SafeNextActions = "change diagnose"
		snapshot.Recovery = "action required"
		applyLifecycleAuditRecovery(&snapshot, current, events)
	case change.StateValidated:
		snapshot.LastOperation = "change verify"
		snapshot.OperationOutcome = "validated"
		snapshot.WorkspaceDisposition = "retained"
		snapshot.SafeNextActions = "change approve; change reject"
	case change.StateApproved:
		snapshot.LastOperation = "change approve"
		snapshot.OperationOutcome = "approved"
		snapshot.WorkspaceDisposition = "retained"
		snapshot.SafeNextActions = "change apply"
	case change.StateRejected:
		snapshot.LastOperation = "change reject or change discard"
		snapshot.OperationOutcome = "rejected"
		snapshot.WorkspaceDisposition = "inspect lifecycle evidence"
		snapshot.SafeNextActions = "change close"
		snapshot.Recovery = "closable"
	case change.StateAuditLocked:
		snapshot.LastOperation = "change close or change apply"
		snapshot.OperationOutcome = "terminal"
		snapshot.WorkspaceDisposition = "cleaned"
		snapshot.SafeNextActions = "change show; change history"
		snapshot.Recovery = "terminal"
	}
	return snapshot
}
