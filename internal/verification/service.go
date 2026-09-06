package verification

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"github.com/Eu-Pedro0ficial/praetor/internal/proposal"
)

const (
	EventVerificationStarted       = "VERIFICATION_STARTED"
	EventVerificationStepCompleted = "VERIFICATION_STEP_COMPLETED"
	EventVerificationCompleted     = "VERIFICATION_COMPLETED"
	EventVerificationFailed        = "VERIFICATION_FAILED"
	defaultStepTimeout             = 2 * time.Minute
)

// LifecycleEvent carries bounded M0.6 verification metadata to audit.
type LifecycleEvent struct {
	EventType      string
	AttemptId      VerificationAttemptId
	Proposal       proposal.Proposal
	PlanStepCount  int
	Evidence       Evidence
	HasEvidence    bool
	EvidenceSet    EvidenceSet
	HasEvidenceSet bool
	Disposition    string
	Failure        string
	OccurredAt     time.Time
}

// LifecycleRecorder appends one verification event.
type LifecycleRecorder func(LifecycleEvent) error

// Service coordinates discovery, optional AI planning, deterministic
// execution, patch integrity, and evidence normalization. It does not approve
// a Change or own workflow state transitions.
type Service struct {
	engine      *Engine
	recorder    LifecycleRecorder
	attemptIds  AttemptIdGenerator
	clock       Clock
	stepTimeout time.Duration
}

// New constructs the M0.6 verification service.
func New(
	engine *Engine,
	recorder LifecycleRecorder,
	attemptIds AttemptIdGenerator,
	clock Clock,
) (*Service, error) {
	if engine == nil {
		return nil, fmt.Errorf("verification engine dependency is not configured")
	}
	if recorder == nil {
		return nil, fmt.Errorf("verification lifecycle recorder dependency is not configured")
	}
	if attemptIds == nil {
		return nil, fmt.Errorf("verification attempt identity dependency is not configured")
	}
	if clock == nil {
		return nil, fmt.Errorf("verification service clock dependency is not configured")
	}
	return &Service{
		engine:      engine,
		recorder:    recorder,
		attemptIds:  attemptIds,
		clock:       clock,
		stepTimeout: defaultStepTimeout,
	}, nil
}

// Verify runs the complete M0.6 gate over one retained, surface-valid
// proposal. A failed gate leaves workflow transition decisions to the caller.
func (service *Service) Verify(
	ctx context.Context,
	currentChange change.Change,
	currentProposal proposal.Proposal,
	planner Planner,
) (Result, error) {
	var result Result
	if service == nil {
		return result, fmt.Errorf("verification service is required")
	}
	if ctx == nil {
		return result, fmt.Errorf("verification context is required")
	}
	if currentChange.State() != change.StateIsolated {
		return result, fmt.Errorf("Change %q must be isolated before verification", currentChange.ChangeId())
	}
	workspace := currentProposal.Workspace()
	artifact, hasArtifact := currentProposal.PatchArtifact()
	if workspace.State() != proposal.WorkspaceRetained || !hasArtifact {
		return result, fmt.Errorf("verification requires a retained surface-valid proposal")
	}
	if currentChange.ProjectId() != workspace.ProjectId() ||
		currentChange.ChangeId() != workspace.ChangeId() ||
		artifact.WorkspaceId() != workspace.WorkspaceId() ||
		artifact.ChangeId() != currentChange.ChangeId() {
		return result, fmt.Errorf("verification Change, Proposal, and PatchArtifact linkage is inconsistent")
	}
	attemptId, err := service.attemptIds()
	if err != nil {
		return result, err
	}
	if err := validateVerificationAttemptId(attemptId); err != nil {
		return result, err
	}
	result.attemptId = attemptId
	if err := service.record(LifecycleEvent{
		EventType:   EventVerificationStarted,
		AttemptId:   attemptId,
		Proposal:    currentProposal,
		Disposition: "started",
		OccurredAt:  service.clock().UTC(),
	}); err != nil {
		return result, fmt.Errorf("record verification start: %w", err)
	}
	fail := func(primary error) (Result, error) {
		recordError := service.record(LifecycleEvent{
			EventType:      EventVerificationFailed,
			AttemptId:      attemptId,
			Proposal:       currentProposal,
			PlanStepCount:  len(result.plan.Steps()),
			EvidenceSet:    result.evidence,
			HasEvidenceSet: result.evidence.Id() != "",
			Disposition:    "failed",
			Failure:        safeFailure(primary, currentProposal),
			OccurredAt:     service.clock().UTC(),
		})
		if recordError != nil {
			recordError = fmt.Errorf("record verification failure: %w", recordError)
		}
		return result, errors.Join(primary, recordError)
	}

	if err := service.engine.VerifyIntegrity(currentProposal); err != nil {
		return fail(fmt.Errorf("pre-verification patch integrity failed: %w", err))
	}
	discovery, err := Discover(workspace.Root())
	if err != nil {
		return fail(err)
	}
	result.discovery = discovery

	var assisted []VerificationCandidate
	if discovery.NeedsPlanning() && planner != nil {
		planningRequest, requestError := newPlanningRequest(currentChange, currentProposal, discovery)
		if requestError != nil {
			return fail(requestError)
		}
		planningResult, planningError := planner(ctx, planningRequest)
		if integrityError := service.engine.VerifyIntegrity(currentProposal); integrityError != nil {
			return fail(fmt.Errorf("verification planner changed protected source state: %w", integrityError))
		}
		if planningError != nil {
			result.planningFailure = safeFailure(planningError, currentProposal)
			if len(discovery.Candidates()) == 0 {
				return fail(fmt.Errorf("verification planning failed with insufficient deterministic repository evidence: %w", planningError))
			}
		} else {
			result.planning = planningResult
			result.planningUsed = true
			assisted = planningResult.Candidates()
		}
	}

	plan, err := BuildPlan(discovery.Candidates(), assisted, service.stepTimeout)
	if err != nil {
		return fail(err)
	}
	result.plan = plan
	evidence, err := service.engine.Execute(ctx, plan, currentProposal)
	if err != nil {
		return fail(err)
	}
	for _, item := range evidence {
		if err := service.record(LifecycleEvent{
			EventType:     EventVerificationStepCompleted,
			AttemptId:     attemptId,
			Proposal:      currentProposal,
			PlanStepCount: len(plan.Steps()),
			Evidence:      item,
			HasEvidence:   true,
			Disposition:   string(item.Outcome()),
			OccurredAt:    service.clock().UTC(),
		}); err != nil {
			return fail(fmt.Errorf("record verification step evidence: %w", err))
		}
	}
	result.evidence = buildEvidenceSet(attemptId, currentProposal, plan, evidence)
	if !result.evidence.Passed() {
		return fail(verificationFailure(evidence, len(plan.Steps())+1))
	}
	if err := service.record(LifecycleEvent{
		EventType:      EventVerificationCompleted,
		AttemptId:      attemptId,
		Proposal:       currentProposal,
		PlanStepCount:  len(plan.Steps()),
		EvidenceSet:    result.evidence,
		HasEvidenceSet: true,
		Disposition:    "passed",
		OccurredAt:     service.clock().UTC(),
	}); err != nil {
		return fail(fmt.Errorf("record verification completion: %w", err))
	}
	return result, nil
}

func (service *Service) record(event LifecycleEvent) error {
	if event.OccurredAt.IsZero() {
		return fmt.Errorf("verification lifecycle timestamp is required")
	}
	return service.recorder(event)
}

func buildEvidenceSet(
	attemptId VerificationAttemptId,
	currentProposal proposal.Proposal,
	plan VerificationPlan,
	evidence []Evidence,
) EvidenceSet {
	artifact, _ := currentProposal.PatchArtifact()
	passed := len(plan.Steps()) > 0 && len(evidence) == len(plan.Steps())+1
	for _, item := range evidence {
		if item.Outcome() != OutcomePass {
			passed = false
		}
	}
	digestInput := strings.Builder{}
	digestInput.WriteString(string(attemptId))
	digestInput.WriteString("\x00")
	digestInput.WriteString(artifact.PatchDigest())
	for _, item := range evidence {
		digestInput.WriteString("\x00")
		digestInput.WriteString(item.StepId())
		digestInput.WriteString("\x00")
		digestInput.WriteString(string(item.Outcome()))
	}
	digest := sha256.Sum256([]byte(digestInput.String()))
	return EvidenceSet{
		id:                    "evidence-" + hex.EncodeToString(digest[:]),
		verificationAttemptId: attemptId,
		projectId:             currentProposal.Workspace().ProjectId(),
		changeId:              currentProposal.Workspace().ChangeId(),
		workspaceId:           currentProposal.Workspace().WorkspaceId(),
		patchDigest:           artifact.PatchDigest(),
		sourceDigest:          currentProposal.Workspace().SourceStateDigest(),
		evidence:              cloneEvidenceSlice(evidence),
		passed:                passed,
	}
}

func verificationFailure(evidence []Evidence, expectedCount int) error {
	for _, item := range evidence {
		if item.Outcome() != OutcomePass {
			return fmt.Errorf("verification gate failed: step %q outcome %s", item.StepId(), item.Outcome())
		}
	}
	return fmt.Errorf("verification gate produced %d evidence items; expected %d", len(evidence), expectedCount)
}

func safeFailure(err error, currentProposal proposal.Proposal) string {
	if err == nil {
		return ""
	}
	value := strings.TrimSpace(err.Error())
	workspace := currentProposal.Workspace()
	for _, replacement := range []struct {
		path  string
		label string
	}{
		{path: workspace.Root(), label: "[proposal-workspace]"},
		{path: workspace.CanonicalRoot(), label: "[canonical-source]"},
	} {
		if replacement.path != "" {
			value = strings.ReplaceAll(value, replacement.path, replacement.label)
		}
	}
	sanitized, _ := sanitizeOutput([]byte(value), 512)
	return sanitized
}

func validateVerificationAttemptId(attemptId VerificationAttemptId) error {
	const prefix = "verification-"
	value := string(attemptId)
	if !strings.HasPrefix(value, prefix) {
		return fmt.Errorf("VerificationAttemptId must use the verification prefix")
	}
	encoded := strings.TrimPrefix(value, prefix)
	if len(encoded) != 32 {
		return fmt.Errorf("VerificationAttemptId must contain 128 bits of random identity")
	}
	if _, err := hex.DecodeString(encoded); err != nil {
		return fmt.Errorf("VerificationAttemptId contains invalid random identity: %w", err)
	}
	return nil
}
