// Package workflow provides Core V0 Change orchestration over either the
// isolated memory adapter or M1.1 durable authority.
package workflow

import (
	"fmt"
	"strings"
	"time"

	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"github.com/Eu-Pedro0ficial/praetor/internal/project"
)

const (
	EventChangeCreated    = "CHANGE_CREATED"
	EventChangeTransition = "CHANGE_TRANSITION"
)

// Clock supplies lifecycle timestamps to the workflow service.
type Clock func() time.Time

// LifecycleRecorder persists one auditable Change lifecycle event.
type LifecycleRecorder func(LifecycleEvent) error

// LifecycleEvent is the application-level audit linkage for Change creation
// and transition provenance.
type LifecycleEvent struct {
	EventType      string
	ChangeId       change.ChangeId
	ProjectId      project.ProjectId
	PreviousState  change.ChangeState
	ResultingState change.ChangeState
	OccurredAt     time.Time
	Context        string
	Intent         change.ChangeIntent
}

// DurableStore is the capability-specific M1.1 Change authority boundary.
// Implementations atomically commit the Change revision and its lifecycle audit.
type DurableStore interface {
	CreateChange(change.Change, WorkflowSnapshot, LifecycleEvent) error
	CommitTransition(expectedRevision uint64, candidate change.Change, event LifecycleEvent) error
	GetChange(change.ChangeId) (change.Change, WorkflowSnapshot, error)
	ListChanges() ([]change.Change, error)
}

// Service coordinates Change state mutation with append-oriented audit.
type Service struct {
	store     *MemoryStore
	durable   DurableStore
	projectId project.ProjectId
	recorder  LifecycleRecorder
	clock     Clock
}

// NewDurable creates the M1.1 workflow service over durable authority.
func NewDurable(store DurableStore, projectId project.ProjectId, clock Clock) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("durable Change store dependency is not configured")
	}
	if !projectId.IsValid() {
		return nil, fmt.Errorf("valid ProjectId is required")
	}
	if clock == nil {
		return nil, fmt.Errorf("workflow clock dependency is not configured")
	}
	return &Service{durable: store, projectId: projectId, clock: clock}, nil
}

// New creates the minimal M0.2 workflow service from explicit dependencies.
func New(
	store *MemoryStore,
	projectId project.ProjectId,
	recorder LifecycleRecorder,
	clock Clock,
) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("Change store dependency is not configured")
	}
	if !projectId.IsValid() {
		return nil, fmt.Errorf("valid ProjectId is required")
	}
	if recorder == nil {
		return nil, fmt.Errorf("lifecycle recorder dependency is not configured")
	}
	if clock == nil {
		return nil, fmt.Errorf("workflow clock dependency is not configured")
	}
	return &Service{
		store:     store,
		projectId: projectId,
		recorder:  recorder,
		clock:     clock,
	}, nil
}

// Create establishes a Change in CREATED only after its lifecycle event is
// recorded successfully.
func (service *Service) Create(
	changeId change.ChangeId,
	intent change.ChangeIntent,
	context string,
) (change.Change, error) {
	creationContext := strings.TrimSpace(context)
	if creationContext == "" {
		return change.Change{}, fmt.Errorf("creation context is required")
	}
	createdChange, err := change.New(
		changeId,
		service.projectId,
		intent,
		service.clock(),
	)
	if err != nil {
		return change.Change{}, err
	}

	event := LifecycleEvent{
		EventType:      EventChangeCreated,
		ChangeId:       createdChange.ChangeId(),
		ProjectId:      createdChange.ProjectId(),
		ResultingState: createdChange.State(),
		OccurredAt:     createdChange.CreatedAt(),
		Context:        creationContext,
		Intent:         createdChange.Intent(),
	}
	if service.durable != nil {
		snapshot, snapshotError := CoreV0Snapshot()
		if snapshotError != nil {
			return change.Change{}, snapshotError
		}
		if err := service.durable.CreateChange(createdChange, snapshot, event); err != nil {
			return change.Change{}, err
		}
		return createdChange, nil
	}
	if err := service.store.create(createdChange, func() error {
		if recordError := service.recorder(event); recordError != nil {
			return fmt.Errorf("record Change creation: %w", recordError)
		}
		return nil
	}); err != nil {
		return change.Change{}, err
	}
	return createdChange, nil
}

// Transition applies one allowed state change only when its lifecycle event
// can be recorded successfully.
func (service *Service) Transition(
	changeId change.ChangeId,
	resultingState change.ChangeState,
	context string,
) (change.Change, error) {
	if service.durable != nil {
		current, snapshot, err := service.durable.GetChange(changeId)
		if err != nil {
			return change.Change{}, err
		}
		if snapshot.SchemaVersion() != SupportedWorkflowSchemaVersion {
			return change.Change{}, fmt.Errorf("%w: Change %q uses schema version %d", ErrUnsupportedWorkflowSchema, changeId, snapshot.SchemaVersion())
		}
		candidate := current
		transition, err := candidate.Transition(resultingState, service.clock(), context)
		if err != nil {
			return change.Change{}, err
		}
		event := LifecycleEvent{EventType: EventChangeTransition, ChangeId: transition.ChangeId, ProjectId: transition.ProjectId, PreviousState: transition.PreviousState, ResultingState: transition.ResultingState, OccurredAt: transition.OccurredAt, Context: transition.Context}
		if err := service.durable.CommitTransition(current.Revision(), candidate, event); err != nil {
			return change.Change{}, err
		}
		return candidate, nil
	}
	return service.store.update(changeId, func(candidate *change.Change) error {
		transition, err := candidate.Transition(
			resultingState,
			service.clock(),
			context,
		)
		if err != nil {
			return err
		}
		event := LifecycleEvent{
			EventType:      EventChangeTransition,
			ChangeId:       transition.ChangeId,
			ProjectId:      transition.ProjectId,
			PreviousState:  transition.PreviousState,
			ResultingState: transition.ResultingState,
			OccurredAt:     transition.OccurredAt,
			Context:        transition.Context,
		}
		if recordError := service.recorder(event); recordError != nil {
			return fmt.Errorf("record Change transition: %w", recordError)
		}
		return nil
	})
}

// Get returns the current Change snapshot from the configured authority.
func (service *Service) Get(changeId change.ChangeId) (change.Change, error) {
	if service.durable != nil {
		current, _, err := service.durable.GetChange(changeId)
		return current, err
	}
	return service.store.get(changeId)
}

// GetDurable returns the Change and exact WorkflowSnapshot authority.
func (service *Service) GetDurable(changeId change.ChangeId) (change.Change, WorkflowSnapshot, error) {
	if service == nil || service.durable == nil {
		return change.Change{}, WorkflowSnapshot{}, fmt.Errorf("durable Change authority is not configured")
	}
	return service.durable.GetChange(changeId)
}

// List returns durable Changes for the active Project without loading artifacts.
func (service *Service) List() ([]change.Change, error) {
	if service == nil || service.durable == nil {
		return nil, fmt.Errorf("durable Change authority is not configured")
	}
	return service.durable.ListChanges()
}
