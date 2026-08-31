package workflow_test

import (
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"github.com/Eu-Pedro0ficial/praetor/internal/project"
	"github.com/Eu-Pedro0ficial/praetor/internal/workflow"
)

const workflowProjectId project.ProjectId = "01890c29-7a78-7abc-8def-0123456789ab"

func TestServiceRecordsCreationAndTransitionProvenance(t *testing.T) {
	var events []workflow.LifecycleEvent
	service := newTestService(t, func(event workflow.LifecycleEvent) error {
		events = append(events, event)
		return nil
	})

	createdChange, err := service.Create("change-1", "implement M0.2", "developer requested Change")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if createdChange.State() != change.StateCreated || createdChange.ProjectId() != workflowProjectId {
		t.Fatalf("created Change = state %q, project %q", createdChange.State(), createdChange.ProjectId())
	}

	plannedChange, err := service.Transition("change-1", change.StatePlanned, "planning established")
	if err != nil {
		t.Fatalf("Transition() error = %v", err)
	}
	if plannedChange.State() != change.StatePlanned {
		t.Fatalf("Transition() state = %q", plannedChange.State())
	}
	storedChange, err := service.Get("change-1")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if storedChange.State() != change.StatePlanned {
		t.Fatalf("Get() state = %q", storedChange.State())
	}

	if len(events) != 2 {
		t.Fatalf("recorded %d events, want 2", len(events))
	}
	if events[0].EventType != workflow.EventChangeCreated || events[0].PreviousState != "" || events[0].ResultingState != change.StateCreated {
		t.Fatalf("creation event = %#v", events[0])
	}
	if events[0].ChangeId != "change-1" || events[0].ProjectId != workflowProjectId || events[0].Intent != "implement M0.2" {
		t.Fatalf("creation identity/intent event = %#v", events[0])
	}
	if events[1].EventType != workflow.EventChangeTransition || events[1].PreviousState != change.StateCreated || events[1].ResultingState != change.StatePlanned {
		t.Fatalf("transition event = %#v", events[1])
	}
	if events[1].ChangeId != "change-1" || events[1].ProjectId != workflowProjectId || events[1].Context != "planning established" {
		t.Fatalf("transition identity/context event = %#v", events[1])
	}
	if !events[1].OccurredAt.Equal(plannedChange.UpdatedAt()) || events[1].OccurredAt.Before(events[0].OccurredAt) {
		t.Fatalf("transition timestamps are inconsistent: %#v", events)
	}
}

func TestServiceRollsBackWhenAuditRecordingFails(t *testing.T) {
	auditFailure := errors.New("audit unavailable")
	transitionRecording := false
	service := newTestService(t, func(event workflow.LifecycleEvent) error {
		if transitionRecording && event.EventType == workflow.EventChangeTransition {
			return auditFailure
		}
		return nil
	})

	if _, err := service.Create("change-rollback", "test rollback", "creation"); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	transitionRecording = true
	if _, err := service.Transition("change-rollback", change.StatePlanned, "planning"); !errors.Is(err, auditFailure) {
		t.Fatalf("Transition() error = %v, want wrapped audit failure", err)
	}

	storedChange, err := service.Get("change-rollback")
	if err != nil {
		t.Fatalf("Get() error = %v", err)
	}
	if storedChange.State() != change.StateCreated {
		t.Fatalf("failed audit mutated state to %q", storedChange.State())
	}
}

func TestServiceDoesNotStoreCreationWhenAuditRecordingFails(t *testing.T) {
	auditFailure := errors.New("audit unavailable")
	service := newTestService(t, func(workflow.LifecycleEvent) error {
		return auditFailure
	})

	if _, err := service.Create("change-not-stored", "test creation rollback", "creation"); !errors.Is(err, auditFailure) {
		t.Fatalf("Create() error = %v, want wrapped audit failure", err)
	}
	if _, err := service.Get("change-not-stored"); err == nil {
		t.Fatal("audit-failed Change was stored")
	}
}

func TestServiceRejectsForbiddenTransitionWithoutAuditEvent(t *testing.T) {
	var events []workflow.LifecycleEvent
	service := newTestService(t, func(event workflow.LifecycleEvent) error {
		events = append(events, event)
		return nil
	})
	if _, err := service.Create("change-forbidden", "test rejection", "creation"); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	_, err := service.Transition("change-forbidden", change.StateValidated, "skip lifecycle")
	var transitionError change.InvalidTransitionError
	if !errors.As(err, &transitionError) {
		t.Fatalf("Transition() error = %v, want InvalidTransitionError", err)
	}
	if len(events) != 1 {
		t.Fatalf("forbidden transition recorded an audit event: %#v", events)
	}
	storedChange, getError := service.Get("change-forbidden")
	if getError != nil || storedChange.State() != change.StateCreated {
		t.Fatalf("forbidden transition changed stored aggregate: state = %q, error = %v", storedChange.State(), getError)
	}
}

func TestServiceSerializesConcurrentTransitions(t *testing.T) {
	var eventMutex sync.Mutex
	var events []workflow.LifecycleEvent
	service := newTestService(t, func(event workflow.LifecycleEvent) error {
		eventMutex.Lock()
		defer eventMutex.Unlock()
		events = append(events, event)
		return nil
	})
	if _, err := service.Create("change-concurrent", "test concurrency", "creation"); err != nil {
		t.Fatalf("Create() error = %v", err)
	}

	const workers = 12
	var waitGroup sync.WaitGroup
	results := make(chan error, workers)
	for i := 0; i < workers; i++ {
		waitGroup.Add(1)
		go func(worker int) {
			defer waitGroup.Done()
			_, err := service.Transition(
				"change-concurrent",
				change.StatePlanned,
				fmt.Sprintf("worker %d", worker),
			)
			results <- err
		}(i)
	}
	waitGroup.Wait()
	close(results)

	successes := 0
	invalidTransitions := 0
	for err := range results {
		if err == nil {
			successes++
			continue
		}
		var transitionError change.InvalidTransitionError
		if errors.As(err, &transitionError) {
			invalidTransitions++
			continue
		}
		t.Fatalf("unexpected concurrent error: %v", err)
	}
	if successes != 1 || invalidTransitions != workers-1 {
		t.Fatalf("concurrent results: successes = %d, invalid = %d", successes, invalidTransitions)
	}
	storedChange, err := service.Get("change-concurrent")
	if err != nil || storedChange.State() != change.StatePlanned {
		t.Fatalf("stored state after concurrency = %q, error = %v", storedChange.State(), err)
	}
	eventMutex.Lock()
	defer eventMutex.Unlock()
	if len(events) != 2 {
		t.Fatalf("recorded %d events, want creation plus one transition", len(events))
	}
}

func TestServiceRejectsDuplicateAndUnknownChanges(t *testing.T) {
	service := newTestService(t, func(workflow.LifecycleEvent) error { return nil })
	if _, err := service.Create("change-duplicate", "test duplicate", "creation"); err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if _, err := service.Create("change-duplicate", "test duplicate", "creation"); err == nil {
		t.Fatal("Create() accepted a duplicate ChangeId")
	}
	if _, err := service.Get("missing-change"); err == nil {
		t.Fatal("Get() accepted an unknown ChangeId")
	}
	if _, err := service.Transition("missing-change", change.StatePlanned, "planning"); err == nil {
		t.Fatal("Transition() accepted an unknown ChangeId")
	}
}

func TestNewServiceRequiresExplicitDependencies(t *testing.T) {
	store := workflow.NewMemoryStore()
	recorder := func(workflow.LifecycleEvent) error { return nil }
	clock := func() time.Time { return time.Now().UTC() }
	tests := []struct {
		name      string
		store     *workflow.MemoryStore
		projectId project.ProjectId
		recorder  workflow.LifecycleRecorder
		clock     workflow.Clock
	}{
		{name: "store", projectId: workflowProjectId, recorder: recorder, clock: clock},
		{name: "ProjectId", store: store, recorder: recorder, clock: clock},
		{name: "recorder", store: store, projectId: workflowProjectId, clock: clock},
		{name: "clock", store: store, projectId: workflowProjectId, recorder: recorder},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := workflow.New(test.store, test.projectId, test.recorder, test.clock); err == nil {
				t.Fatalf("workflow.New() accepted missing %s dependency", test.name)
			}
		})
	}
}

func newTestService(t *testing.T, recorder workflow.LifecycleRecorder) *workflow.Service {
	t.Helper()
	base := time.Date(2026, time.August, 31, 12, 0, 0, 0, time.UTC)
	var tick atomic.Int64
	service, err := workflow.New(
		workflow.NewMemoryStore(),
		workflowProjectId,
		recorder,
		func() time.Time {
			return base.Add(time.Duration(tick.Add(1)) * time.Millisecond)
		},
	)
	if err != nil {
		t.Fatalf("workflow.New() error = %v", err)
	}
	return service
}
