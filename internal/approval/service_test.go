package approval

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"github.com/Eu-Pedro0ficial/praetor/internal/project"
	"github.com/Eu-Pedro0ficial/praetor/internal/proposal"
	"github.com/Eu-Pedro0ficial/praetor/internal/verification"
	"github.com/Eu-Pedro0ficial/praetor/internal/workflow"
)

const approvalTestProjectId project.ProjectId = "01890f47-9f20-7cc1-98c8-1123456789ab"

func TestNewRequiresExplicitApprovalDependencies(t *testing.T) {
	changeWorkflow := newApprovalTestWorkflow(t)
	integrity := func(proposal.Proposal) error { return nil }
	recorder := func(LifecycleEvent) error { return nil }
	clock := func() time.Time { return time.Now().UTC() }
	tests := []struct {
		name      string
		workflow  *workflow.Service
		integrity IntegrityVerifier
		recorder  LifecycleRecorder
		clock     Clock
	}{
		{name: "workflow", integrity: integrity, recorder: recorder, clock: clock},
		{name: "integrity", workflow: changeWorkflow, recorder: recorder, clock: clock},
		{name: "recorder", workflow: changeWorkflow, integrity: integrity, clock: clock},
		{name: "clock", workflow: changeWorkflow, integrity: integrity, recorder: recorder},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := New(test.workflow, test.integrity, test.recorder, test.clock); err == nil {
				t.Fatalf("New() accepted missing %s dependency", test.name)
			}
		})
	}
}

func TestDecideRejectsCancellationInvalidStateAndInvalidDecisionBeforeAudit(t *testing.T) {
	var events []LifecycleEvent
	service, err := New(
		newApprovalTestWorkflow(t),
		func(proposal.Proposal) error { return nil },
		func(event LifecycleEvent) error {
			events = append(events, event)
			return nil
		},
		func() time.Time { return time.Now().UTC() },
	)
	if err != nil {
		t.Fatal(err)
	}
	created, err := change.New("change-invalid-decision", approvalTestProjectId, "test explicit decisions", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := service.Decide(cancelled, created, proposal.Proposal{}, verification.Result{}, DecisionApprove, ""); err == nil || !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled Decide() error = %v", err)
	}
	prevalidated := []change.Change{created}
	planned := created
	if _, err := planned.Transition(change.StatePlanned, created.UpdatedAt().Add(time.Second), "prepare planned Change"); err != nil {
		t.Fatal(err)
	}
	prevalidated = append(prevalidated, planned)
	isolated := planned
	if _, err := isolated.Transition(change.StateIsolated, planned.UpdatedAt().Add(time.Second), "prepare isolated Change"); err != nil {
		t.Fatal(err)
	}
	prevalidated = append(prevalidated, isolated)
	for _, candidate := range prevalidated {
		if _, _, err := service.Decide(context.Background(), candidate, proposal.Proposal{}, verification.Result{}, DecisionApprove, ""); err == nil || !strings.Contains(err.Error(), "must be validated") {
			t.Fatalf("Decide() from %q error = %v", candidate.State(), err)
		}
	}
	validated, transitionTime := approvalChangeInValidatedState(t)
	if _, _, err := service.Decide(context.Background(), validated, proposal.Proposal{}, verification.Result{}, "", ""); err == nil || !strings.Contains(err.Error(), "unknown human decision") {
		t.Fatalf("invalid-kind Decide() error = %v", err)
	}
	if len(events) != 0 {
		t.Fatalf("invalid decisions reached audit: %#v", events)
	}
	if !validated.UpdatedAt().Equal(transitionTime) {
		t.Fatal("invalid decision mutated the supplied Change value")
	}
}

func newApprovalTestWorkflow(t *testing.T) *workflow.Service {
	t.Helper()
	service, err := workflow.New(
		workflow.NewMemoryStore(),
		approvalTestProjectId,
		func(workflow.LifecycleEvent) error { return nil },
		func() time.Time { return time.Now().UTC() },
	)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func approvalChangeInValidatedState(t *testing.T) (change.Change, time.Time) {
	t.Helper()
	base := time.Date(2026, time.September, 6, 12, 0, 0, 0, time.UTC)
	current, err := change.New("change-validated", approvalTestProjectId, "validated decision", base)
	if err != nil {
		t.Fatal(err)
	}
	for index, state := range []change.ChangeState{change.StatePlanned, change.StateIsolated, change.StateValidated} {
		if _, err := current.Transition(state, base.Add(time.Duration(index+1)*time.Second), "prepare validated Change"); err != nil {
			t.Fatal(err)
		}
	}
	return current, current.UpdatedAt()
}
