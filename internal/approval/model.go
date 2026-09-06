// Package approval owns the M0.7 explicit local-human decision gate.
package approval

import (
	"fmt"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"github.com/Eu-Pedro0ficial/praetor/internal/project"
	"github.com/Eu-Pedro0ficial/praetor/internal/proposal"
	"github.com/Eu-Pedro0ficial/praetor/internal/source"
	"github.com/Eu-Pedro0ficial/praetor/internal/verification"
)

const maximumRationaleBytes = 1024

// DecisionKind is the exact M0.7 human disposition vocabulary.
type DecisionKind string

const (
	DecisionApprove DecisionKind = "APPROVE"
	DecisionReject  DecisionKind = "REJECT"
)

// ActorProvenance truthfully identifies the assurance available in local V0.
// It is not an authenticated operating-system or enterprise identity.
type ActorProvenance string

const ActorLocalInteractiveHuman ActorProvenance = "local-interactive-human"

// Rationale is one optional bounded, single-line human explanation.
type Rationale string

// NewRationale validates terminal- and audit-safe decision rationale text.
func NewRationale(value string) (Rationale, error) {
	if !utf8.ValidString(value) {
		return "", fmt.Errorf("human decision rationale contains invalid UTF-8")
	}
	if len(value) > maximumRationaleBytes {
		return "", fmt.Errorf("human decision rationale exceeds %d bytes", maximumRationaleBytes)
	}
	for _, character := range value {
		if unicode.IsControl(character) || unicode.In(character, unicode.Zl, unicode.Zp) {
			return "", fmt.Errorf("human decision rationale contains control characters")
		}
	}
	trimmed := strings.TrimSpace(value)
	return Rationale(trimmed), nil
}

// HumanDecision is immutable M0.7 authorization provenance. It contains only
// bounded linkage and never embeds patch, source, provider, or process output.
type HumanDecision struct {
	projectId             project.ProjectId
	changeId              change.ChangeId
	workspaceId           proposal.WorkspaceId
	kind                  DecisionKind
	occurredAt            time.Time
	rationale             Rationale
	actor                 ActorProvenance
	requestedState        change.ChangeState
	baseRevision          string
	sourceStateDigest     source.SourceStateDigest
	patchDigest           string
	verificationAttemptId verification.VerificationAttemptId
	evidenceSetId         string
	evidenceCount         int
	changedPathCount      int
}

func newHumanDecision(
	currentChange change.Change,
	currentProposal proposal.Proposal,
	verificationResult verification.Result,
	kind DecisionKind,
	rationale Rationale,
	occurredAt time.Time,
) (HumanDecision, error) {
	requestedState, err := resultingState(kind)
	if err != nil {
		return HumanDecision{}, err
	}
	if occurredAt.IsZero() {
		return HumanDecision{}, fmt.Errorf("human decision timestamp is required")
	}
	if occurredAt.UTC().Before(currentChange.UpdatedAt()) {
		return HumanDecision{}, fmt.Errorf("human decision timestamp precedes the validated Change state")
	}
	artifact, hasArtifact := currentProposal.PatchArtifact()
	if !hasArtifact {
		return HumanDecision{}, fmt.Errorf("human decision requires a retained PatchArtifact")
	}
	evidenceSet := verificationResult.EvidenceSet()
	return HumanDecision{
		projectId:             currentChange.ProjectId(),
		changeId:              currentChange.ChangeId(),
		workspaceId:           currentProposal.Workspace().WorkspaceId(),
		kind:                  kind,
		occurredAt:            occurredAt.UTC(),
		rationale:             rationale,
		actor:                 ActorLocalInteractiveHuman,
		requestedState:        requestedState,
		baseRevision:          artifact.BaseRevision(),
		sourceStateDigest:     artifact.SourceStateDigest(),
		patchDigest:           artifact.PatchDigest(),
		verificationAttemptId: evidenceSet.VerificationAttemptId(),
		evidenceSetId:         evidenceSet.Id(),
		evidenceCount:         len(evidenceSet.Evidence()),
		changedPathCount:      len(artifact.ChangedPaths()),
	}, nil
}

func resultingState(kind DecisionKind) (change.ChangeState, error) {
	switch kind {
	case DecisionApprove:
		return change.StateApproved, nil
	case DecisionReject:
		return change.StateRejected, nil
	default:
		return "", fmt.Errorf("unknown human decision %q", kind)
	}
}

func (decision HumanDecision) ProjectId() project.ProjectId { return decision.projectId }
func (decision HumanDecision) ChangeId() change.ChangeId    { return decision.changeId }
func (decision HumanDecision) WorkspaceId() proposal.WorkspaceId {
	return decision.workspaceId
}
func (decision HumanDecision) Kind() DecisionKind                 { return decision.kind }
func (decision HumanDecision) OccurredAt() time.Time              { return decision.occurredAt }
func (decision HumanDecision) Rationale() Rationale               { return decision.rationale }
func (decision HumanDecision) Actor() ActorProvenance             { return decision.actor }
func (decision HumanDecision) RequestedState() change.ChangeState { return decision.requestedState }
func (decision HumanDecision) BaseRevision() string               { return decision.baseRevision }
func (decision HumanDecision) SourceStateDigest() source.SourceStateDigest {
	return decision.sourceStateDigest
}
func (decision HumanDecision) PatchDigest() string { return decision.patchDigest }
func (decision HumanDecision) VerificationAttemptId() verification.VerificationAttemptId {
	return decision.verificationAttemptId
}
func (decision HumanDecision) EvidenceSetId() string { return decision.evidenceSetId }
func (decision HumanDecision) EvidenceCount() int    { return decision.evidenceCount }
func (decision HumanDecision) ChangedPathCount() int { return decision.changedPathCount }
