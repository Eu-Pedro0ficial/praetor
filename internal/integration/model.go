// Package integration owns the M0.8 governed publication of an explicitly
// approved PatchArtifact to canonical source. It does not commit, push, merge,
// or provide remote SCM behavior.
package integration

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/Eu-Pedro0ficial/praetor/internal/approval"
	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"github.com/Eu-Pedro0ficial/praetor/internal/project"
	"github.com/Eu-Pedro0ficial/praetor/internal/proposal"
	"github.com/Eu-Pedro0ficial/praetor/internal/source"
	"github.com/Eu-Pedro0ficial/praetor/internal/verification"
)

// ApplicationRequest is the exact retained proposal boundary presented to
// the canonical-source adapter. Patch bytes are passed through memory and are
// never persisted by this capability.
type ApplicationRequest struct {
	Proposal proposal.Proposal
}

// CanonicalProof is deterministic adapter evidence that the canonical
// working tree contains exactly the retained patch while HEAD and the index
// remain at the approved base revision.
type CanonicalProof struct {
	headRevision string
	patchDigest  string
	changedPaths []string
	indexClean   bool
}

// NewCanonicalProof validates proof produced by a canonical-source adapter.
func NewCanonicalProof(
	headRevision string,
	patchDigest string,
	changedPaths []string,
	indexClean bool,
) (CanonicalProof, error) {
	headRevision = strings.TrimSpace(headRevision)
	if headRevision == "" {
		return CanonicalProof{}, fmt.Errorf("canonical proof HEAD revision is required")
	}
	if !validSHA256Digest(patchDigest) {
		return CanonicalProof{}, fmt.Errorf("canonical proof patch digest is invalid")
	}
	paths := append([]string(nil), changedPaths...)
	if len(paths) == 0 {
		return CanonicalProof{}, fmt.Errorf("canonical proof changed paths are required")
	}
	sort.Strings(paths)
	for index, path := range paths {
		if _, err := source.NormalizeRepositoryPath(path); err != nil {
			return CanonicalProof{}, fmt.Errorf("canonical proof changed path: %w", err)
		}
		if index > 0 && paths[index-1] == path {
			return CanonicalProof{}, fmt.Errorf("canonical proof contains duplicate changed path %q", path)
		}
	}
	if !indexClean {
		return CanonicalProof{}, fmt.Errorf("canonical proof requires an unchanged Git index")
	}
	return CanonicalProof{
		headRevision: headRevision,
		patchDigest:  patchDigest,
		changedPaths: paths,
		indexClean:   true,
	}, nil
}

func (proof CanonicalProof) HeadRevision() string { return proof.headRevision }
func (proof CanonicalProof) PatchDigest() string  { return proof.patchDigest }
func (proof CanonicalProof) ChangedPaths() []string {
	return append([]string(nil), proof.changedPaths...)
}
func (proof CanonicalProof) IndexUnchanged() bool { return proof.indexClean }

// Result records bounded deterministic proof for one successful application.
// CanonicalMutationOccurred may be true on an error when mutation completed
// but later proof or audit could not be completed.
type Result struct {
	projectId                  project.ProjectId
	changeId                   change.ChangeId
	workspaceId                proposal.WorkspaceId
	baseRevision               string
	sourceStateDigest          source.SourceStateDigest
	resultingSourceStateDigest source.SourceStateDigest
	patchDigest                string
	verificationAttemptId      verification.VerificationAttemptId
	evidenceSetId              string
	decisionKind               approval.DecisionKind
	changedPaths               []string
	canonicalHead              string
	indexUnchanged             bool
	canonicalMutationOccurred  bool
	canonicalApplicationProven bool
	resultDigest               string
	completedAt                time.Time
}

func newResult(
	currentChange change.Change,
	currentProposal proposal.Proposal,
	verificationResult verification.Result,
	decision approval.HumanDecision,
	proof CanonicalProof,
	resultingSource source.SourceSnapshot,
	completedAt time.Time,
) (Result, error) {
	artifact, hasArtifact := currentProposal.PatchArtifact()
	if !hasArtifact {
		return Result{}, fmt.Errorf("canonical application result requires PatchArtifact")
	}
	if completedAt.IsZero() {
		return Result{}, fmt.Errorf("canonical application completion timestamp is required")
	}
	if resultingSource.ProjectId() != currentChange.ProjectId() ||
		resultingSource.RepositoryRoot() != currentProposal.CanonicalSource().RepositoryRoot() ||
		resultingSource.HeadRevision() != artifact.BaseRevision() ||
		resultingSource.WorkingTreeState() != source.WorkingTreeDirty ||
		resultingSource.SourceStateDigest() == artifact.SourceStateDigest() ||
		!equalRepositoryPaths(resultingSource.TrackedPaths(), currentProposal.CanonicalSource().TrackedPaths()) {
		return Result{}, fmt.Errorf("post-application SourceSnapshot does not prove the authorized canonical result")
	}
	if proof.HeadRevision() != artifact.BaseRevision() ||
		proof.PatchDigest() != artifact.PatchDigest() ||
		!proof.IndexUnchanged() ||
		!equalStrings(proof.ChangedPaths(), artifact.ChangedPaths()) {
		return Result{}, fmt.Errorf("canonical adapter proof does not match the approved PatchArtifact")
	}
	digest := resultIdentity(
		artifact,
		verificationResult.EvidenceSet(),
		decision,
		resultingSource.SourceStateDigest(),
		proof,
	)
	return Result{
		projectId:                  currentChange.ProjectId(),
		changeId:                   currentChange.ChangeId(),
		workspaceId:                currentProposal.Workspace().WorkspaceId(),
		baseRevision:               artifact.BaseRevision(),
		sourceStateDigest:          artifact.SourceStateDigest(),
		resultingSourceStateDigest: resultingSource.SourceStateDigest(),
		patchDigest:                artifact.PatchDigest(),
		verificationAttemptId:      verificationResult.EvidenceSet().VerificationAttemptId(),
		evidenceSetId:              verificationResult.EvidenceSet().Id(),
		decisionKind:               decision.Kind(),
		changedPaths:               artifact.ChangedPaths(),
		canonicalHead:              proof.HeadRevision(),
		indexUnchanged:             proof.IndexUnchanged(),
		canonicalMutationOccurred:  true,
		canonicalApplicationProven: true,
		resultDigest:               digest,
		completedAt:                completedAt.UTC(),
	}, nil
}

func (result Result) ProjectId() project.ProjectId                { return result.projectId }
func (result Result) ChangeId() change.ChangeId                   { return result.changeId }
func (result Result) WorkspaceId() proposal.WorkspaceId           { return result.workspaceId }
func (result Result) BaseRevision() string                        { return result.baseRevision }
func (result Result) SourceStateDigest() source.SourceStateDigest { return result.sourceStateDigest }
func (result Result) ResultingSourceStateDigest() source.SourceStateDigest {
	return result.resultingSourceStateDigest
}
func (result Result) PatchDigest() string { return result.patchDigest }
func (result Result) VerificationAttemptId() verification.VerificationAttemptId {
	return result.verificationAttemptId
}
func (result Result) EvidenceSetId() string               { return result.evidenceSetId }
func (result Result) DecisionKind() approval.DecisionKind { return result.decisionKind }
func (result Result) ChangedPaths() []string              { return append([]string(nil), result.changedPaths...) }
func (result Result) CanonicalHead() string               { return result.canonicalHead }
func (result Result) IndexUnchanged() bool                { return result.indexUnchanged }
func (result Result) CanonicalMutationOccurred() bool     { return result.canonicalMutationOccurred }
func (result Result) CanonicalApplicationProven() bool    { return result.canonicalApplicationProven }
func (result Result) ResultDigest() string                { return result.resultDigest }
func (result Result) CompletedAt() time.Time              { return result.completedAt }

func resultIdentity(
	artifact proposal.PatchArtifact,
	evidence verification.EvidenceSet,
	decision approval.HumanDecision,
	resultingDigest source.SourceStateDigest,
	proof CanonicalProof,
) string {
	values := []string{
		"praetor-canonical-application-v1",
		artifact.BaseRevision(),
		string(artifact.SourceStateDigest()),
		artifact.PatchDigest(),
		string(evidence.VerificationAttemptId()),
		evidence.Id(),
		string(decision.Kind()),
		string(resultingDigest),
		proof.HeadRevision(),
		strings.Join(proof.ChangedPaths(), "\x00"),
	}
	digest := sha256.Sum256([]byte(strings.Join(values, "\x1f")))
	return "sha256:" + hex.EncodeToString(digest[:])
}

func validSHA256Digest(value string) bool {
	if !strings.HasPrefix(value, "sha256:") {
		return false
	}
	encoded := strings.TrimPrefix(value, "sha256:")
	if len(encoded) != 64 {
		return false
	}
	_, err := hex.DecodeString(encoded)
	return err == nil
}

func equalStrings(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}

func equalRepositoryPaths(left, right []source.RepositoryPath) bool {
	if len(left) != len(right) {
		return false
	}
	for index := range left {
		if left[index] != right[index] {
			return false
		}
	}
	return true
}
