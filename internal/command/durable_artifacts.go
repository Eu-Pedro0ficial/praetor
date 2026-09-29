package command

import (
	"encoding/json"
	"fmt"
	"slices"
	"time"

	"github.com/Eu-Pedro0ficial/praetor/internal/approval"
	"github.com/Eu-Pedro0ficial/praetor/internal/artifact"
	"github.com/Eu-Pedro0ficial/praetor/internal/audit"
	"github.com/Eu-Pedro0ficial/praetor/internal/authority"
	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"github.com/Eu-Pedro0ficial/praetor/internal/integration"
	"github.com/Eu-Pedro0ficial/praetor/internal/policy"
	"github.com/Eu-Pedro0ficial/praetor/internal/proposal"
	"github.com/Eu-Pedro0ficial/praetor/internal/verification"
	"github.com/Eu-Pedro0ficial/praetor/internal/workflow"
)

type durableArtifactSpec struct {
	kind              artifact.Kind
	kindSchemaVersion uint32
	role              string
	mediaType         string
	payload           []byte
	createdAt         time.Time
	allowLarge        bool
}

func proposalFoundationSpecs(current proposal.Proposal) ([]durableArtifactSpec, error) {
	snapshot := current.CanonicalSource()
	scope := current.ApprovedScope()
	trackedPaths := make([]string, 0, len(snapshot.TrackedPaths()))
	for _, path := range snapshot.TrackedPaths() {
		trackedPaths = append(trackedPaths, string(path))
	}
	sourcePayload, err := artifact.EncodeSourceSnapshotPayload(artifact.SourceSnapshotPayload{RepositoryRoot: snapshot.RepositoryRoot(), WorkspaceId: string(current.Workspace().WorkspaceId()), WorkspaceRoot: current.Workspace().Root(), HeadRevision: snapshot.HeadRevision(), TrackedPaths: trackedPaths, SourceStateDigest: string(snapshot.SourceStateDigest())})
	if err != nil {
		return nil, err
	}
	sourceSpec := durableArtifactSpec{kind: artifact.KindSourceSnapshot, role: "source-snapshot", mediaType: "application/json", payload: sourcePayload, createdAt: time.Now()}
	scopeSpec, err := jsonArtifactSpec(artifact.KindApprovedScope, "approved-scope", map[string]any{"authorization_mode": scope.Surface().AuthorizationMode(), "expected": scope.Surface().ExpectedPaths(), "possible": scope.Surface().PossiblePaths(), "protected": scope.Surface().ProtectedPaths()}, time.Now())
	if err != nil {
		return nil, err
	}
	scopeSpec.kindSchemaVersion = 2
	return []durableArtifactSpec{sourceSpec, scopeSpec}, nil
}

func replacementProposalFoundationSpec(current proposal.Proposal) (durableArtifactSpec, error) {
	snapshot := current.CanonicalSource()
	trackedPaths := make([]string, 0, len(snapshot.TrackedPaths()))
	for _, path := range snapshot.TrackedPaths() {
		trackedPaths = append(trackedPaths, string(path))
	}
	payload, err := artifact.EncodeSourceSnapshotPayload(artifact.SourceSnapshotPayload{
		RepositoryRoot:    snapshot.RepositoryRoot(),
		WorkspaceId:       string(current.Workspace().WorkspaceId()),
		WorkspaceRoot:     current.Workspace().Root(),
		HeadRevision:      snapshot.HeadRevision(),
		TrackedPaths:      trackedPaths,
		SourceStateDigest: string(snapshot.SourceStateDigest()),
	})
	if err != nil {
		return durableArtifactSpec{}, err
	}
	return durableArtifactSpec{
		kind:      artifact.KindSourceSnapshot,
		role:      "source-snapshot:" + string(current.Workspace().WorkspaceId()),
		mediaType: "application/json",
		payload:   payload,
		createdAt: time.Now(),
	}, nil
}

func patchSpec(current proposal.Proposal) (durableArtifactSpec, error) {
	item, ok := current.PatchArtifact()
	if !ok {
		return durableArtifactSpec{}, fmt.Errorf("PatchArtifact is required")
	}
	payload, err := artifact.EncodePatchPayload(artifact.PatchPayload{WorkspaceId: string(item.WorkspaceId()), BaseRevision: item.BaseRevision(), SourceStateDigest: string(item.SourceStateDigest()), ChangedPaths: item.ChangedPaths(), PatchDigest: item.PatchDigest(), Content: item.Content(), CreatedAt: item.CreatedAt().Format(time.RFC3339Nano)})
	if err != nil {
		return durableArtifactSpec{}, err
	}
	return durableArtifactSpec{kind: artifact.KindPatch, role: "patch", mediaType: "application/json", payload: payload, createdAt: item.CreatedAt()}, nil
}

func verificationSpecs(result verification.Result, decision policy.BundleDecision) ([]durableArtifactSpec, error) {
	steps := make([]map[string]any, 0, len(result.Plan().Steps()))
	for _, step := range result.Plan().Steps() {
		steps = append(steps, map[string]any{"id": step.Id(), "kind": step.Kind(), "executable": step.Executable(), "arguments": step.Arguments(), "working_directory": step.WorkingDirectory(), "origin": step.Origin(), "timeout": step.Timeout().String(), "supporting_evidence": step.SupportingEvidence()})
	}
	plan, err := jsonArtifactSpec(artifact.KindVerificationPlan, "verification-plan", map[string]any{"attempt_id": result.AttemptId(), "steps": steps}, time.Now())
	if err != nil {
		return nil, err
	}
	evidence := result.EvidenceSet()
	items := make([]map[string]any, 0, len(evidence.Evidence()))
	for _, item := range evidence.Evidence() {
		exit, hasExit := item.ExitCode()
		supporting := make([]string, 0, len(item.SupportingEvidence()))
		for _, path := range item.SupportingEvidence() {
			supporting = append(supporting, string(path))
		}
		items = append(items, map[string]any{"step_id": item.StepId(), "kind": item.Kind(), "executable": item.Executable(), "arguments": item.Arguments(), "working_directory": item.WorkingDirectory(), "resolved_executable": item.ResolvedExecutable(), "origin": item.Origin(), "supporting_evidence": supporting, "started_at": item.StartedAt(), "completed_at": item.CompletedAt(), "exit_code": exit, "has_exit_code": hasExit, "outcome": item.Outcome(), "standard_output": item.StandardOutput(), "standard_error": item.StandardError(), "output_truncated": item.OutputTruncated()})
	}
	evidenceSpec, err := jsonArtifactSpec(artifact.KindEvidenceSet, "evidence-set", map[string]any{"id": evidence.Id(), "project_id": evidence.ProjectId(), "change_id": evidence.ChangeId(), "verification_attempt_id": evidence.VerificationAttemptId(), "workspace_id": evidence.WorkspaceId(), "patch_digest": evidence.PatchDigest(), "source_state_digest": evidence.SourceStateDigest(), "passed": evidence.Passed(), "evidence": items}, time.Now())
	if err != nil {
		return nil, err
	}
	decisions := make([]map[string]any, 0, len(decision.Decisions()))
	for _, item := range decision.Decisions() {
		decisions = append(decisions, map[string]any{"policy_id": item.Policy().Id(), "policy_version": item.Policy().Version(), "outcome": item.Outcome(), "reason": item.Reason(), "evidence_ids": item.EvidenceIds()})
	}
	policies := make([]map[string]any, 0, len(decision.Bundle().Policies()))
	for _, item := range decision.Bundle().Policies() {
		policies = append(policies, map[string]any{"id": item.Id(), "version": item.Version(), "family": item.Family(), "description": item.Description(), "severity": item.Severity(), "outcome": item.Outcome(), "required_evidence_kind": item.RequiredEvidenceKind(), "non_overridable": item.NonOverridable(), "exception_candidate_allowed": item.ExceptionCandidateAllowed()})
	}
	policySpec, err := jsonArtifactSpec(artifact.KindPolicyDecision, "policy-decision", map[string]any{"id": decision.Id(), "project_id": decision.ProjectId(), "change_id": decision.ChangeId(), "workspace_id": decision.WorkspaceId(), "verification_attempt_id": decision.VerificationAttemptId(), "evidence_set_id": decision.EvidenceSetId(), "patch_digest": decision.PatchDigest(), "source_state_digest": decision.SourceStateDigest(), "bundle_id": decision.Bundle().Id(), "bundle_version": decision.Bundle().Version(), "bundle_digest": decision.Bundle().Digest(), "policies": policies, "evaluated_at": decision.EvaluatedAt(), "denied": decision.Aggregate().Denied, "requires_review": decision.Aggregate().RequiresReview, "requires_approval": decision.Aggregate().RequiresApproval, "decisions": decisions}, decision.EvaluatedAt())
	if err != nil {
		return nil, err
	}
	return []durableArtifactSpec{plan, evidenceSpec, policySpec}, nil
}

func humanDecisionSpec(decision approval.HumanDecision) (durableArtifactSpec, error) {
	return jsonArtifactSpec(artifact.KindHumanDecision, "human-decision", map[string]any{"kind": decision.Kind(), "actor": decision.Actor(), "rationale": decision.Rationale(), "requested_state": decision.RequestedState(), "workspace_id": decision.WorkspaceId(), "base_revision": decision.BaseRevision(), "source_state_digest": decision.SourceStateDigest(), "patch_digest": decision.PatchDigest(), "verification_attempt_id": decision.VerificationAttemptId(), "evidence_set_id": decision.EvidenceSetId(), "evidence_count": decision.EvidenceCount(), "changed_path_count": decision.ChangedPathCount(), "policy_evaluation_id": decision.PolicyEvaluationId(), "policy_bundle_digest": decision.PolicyBundleDigest(), "policy_denied": decision.PolicyDenied(), "policy_requires_review": decision.PolicyRequiresReview(), "policy_requires_approval": decision.PolicyRequiresApproval()}, decision.OccurredAt())
}

func (session *Session) policyDecisionAuditEvent(decision policy.BundleDecision) (audit.Event, error) {
	individual := make([]map[string]any, 0, len(decision.Decisions()))
	for _, item := range decision.Decisions() {
		individual = append(individual, map[string]any{
			"policy_id":      string(item.Policy().Id()),
			"policy_version": string(item.Policy().Version()),
			"family":         string(item.Policy().Family()),
			"severity":       string(item.Policy().Severity()),
			"outcome":        string(item.Outcome()),
			"evidence_count": len(item.EvidenceIds()),
			"reason":         item.Reason(),
		})
	}
	return audit.NewEvent(policy.EventPolicyDecisionRecorded, string(decision.ProjectId()), string(decision.ChangeId()), session.registration.RepositoryRoot, map[string]any{
		"policy_evaluation_id":    decision.Id(),
		"policy_bundle_id":        string(decision.Bundle().Id()),
		"policy_bundle_version":   string(decision.Bundle().Version()),
		"policy_bundle_digest":    decision.Bundle().Digest(),
		"workspace_id":            string(decision.WorkspaceId()),
		"verification_attempt_id": string(decision.VerificationAttemptId()),
		"evidence_set_id":         decision.EvidenceSetId(),
		"patch_digest":            decision.PatchDigest(),
		"source_state_digest":     string(decision.SourceStateDigest()),
		"policy_count":            len(decision.Decisions()),
		"denied":                  decision.Aggregate().Denied,
		"requires_review":         decision.Aggregate().RequiresReview,
		"requires_approval":       decision.Aggregate().RequiresApproval,
		"policy_decisions":        individual,
	}, decision.EvaluatedAt())
}

func (session *Session) humanDecisionAuditEvent(decision approval.HumanDecision) (audit.Event, error) {
	metadata := map[string]any{
		"decision":                 string(decision.Kind()),
		"decision_timestamp":       decision.OccurredAt().Format(time.RFC3339Nano),
		"actor_type":               "human",
		"actor_provenance":         string(decision.Actor()),
		"identity_assurance":       "local-process-interaction-only",
		"requested_state":          string(decision.RequestedState()),
		"workspace_id":             string(decision.WorkspaceId()),
		"base_revision":            decision.BaseRevision(),
		"source_state_digest":      string(decision.SourceStateDigest()),
		"patch_digest":             decision.PatchDigest(),
		"verification_attempt_id":  string(decision.VerificationAttemptId()),
		"evidence_set_id":          decision.EvidenceSetId(),
		"evidence_count":           decision.EvidenceCount(),
		"changed_path_count":       decision.ChangedPathCount(),
		"policy_evaluation_id":     decision.PolicyEvaluationId(),
		"policy_bundle_digest":     decision.PolicyBundleDigest(),
		"policy_denied":            decision.PolicyDenied(),
		"policy_requires_review":   decision.PolicyRequiresReview(),
		"policy_requires_approval": decision.PolicyRequiresApproval(),
	}
	if decision.Rationale() != "" {
		metadata["rationale"] = string(decision.Rationale())
	}
	return audit.NewEvent(approval.EventHumanDecisionRecorded, string(decision.ProjectId()), string(decision.ChangeId()), session.registration.RepositoryRoot, metadata, decision.OccurredAt())
}

func applicationResultSpec(result integration.Result) (durableArtifactSpec, error) {
	return jsonArtifactSpec(artifact.KindApplicationResult, "application-result", map[string]any{"workspace_id": result.WorkspaceId(), "base_revision": result.BaseRevision(), "source_state_digest": result.SourceStateDigest(), "resulting_source_state_digest": result.ResultingSourceStateDigest(), "patch_digest": result.PatchDigest(), "verification_attempt_id": result.VerificationAttemptId(), "evidence_set_id": result.EvidenceSetId(), "decision_kind": result.DecisionKind(), "changed_paths": result.ChangedPaths(), "canonical_head": result.CanonicalHead(), "index_unchanged": result.IndexUnchanged(), "result_digest": result.ResultDigest()}, result.CompletedAt())
}

func jsonArtifactSpec(kind artifact.Kind, role string, value any, createdAt time.Time) (durableArtifactSpec, error) {
	payload, err := json.Marshal(value)
	if err != nil {
		return durableArtifactSpec{}, err
	}
	return durableArtifactSpec{kind: kind, role: role, mediaType: "application/json", payload: payload, createdAt: createdAt}, nil
}

func (session *Session) persistGovernedArtifacts(current change.Change, specs ...durableArtifactSpec) error {
	if session == nil || session.durableAuthority == nil || len(specs) == 0 {
		return nil
	}
	items, bindings, identities, err := buildDurableArtifacts(current, current.Revision(), specs)
	if err != nil {
		return err
	}
	event, err := audit.NewEvent(audit.EventArtifactCommitted, string(current.ProjectId()), string(current.ChangeId()), session.registration.RepositoryRoot, map[string]any{"artifact_ids": identities, "change_revision": current.Revision()}, time.Now().UTC())
	if err != nil {
		return err
	}
	if err := session.durableAuthority.CommitArtifacts(current.ChangeId(), current.Revision(), items, nil, nil, bindings, []audit.Event{event}); err != nil {
		return fmt.Errorf("persist governed artifacts: %w", err)
	}
	return nil
}

func buildDurableArtifacts(current change.Change, bindingRevision uint64, specs []durableArtifactSpec) ([]artifact.Artifact, []authority.ArtifactBinding, []string, error) {
	items := make([]artifact.Artifact, 0, len(specs))
	bindings := make([]authority.ArtifactBinding, 0, len(specs))
	identities := make([]string, 0, len(specs))
	for _, spec := range specs {
		id, err := artifact.GenerateId()
		if err != nil {
			return nil, nil, nil, err
		}
		createdAt := spec.createdAt
		if createdAt.IsZero() {
			createdAt = time.Now().UTC()
		}
		kindSchemaVersion := spec.kindSchemaVersion
		if kindSchemaVersion == 0 {
			kindSchemaVersion = 1
		}
		item, err := artifact.New(id, current.ProjectId(), current.ChangeId(), spec.kind, 1, kindSchemaVersion, spec.mediaType, createdAt, artifact.Producer{Component: "praetor-runtime"}, spec.payload, spec.allowLarge)
		if err != nil {
			return nil, nil, nil, err
		}
		items = append(items, item)
		bindings = append(bindings, authority.ArtifactBinding{Role: spec.role, ArtifactId: id, Revision: bindingRevision})
		identities = append(identities, string(id))
	}
	return items, bindings, identities, nil
}

func (session *Session) commitTransitionWithArtifacts(current change.Change, resultingState change.ChangeState, context string, specs []durableArtifactSpec, events ...audit.Event) (change.Change, error) {
	if session == nil || session.durableAuthority == nil {
		return change.Change{}, fmt.Errorf("durable authority is not configured")
	}
	persisted, snapshot, err := session.durableAuthority.GetChange(current.ChangeId())
	if err != nil {
		return change.Change{}, err
	}
	if persisted.Revision() != current.Revision() || persisted.State() != current.State() {
		return change.Change{}, fmt.Errorf("%w: Change %q expected revision %d", authority.ErrStaleRevision, current.ChangeId(), current.Revision())
	}
	if !snapshot.Executable() {
		return change.Change{}, fmt.Errorf("%w: Change %q uses unsupported workflow schema version %d", authority.ErrIncompatible, current.ChangeId(), snapshot.SchemaVersion())
	}
	candidate := persisted
	transition, err := candidate.Transition(resultingState, time.Now().UTC(), context)
	if err != nil {
		return change.Change{}, err
	}
	items, bindings, identities, err := buildDurableArtifacts(candidate, candidate.Revision(), specs)
	if err != nil {
		return change.Change{}, err
	}
	lifecycleEvent, err := audit.NewEvent(workflow.EventChangeTransition, string(candidate.ProjectId()), string(candidate.ChangeId()), session.registration.RepositoryRoot, map[string]any{"previous_state": string(transition.PreviousState), "resulting_state": string(transition.ResultingState), "transition_timestamp": transition.OccurredAt.Format(time.RFC3339Nano), "context": transition.Context}, transition.OccurredAt)
	if err != nil {
		return change.Change{}, err
	}
	events = append(events, lifecycleEvent)
	if len(items) > 0 {
		artifactEvent, eventError := audit.NewEvent(audit.EventArtifactCommitted, string(candidate.ProjectId()), string(candidate.ChangeId()), session.registration.RepositoryRoot, map[string]any{"artifact_ids": identities, "change_revision": candidate.Revision()}, time.Now().UTC())
		if eventError != nil {
			return change.Change{}, eventError
		}
		events = append(events, artifactEvent)
	}
	if err := session.durableAuthority.CommitAuthority(authority.AuthorityCommit{ExpectedRevision: persisted.Revision(), Candidate: candidate, Artifacts: items, Bindings: bindings, AuditEvents: events}); err != nil {
		return change.Change{}, fmt.Errorf("commit authoritative Change transition: %w", err)
	}
	return candidate, nil
}

// finalizeRecoveredCanonical advances only an exact recovered POST. The
// terminal Change revision, recovery result artifact/binding, completed
// operation reference, and audit records commit as one authority transaction.
func (session *Session) finalizeRecoveredCanonical(operation authority.Operation, proof integration.CanonicalProof) (change.Change, error) {
	operationId := operation.Id
	if operation.State != authority.OperationCompleted {
		return change.Change{}, fmt.Errorf("canonical recovery operation %q is not completed", operationId)
	}
	storedResult, err := authority.DecodeCanonicalResult(operation.Result)
	if err != nil {
		return change.Change{}, err
	}
	if storedResult.Head != proof.HeadRevision() || storedResult.Patch != proof.PatchDigest() || storedResult.Index != proof.IndexUnchanged() || !slices.Equal(storedResult.Paths, proof.ChangedPaths()) {
		return change.Change{}, fmt.Errorf("%w: completed operation result disagrees with canonical POST", authority.ErrCorrupt)
	}
	current, snapshot, err := session.durableAuthority.GetChange(operation.ChangeId)
	if err != nil {
		return change.Change{}, err
	}
	if !snapshot.Executable() {
		return change.Change{}, fmt.Errorf("%w: Change %q uses unsupported workflow schema version %d", authority.ErrIncompatible, current.ChangeId(), snapshot.SchemaVersion())
	}
	if current.State() == change.StateAuditLocked {
		if err := session.verifyApplicationResultAuthority(current, operation, proof); err != nil {
			return change.Change{}, err
		}
		return current, nil
	}
	if current.State() != change.StateApproved {
		return change.Change{}, fmt.Errorf("recovered canonical POST requires approved Change, found %s", current.State())
	}
	candidate := current
	now := time.Now().UTC()
	transition, err := candidate.Transition(change.StateAuditLocked, now, "exact canonical POST finalized by explicit durable recovery")
	if err != nil {
		return change.Change{}, err
	}
	payload, err := json.Marshal(map[string]any{"operation_id": operationId, "recovered": true, "canonical_head": proof.HeadRevision(), "patch_digest": proof.PatchDigest(), "changed_paths": proof.ChangedPaths(), "index_unchanged": proof.IndexUnchanged()})
	if err != nil {
		return change.Change{}, err
	}
	id, err := artifact.GenerateId()
	if err != nil {
		return change.Change{}, err
	}
	resultArtifact, err := artifact.New(id, candidate.ProjectId(), candidate.ChangeId(), artifact.KindApplicationResult, 1, 2, "application/json", now, artifact.Producer{Component: "praetor-runtime", OperationId: string(operationId)}, payload, false)
	if err != nil {
		return change.Change{}, err
	}
	lifecycleMetadata := map[string]any{"previous_state": string(transition.PreviousState), "resulting_state": string(transition.ResultingState), "transition_timestamp": transition.OccurredAt.Format(time.RFC3339Nano), "context": transition.Context}
	lifecycleEvent, err := audit.NewEvent(workflow.EventChangeTransition, string(candidate.ProjectId()), string(candidate.ChangeId()), session.registration.RepositoryRoot, lifecycleMetadata, transition.OccurredAt)
	if err != nil {
		return change.Change{}, err
	}
	artifactEvent, err := audit.NewEvent(audit.EventArtifactCommitted, string(candidate.ProjectId()), string(candidate.ChangeId()), session.registration.RepositoryRoot, map[string]any{"artifact_ids": []string{string(id)}, "change_revision": candidate.Revision(), "recovered_operation_id": operationId}, now)
	if err != nil {
		return change.Change{}, err
	}
	recoveryEvent, err := audit.NewEvent(audit.EventOperationRecovered, string(candidate.ProjectId()), string(candidate.ChangeId()), session.registration.RepositoryRoot, map[string]any{"operation_id": operationId, "condition": string(integration.ConditionPOST), "terminal_revision": candidate.Revision()}, now)
	if err != nil {
		return change.Change{}, err
	}
	commit := authority.AuthorityCommit{ExpectedRevision: current.Revision(), Candidate: candidate, Artifacts: []artifact.Artifact{resultArtifact}, Bindings: []authority.ArtifactBinding{{Role: "application-result", ArtifactId: id, Revision: candidate.Revision()}}, Operation: &operation, AuditEvents: []audit.Event{recoveryEvent, lifecycleEvent, artifactEvent}}
	if err := session.durableAuthority.CommitAuthority(commit); err != nil {
		return change.Change{}, err
	}
	return candidate, nil
}

func (session *Session) verifyApplicationResultAuthority(current change.Change, operation authority.Operation, proof integration.CanonicalProof) error {
	bindings, err := session.durableAuthority.ListBindings(current.ChangeId())
	if err != nil {
		return err
	}
	var resultId artifact.ArtifactId
	for _, binding := range bindings {
		if binding.Role == "application-result" {
			resultId = binding.ArtifactId
		}
	}
	if resultId == "" {
		return fmt.Errorf("%w: audit-locked Change %q has no application-result authority", authority.ErrCorrupt, current.ChangeId())
	}
	item, err := session.durableAuthority.GetArtifact(current.ChangeId(), resultId, true)
	if err != nil {
		return err
	}
	var payload struct {
		OperationId    authority.OperationId `json:"operation_id"`
		CanonicalHead  string                `json:"canonical_head"`
		PatchDigest    string                `json:"patch_digest"`
		ChangedPaths   []string              `json:"changed_paths"`
		IndexUnchanged bool                  `json:"index_unchanged"`
	}
	if item.Kind() != artifact.KindApplicationResult || json.Unmarshal(item.Payload(), &payload) != nil || payload.OperationId != operation.Id || payload.CanonicalHead != proof.HeadRevision() || payload.PatchDigest != proof.PatchDigest() || !slices.Equal(payload.ChangedPaths, proof.ChangedPaths()) || !payload.IndexUnchanged {
		return fmt.Errorf("%w: application-result authority does not match completed operation %q", authority.ErrCorrupt, operation.Id)
	}
	return nil
}
