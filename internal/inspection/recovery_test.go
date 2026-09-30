package inspection

import (
	"testing"
	"time"

	"github.com/Eu-Pedro0ficial/praetor/internal/audit"
	"github.com/Eu-Pedro0ficial/praetor/internal/authority"
	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"github.com/Eu-Pedro0ficial/praetor/internal/integration"
	"github.com/Eu-Pedro0ficial/praetor/internal/project"
	"github.com/Eu-Pedro0ficial/praetor/internal/proposal"
)

const recoveryTestProjectId project.ProjectId = "01890c29-7a78-7abc-8def-0123456789ab"

func TestDeriveRecoveryOperatorStateMatrix(t *testing.T) {
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	event := func(eventType string) audit.Event {
		return audit.Event{EventType: eventType, Metadata: map[string]any{"workspace_id": "proposal-test", "execution_attempt_id": "attempt-test"}}
	}
	reservedWorkspace := authority.Operation{Id: "op-workspace", Kind: proposal.OperationProposalWorkspaceCreate, State: authority.OperationReserved, UpdatedAt: now}
	reservedCanonical := authority.Operation{Id: "op-canonical", Kind: "canonical-git-apply", State: authority.OperationReserved, UpdatedAt: now}
	completed := authority.Operation{Id: "op-completed", Kind: proposal.OperationProposalWorkspaceCreate, State: authority.OperationCompleted, UpdatedAt: now}
	tests := []struct {
		name           string
		state          change.ChangeState
		events         []audit.Event
		operations     []authority.Operation
		classification RecoveryClassification
		actionability  RecoveryActionability
	}{
		{name: "healthy planned", state: change.StatePlanned, classification: RecoverySafe, actionability: ActionInformational},
		{name: "healthy isolated", state: change.StateIsolated, classification: RecoverySafe, actionability: ActionInformational},
		{name: "healthy validated", state: change.StateValidated, classification: RecoverySafe, actionability: ActionInformational},
		{name: "healthy approved", state: change.StateApproved, classification: RecoverySafe, actionability: ActionInformational},
		{name: "healthy rejected", state: change.StateRejected, classification: RecoverySafe, actionability: ActionInformational},
		{name: "healthy terminal", state: change.StateAuditLocked, classification: RecoverySafe, actionability: ActionInformational},
		{name: "reserved workspace operation", state: change.StateIsolated, operations: []authority.Operation{reservedWorkspace}, classification: RecoveryRecoverable, actionability: ActionOperationRecovery},
		{name: "reserved canonical operation", state: change.StateApproved, operations: []authority.Operation{reservedCanonical}, classification: RecoveryRecoverable, actionability: ActionOperationRecovery},
		{name: "completed operation is not incomplete", state: change.StateIsolated, operations: []authority.Operation{completed}, classification: RecoverySafe, actionability: ActionInformational},
		{name: "provider started without terminal outcome", state: change.StateIsolated, events: []audit.Event{event(audit.EventProviderExecutionStarted)}, classification: RecoveryAmbiguous, actionability: ActionWorkspaceDiscard},
		{name: "workspace failure cannot terminalize provider attempt", state: change.StateIsolated, events: []audit.Event{event(audit.EventProviderExecutionStarted), event(audit.EventProposalWorkspaceFailed)}, classification: RecoveryAmbiguous, actionability: ActionWorkspaceDiscard},
		{name: "patch event cannot terminalize provider attempt", state: change.StateIsolated, events: []audit.Event{event(audit.EventProviderExecutionStarted), event(audit.EventPatchRejected)}, classification: RecoveryAmbiguous, actionability: ActionWorkspaceDiscard},
		{name: "provider completed without patch outcome", state: change.StateIsolated, events: []audit.Event{event(audit.EventProviderExecutionStarted), event(audit.EventProviderExecutionCompleted)}, classification: RecoveryRecoverable, actionability: ActionWorkspaceDiscard},
		{name: "provider failed", state: change.StateIsolated, events: []audit.Event{event(audit.EventProviderExecutionStarted), event(audit.EventProviderExecutionFailed)}, classification: RecoveryRecoverable, actionability: ActionSafeRetry},
		{name: "workspace failed", state: change.StateIsolated, events: []audit.Event{event(audit.EventProposalWorkspaceFailed)}, classification: RecoveryRecoverable, actionability: ActionSafeRetry},
		{name: "patch rejected", state: change.StateIsolated, events: []audit.Event{event(audit.EventProviderExecutionCompleted), event(audit.EventPatchRejected)}, classification: RecoveryRecoverable, actionability: ActionSafeRetry},
		{name: "patch surface validated", state: change.StateIsolated, events: []audit.Event{event(audit.EventProviderExecutionCompleted), event(audit.EventPatchSurfaceValidated)}, classification: RecoverySafe, actionability: ActionInformational},
		{name: "cleanup failed", state: change.StateIsolated, events: []audit.Event{event(audit.EventProposalWorkspaceCleanupFailed)}, classification: RecoveryAmbiguous, actionability: ActionManualInvestigation},
		{name: "discard durable", state: change.StateIsolated, events: []audit.Event{event(audit.EventProposalWorkspaceDiscarded)}, classification: RecoverySafe, actionability: ActionSafeRetry},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			current := recoveryChangeInState(t, test.state)
			report := deriveRecovery(current, test.events, test.operations)
			if report.Classification != test.classification || report.Actionability != test.actionability {
				t.Fatalf("classification/actionability = %s/%s, want %s/%s; reason=%s", report.Classification, report.Actionability, test.classification, test.actionability, report.Reason)
			}
			if report.ChangeId == "" || report.Reason == "" {
				t.Fatalf("incomplete report: %#v", report)
			}
		})
	}
}

func TestWorkspaceEvidenceClassificationMatrix(t *testing.T) {
	base := RecoveryReport{ChangeId: "change-recovery", ChangeState: change.StateIsolated}
	operation := authority.Operation{Id: "op-test", Kind: proposal.OperationProposalWorkspaceCreate, State: authority.OperationReserved}
	tests := []struct {
		name           string
		evidence       proposal.WorkspaceCreationInspection
		classification RecoveryClassification
	}{
		{name: "reserved absent", evidence: proposal.WorkspaceCreationInspection{Authority: proposal.WorkspaceAuthorityMissing, ExternalCondition: proposal.WorkspaceReservationAbsent}, classification: RecoveryRecoverable},
		{name: "reserved owned orphan", evidence: proposal.WorkspaceCreationInspection{Authority: proposal.WorkspaceAuthorityMissing, ExternalCondition: proposal.WorkspaceReservationPresent}, classification: RecoveryRecoverable},
		{name: "persisted foundation and exact worktree", evidence: proposal.WorkspaceCreationInspection{Authority: proposal.WorkspaceAuthorityExact, ExternalCondition: proposal.WorkspaceReservationPresent, BaseVerified: true}, classification: RecoveryRecoverable},
		{name: "cleanup evidence gap", evidence: proposal.WorkspaceCreationInspection{Authority: proposal.WorkspaceAuthorityExact, ExternalCondition: proposal.WorkspaceReservationAbsent}, classification: RecoveryAmbiguous},
		{name: "contradictory authority", evidence: proposal.WorkspaceCreationInspection{Authority: proposal.WorkspaceAuthorityContradictory, ExternalCondition: proposal.WorkspaceReservationPresent}, classification: RecoveryCorrupt},
		{name: "unproven external ownership", evidence: proposal.WorkspaceCreationInspection{Authority: proposal.WorkspaceAuthorityMissing, ExternalCondition: proposal.WorkspaceReservationAmbiguous}, classification: RecoveryAmbiguous},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			report := classifyWorkspaceOperation(base, operation, test.evidence)
			if report.Classification != test.classification {
				t.Fatalf("classification=%s want=%s reason=%s", report.Classification, test.classification, report.Reason)
			}
		})
	}
}

func TestCanonicalEvidenceClassificationMatrix(t *testing.T) {
	base := RecoveryReport{ChangeId: "change-recovery", ChangeState: change.StateApproved}
	operation := authority.Operation{Id: "op-test", Kind: "canonical-git-apply", State: authority.OperationReserved}
	for _, test := range []struct {
		condition      integration.ExternalCondition
		classification RecoveryClassification
	}{
		{condition: integration.ConditionPRE, classification: RecoveryRecoverable},
		{condition: integration.ConditionPOST, classification: RecoveryRecoverable},
		{condition: integration.ConditionAmbiguous, classification: RecoveryAmbiguous},
	} {
		report := classifyCanonicalOperation(base, operation, test.condition)
		if report.Classification != test.classification {
			t.Fatalf("condition %s classification=%s want=%s", test.condition, report.Classification, test.classification)
		}
	}
}

func TestCompletedCanonicalOperationRequiresRecoveryUntilChangeIsTerminal(t *testing.T) {
	operation := authority.Operation{Id: "op-test", Kind: "canonical-git-apply", State: authority.OperationCompleted}
	approved := classifyCanonicalOperation(RecoveryReport{ChangeId: "change-recovery", ChangeState: change.StateApproved}, operation, integration.ConditionPOST)
	if approved.Classification != RecoveryRecoverable || approved.Actionability != ActionOperationRecovery || len(approved.NextActions) != 1 || approved.NextActions[0].Kind != RecoveryActionRecover {
		t.Fatalf("approved completed canonical report=%#v", approved)
	}
	terminal := classifyCanonicalOperation(healthyRecovery(recoveryChangeInState(t, change.StateAuditLocked)), operation, integration.ConditionPOST)
	if terminal.Classification != RecoverySafe {
		t.Fatalf("terminal completed canonical classification=%s", terminal.Classification)
	}
}

func TestHistoricalProposalEventsDoNotOverrideCurrentChangeActions(t *testing.T) {
	current := recoveryChangeInState(t, change.StateApproved)
	report := deriveRecovery(current, []audit.Event{{EventType: audit.EventPatchSurfaceValidated}}, nil)
	if report.Classification != RecoverySafe || len(report.NextActions) != 1 || report.NextActions[0].Kind != RecoveryActionApply {
		t.Fatalf("approved report was overridden by historical proposal event: %#v", report)
	}
}

func TestCompletedWorkspaceWithoutFoundationIsCorrupt(t *testing.T) {
	base := RecoveryReport{ChangeId: "change-recovery", ChangeState: change.StateIsolated}
	operation := authority.Operation{Id: "op-test", Kind: proposal.OperationProposalWorkspaceCreate, State: authority.OperationCompleted}
	evidence := proposal.WorkspaceCreationInspection{Authority: proposal.WorkspaceAuthorityMissing, ExternalCondition: proposal.WorkspaceReservationAbsent}
	report := classifyWorkspaceOperation(base, operation, evidence)
	if report.Classification != RecoveryCorrupt || report.Actionability != ActionManualInvestigation {
		t.Fatalf("report=%#v", report)
	}
}

func recoveryChangeInState(t *testing.T, state change.ChangeState) change.Change {
	t.Helper()
	now := time.Date(2026, 9, 29, 12, 0, 0, 0, time.UTC)
	current, err := change.New("change-recovery", recoveryTestProjectId, "diagnose recovery", now)
	if err != nil {
		t.Fatal(err)
	}
	path := []change.ChangeState{change.StatePlanned, change.StateIsolated, change.StateValidated, change.StateApproved, change.StateAuditLocked}
	if state == change.StateRejected {
		path = []change.ChangeState{change.StatePlanned, change.StateRejected}
	}
	for _, next := range path {
		if current.State() == state {
			break
		}
		if _, err := current.Transition(next, now.Add(time.Second), "test"); err != nil {
			t.Fatal(err)
		}
	}
	if current.State() != state {
		t.Fatalf("fixture state=%s want=%s", current.State(), state)
	}
	return current
}
