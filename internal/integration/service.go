package integration

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/Eu-Pedro0ficial/praetor/internal/approval"
	"github.com/Eu-Pedro0ficial/praetor/internal/artifact"
	"github.com/Eu-Pedro0ficial/praetor/internal/audit"
	"github.com/Eu-Pedro0ficial/praetor/internal/authority"
	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"github.com/Eu-Pedro0ficial/praetor/internal/policy"
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
	workflow       *workflow.Service
	integrity      IntegrityVerifier
	canonical      CanonicalSourcePort
	inspector      RepositoryInspector
	recorder       LifecycleRecorder
	clock          Clock
	durable        authority.Store
	repositoryRoot string
}

// NewDurable composes M1.1 terminal authority over the same M0.8 validation
// path. The concrete store remains behind the application-owned authority port.
func NewDurable(changeWorkflow *workflow.Service, integrity IntegrityVerifier, canonical CanonicalSourcePort, inspector RepositoryInspector, recorder LifecycleRecorder, clock Clock, store authority.Store, repositoryRoot string) (*Service, error) {
	service, err := New(changeWorkflow, integrity, canonical, inspector, recorder, clock)
	if err != nil {
		return nil, err
	}
	if store == nil || strings.TrimSpace(repositoryRoot) == "" {
		return nil, fmt.Errorf("durable canonical authority dependencies are incomplete")
	}
	service.durable = store
	service.repositoryRoot = repositoryRoot
	return service, nil
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
	policyDecision policy.BundleDecision,
	decision approval.HumanDecision,
) (Result, change.Change, error) {
	var result Result
	var err error
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
	if err := validateDisposition(currentChange, currentProposal, verificationResult, policyDecision, decision, approval.DecisionApprove); err != nil {
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

	if err := validateDisposition(currentChange, currentProposal, verificationResult, policyDecision, decision, approval.DecisionApprove); err != nil {
		return fail("coherence-recheck", err, false)
	}
	if err := service.integrity(currentProposal); err != nil {
		return fail("integrity-recheck", fmt.Errorf("pre-mutation proposal integrity failed: %w", err), false)
	}
	if err := ctx.Err(); err != nil {
		return fail("cancellation", fmt.Errorf("canonical application cancelled before mutation: %w", err), false)
	}
	var proof CanonicalProof
	var mutated bool
	var terminal change.Change
	if service.durable != nil {
		coordinated, ok := service.canonical.(coordinatedCompletion)
		if !ok {
			return fail("atomic-application", fmt.Errorf("durable canonical source does not support locked authority finalization"), false)
		}
		proof, mutated, err = coordinated.ApplyCoordinated(request, func(operation authority.Operation, lockedProof CanonicalProof) error {
			resultingSource, inspectError := service.inspector(currentChange.ProjectId(), currentProposal.CanonicalSource().RepositoryRoot())
			if inspectError != nil {
				return fmt.Errorf("inspect canonical application result: %w", inspectError)
			}
			result, inspectError = newResult(currentChange, currentProposal, verificationResult, decision, lockedProof, resultingSource, service.clock())
			if inspectError != nil {
				return inspectError
			}
			terminal, inspectError = service.commitCanonicalCompletion(currentChange, currentProposal, verificationResult, decision, result, operation)
			return inspectError
		})
	} else {
		proof, mutated, err = service.canonical.Apply(request)
	}
	if err != nil {
		return fail("atomic-application", fmt.Errorf("apply approved PatchArtifact: %w", err), mutated)
	}
	if !mutated {
		return fail("atomic-application", fmt.Errorf("canonical adapter returned success without mutation"), false)
	}
	result.canonicalMutationOccurred = true
	if service.durable != nil {
		return result, terminal, nil
	}
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
	terminal, err = service.workflow.Transition(
		currentChange.ChangeId(),
		change.StateAuditLocked,
		"approved PatchArtifact applied and deterministically proven in canonical working tree",
	)
	if err != nil {
		return result, currentChange, err
	}
	return result, terminal, nil
}

func (service *Service) commitCanonicalCompletion(current change.Change, currentProposal proposal.Proposal, verificationResult verification.Result, decision approval.HumanDecision, result Result, operation authority.Operation) (change.Change, error) {
	candidate := current
	transition, err := candidate.Transition(change.StateAuditLocked, result.CompletedAt(), "approved PatchArtifact applied and deterministically proven in canonical working tree")
	if err != nil {
		return change.Change{}, err
	}
	payload, err := json.Marshal(map[string]any{"operation_id": operation.Id, "workspace_id": result.WorkspaceId(), "base_revision": result.BaseRevision(), "source_state_digest": result.SourceStateDigest(), "resulting_source_state_digest": result.ResultingSourceStateDigest(), "patch_digest": result.PatchDigest(), "verification_attempt_id": result.VerificationAttemptId(), "evidence_set_id": result.EvidenceSetId(), "decision_kind": result.DecisionKind(), "changed_paths": result.ChangedPaths(), "canonical_head": result.CanonicalHead(), "index_unchanged": result.IndexUnchanged(), "result_digest": result.ResultDigest()})
	if err != nil {
		return change.Change{}, err
	}
	id, err := artifact.GenerateId()
	if err != nil {
		return change.Change{}, err
	}
	item, err := artifact.New(id, candidate.ProjectId(), candidate.ChangeId(), artifact.KindApplicationResult, 1, 1, "application/json", result.CompletedAt(), artifact.Producer{Component: "praetor-runtime", OperationId: string(operation.Id)}, payload, false)
	if err != nil {
		return change.Change{}, err
	}
	completedMetadata, err := durableIntegrationMetadata(currentProposal, verificationResult, decision)
	if err != nil {
		return change.Change{}, err
	}
	completedMetadata["operation_id"] = operation.Id
	completedMetadata["canonical_mutation_occurred"] = true
	completedMetadata["event_timestamp"] = result.CompletedAt().UTC().Format(time.RFC3339Nano)
	completedMetadata["disposition"] = "completed"
	completedMetadata["resulting_source_state_digest"] = string(result.ResultingSourceStateDigest())
	completedMetadata["canonical_head"] = result.CanonicalHead()
	completedMetadata["index_unchanged"] = result.IndexUnchanged()
	completedMetadata["canonical_result_digest"] = result.ResultDigest()
	completed, err := audit.NewEvent(EventCanonicalApplicationCompleted, string(candidate.ProjectId()), string(candidate.ChangeId()), service.repositoryRoot, completedMetadata, result.CompletedAt())
	if err != nil {
		return change.Change{}, err
	}
	lifecycle, err := audit.NewEvent(workflow.EventChangeTransition, string(candidate.ProjectId()), string(candidate.ChangeId()), service.repositoryRoot, map[string]any{"previous_state": transition.PreviousState, "resulting_state": transition.ResultingState, "transition_timestamp": transition.OccurredAt.Format(time.RFC3339Nano), "context": transition.Context}, transition.OccurredAt)
	if err != nil {
		return change.Change{}, err
	}
	committed, err := audit.NewEvent(audit.EventArtifactCommitted, string(candidate.ProjectId()), string(candidate.ChangeId()), service.repositoryRoot, map[string]any{"artifact_ids": []string{string(id)}, "change_revision": candidate.Revision(), "operation_id": operation.Id}, result.CompletedAt())
	if err != nil {
		return change.Change{}, err
	}
	commit := authority.AuthorityCommit{ExpectedRevision: current.Revision(), Candidate: candidate, Artifacts: []artifact.Artifact{item}, Bindings: []authority.ArtifactBinding{{Role: "application-result", ArtifactId: id, Revision: candidate.Revision()}}, Operation: &operation, AuditEvents: []audit.Event{completed, lifecycle, committed}}
	if err := service.durable.CommitAuthority(commit); err != nil {
		return change.Change{}, err
	}
	return candidate, nil
}

// CloseRejected records that canonical source is still unchanged, then uses
// the existing rejected -> audit-locked edge. It never invokes Apply.
func (service *Service) CloseRejected(
	ctx context.Context,
	currentChange change.Change,
	currentProposal proposal.Proposal,
	verificationResult verification.Result,
	policyDecision policy.BundleDecision,
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
	if err := validateDisposition(currentChange, currentProposal, verificationResult, policyDecision, decision, approval.DecisionReject); err != nil {
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
	if service.durable != nil {
		if err := service.integrity(currentProposal); err != nil {
			return currentChange, fmt.Errorf("rejected Change integrity changed before audit lock: %w", err)
		}
		candidate := currentChange
		transition, err := candidate.Transition(change.StateAuditLocked, event.OccurredAt, "rejected Change closed with canonical source unchanged")
		if err != nil {
			return currentChange, err
		}
		closureMetadata, err := durableIntegrationMetadata(currentProposal, verificationResult, decision)
		if err != nil {
			return currentChange, err
		}
		closureMetadata["canonical_mutation_occurred"] = false
		closureMetadata["event_timestamp"] = event.OccurredAt.UTC().Format(time.RFC3339Nano)
		closureMetadata["disposition"] = "rejected-closure"
		closureMetadata["canonical_source_unchanged"] = true
		closure, err := audit.NewEvent(EventChangeClosureRecorded, string(candidate.ProjectId()), string(candidate.ChangeId()), service.repositoryRoot, closureMetadata, event.OccurredAt)
		if err != nil {
			return currentChange, err
		}
		lifecycle, err := audit.NewEvent(workflow.EventChangeTransition, string(candidate.ProjectId()), string(candidate.ChangeId()), service.repositoryRoot, map[string]any{"previous_state": transition.PreviousState, "resulting_state": transition.ResultingState, "transition_timestamp": transition.OccurredAt.Format(time.RFC3339Nano), "context": transition.Context}, transition.OccurredAt)
		if err != nil {
			return currentChange, err
		}
		if err := service.durable.CommitAuthority(authority.AuthorityCommit{ExpectedRevision: currentChange.Revision(), Candidate: candidate, AuditEvents: []audit.Event{closure, lifecycle}}); err != nil {
			return currentChange, err
		}
		return candidate, nil
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

// CloseAbandoned closes an explicitly discarded Change after proving that the
// canonical repository still equals its durable isolation snapshot. It never
// invokes patch application and requires durable authority so the closure fact
// and terminal transition commit atomically.
func (service *Service) CloseAbandoned(
	ctx context.Context,
	currentChange change.Change,
	snapshot source.SourceSnapshot,
	reason string,
) (change.Change, error) {
	if service == nil || service.durable == nil {
		return currentChange, fmt.Errorf("durable abandoned Change closure is not configured")
	}
	if ctx == nil {
		return currentChange, fmt.Errorf("abandoned Change closure context is required")
	}
	if err := ctx.Err(); err != nil {
		return currentChange, fmt.Errorf("abandoned Change closure cancelled: %w", err)
	}
	if currentChange.State() != change.StateRejected {
		return currentChange, fmt.Errorf("Change %q must be rejected before abandoned closure", currentChange.ChangeId())
	}
	reason = strings.TrimSpace(reason)
	if reason == "" {
		return currentChange, fmt.Errorf("abandoned Change closure reason is required")
	}
	actual, err := service.inspector(currentChange.ProjectId(), service.repositoryRoot)
	if err != nil {
		return currentChange, fmt.Errorf("inspect canonical source for abandoned closure: %w", err)
	}
	if snapshot.ProjectId() != currentChange.ProjectId() ||
		snapshot.RepositoryRoot() != service.repositoryRoot ||
		actual.ProjectId() != snapshot.ProjectId() ||
		actual.RepositoryRoot() != snapshot.RepositoryRoot() ||
		actual.HeadRevision() != snapshot.HeadRevision() ||
		actual.WorkingTreeState() != source.WorkingTreeClean ||
		actual.SourceStateDigest() != snapshot.SourceStateDigest() ||
		!equalRepositoryPaths(actual.TrackedPaths(), snapshot.TrackedPaths()) {
		return currentChange, fmt.Errorf("abandoned Change canonical source no longer matches durable isolation authority")
	}
	now := service.clock().UTC()
	candidate := currentChange
	transition, err := candidate.Transition(change.StateAuditLocked, now, "discarded Change closed with canonical source unchanged")
	if err != nil {
		return currentChange, err
	}
	closure, err := audit.NewEvent(EventChangeClosureRecorded, string(candidate.ProjectId()), string(candidate.ChangeId()), service.repositoryRoot, map[string]any{
		"base_revision":               snapshot.HeadRevision(),
		"source_state_digest":         string(snapshot.SourceStateDigest()),
		"canonical_mutation_occurred": false,
		"canonical_source_unchanged":  true,
		"disposition":                 "abandoned-closure",
		"reason":                      reason,
		"event_timestamp":             now.Format(time.RFC3339Nano),
	}, now)
	if err != nil {
		return currentChange, err
	}
	lifecycle, err := audit.NewEvent(workflow.EventChangeTransition, string(candidate.ProjectId()), string(candidate.ChangeId()), service.repositoryRoot, map[string]any{
		"previous_state":       transition.PreviousState,
		"resulting_state":      transition.ResultingState,
		"transition_timestamp": transition.OccurredAt.Format(time.RFC3339Nano),
		"context":              transition.Context,
	}, transition.OccurredAt)
	if err != nil {
		return currentChange, err
	}
	if err := service.durable.CommitAuthority(authority.AuthorityCommit{
		ExpectedRevision: currentChange.Revision(),
		Candidate:        candidate,
		AuditEvents:      []audit.Event{closure, lifecycle},
	}); err != nil {
		return currentChange, err
	}
	return candidate, nil
}

func durableIntegrationMetadata(currentProposal proposal.Proposal, verificationResult verification.Result, decision approval.HumanDecision) (map[string]any, error) {
	patch, ok := currentProposal.PatchArtifact()
	if !ok {
		return nil, fmt.Errorf("canonical lifecycle metadata requires PatchArtifact")
	}
	workspace := currentProposal.Workspace()
	evidence := verificationResult.EvidenceSet()
	if patch.WorkspaceId() != workspace.WorkspaceId() ||
		evidence.WorkspaceId() != workspace.WorkspaceId() ||
		decision.WorkspaceId() != workspace.WorkspaceId() {
		return nil, fmt.Errorf("canonical lifecycle metadata linkage is inconsistent")
	}
	return map[string]any{
		"workspace_id":            string(workspace.WorkspaceId()),
		"base_revision":           patch.BaseRevision(),
		"source_state_digest":     string(patch.SourceStateDigest()),
		"patch_digest":            patch.PatchDigest(),
		"changed_paths":           patch.ChangedPaths(),
		"changed_path_count":      len(patch.ChangedPaths()),
		"verification_attempt_id": string(evidence.VerificationAttemptId()),
		"evidence_set_id":         evidence.Id(),
		"evidence_count":          len(evidence.Evidence()),
		"human_decision":          string(decision.Kind()),
		"policy_evaluation_id":    decision.PolicyEvaluationId(),
		"policy_bundle_digest":    decision.PolicyBundleDigest(),
	}, nil
}

func validateDisposition(
	currentChange change.Change,
	currentProposal proposal.Proposal,
	verificationResult verification.Result,
	policyDecision policy.BundleDecision,
	decision approval.HumanDecision,
	expected approval.DecisionKind,
) error {
	if err := verification.ValidateResultForDecision(currentChange, currentProposal, verificationResult); err != nil {
		return fmt.Errorf("canonical integration evidence coherence failed: %w", err)
	}
	artifact, _ := currentProposal.PatchArtifact()
	evidence := verificationResult.EvidenceSet()
	if err := policy.ValidateLinkage(policyDecision, currentChange.ProjectId(), currentChange.ChangeId(), evidence); err != nil {
		return fmt.Errorf("canonical integration policy coherence failed: %w", err)
	}
	if decision.PolicyEvaluationId() != policyDecision.Id() || decision.PolicyBundleDigest() != policyDecision.Bundle().Digest() {
		return fmt.Errorf("human decision policy authority does not match retained policy decision")
	}
	if expected == approval.DecisionApprove {
		requirements := policyDecision.Aggregate()
		if requirements.Denied || requirements.RequiresReview {
			return fmt.Errorf("retained policy decision does not permit canonical application")
		}
	}
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
