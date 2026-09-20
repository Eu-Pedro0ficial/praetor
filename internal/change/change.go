// Package change contains the M0.2 Change aggregate and its deterministic
// lifecycle state machine.
package change

import (
	"fmt"
	"strings"
	"time"

	"github.com/Eu-Pedro0ficial/praetor/internal/project"
)

// ChangeId is the stable opaque identity of one Change. M0.2 deliberately
// does not define a public serialization format for this value.
type ChangeId string

// ChangeIntent is the normalized developer request associated with a Change.
type ChangeIntent string

// ChangeState is one explicit lifecycle state in the M0.2 state machine.
type ChangeState string

const (
	StateCreated     ChangeState = "created"
	StatePlanned     ChangeState = "planned"
	StateIsolated    ChangeState = "isolated"
	StateValidated   ChangeState = "validated"
	StateApproved    ChangeState = "approved"
	StateRejected    ChangeState = "rejected"
	StateAuditLocked ChangeState = "audit-locked"
)

// Change is the consistency boundary for one proposed software modification.
// Its state is private so lifecycle changes can occur only through Transition.
type Change struct {
	changeId  ChangeId
	projectId project.ProjectId
	intent    ChangeIntent
	state     ChangeState
	createdAt time.Time
	updatedAt time.Time
	revision  uint64
}

// Transition records the provenance produced by one successful state change.
type Transition struct {
	ChangeId       ChangeId
	ProjectId      project.ProjectId
	PreviousState  ChangeState
	ResultingState ChangeState
	OccurredAt     time.Time
	Context        string
}

// InvalidTransitionError describes a deterministic state-machine rejection.
type InvalidTransitionError struct {
	ChangeId ChangeId
	From     ChangeState
	To       ChangeState
}

func (transitionError InvalidTransitionError) Error() string {
	return fmt.Sprintf(
		"Change %q cannot transition from %q to %q",
		transitionError.ChangeId,
		transitionError.From,
		transitionError.To,
	)
}

// NewChangeId validates an opaque caller-supplied Change identity without
// imposing a canonical representation.
func NewChangeId(value string) (ChangeId, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", fmt.Errorf("ChangeId is required")
	}
	return ChangeId(trimmed), nil
}

// NewChangeIntent validates and normalizes a developer request.
func NewChangeIntent(value string) (ChangeIntent, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", fmt.Errorf("ChangeIntent is required")
	}
	return ChangeIntent(trimmed), nil
}

// ParseState resolves one of the authoritative M0.2 state names.
func ParseState(value string) (ChangeState, error) {
	state := ChangeState(strings.ToLower(strings.TrimSpace(value)))
	if state.isKnown() {
		return state, nil
	}
	return "", fmt.Errorf("unknown ChangeState %q", value)
}

// New creates a Change in the CREATED state.
func New(
	changeId ChangeId,
	projectId project.ProjectId,
	intent ChangeIntent,
	createdAt time.Time,
) (Change, error) {
	validatedChangeId, err := NewChangeId(string(changeId))
	if err != nil {
		return Change{}, err
	}
	if !projectId.IsValid() {
		return Change{}, fmt.Errorf("valid ProjectId is required")
	}
	if string(validatedChangeId) == string(projectId) {
		return Change{}, fmt.Errorf("ChangeId must not reuse ProjectId")
	}
	validatedIntent, err := NewChangeIntent(string(intent))
	if err != nil {
		return Change{}, err
	}
	if createdAt.IsZero() {
		return Change{}, fmt.Errorf("Change creation timestamp is required")
	}

	timestamp := createdAt.UTC()
	return Change{
		changeId:  validatedChangeId,
		projectId: projectId,
		intent:    validatedIntent,
		state:     StateCreated,
		createdAt: timestamp,
		updatedAt: timestamp,
		revision:  1,
	}, nil
}

// Rehydrate reconstructs a durable Change snapshot while revalidating every
// aggregate invariant. Persistence adapters must use this boundary rather
// than leaking storage rows into the domain.
func Rehydrate(
	changeId ChangeId,
	projectId project.ProjectId,
	intent ChangeIntent,
	state ChangeState,
	createdAt time.Time,
	updatedAt time.Time,
	revision uint64,
) (Change, error) {
	validatedChangeId, err := NewChangeId(string(changeId))
	if err != nil {
		return Change{}, err
	}
	if !projectId.IsValid() {
		return Change{}, fmt.Errorf("valid ProjectId is required")
	}
	if string(validatedChangeId) == string(projectId) {
		return Change{}, fmt.Errorf("ChangeId must not reuse ProjectId")
	}
	validatedIntent, err := NewChangeIntent(string(intent))
	if err != nil {
		return Change{}, err
	}
	if !state.isKnown() {
		return Change{}, fmt.Errorf("unknown ChangeState %q", state)
	}
	if createdAt.IsZero() || updatedAt.IsZero() {
		return Change{}, fmt.Errorf("Change timestamps are required")
	}
	createdAt = createdAt.UTC()
	updatedAt = updatedAt.UTC()
	if updatedAt.Before(createdAt) {
		return Change{}, fmt.Errorf("Change update timestamp precedes creation")
	}
	if revision == 0 {
		return Change{}, fmt.Errorf("ChangeRevision must be positive")
	}
	return Change{
		changeId: validatedChangeId, projectId: projectId, intent: validatedIntent,
		state: state, createdAt: createdAt, updatedAt: updatedAt, revision: revision,
	}, nil
}

// ChangeId returns the aggregate identity.
func (change Change) ChangeId() ChangeId {
	return change.changeId
}

// ProjectId returns the registered Project associated with this Change.
func (change Change) ProjectId() project.ProjectId {
	return change.projectId
}

// Intent returns the immutable developer request.
func (change Change) Intent() ChangeIntent {
	return change.intent
}

// State returns the current lifecycle state.
func (change Change) State() ChangeState {
	return change.state
}

// CreatedAt returns the creation provenance timestamp.
func (change Change) CreatedAt() time.Time {
	return change.createdAt
}

// UpdatedAt returns the most recent successful transition timestamp.
func (change Change) UpdatedAt() time.Time {
	return change.updatedAt
}

// Revision returns the optimistic concurrency revision of this snapshot.
func (change Change) Revision() uint64 { return change.revision }

// Transition enforces and applies one deterministic lifecycle transition.
func (change *Change) Transition(
	resultingState ChangeState,
	occurredAt time.Time,
	context string,
) (Transition, error) {
	if change == nil {
		return Transition{}, fmt.Errorf("Change is required")
	}
	if !resultingState.isKnown() {
		return Transition{}, fmt.Errorf("unknown ChangeState %q", resultingState)
	}
	if !isAllowed(change.state, resultingState) {
		return Transition{}, InvalidTransitionError{
			ChangeId: change.changeId,
			From:     change.state,
			To:       resultingState,
		}
	}
	if occurredAt.IsZero() {
		return Transition{}, fmt.Errorf("transition timestamp is required")
	}
	timestamp := occurredAt.UTC()
	if timestamp.Before(change.updatedAt) {
		return Transition{}, fmt.Errorf(
			"transition timestamp %s precedes current Change timestamp %s",
			timestamp.Format(time.RFC3339Nano),
			change.updatedAt.Format(time.RFC3339Nano),
		)
	}
	transitionContext := strings.TrimSpace(context)
	if transitionContext == "" {
		return Transition{}, fmt.Errorf("transition context is required")
	}

	transition := Transition{
		ChangeId:       change.changeId,
		ProjectId:      change.projectId,
		PreviousState:  change.state,
		ResultingState: resultingState,
		OccurredAt:     timestamp,
		Context:        transitionContext,
	}
	change.state = resultingState
	change.updatedAt = timestamp
	change.revision++
	return transition, nil
}

func (state ChangeState) isKnown() bool {
	switch state {
	case StateCreated,
		StatePlanned,
		StateIsolated,
		StateValidated,
		StateApproved,
		StateRejected,
		StateAuditLocked:
		return true
	default:
		return false
	}
}

func isAllowed(previousState ChangeState, resultingState ChangeState) bool {
	switch previousState {
	case StateCreated:
		return resultingState == StatePlanned || resultingState == StateRejected
	case StatePlanned:
		return resultingState == StateIsolated || resultingState == StateRejected
	case StateIsolated:
		return resultingState == StateValidated || resultingState == StateRejected
	case StateValidated:
		return resultingState == StateApproved || resultingState == StateRejected
	case StateApproved, StateRejected:
		return resultingState == StateAuditLocked
	case StateAuditLocked:
		return false
	default:
		return false
	}
}
