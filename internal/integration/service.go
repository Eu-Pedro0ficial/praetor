package integration

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Eu-Pedro0ficial/praetor/internal/approval"
	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"github.com/Eu-Pedro0ficial/praetor/internal/project"
	"github.com/Eu-Pedro0ficial/praetor/internal/proposal"
	"github.com/Eu-Pedro0ficial/praetor/internal/source"
	"github.com/Eu-Pedro0ficial/praetor/internal/verification"
	"github.com/Eu-Pedro0ficial/praetor/internal/workflow"
)

const (
	EventCanonicalApplicationStarted   = "CANONICAL_APPLICATION_STARTED"
	EventCanonicalApplicationCompleted = "CANONICAL_APPLICATION_COMPLETED"
	EventCanonicalApplicationFailed    = "CANONICAL_APPLICATION_FAILED"
	EventChangeClosureRecorded         = "CHANGE_CLOSURE_RECORDED"
)

// CanonicalSourcePort is the narrow M0.8 outbound boundary for checking and
// atomically applying one exact Git patch to the canonical working tree.
type CanonicalSourcePort interface {
	Preflight(ApplicationRequest) error
	Apply(ApplicationRequest) (CanonicalProof, bool, error)
}

// IntegrityVerifier proves that the retained workspace still yields the
// approved patch and that canonical source still equals the approved base.
type IntegrityVerifier func(proposal.Proposal) error

// RepositoryInspector captures deterministic canonical source state.
type RepositoryInspector func(project.ProjectId, string) (source.SourceSnapshot, error)

// LifecycleEvent carries bounded M0.8 application/closure provenance.
type LifecycleEvent struct {
	EventType                 string
	Change                    change.Change
	Proposal                  proposal.Proposal
	Verification              verification.Result
	Decision                  approval.HumanDecision
	Result                    Result
	CanonicalMutationOccurred bool
	FailureStage              string
	Failure                   string
	OccurredAt                time.Time
}

// LifecycleRecorder appends one M0.8 event.
type LifecycleRecorder func(LifecycleEvent) error

// Clock supplies application and closure timestamps.
type Clock func() time.Time

// Service enforces the M0.8 coherence gate, canonical application, proof,
// append-oriented audit ordering, and existing terminal state transitions.
type Service struct {
	workflow  *workflow.Service
	integrity IntegrityVerifier
	canonical CanonicalSourcePort
	inspector RepositoryInspector
	recorder  LifecycleRecorder
	clock     Clock
}

// New constructs the M0.8 integration service from explicit dependencies.
func New(
	changeWorkflow *workflow.Service,
	integrity IntegrityVerifier,
	canonical CanonicalSourcePort,
	inspector RepositoryInspector,
	recorder LifecycleRecorder,
	clock Clock,
) (*Service, error) {
	if changeWorkflow == nil {
		return nil, fmt.Errorf("Change workflow dependency is not configured")
	}
	if integrity == nil {
		return nil, fmt.Errorf("proposal integrity dependency is not configured")
	}
	if canonical == nil {
		return nil, fmt.Errorf("canonical source application dependency is not configured")
	}
	if inspector == nil {
		return nil, fmt.Errorf("canonical source inspector dependency is not configured")
	}
	if recorder == nil {
		return nil, fmt.Errorf("canonical integration recorder dependency is not configured")
	}
	if clock == nil {
		return nil, fmt.Errorf("canonical integration clock dependency is not configured")
	}
	return &Service{
		workflow:  changeWorkflow,
		integrity: integrity,
		canonical: canonical,
		inspector: inspector,
		recorder:  recorder,
		clock:     clock,
	}, nil
}

// Apply publishes the exact approved patch to the canonical working tree.
// The start event is durable before mutation. A successful proof event is
// durable before approved -> audit-locked. Git commit and push are absent.
func (service *Service) Apply(
	ctx context.Context,
	currentChange change.Change,
	currentProposal proposal.Proposal,
	verificationResult verification.Result,
	decision approval.HumanDecision,
) (Result, change.Change, error) {
	var result Result
	if service == nil {
		return result, currentChange, fmt.Errorf("canonical integration service is required")
	}
	if ctx == nil {
		return result, currentChange, fmt.Errorf("canonical application context is required")
	}
	if err := ctx.Err(); err != nil {
		return result, currentChange, fmt.Errorf("canonical application cancelled before preflight: %w", err)
	}
	if currentChange.State() != change.StateApproved {
		return result, currentChange, fmt.Errorf("Change %q must be approved before canonical application", currentChange.ChangeId())
	}
	if err := validateDisposition(currentChange, currentProposal, verificationResult, decision, approval.DecisionApprove); err != nil {
		return result, currentChange, err
	}
	request := ApplicationRequest{Proposal: currentProposal}
	if err := service.integrity(currentProposal); err != nil {
		return result, currentChange, fmt.Errorf("pre-application proposal integrity failed: %w", err)
	}
	if err := service.canonical.Preflight(request); err != nil {
		return result, currentChange, fmt.Errorf("canonical application preflight failed: %w", err)
	}
	startedAt := service.clock().UTC()
	if startedAt.IsZero() {
		return result, currentChange, fmt.Errorf("canonical application start timestamp is required")
	}
	baseEvent := LifecycleEvent{
		Change:       currentChange,
		Proposal:     currentProposal,
		Verification: verificationResult,
		Decision:     decision,
	}
	startEvent := baseEvent
	startEvent.EventType = EventCanonicalApplicationStarted
	startEvent.OccurredAt = startedAt
	if err := service.recorder(startEvent); err != nil {
		return result, currentChange, fmt.Errorf("record canonical application start: %w", err)
	}
	fail := func(stage string, primary error, mutated bool) (Result, change.Change, error) {
		if mutated {
			result.canonicalMutationOccurred = true
		}
		failureEvent := baseEvent
		failureEvent.EventType = EventCanonicalApplicationFailed
		failureEvent.Result = result
		failureEvent.CanonicalMutationOccurred = mutated
		failureEvent.FailureStage = stage
		failureEvent.Failure = boundedFailure(primary, currentProposal)
		failureEvent.OccurredAt = service.clock().UTC()
		recordError := service.recorder(failureEvent)
		if recordError != nil {
			recordError = fmt.Errorf("record canonical application failure: %w", recordError)
		}
		return result, currentChange, errors.Join(primary, recordError)
	}

	if err := validateDisposition(currentChange, currentProposal, verificationResult, decision, approval.DecisionApprove); err != nil {
		return fail("coherence-recheck", err, false)
	}
	if err := service.integrity(currentProposal); err != nil {
		return fail("integrity-recheck", fmt.Errorf("pre-mutation proposal integrity failed: %w", err), false)
	}
	if err := ctx.Err(); err != nil {
		return fail("cancellation", fmt.Errorf("canonical application cancelled before mutation: %w", err), false)
	}
	proof, mutated, err := service.canonical.Apply(request)
	if err != nil {
		return fail("atomic-application", fmt.Errorf("apply approved PatchArtifact: %w", err), mutated)
	}
	if !mutated {
		return fail("atomic-application", fmt.Errorf("canonical adapter returned success without mutation"), false)
	}
	result.canonicalMutationOccurred = true
	resultingSource, err := service.inspector(currentChange.ProjectId(), currentProposal.CanonicalSource().RepositoryRoot())
	if err != nil {
		return fail("post-application-inspection", fmt.Errorf("inspect canonical application result: %w", err), true)
	}
	result, err = newResult(
		currentChange,
		currentProposal,
		verificationResult,
		decision,
		proof,
		resultingSource,
		service.clock(),
	)
	if err != nil {
		return fail("post-application-proof", err, true)
	}
	completedEvent := baseEvent
	completedEvent.EventType = EventCanonicalApplicationCompleted
	completedEvent.Result = result
	completedEvent.CanonicalMutationOccurred = true
	completedEvent.OccurredAt = result.CompletedAt()
	if err := service.recorder(completedEvent); err != nil {
		return fail("completion-audit", fmt.Errorf("record canonical application completion: %w", err), true)
	}
	terminal, err := service.workflow.Transition(
		currentChange.ChangeId(),
		change.StateAuditLocked,
		"approved PatchArtifact applied and deterministically proven in canonical working tree",
	)
	if err != nil {
		return result, currentChange, err
	}
	return result, terminal, nil
}

// CloseRejected records that canonical source is still unchanged, then uses
// the existing rejected -> audit-locked edge. It never invokes Apply.
func (service *Service) CloseRejected(
	ctx context.Context,
	currentChange change.Change,
	currentProposal proposal.Proposal,
	verificationResult verification.Result,
	decision approval.HumanDecision,
) (change.Change, error) {
	if service == nil {
		return currentChange, fmt.Errorf("canonical integration service is required")
	}
	if ctx == nil {
		return currentChange, fmt.Errorf("rejected Change closure context is required")
	}
	if err := ctx.Err(); err != nil {
		return currentChange, fmt.Errorf("rejected Change closure cancelled: %w", err)
	}
	if currentChange.State() != change.StateRejected {
		return currentChange, fmt.Errorf("Change %q must be rejected before rejection closure", currentChange.ChangeId())
	}
	if err := validateDisposition(currentChange, currentProposal, verificationResult, decision, approval.DecisionReject); err != nil {
		return currentChange, err
	}
	if err := service.integrity(currentProposal); err != nil {
		return currentChange, fmt.Errorf("rejected Change canonical integrity failed: %w", err)
	}
	event := LifecycleEvent{
		EventType:                 EventChangeClosureRecorded,
		Change:                    currentChange,
		Proposal:                  currentProposal,
		Verification:              verificationResult,
		Decision:                  decision,
		CanonicalMutationOccurred: false,
		OccurredAt:                service.clock().UTC(),
	}
	if event.OccurredAt.IsZero() {
		return currentChange, fmt.Errorf("rejected Change closure timestamp is required")
	}
	if err := service.recorder(event); err != nil {
		return currentChange, fmt.Errorf("record rejected Change closure: %w", err)
	}
	if err := service.integrity(currentProposal); err != nil {
		return currentChange, fmt.Errorf("rejected Change integrity changed before audit lock: %w", err)
	}
	if err := ctx.Err(); err != nil {
		return currentChange, fmt.Errorf("rejected Change closure cancelled before audit lock: %w", err)
	}
	return service.workflow.Transition(
		currentChange.ChangeId(),
		change.StateAuditLocked,
		"rejected Change closed with canonical source unchanged",
	)
}

func validateDisposition(
	currentChange change.Change,
	currentProposal proposal.Proposal,
	verificationResult verification.Result,
	decision approval.HumanDecision,
	expected approval.DecisionKind,
) error {
	if err := verification.ValidateResultForDecision(currentChange, currentProposal, verificationResult); err != nil {
		return fmt.Errorf("canonical integration evidence coherence failed: %w", err)
	}
	artifact, _ := currentProposal.PatchArtifact()
	evidence := verificationResult.EvidenceSet()
	if decision.Kind() != expected || decision.RequestedState() != currentChange.State() {
		return fmt.Errorf("human decision does not authorize current Change disposition")
	}
	if decision.ProjectId() != currentChange.ProjectId() ||
		decision.ChangeId() != currentChange.ChangeId() ||
		decision.WorkspaceId() != currentProposal.Workspace().WorkspaceId() ||
		decision.BaseRevision() != artifact.BaseRevision() ||
		decision.SourceStateDigest() != artifact.SourceStateDigest() ||
		decision.PatchDigest() != artifact.PatchDigest() ||
		decision.VerificationAttemptId() != evidence.VerificationAttemptId() ||
		decision.EvidenceSetId() != evidence.Id() ||
		decision.EvidenceCount() != len(evidence.Evidence()) ||
		decision.ChangedPathCount() != len(artifact.ChangedPaths()) {
		return fmt.Errorf("human decision linkage does not match retained proposal and EvidenceSet")
	}
	return nil
}

func boundedFailure(err error, currentProposal proposal.Proposal) string {
	if err == nil {
		return ""
	}
	value := strings.TrimSpace(err.Error())
	workspace := currentProposal.Workspace()
	value = strings.ReplaceAll(value, workspace.Root(), "[proposal-workspace]")
	value = strings.ReplaceAll(value, workspace.CanonicalRoot(), "[canonical-source]")
	value = strings.ToValidUTF8(value, "�")
	value = strings.Map(func(character rune) rune {
		if character < 0x20 || character == 0x7f {
			return ' '
		}
		return character
	}, value)
	if len(value) > 512 {
		var bounded strings.Builder
		bounded.Grow(512)
		for _, character := range value {
			width := utf8.RuneLen(character)
			if width < 0 || bounded.Len()+width > 512 {
				break
			}
			bounded.WriteRune(character)
		}
		value = bounded.String()
	}
	return strings.TrimSpace(value)
}
