package change_test

import (
	"errors"
	"testing"
	"time"

	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"github.com/Eu-Pedro0ficial/praetor/internal/project"
)

const testProjectId project.ProjectId = "01890c29-7a78-7abc-8def-0123456789ab"

var allStates = []change.ChangeState{
	change.StateCreated,
	change.StatePlanned,
	change.StateIsolated,
	change.StateValidated,
	change.StateApproved,
	change.StateRejected,
	change.StateAuditLocked,
}

func TestNewChangeEstablishesIdentityIntentProjectAndInitialState(t *testing.T) {
	createdAt := time.Date(2026, time.August, 31, 12, 0, 0, 123, time.FixedZone("test", -3*60*60))

	createdChange, err := change.New(
		change.ChangeId(" CHG-test-001 "),
		testProjectId,
		change.ChangeIntent(" prove the lifecycle "),
		createdAt,
	)
	if err != nil {
		t.Fatalf("change.New() error = %v", err)
	}

	if createdChange.ChangeId() != "CHG-test-001" {
		t.Fatalf("ChangeId() = %q, want %q", createdChange.ChangeId(), "CHG-test-001")
	}
	if createdChange.ProjectId() != testProjectId {
		t.Fatalf("ProjectId() = %q, want %q", createdChange.ProjectId(), testProjectId)
	}
	if createdChange.Intent() != "prove the lifecycle" {
		t.Fatalf("Intent() = %q, want normalized intent", createdChange.Intent())
	}
	if createdChange.State() != change.StateCreated {
		t.Fatalf("State() = %q, want %q", createdChange.State(), change.StateCreated)
	}
	if !createdChange.CreatedAt().Equal(createdAt) || createdChange.CreatedAt().Location() != time.UTC {
		t.Fatalf("CreatedAt() = %v, want equivalent UTC timestamp", createdChange.CreatedAt())
	}
	if !createdChange.UpdatedAt().Equal(createdChange.CreatedAt()) {
		t.Fatalf("UpdatedAt() = %v, want creation timestamp %v", createdChange.UpdatedAt(), createdChange.CreatedAt())
	}
}

func TestNewChangeRejectsInvalidDomainInputs(t *testing.T) {
	validTime := time.Date(2026, time.August, 31, 12, 0, 0, 0, time.UTC)
	tests := []struct {
		name      string
		changeId  change.ChangeId
		projectId project.ProjectId
		intent    change.ChangeIntent
		createdAt time.Time
	}{
		{name: "empty ChangeId", projectId: testProjectId, intent: "intent", createdAt: validTime},
		{name: "empty ProjectId", changeId: "change-1", intent: "intent", createdAt: validTime},
		{name: "invalid ProjectId", changeId: "change-1", projectId: "not-a-project-id", intent: "intent", createdAt: validTime},
		{name: "ProjectId reused as ChangeId", changeId: change.ChangeId(testProjectId), projectId: testProjectId, intent: "intent", createdAt: validTime},
		{name: "empty intent", changeId: "change-1", projectId: testProjectId, createdAt: validTime},
		{name: "zero timestamp", changeId: "change-1", projectId: testProjectId, intent: "intent"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := change.New(test.changeId, test.projectId, test.intent, test.createdAt); err == nil {
				t.Fatal("change.New() accepted invalid domain input")
			}
		})
	}
}

func TestParseStateAcceptsOnlyAuthoritativeM02States(t *testing.T) {
	for _, state := range allStates {
		parsed, err := change.ParseState("  " + string(state) + "  ")
		if err != nil {
			t.Fatalf("ParseState(%q) error = %v", state, err)
		}
		if parsed != state {
			t.Fatalf("ParseState(%q) = %q", state, parsed)
		}
	}

	parsed, err := change.ParseState("APPROVED")
	if err != nil || parsed != change.StateApproved {
		t.Fatalf("ParseState() should normalize case: state = %q, error = %v", parsed, err)
	}
	if _, err := change.ParseState("reviewing"); err == nil {
		t.Fatal("ParseState() accepted a state outside the M0.2 model")
	}
}

func TestTransitionMatrixAllowsOnlyAuthoritativeEdges(t *testing.T) {
	allowed := map[change.ChangeState]map[change.ChangeState]bool{
		change.StateCreated: {
			change.StatePlanned:  true,
			change.StateRejected: true,
		},
		change.StatePlanned: {
			change.StateIsolated: true,
			change.StateRejected: true,
		},
		change.StateIsolated: {
			change.StateValidated: true,
			change.StateRejected:  true,
		},
		change.StateValidated: {
			change.StateApproved: true,
			change.StateRejected: true,
		},
		change.StateApproved: {
			change.StateAuditLocked: true,
		},
		change.StateRejected: {
			change.StateAuditLocked: true,
		},
		change.StateAuditLocked: {},
	}

	for _, previousState := range allStates {
		for _, resultingState := range allStates {
			name := string(previousState) + "_to_" + string(resultingState)
			t.Run(name, func(t *testing.T) {
				candidate, transitionTime := changeInState(t, previousState)
				beforeUpdatedAt := candidate.UpdatedAt()

				provenance, err := candidate.Transition(resultingState, transitionTime, " test transition ")
				if allowed[previousState][resultingState] {
					if err != nil {
						t.Fatalf("Transition() error = %v", err)
					}
					if candidate.State() != resultingState {
						t.Fatalf("State() = %q, want %q", candidate.State(), resultingState)
					}
					if provenance.ChangeId != candidate.ChangeId() || provenance.ProjectId != testProjectId {
						t.Fatalf("transition identity provenance = %#v", provenance)
					}
					if provenance.PreviousState != previousState || provenance.ResultingState != resultingState {
						t.Fatalf("transition state provenance = %#v", provenance)
					}
					if provenance.Context != "test transition" || !provenance.OccurredAt.Equal(transitionTime) {
						t.Fatalf("transition context/time provenance = %#v", provenance)
					}
					return
				}

				var transitionError change.InvalidTransitionError
				if !errors.As(err, &transitionError) {
					t.Fatalf("Transition() error = %v, want InvalidTransitionError", err)
				}
				if transitionError.ChangeId != candidate.ChangeId() || transitionError.From != previousState || transitionError.To != resultingState {
					t.Fatalf("InvalidTransitionError = %#v", transitionError)
				}
				if candidate.State() != previousState || !candidate.UpdatedAt().Equal(beforeUpdatedAt) {
					t.Fatalf("forbidden transition mutated Change: state = %q, updated = %v", candidate.State(), candidate.UpdatedAt())
				}
			})
		}
	}
}

func TestTransitionValidationDoesNotMutateChange(t *testing.T) {
	tests := []struct {
		name      string
		timestamp time.Time
		context   string
	}{
		{name: "zero timestamp", context: "context"},
		{name: "timestamp before current state", timestamp: time.Date(2026, time.August, 31, 11, 59, 59, 0, time.UTC), context: "context"},
		{name: "empty context", timestamp: time.Date(2026, time.August, 31, 12, 0, 1, 0, time.UTC)},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			candidate, err := change.New(
				"change-validation",
				testProjectId,
				"test validation",
				time.Date(2026, time.August, 31, 12, 0, 0, 0, time.UTC),
			)
			if err != nil {
				t.Fatalf("change.New() error = %v", err)
			}
			beforeUpdatedAt := candidate.UpdatedAt()

			if _, err := candidate.Transition(change.StatePlanned, test.timestamp, test.context); err == nil {
				t.Fatal("Transition() accepted invalid provenance")
			}
			if candidate.State() != change.StateCreated || !candidate.UpdatedAt().Equal(beforeUpdatedAt) {
				t.Fatalf("invalid provenance mutated Change: state = %q, updated = %v", candidate.State(), candidate.UpdatedAt())
			}
		})
	}

	var missingChange *change.Change
	if _, err := missingChange.Transition(change.StatePlanned, time.Now(), "context"); err == nil {
		t.Fatal("Transition() accepted a nil Change")
	}
}

func changeInState(t *testing.T, state change.ChangeState) (change.Change, time.Time) {
	t.Helper()
	base := time.Date(2026, time.August, 31, 12, 0, 0, 0, time.UTC)
	candidate, err := change.New("matrix-change", testProjectId, "test matrix", base)
	if err != nil {
		t.Fatalf("change.New() error = %v", err)
	}

	paths := map[change.ChangeState][]change.ChangeState{
		change.StateCreated:     {},
		change.StatePlanned:     {change.StatePlanned},
		change.StateIsolated:    {change.StatePlanned, change.StateIsolated},
		change.StateValidated:   {change.StatePlanned, change.StateIsolated, change.StateValidated},
		change.StateApproved:    {change.StatePlanned, change.StateIsolated, change.StateValidated, change.StateApproved},
		change.StateRejected:    {change.StateRejected},
		change.StateAuditLocked: {change.StateRejected, change.StateAuditLocked},
	}
	for i, nextState := range paths[state] {
		if _, err := candidate.Transition(nextState, base.Add(time.Duration(i+1)*time.Second), "prepare matrix state"); err != nil {
			t.Fatalf("prepare %q state: %v", state, err)
		}
	}
	return candidate, candidate.UpdatedAt().Add(time.Second)
}
