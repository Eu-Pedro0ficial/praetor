package command

import (
	"bytes"
	"errors"
	"strings"
	"testing"
	"time"

	sqliteadapter "github.com/Eu-Pedro0ficial/praetor/internal/adapters/persistence/sqlite"
	"github.com/Eu-Pedro0ficial/praetor/internal/authority"
	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"github.com/Eu-Pedro0ficial/praetor/internal/inspection"
	"github.com/Eu-Pedro0ficial/praetor/internal/project"
	"github.com/Eu-Pedro0ficial/praetor/internal/proposal"
	"github.com/Eu-Pedro0ficial/praetor/internal/workflow"
)

func TestWave6DiagnoseWithoutSelectedChangeExplainsReadOnlyDiscoveryPath(t *testing.T) {
	store, session, _ := wave6RecoveryFixture(t)
	defer store.Close()
	var output bytes.Buffer
	if _, err := handleChangeDiagnose(session, Invocation{}, &output); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"No Change selected", "read-only", "change list", "change diagnose <change-id>"} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("output missing %q:\n%s", expected, output.String())
		}
	}
}

func TestWave6HealthyDiagnosisIsReadOnlyAndNeedsNoProvider(t *testing.T) {
	store, session, current := wave6RecoveryFixture(t)
	defer store.Close()
	session.setCurrentChange(current)
	before, _, err := store.GetChange(current.ChangeId())
	if err != nil {
		t.Fatal(err)
	}
	historyBefore, err := store.AuditHistory(current.ChangeId())
	if err != nil {
		t.Fatal(err)
	}
	operationsBefore, err := store.ListOperations(current.ChangeId())
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if _, err := handleChangeDiagnose(session, Invocation{}, &output); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"Recovery diagnosis:", "Classification: SAFE", "State: created", "change isolate change-recovery <intent>"} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("output missing %q:\n%s", expected, output.String())
		}
	}
	after, _, err := store.GetChange(current.ChangeId())
	if err != nil {
		t.Fatal(err)
	}
	historyAfter, _ := store.AuditHistory(current.ChangeId())
	operationsAfter, _ := store.ListOperations(current.ChangeId())
	if after.Revision() != before.Revision() || len(historyAfter) != len(historyBefore) || len(operationsAfter) != len(operationsBefore) {
		t.Fatalf("diagnosis mutated authority: revision %d->%d history %d->%d operations %d->%d", before.Revision(), after.Revision(), len(historyBefore), len(historyAfter), len(operationsBefore), len(operationsAfter))
	}
}

func TestWave6ProviderAmbiguityRenderingExplainsBlockedReplayAndDiffAuthority(t *testing.T) {
	report := inspection.RecoveryReport{
		ChangeId:          "change-recovery",
		ChangeState:       change.StateIsolated,
		Classification:    inspection.RecoveryAmbiguous,
		Actionability:     inspection.ActionWorkspaceDiscard,
		Reason:            "provider execution may have occurred and the workspace may contain partial mutations; Git diff alone cannot prove provider completion",
		WorkspaceId:       "proposal-test",
		ProviderAttemptId: "attempt-test",
		ProviderOutcome:   "started-without-terminal-outcome",
		NextActions:       []inspection.RecoveryAction{{Kind: inspection.RecoveryActionShow, ChangeId: "change-recovery"}, {Kind: inspection.RecoveryActionDiscard, ChangeId: "change-recovery"}},
	}
	var output bytes.Buffer
	writeRecoveryReport(&output, &Session{}, report)
	for _, expected := range []string{"Classification: AMBIGUOUS", "partial mutations", "Git diff", "Provider replay: blocked", "Workspace reuse: blocked", "change select change-recovery; then change discard"} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("output missing %q:\n%s", expected, output.String())
		}
	}
}

func TestWave6RecoverUnknownOperationUsesProjectScopedOperatorError(t *testing.T) {
	store, session, _ := wave6RecoveryFixture(t)
	defer store.Close()
	_, err := handleChangeRecover(session, Invocation{Arguments: []string{"op-unknown"}}, &bytes.Buffer{})
	if err == nil || !strings.Contains(err.Error(), `operation "op-unknown" is not available in the active Project`) {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestWave6RecoverTerminalWorkspaceOperationIsReadOnlyAndIdempotent(t *testing.T) {
	store, session, current := wave6RecoveryFixture(t)
	defer store.Close()
	other, err := change.New("other-selection", current.ProjectId(), "preserve selected Change", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	session.setCurrentChange(other)
	now := time.Now().UTC()
	operation := authority.Operation{Id: "op-0123456789abcdef0123456789abcdef", ProjectId: current.ProjectId(), ChangeId: current.ChangeId(), Kind: proposal.OperationProposalWorkspaceCreate, RequestDigest: "sha256:" + strings.Repeat("a", 64), ExpectedRevision: current.Revision(), State: authority.OperationReserved, CreatedAt: now, UpdatedAt: now}
	if _, _, err := store.ReserveOperation(operation); err != nil {
		t.Fatal(err)
	}
	if _, err := store.CompleteOperation(operation.Id, operation.RequestDigest, []byte(`{"outcome":"completed"}`), nil); err != nil {
		t.Fatal(err)
	}
	before, err := store.GetOperation(operation.Id)
	if err != nil {
		t.Fatal(err)
	}
	for attempt := 0; attempt < 2; attempt++ {
		var output bytes.Buffer
		if _, err := handleChangeRecover(session, Invocation{Arguments: []string{string(operation.Id)}}, &output); err != nil {
			t.Fatal(err)
		}
		for _, expected := range []string{"Recovery diagnosis:", "Operation: " + string(operation.Id), "Recovery action: none; operation is already completed."} {
			if !strings.Contains(output.String(), expected) {
				t.Fatalf("output missing %q:\n%s", expected, output.String())
			}
		}
	}
	after, err := store.GetOperation(operation.Id)
	if err != nil {
		t.Fatal(err)
	}
	if after.State != before.State || after.UpdatedAt != before.UpdatedAt || string(after.Result) != string(before.Result) {
		t.Fatalf("terminal recovery mutated operation: before=%#v after=%#v", before, after)
	}
	selected, ok := session.CurrentChange()
	if !ok || selected.ChangeId() != other.ChangeId() {
		t.Fatalf("workspace recovery changed unrelated selection: %#v available=%t", selected, ok)
	}
}

func TestWave6RecoverRequiresExactlyOneOperationId(t *testing.T) {
	for _, arguments := range [][]string{nil, {"one", "two"}} {
		if _, err := handleChangeRecover(&Session{}, Invocation{Arguments: arguments}, &bytes.Buffer{}); !errors.Is(err, errInvalidArguments) {
			t.Fatalf("arguments=%v error=%v", arguments, err)
		}
	}
}

func wave6RecoveryFixture(t *testing.T) (authority.Store, *Session, change.Change) {
	t.Helper()
	projectId, err := project.GenerateProjectID()
	if err != nil {
		t.Fatal(err)
	}
	registration := project.Registration{ProjectId: projectId, RepositoryRoot: t.TempDir(), SchemaVersion: 1, CreatedAt: time.Now().UTC()}
	store, err := sqliteadapter.Open(t.TempDir(), registration)
	if err != nil {
		t.Fatal(err)
	}
	current, err := change.New("change-recovery", projectId, "diagnose recovery", time.Now().UTC())
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	snapshot, _ := workflow.CoreV0Snapshot()
	created := workflow.LifecycleEvent{EventType: workflow.EventChangeCreated, ChangeId: current.ChangeId(), ProjectId: projectId, ResultingState: current.State(), OccurredAt: current.CreatedAt(), Context: "test", Intent: current.Intent()}
	if err := store.CreateChange(current, snapshot, created); err != nil {
		store.Close()
		t.Fatal(err)
	}
	service, err := inspection.New(store)
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	return store, &Session{registration: registration, durableAuthority: store, durableInspection: service}, current
}
