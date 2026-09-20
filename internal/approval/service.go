package approval

import (
	"context"
	"fmt"
	"time"

	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"github.com/Eu-Pedro0ficial/praetor/internal/policy"
	"github.com/Eu-Pedro0ficial/praetor/internal/proposal"
	"github.com/Eu-Pedro0ficial/praetor/internal/verification"
	"github.com/Eu-Pedro0ficial/praetor/internal/workflow"
)

const EventHumanDecisionRecorded = "HUMAN_DECISION_RECORDED"

// Port is the narrow application-owned inbound boundary used by the local
// interactive command adapter. It contains no terminal-library concepts.
type Port interface {
	PrepareDecision(
		context.Context,
		change.Change,
		proposal.Proposal,
		verification.Result,
		policy.BundleDecision,
		DecisionKind,
		string,
	) (HumanDecision, error)
	Decide(
		context.Context,
		change.Change,
		proposal.Proposal,
		verification.Result,
		policy.BundleDecision,
		DecisionKind,
		string,
	) (HumanDecision, change.Change, error)
}

// IntegrityVerifier reuses the existing retained-patch and canonical-source
// guard without duplicating Git inspection in the approval capability.
type IntegrityVerifier func(proposal.Proposal) error

// LifecycleEvent carries one bounded human decision to the Audit Port.
type LifecycleEvent struct {
	EventType  string
	Decision   HumanDecision
	OccurredAt time.Time
}

// LifecycleRecorder appends one human-decision event.
type LifecycleRecorder func(LifecycleEvent) error

// Clock supplies decision timestamps.
type Clock func() time.Time

// Service validates and records explicit local-human authorization before it
// asks the existing workflow service to perform its separately audited state
// transition. It never mutates canonical source or locks the audit lifecycle.
type Service struct {
	workflow  *workflow.Service
	integrity IntegrityVerifier
	recorder  LifecycleRecorder
	clock     Clock
}

// New constructs the M0.7 approval gate from explicit dependencies.
func New(
	changeWorkflow *workflow.Service,
	integrity IntegrityVerifier,
	recorder LifecycleRecorder,
	clock Clock,
) (*Service, error) {
	if changeWorkflow == nil {
		return nil, fmt.Errorf("Change workflow dependency is not configured")
	}
	if integrity == nil {
		return nil, fmt.Errorf("proposal integrity dependency is not configured")
	}
	if recorder == nil {
		return nil, fmt.Errorf("human decision recorder dependency is not configured")
	}
	if clock == nil {
		return nil, fmt.Errorf("human decision clock dependency is not configured")
	}
	return &Service{
		workflow:  changeWorkflow,
		integrity: integrity,
		recorder:  recorder,
		clock:     clock,
	}, nil
}

// Decide records exactly one explicit APPROVE or REJECT choice. Decision
// audit is written before the existing audited state transition so any audit
// failure leaves Change authority in VALIDATED.
func (service *Service) Decide(
	ctx context.Context,
	currentChange change.Change,
	currentProposal proposal.Proposal,
	verificationResult verification.Result,
	policyDecision policy.BundleDecision,
	kind DecisionKind,
	rationaleValue string,
) (HumanDecision, change.Change, error) {
	decision, err := service.PrepareDecision(ctx, currentChange, currentProposal, verificationResult, policyDecision, kind, rationaleValue)
	if err != nil {
		return HumanDecision{}, currentChange, err
	}
	if err := service.recorder(LifecycleEvent{
		EventType:  EventHumanDecisionRecorded,
		Decision:   decision,
		OccurredAt: decision.OccurredAt(),
	}); err != nil {
		return HumanDecision{}, currentChange, fmt.Errorf("record human decision: %w", err)
	}
	transitioned, err := service.workflow.Transition(
		currentChange.ChangeId(),
		decision.RequestedState(),
		transitionContext(decision),
	)
	if err != nil {
		return decision, currentChange, err
	}
	return decision, transitioned, nil
}

// PrepareDecision validates and constructs a decision without publishing
// audit or Change authority. Durable callers commit all required records
// atomically after this method succeeds.
func (service *Service) PrepareDecision(
	ctx context.Context,
	currentChange change.Change,
	currentProposal proposal.Proposal,
	verificationResult verification.Result,
	policyDecision policy.BundleDecision,
	kind DecisionKind,
	rationaleValue string,
) (HumanDecision, error) {
	if service == nil {
		return HumanDecision{}, fmt.Errorf("approval service is required")
	}
	if ctx == nil {
		return HumanDecision{}, fmt.Errorf("human decision context is required")
	}
	if err := ctx.Err(); err != nil {
		return HumanDecision{}, fmt.Errorf("human decision cancelled before authorization: %w", err)
	}
	if currentChange.State() != change.StateValidated {
		return HumanDecision{}, fmt.Errorf(
			"Change %q must be validated before a human decision",
			currentChange.ChangeId(),
		)
	}
	if _, err := resultingState(kind); err != nil {
		return HumanDecision{}, err
	}
	rationale, err := NewRationale(rationaleValue)
	if err != nil {
		return HumanDecision{}, err
	}
	if err := verification.ValidateResultForDecision(currentChange, currentProposal, verificationResult); err != nil {
		return HumanDecision{}, err
	}
	if err := service.integrity(currentProposal); err != nil {
		return HumanDecision{}, fmt.Errorf("pre-decision proposal integrity failed: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return HumanDecision{}, fmt.Errorf("human decision cancelled before authorization: %w", err)
	}
	decision, err := newHumanDecision(
		currentChange,
		currentProposal,
		verificationResult,
		policyDecision,
		kind,
		rationale,
		service.clock(),
	)
	if err != nil {
		return HumanDecision{}, err
	}
	if err := service.integrity(currentProposal); err != nil {
		return HumanDecision{}, fmt.Errorf("post-decision proposal integrity failed before authority commit: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return HumanDecision{}, fmt.Errorf("human decision cancelled before authority commit: %w", err)
	}
	return decision, nil
}

func transitionContext(decision HumanDecision) string {
	if decision.Kind() == DecisionApprove {
		return "explicit local human approval recorded"
	}
	return "explicit local human rejection recorded"
}
