// Package workflow provides the smallest M0.2 orchestration needed to make
// Change transitions auditable and transactional within a process.
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

// Service coordinates Change state mutation with append-oriented audit.
type Service struct {
	store     *MemoryStore
	projectId project.ProjectId
	recorder  LifecycleRecorder
	clock     Clock
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

// Get returns the current process-local Change snapshot.
func (service *Service) Get(changeId change.ChangeId) (change.Change, error) {
	return service.store.get(changeId)
}
