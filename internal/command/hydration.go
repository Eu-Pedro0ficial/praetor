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
	"github.com/Eu-Pedro0ficial/praetor/internal/inspection"
	"github.com/Eu-Pedro0ficial/praetor/internal/policy"
	"github.com/Eu-Pedro0ficial/praetor/internal/project"
	"github.com/Eu-Pedro0ficial/praetor/internal/proposal"
	"github.com/Eu-Pedro0ficial/praetor/internal/source"
	"github.com/Eu-Pedro0ficial/praetor/internal/verification"
)

func (session *Session) hydrateDurableChange(detail inspection.ChangeDetail) error {
	staged := *session
	if err := staged.hydrateDurableChangeStaged(detail); err != nil {
		return err
	}
	*session = staged
	if staged.hasLastPolicyDecision {
		session.policy.RetainDecision(staged.lastPolicyDecision)
	}
	return nil
}

func (session *Session) hydrateDurableChangeStaged(detail inspection.ChangeDetail) error {
	current := detail.Change
	session.currentProposal = proposal.Proposal{}
	session.hasCurrentProposal = false
	session.clearProposalFoundation()
	session.lastVerification = verification.Result{}
	session.hasLastVerification = false
	session.lastPolicyDecision = policy.BundleDecision{}
	session.hasLastPolicyDecision = false
	session.lastDecision = approval.HumanDecision{}
	session.hasLastDecision = false
	session.canonicalMutationOccurred = false
	session.setCurrentChange(current)
	if !detail.Workflow.Executable() || current.State() == change.StateCreated || current.State() == change.StatePlanned || current.State() == change.StateAuditLocked {
		return nil
	}
	bindings := make(map[string]authority.ArtifactBinding, len(detail.Bindings))
	for _, binding := range detail.Bindings {
		bindings[binding.Role] = binding
	}
	loadWithSchemas := func(role string, kind artifact.Kind, supportedKindSchemas ...uint32) (artifact.Artifact, error) {
		binding, ok := bindings[role]
		if !ok {
			return artifact.Artifact{}, fmt.Errorf("durable Change %q is missing required %s binding", current.ChangeId(), role)
		}
		item, err := session.durableAuthority.GetArtifact(current.ChangeId(), binding.ArtifactId, true)
		if err != nil {
			return artifact.Artifact{}, fmt.Errorf("load durable %s authority: %w", role, err)
		}
		if item.Kind() != kind || item.ProjectId() != current.ProjectId() || item.ChangeId() != current.ChangeId() || item.EnvelopeSchemaVersion() != 1 || !slices.Contains(supportedKindSchemas, item.KindSchemaVersion()) {
			return artifact.Artifact{}, fmt.Errorf("%w: durable %s artifact envelope is incompatible", authority.ErrCorrupt, role)
		}
		return item, nil
	}
	load := func(role string, kind artifact.Kind) (artifact.Artifact, error) {
		return loadWithSchemas(role, kind, 1)
	}
	sourceRole := "source-snapshot"
	if latestWorkspace := latestCreatedWorkspaceId(detail.Audit); latestWorkspace != "" {
		candidateRole := "source-snapshot:" + latestWorkspace
		if _, exists := bindings[candidateRole]; exists {
			sourceRole = candidateRole
		}
	}
	sourceItem, err := load(sourceRole, artifact.KindSourceSnapshot)
	if err != nil {
		return err
	}
	sourcePayload, err := artifact.DecodeSourceSnapshotPayload(sourceItem.Payload())
	if err != nil {
		return fmt.Errorf("%w: %v", authority.ErrCorrupt, err)
	}
	if sourcePayload.RepositoryRoot != session.registration.RepositoryRoot {
		return fmt.Errorf("%w: durable source RepositoryRoot does not match active Project association", authority.ErrCorrupt)
	}
	snapshot, err := source.NewSourceSnapshot(current.ProjectId(), sourcePayload.RepositoryRoot, sourcePayload.HeadRevision, source.WorkingTreeClean, sourcePayload.TrackedPaths, source.SourceStateDigest(sourcePayload.SourceStateDigest))
	if err != nil {
		return fmt.Errorf("%w: %v", authority.ErrCorrupt, err)
	}
	scopeItem, err := loadWithSchemas("approved-scope", artifact.KindApprovedScope, 1, 2)
	if err != nil {
		return err
	}
	scopeRequest, err := decodeApprovedScopeRequest(scopeItem)
	if err != nil {
		return fmt.Errorf("%w: decode ApprovedScope: %v", authority.ErrCorrupt, err)
	}
	approvedScope, err := source.RehydrateApprovedScope(current.ChangeId(), current.ProjectId(), snapshot.SourceStateDigest(), scopeRequest)
	if err != nil {
		return fmt.Errorf("%w: %v", authority.ErrCorrupt, err)
	}
	session.setProposalFoundation(snapshot, approvedScope)
	workspaceId := proposal.WorkspaceId(sourcePayload.WorkspaceId)
	workspaceState := durableWorkspaceState(detail.Audit, workspaceId)
	if workspaceState == proposal.WorkspaceCleaned {
		return nil
	}
	patchBinding, hasPatch := bindings["patch"]
	if !hasPatch {
		currentProposal, err := proposal.RehydrateWorkspaceProposalWithState(workspaceId, sourcePayload.WorkspaceRoot, snapshot, approvedScope, workspaceState)
		if err != nil {
			return fmt.Errorf("rehydrate durable ProposalWorkspace: %w", err)
		}
		if err := session.proposalLifecycle.Reattach(currentProposal); err != nil {
			return fmt.Errorf("reattach durable ProposalWorkspace: %w", err)
		}
		session.setCurrentProposal(currentProposal)
		return nil
	}
	patchItem, err := session.durableAuthority.GetArtifact(current.ChangeId(), patchBinding.ArtifactId, true)
	if err != nil || patchItem.Kind() != artifact.KindPatch {
		return fmt.Errorf("%w: load durable patch authority: %v", authority.ErrCorrupt, err)
	}
	patchPayload, err := artifact.DecodePatchPayload(patchItem.Payload())
	if err != nil {
		return fmt.Errorf("%w: %v", authority.ErrCorrupt, err)
	}
	createdAt, err := time.Parse(time.RFC3339Nano, patchPayload.CreatedAt)
	if err != nil || patchPayload.WorkspaceId != sourcePayload.WorkspaceId || patchPayload.BaseRevision != snapshot.HeadRevision() || patchPayload.SourceStateDigest != string(snapshot.SourceStateDigest()) {
		return fmt.Errorf("%w: durable patch/source linkage is inconsistent", authority.ErrCorrupt)
	}
	currentProposal, err := proposal.RehydrateProposal(proposal.WorkspaceId(sourcePayload.WorkspaceId), sourcePayload.WorkspaceRoot, snapshot, approvedScope, patchPayload.Content, patchPayload.ChangedPaths, patchPayload.PatchDigest, createdAt)
	if err != nil {
		return fmt.Errorf("rehydrate durable Proposal: %w", err)
	}
	if err := session.proposalLifecycle.Reattach(currentProposal); err != nil {
		return fmt.Errorf("reattach durable Proposal: %w", err)
	}
	if current.State() == change.StateIsolated {
		session.setCurrentProposal(currentProposal)
		return nil
	}
	verificationResult, err := session.hydrateVerification(current, currentProposal, load)
	if err != nil {
		return err
	}
	policyDecision, err := session.hydratePolicyDecision(current, verificationResult, load)
	if err != nil {
		return err
	}
	var humanDecision approval.HumanDecision
	hasHumanDecision := current.State() == change.StateApproved || current.State() == change.StateRejected
	if hasHumanDecision {
		humanDecision, err = session.hydrateHumanDecision(current, currentProposal, verificationResult, policyDecision, load)
		if err != nil {
			return err
		}
	}
	session.setCurrentProposal(currentProposal)
	session.setLastVerification(verificationResult)
	session.setLastPolicyDecision(policyDecision)
	if hasHumanDecision {
		session.setLastDecision(humanDecision)
	}
	return nil
}

type approvedScopePayload struct {
	AuthorizationMode source.AuthorizationMode `json:"authorization_mode"`
	Expected          []string                 `json:"expected"`
	Possible          []string                 `json:"possible"`
	Protected         []string                 `json:"protected"`
}

func decodeApprovedScopeRequest(item artifact.Artifact) (source.ScopeRequest, error) {
	var payload approvedScopePayload
	if err := json.Unmarshal(item.Payload(), &payload); err != nil {
		return source.ScopeRequest{}, err
	}
	switch item.KindSchemaVersion() {
	case 1:
		payload.AuthorizationMode = source.AuthorizationExplicitPaths
	case 2:
		if payload.AuthorizationMode == "" {
			return source.ScopeRequest{}, fmt.Errorf("schema v2 requires authorization_mode")
		}
	default:
		return source.ScopeRequest{}, fmt.Errorf("unsupported schema version %d", item.KindSchemaVersion())
	}
	return source.ScopeRequest{
		AuthorizationMode: payload.AuthorizationMode,
		Expected:          payload.Expected,
		Possible:          payload.Possible,
		Protected:         payload.Protected,
	}, nil
}

type artifactLoader func(string, artifact.Kind) (artifact.Artifact, error)

func (session *Session) hydrateVerification(current change.Change, currentProposal proposal.Proposal, load artifactLoader) (verification.Result, error) {
	planItem, err := load("verification-plan", artifact.KindVerificationPlan)
	if err != nil {
		return verification.Result{}, err
	}
	var planPayload struct {
		AttemptId verification.VerificationAttemptId `json:"attempt_id"`
		Steps     []struct {
			Id                 string                       `json:"id"`
			Kind               verification.StepKind        `json:"kind"`
			Executable         string                       `json:"executable"`
			Arguments          []string                     `json:"arguments"`
			WorkingDirectory   string                       `json:"working_directory"`
			Origin             verification.CandidateOrigin `json:"origin"`
			Timeout            string                       `json:"timeout"`
			SupportingEvidence []string                     `json:"supporting_evidence"`
		} `json:"steps"`
	}
	if err := json.Unmarshal(planItem.Payload(), &planPayload); err != nil {
		return verification.Result{}, fmt.Errorf("%w: decode VerificationPlan: %v", authority.ErrCorrupt, err)
	}
	evidenceItem, err := load("evidence-set", artifact.KindEvidenceSet)
	if err != nil {
		return verification.Result{}, err
	}
	var evidencePayload struct {
		Id                    string                             `json:"id"`
		ProjectId             project.ProjectId                  `json:"project_id"`
		ChangeId              change.ChangeId                    `json:"change_id"`
		VerificationAttemptId verification.VerificationAttemptId `json:"verification_attempt_id"`
		WorkspaceId           proposal.WorkspaceId               `json:"workspace_id"`
		PatchDigest           string                             `json:"patch_digest"`
		SourceStateDigest     source.SourceStateDigest           `json:"source_state_digest"`
		Passed                bool                               `json:"passed"`
		Evidence              []struct {
			StepId             string                        `json:"step_id"`
			Kind               verification.StepKind         `json:"kind"`
			Executable         string                        `json:"executable"`
			Arguments          []string                      `json:"arguments"`
			WorkingDirectory   string                        `json:"working_directory"`
			ResolvedExecutable string                        `json:"resolved_executable"`
			Origin             verification.CandidateOrigin  `json:"origin"`
			SupportingEvidence []string                      `json:"supporting_evidence"`
			StartedAt          time.Time                     `json:"started_at"`
			CompletedAt        time.Time                     `json:"completed_at"`
			ExitCode           int                           `json:"exit_code"`
			HasExitCode        bool                          `json:"has_exit_code"`
			Outcome            verification.ExecutionOutcome `json:"outcome"`
			StandardOutput     string                        `json:"standard_output"`
			StandardError      string                        `json:"standard_error"`
			OutputTruncated    bool                          `json:"output_truncated"`
		} `json:"evidence"`
	}
	if err := json.Unmarshal(evidenceItem.Payload(), &evidencePayload); err != nil {
		return verification.Result{}, fmt.Errorf("%w: decode EvidenceSet: %v", authority.ErrCorrupt, err)
	}
	steps := make([]verification.DurableStep, len(planPayload.Steps))
	for index, item := range planPayload.Steps {
		timeout, parseError := time.ParseDuration(item.Timeout)
		if parseError != nil {
			return verification.Result{}, fmt.Errorf("%w: invalid durable VerificationStep timeout", authority.ErrCorrupt)
		}
		steps[index] = verification.DurableStep{Id: item.Id, Kind: item.Kind, Executable: item.Executable, Arguments: item.Arguments, WorkingDirectory: item.WorkingDirectory, Origin: item.Origin, Timeout: timeout, SupportingEvidence: item.SupportingEvidence}
	}
	evidence := make([]verification.DurableEvidence, len(evidencePayload.Evidence))
	for index, item := range evidencePayload.Evidence {
		evidence[index] = verification.DurableEvidence{StepId: item.StepId, Kind: item.Kind, Executable: item.Executable, Arguments: item.Arguments, WorkingDirectory: item.WorkingDirectory, ResolvedExecutable: item.ResolvedExecutable, Origin: item.Origin, SupportingEvidence: item.SupportingEvidence, StartedAt: item.StartedAt, CompletedAt: item.CompletedAt, ExitCode: item.ExitCode, HasExitCode: item.HasExitCode, Outcome: item.Outcome, StandardOutput: item.StandardOutput, StandardError: item.StandardError, OutputTruncated: item.OutputTruncated}
	}
	patch, _ := currentProposal.PatchArtifact()
	value := verification.DurableResult{AttemptId: planPayload.AttemptId, EvidenceSetId: evidencePayload.Id, ProjectId: evidencePayload.ProjectId, ChangeId: evidencePayload.ChangeId, WorkspaceId: evidencePayload.WorkspaceId, PatchDigest: evidencePayload.PatchDigest, SourceDigest: evidencePayload.SourceStateDigest, Passed: evidencePayload.Passed, Steps: steps, Evidence: evidence}
	result, err := verification.RehydrateResult(value)
	if err != nil || result.AttemptId() != evidencePayload.VerificationAttemptId || result.EvidenceSet().ProjectId() != current.ProjectId() || result.EvidenceSet().ChangeId() != current.ChangeId() || result.EvidenceSet().WorkspaceId() != currentProposal.Workspace().WorkspaceId() || result.EvidenceSet().PatchDigest() != patch.PatchDigest() {
		return verification.Result{}, fmt.Errorf("%w: durable verification authority linkage is invalid: %v", authority.ErrCorrupt, err)
	}
	return result, nil
}

func (session *Session) hydratePolicyDecision(current change.Change, result verification.Result, load artifactLoader) (policy.BundleDecision, error) {
	item, err := load("policy-decision", artifact.KindPolicyDecision)
	if err != nil {
		return policy.BundleDecision{}, err
	}
	var payload struct {
		Id                    string                             `json:"id"`
		ProjectId             project.ProjectId                  `json:"project_id"`
		ChangeId              change.ChangeId                    `json:"change_id"`
		WorkspaceId           proposal.WorkspaceId               `json:"workspace_id"`
		VerificationAttemptId verification.VerificationAttemptId `json:"verification_attempt_id"`
		EvidenceSetId         string                             `json:"evidence_set_id"`
		PatchDigest           string                             `json:"patch_digest"`
		SourceStateDigest     source.SourceStateDigest           `json:"source_state_digest"`
		BundleId              policy.PolicyId                    `json:"bundle_id"`
		BundleVersion         policy.PolicyVersion               `json:"bundle_version"`
		BundleDigest          string                             `json:"bundle_digest"`
		EvaluatedAt           time.Time                          `json:"evaluated_at"`
		Policies              []struct {
			Id                        policy.PolicyId           `json:"id"`
			Version                   policy.PolicyVersion      `json:"version"`
			Family                    policy.PolicyFamily       `json:"family"`
			Description               string                    `json:"description"`
			Severity                  policy.Severity           `json:"severity"`
			Outcome                   policy.EnforcementOutcome `json:"outcome"`
			RequiredEvidenceKind      verification.StepKind     `json:"required_evidence_kind"`
			NonOverridable            bool                      `json:"non_overridable"`
			ExceptionCandidateAllowed bool                      `json:"exception_candidate_allowed"`
		} `json:"policies"`
		Decisions []struct {
			PolicyId      policy.PolicyId           `json:"policy_id"`
			PolicyVersion policy.PolicyVersion      `json:"policy_version"`
			Outcome       policy.EnforcementOutcome `json:"outcome"`
			Reason        string                    `json:"reason"`
			EvidenceIds   []string                  `json:"evidence_ids"`
		} `json:"decisions"`
	}
	if err := json.Unmarshal(item.Payload(), &payload); err != nil {
		return policy.BundleDecision{}, fmt.Errorf("%w: decode PolicyDecision: %v", authority.ErrCorrupt, err)
	}
	policies := make([]policy.DurablePolicy, len(payload.Policies))
	for index, value := range payload.Policies {
		policies[index] = policy.DurablePolicy{Id: value.Id, Version: value.Version, Family: value.Family, Description: value.Description, Severity: value.Severity, Outcome: value.Outcome, RequiredEvidenceKind: value.RequiredEvidenceKind, NonOverridable: value.NonOverridable, ExceptionCandidateAllowed: value.ExceptionCandidateAllowed}
	}
	decisions := make([]policy.DurablePolicyDecision, len(payload.Decisions))
	for index, value := range payload.Decisions {
		decisions[index] = policy.DurablePolicyDecision{PolicyId: value.PolicyId, PolicyVersion: value.PolicyVersion, Outcome: value.Outcome, Reason: value.Reason, EvidenceIds: value.EvidenceIds}
	}
	decision, err := policy.RehydrateBundleDecision(policy.DurableBundleDecision{Id: payload.Id, ProjectId: payload.ProjectId, ChangeId: payload.ChangeId, WorkspaceId: payload.WorkspaceId, VerificationAttemptId: payload.VerificationAttemptId, EvidenceSetId: payload.EvidenceSetId, PatchDigest: payload.PatchDigest, SourceStateDigest: payload.SourceStateDigest, BundleId: payload.BundleId, BundleVersion: payload.BundleVersion, BundleDigest: payload.BundleDigest, Policies: policies, Decisions: decisions, EvaluatedAt: payload.EvaluatedAt})
	if err != nil || policy.ValidateLinkage(decision, current.ProjectId(), current.ChangeId(), result.EvidenceSet()) != nil {
		return policy.BundleDecision{}, fmt.Errorf("%w: durable PolicyDecision linkage is invalid: %v", authority.ErrCorrupt, err)
	}
	return decision, nil
}

func (session *Session) hydrateHumanDecision(current change.Change, currentProposal proposal.Proposal, result verification.Result, policyDecision policy.BundleDecision, load artifactLoader) (approval.HumanDecision, error) {
	item, err := load("human-decision", artifact.KindHumanDecision)
	if err != nil {
		return approval.HumanDecision{}, err
	}
	var payload struct {
		Kind                   approval.DecisionKind              `json:"kind"`
		Actor                  approval.ActorProvenance           `json:"actor"`
		Rationale              string                             `json:"rationale"`
		RequestedState         change.ChangeState                 `json:"requested_state"`
		WorkspaceId            proposal.WorkspaceId               `json:"workspace_id"`
		BaseRevision           string                             `json:"base_revision"`
		SourceStateDigest      source.SourceStateDigest           `json:"source_state_digest"`
		PatchDigest            string                             `json:"patch_digest"`
		VerificationAttemptId  verification.VerificationAttemptId `json:"verification_attempt_id"`
		EvidenceSetId          string                             `json:"evidence_set_id"`
		EvidenceCount          int                                `json:"evidence_count"`
		ChangedPathCount       int                                `json:"changed_path_count"`
		PolicyEvaluationId     string                             `json:"policy_evaluation_id"`
		PolicyBundleDigest     string                             `json:"policy_bundle_digest"`
		PolicyDenied           bool                               `json:"policy_denied"`
		PolicyRequiresReview   bool                               `json:"policy_requires_review"`
		PolicyRequiresApproval bool                               `json:"policy_requires_approval"`
	}
	if err := json.Unmarshal(item.Payload(), &payload); err != nil {
		return approval.HumanDecision{}, fmt.Errorf("%w: decode HumanDecision: %v", authority.ErrCorrupt, err)
	}
	decision, err := approval.RehydrateHumanDecision(approval.DurableHumanDecision{ProjectId: current.ProjectId(), ChangeId: current.ChangeId(), WorkspaceId: payload.WorkspaceId, Kind: payload.Kind, OccurredAt: item.CreatedAt(), Rationale: payload.Rationale, Actor: payload.Actor, RequestedState: payload.RequestedState, BaseRevision: payload.BaseRevision, SourceStateDigest: payload.SourceStateDigest, PatchDigest: payload.PatchDigest, VerificationAttemptId: payload.VerificationAttemptId, EvidenceSetId: payload.EvidenceSetId, EvidenceCount: payload.EvidenceCount, ChangedPathCount: payload.ChangedPathCount, PolicyEvaluationId: payload.PolicyEvaluationId, PolicyBundleDigest: payload.PolicyBundleDigest, PolicyDenied: payload.PolicyDenied, PolicyRequiresReview: payload.PolicyRequiresReview, PolicyRequiresApproval: payload.PolicyRequiresApproval})
	patch, _ := currentProposal.PatchArtifact()
	if err != nil || decision.WorkspaceId() != currentProposal.Workspace().WorkspaceId() || decision.PatchDigest() != patch.PatchDigest() || decision.EvidenceSetId() != result.EvidenceSet().Id() || decision.PolicyEvaluationId() != policyDecision.Id() || decision.RequestedState() != current.State() {
		return approval.HumanDecision{}, fmt.Errorf("%w: durable HumanDecision linkage is invalid: %v", authority.ErrCorrupt, err)
	}
	return decision, nil
}

func durableWorkspaceState(events []audit.Event, workspaceId proposal.WorkspaceId) proposal.WorkspaceState {
	state := proposal.WorkspaceActive
	for _, event := range events {
		if event.Metadata["workspace_id"] != string(workspaceId) {
			continue
		}
		switch event.EventType {
		case proposal.EventProposalWorkspaceCreated:
			state = proposal.WorkspaceActive
		case proposal.EventPatchSurfaceValidated:
			state = proposal.WorkspaceRetained
		case proposal.EventPatchRejected:
			state = proposal.WorkspaceRejected
		case proposal.EventProposalWorkspaceFailed:
			state = proposal.WorkspaceFailed
		case proposal.EventProposalWorkspaceCleanupFailed:
			state = proposal.WorkspaceCleanupFailed
		case proposal.EventProposalWorkspaceDiscarded:
			state = proposal.WorkspaceCleaned
		case "PROVIDER_EXECUTION_FAILED":
			if event.Metadata["workspace_may_be_changed"] == true {
				state = proposal.WorkspaceFailed
			}
		}
	}
	return state
}

func latestCreatedWorkspaceId(events []audit.Event) string {
	latest := ""
	for _, event := range events {
		if event.EventType == proposal.EventProposalWorkspaceCreated {
			if workspaceId, ok := event.Metadata["workspace_id"].(string); ok {
				latest = workspaceId
			}
		}
	}
	return latest
}
