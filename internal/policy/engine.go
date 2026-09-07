package policy

import (
	"fmt"
	"sort"
	"time"

	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"github.com/Eu-Pedro0ficial/praetor/internal/project"
	"github.com/Eu-Pedro0ficial/praetor/internal/verification"
)

// Port is the provider- and representation-independent policy evaluation boundary.
type Port interface {
	Evaluate(project.ProjectId, change.ChangeId, verification.EvidenceSet, PolicyBundle, time.Time) (BundleDecision, error)
}

type Engine struct{}

func NewEngine() *Engine { return &Engine{} }

func (engine *Engine) Evaluate(projectId project.ProjectId, changeId change.ChangeId, evidence verification.EvidenceSet, bundle PolicyBundle, evaluatedAt time.Time) (BundleDecision, error) {
	if engine == nil {
		return BundleDecision{}, fmt.Errorf("Policy Engine is required")
	}
	if !projectId.IsValid() || changeId == "" || evidence.Id() == "" || bundle.Digest() == "" || evaluatedAt.IsZero() {
		return BundleDecision{}, fmt.Errorf("policy evaluation linkage is incomplete")
	}
	if evidence.ProjectId() != projectId || evidence.ChangeId() != changeId || evidence.WorkspaceId() == "" || evidence.VerificationAttemptId() == "" || evidence.PatchDigest() == "" || evidence.SourceStateDigest() == "" {
		return BundleDecision{}, fmt.Errorf("policy evaluation EvidenceSet linkage is inconsistent")
	}

	byKind := map[verification.StepKind][]verification.Evidence{}
	for _, item := range evidence.Evidence() {
		byKind[item.Kind()] = append(byKind[item.Kind()], item)
	}
	decisions := make([]PolicyDecision, 0, len(bundle.policies))
	aggregate := AggregateRequirements{}
	for _, rule := range bundle.policies {
		matches := byKind[rule.requiredEvidenceKind]
		outcome := rule.outcome
		reason := fmt.Sprintf("required %s evidence passed", rule.requiredEvidenceKind)
		ids := make([]string, 0, len(matches))
		passing := len(matches) > 0
		for _, item := range matches {
			ids = append(ids, item.StepId())
			if item.Outcome() != verification.OutcomePass {
				passing = false
			}
		}
		sort.Strings(ids)
		if !passing || !evidence.Passed() {
			outcome = OutcomeForbidden
			reason = fmt.Sprintf("required passing %s evidence is missing or failed", rule.requiredEvidenceKind)
		}
		decisions = append(decisions, PolicyDecision{policy: rule, outcome: outcome, reason: reason, evidenceIds: ids})
	}
	aggregate, err := AggregateOutcomes(decisionOutcomes(decisions))
	if err != nil {
		return BundleDecision{}, err
	}
	return BundleDecision{id: decisionIdentity(bundle.digest, evidence.Id()), projectId: projectId, changeId: changeId, workspaceId: evidence.WorkspaceId(), verificationAttemptId: evidence.VerificationAttemptId(), evidenceSetId: evidence.Id(), patchDigest: evidence.PatchDigest(), sourceStateDigest: evidence.SourceStateDigest(), bundle: bundle, decisions: decisions, aggregate: aggregate, evaluatedAt: evaluatedAt.UTC()}, nil
}

// AggregateOutcomes deterministically accumulates independent governance requirements.
func AggregateOutcomes(outcomes []EnforcementOutcome) (AggregateRequirements, error) {
	var aggregate AggregateRequirements
	for _, outcome := range outcomes {
		switch outcome {
		case OutcomeAuto:
		case OutcomeReview:
			aggregate.RequiresReview = true
		case OutcomeApproval:
			aggregate.RequiresApproval = true
		case OutcomeForbidden:
			aggregate.Denied = true
		default:
			return AggregateRequirements{}, fmt.Errorf("unknown EnforcementOutcome %q", outcome)
		}
	}
	return aggregate, nil
}

func decisionOutcomes(decisions []PolicyDecision) []EnforcementOutcome {
	values := make([]EnforcementOutcome, len(decisions))
	for index, decision := range decisions {
		values[index] = decision.outcome
	}
	return values
}

func ValidateLinkage(decision BundleDecision, projectId project.ProjectId, changeId change.ChangeId, evidence verification.EvidenceSet) error {
	if !decision.IsValid() || decision.ProjectId() != projectId || decision.ChangeId() != changeId || decision.WorkspaceId() != evidence.WorkspaceId() || decision.VerificationAttemptId() != evidence.VerificationAttemptId() || decision.EvidenceSetId() != evidence.Id() || decision.PatchDigest() != evidence.PatchDigest() || decision.SourceStateDigest() != evidence.SourceStateDigest() {
		return fmt.Errorf("policy decision authority linkage is inconsistent")
	}
	return nil
}
