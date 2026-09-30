package command

import (
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"testing"
	"time"

	sqliteadapter "github.com/Eu-Pedro0ficial/praetor/internal/adapters/persistence/sqlite"
	"github.com/Eu-Pedro0ficial/praetor/internal/artifact"
	"github.com/Eu-Pedro0ficial/praetor/internal/audit"
	"github.com/Eu-Pedro0ficial/praetor/internal/authority"
	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"github.com/Eu-Pedro0ficial/praetor/internal/inspection"
	"github.com/Eu-Pedro0ficial/praetor/internal/integration"
	"github.com/Eu-Pedro0ficial/praetor/internal/project"
	"github.com/Eu-Pedro0ficial/praetor/internal/workflow"
)

func TestFinalizeRecoveredCanonicalCommitsTerminalAuthorityAtomically(t *testing.T) {
	projectId, err := project.GenerateProjectID()
	if err != nil {
		t.Fatal(err)
	}
	registration := project.Registration{ProjectId: projectId, RepositoryRoot: t.TempDir(), SchemaVersion: 1, CreatedAt: time.Now().UTC()}
	store, err := sqliteadapter.Open(t.TempDir(), registration)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	current, err := change.New("recovered-change", projectId, "recover exact POST", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	snapshot, _ := workflow.CoreV0Snapshot()
	createdEvent := workflow.LifecycleEvent{EventType: workflow.EventChangeCreated, ChangeId: current.ChangeId(), ProjectId: projectId, ResultingState: current.State(), OccurredAt: current.CreatedAt(), Context: "test", Intent: current.Intent()}
	if err := store.CreateChange(current, snapshot, createdEvent); err != nil {
		t.Fatal(err)
	}
	for _, state := range []change.ChangeState{change.StatePlanned, change.StateIsolated, change.StateValidated, change.StateApproved} {
		previous := current
		transition, err := current.Transition(state, time.Now().UTC(), "test")
		if err != nil {
			t.Fatal(err)
		}
		event := workflow.LifecycleEvent{EventType: workflow.EventChangeTransition, ChangeId: current.ChangeId(), ProjectId: projectId, PreviousState: transition.PreviousState, ResultingState: transition.ResultingState, OccurredAt: transition.OccurredAt, Context: transition.Context}
		if err := store.CommitTransition(previous.Revision(), current, event); err != nil {
			t.Fatal(err)
		}
	}
	previousSelection, err := change.New("previous-selection", projectId, "preserve session authority", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	hydrationSession := &Session{registration: registration, durableAuthority: store}
	hydrationSession.setCurrentChange(previousSelection)
	durableInspection, err := inspection.New(store)
	if err != nil {
		t.Fatal(err)
	}
	hydrationSession.durableInspection = durableInspection
	if _, err := handleChangeSelect(hydrationSession, Invocation{Arguments: []string{string(current.ChangeId())}}, io.Discard); err == nil {
		t.Fatal("incomplete durable projection unexpectedly hydrated")
	}
	selected, ok := hydrationSession.CurrentChange()
	if !ok || selected.ChangeId() != previousSelection.ChangeId() {
		t.Fatalf("failed hydration partially published selected Change: %#v available=%t", selected, ok)
	}
	operationId, _ := authority.GenerateOperationId()
	now := time.Now().UTC()
	requestDigest := "sha256:" + strings.Repeat("1", 64)
	operation := authority.Operation{Id: operationId, ProjectId: projectId, ChangeId: current.ChangeId(), Kind: "canonical-git-apply", RequestDigest: requestDigest, ExpectedRevision: current.Revision(), State: authority.OperationReserved, CreatedAt: now, UpdatedAt: now}
	if _, _, err := store.ReserveOperation(operation); err != nil {
		t.Fatal(err)
	}
	session := &Session{registration: registration, durableAuthority: store}
	if got := session.StatusSnapshot().Recovery; got != "1 incomplete" {
		t.Fatalf("attach recovery status=%q", got)
	}
	proof, err := integration.NewCanonicalProof("head", "sha256:"+strings.Repeat("2", 64), []string{"file.go"}, true)
	if err != nil {
		t.Fatal(err)
	}
	completedResult, err := (authority.CanonicalResult{Head: proof.HeadRevision(), Patch: proof.PatchDigest(), Paths: proof.ChangedPaths(), Index: proof.IndexUnchanged()}).Encode()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := store.CompleteOperation(operationId, requestDigest, completedResult, nil); err != nil {
		t.Fatal(err)
	}
	completed, err := store.GetOperation(operationId)
	if err != nil {
		t.Fatal(err)
	}
	equivalentJSON, err := json.Marshal(map[string]any{"head": proof.HeadRevision(), "patch": proof.PatchDigest(), "paths": proof.ChangedPaths(), "index": true})
	if err != nil {
		t.Fatal(err)
	}
	if string(equivalentJSON) == string(completed.Result) {
		t.Fatal("idempotency fixture did not use a different JSON encoding")
	}
	resolved, err := store.CompleteOperation(operationId, requestDigest, equivalentJSON, nil)
	if err != nil || string(resolved.Result) != string(completed.Result) {
		t.Fatalf("semantic idempotency changed completed result: %#v err=%v", resolved, err)
	}
	contradictory, err := json.Marshal(map[string]any{"operation_id": operationId, "canonical_head": proof.HeadRevision(), "patch_digest": "sha256:" + strings.Repeat("f", 64), "changed_paths": proof.ChangedPaths(), "index_unchanged": true})
	if err != nil {
		t.Fatal(err)
	}
	wrongId, _ := artifact.GenerateId()
	wrongArtifact, err := artifact.New(wrongId, projectId, current.ChangeId(), artifact.KindApplicationResult, 1, 2, "application/json", time.Now().UTC(), artifact.Producer{Component: "test", OperationId: string(operationId)}, contradictory, false)
	if err != nil {
		t.Fatal(err)
	}
	wrongCandidate := current
	if _, err := wrongCandidate.Transition(change.StateAuditLocked, time.Now().UTC(), "test conflicting commit"); err != nil {
		t.Fatal(err)
	}
	if err := store.CommitAuthority(authority.AuthorityCommit{ExpectedRevision: current.Revision(), Candidate: wrongCandidate, Operation: &completed, Artifacts: []artifact.Artifact{wrongArtifact}, Bindings: []authority.ArtifactBinding{{Role: "application-result", ArtifactId: wrongId, Revision: wrongCandidate.Revision()}}}); !errors.Is(err, authority.ErrCorrupt) {
		t.Fatalf("conflicting completed result authority commit error=%v", err)
	}
	conflictingOperation := completed
	conflictingResult, err := (authority.CanonicalResult{Head: proof.HeadRevision(), Patch: "sha256:" + strings.Repeat("f", 64), Paths: proof.ChangedPaths(), Index: true}).Encode()
	if err != nil {
		t.Fatal(err)
	}
	conflictingOperation.Result = conflictingResult
	if err := store.CommitAuthority(authority.AuthorityCommit{ExpectedRevision: current.Revision(), Candidate: wrongCandidate, Operation: &conflictingOperation, Artifacts: []artifact.Artifact{wrongArtifact}, Bindings: []authority.ArtifactBinding{{Role: "application-result", ArtifactId: wrongId, Revision: wrongCandidate.Revision()}}}); !errors.Is(err, authority.ErrOperationConflict) {
		t.Fatalf("completed operation outcome replacement was accepted: %v", err)
	}
	stillApproved, _, err := store.GetChange(current.ChangeId())
	if err != nil || stillApproved.State() != change.StateApproved {
		t.Fatalf("conflicting commit published Change=%#v err=%v", stillApproved, err)
	}
	terminal, err := session.finalizeRecoveredCanonical(completed, proof)
	if err != nil {
		t.Fatal(err)
	}
	if terminal.State() != change.StateAuditLocked || terminal.Revision() != 6 {
		t.Fatalf("terminal state=%s revision=%d", terminal.State(), terminal.Revision())
	}
	bindings, err := store.ListBindings(current.ChangeId())
	if err != nil || len(bindings) != 1 || bindings[0].Role != "application-result" || bindings[0].Revision != terminal.Revision() {
		t.Fatalf("bindings=%#v error=%v", bindings, err)
	}
	history, err := store.AuditHistory(current.ChangeId())
	if err != nil {
		t.Fatal(err)
	}
	if history[len(history)-3].EventType != audit.EventOperationRecovered || history[len(history)-2].EventType != workflow.EventChangeTransition || history[len(history)-1].EventType != audit.EventArtifactCommitted {
		t.Fatalf("terminal audit tail=%#v", history[len(history)-3:])
	}
	raw, err := sql.Open("sqlite", store.Path())
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`DELETE FROM artifact_bindings WHERE change_id=? AND role='application-result'`, current.ChangeId()); err != nil {
		raw.Close()
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := session.finalizeRecoveredCanonical(completed, proof); !errors.Is(err, authority.ErrCorrupt) {
		t.Fatalf("audit-locked Change without application-result authority error=%v", err)
	}
}

func TestActiveWorkspaceAuditBlockerFailsClosedForIncompleteProviderOutcome(t *testing.T) {
	const workspaceId = "proposal-00112233445566778899aabbccddeeff"
	event := func(eventType string, metadata map[string]any) audit.Event {
		if metadata == nil {
			metadata = map[string]any{}
		}
		metadata["workspace_id"] = workspaceId
		return audit.Event{EventType: eventType, Metadata: metadata}
	}
	tests := []struct {
		name   string
		events []audit.Event
		want   string
	}{
		{
			name:   "started without outcome",
			events: []audit.Event{event(audit.EventProposalWorkspaceCreated, nil), event(audit.EventProviderExecutionStarted, nil)},
			want:   "provider attempt has no durable outcome",
		},
		{
			name: "workspace failure without provider outcome",
			events: []audit.Event{
				event(audit.EventProviderExecutionStarted, nil),
				event(audit.EventProposalWorkspaceFailed, nil),
			},
			want: "provider attempt has no durable outcome",
		},
		{
			name:   "completed without patch outcome",
			events: []audit.Event{event(audit.EventProviderExecutionCompleted, nil)},
			want:   "provider completion has no durable patch outcome",
		},
		{
			name:   "pre-invocation guard failure",
			events: []audit.Event{event(audit.EventProviderExecutionFailed, map[string]any{"failure_stage": "pre-invocation-guard"})},
			want:   "canonical source guard failed",
		},
		{
			name:   "classified rejection is safe to replace",
			events: []audit.Event{event(audit.EventProviderExecutionCompleted, nil), event(audit.EventPatchRejected, nil)},
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got := activeWorkspaceAuditBlocker(test.events, workspaceId)
			if !strings.Contains(got, test.want) {
				t.Fatalf("blocker = %q, want containing %q", got, test.want)
			}
		})
	}
}
