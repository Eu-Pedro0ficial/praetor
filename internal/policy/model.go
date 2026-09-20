// Package policy owns representation-independent M1.0 governance rules and decisions.
package policy

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"github.com/Eu-Pedro0ficial/praetor/internal/project"
	"github.com/Eu-Pedro0ficial/praetor/internal/proposal"
	"github.com/Eu-Pedro0ficial/praetor/internal/source"
	"github.com/Eu-Pedro0ficial/praetor/internal/verification"
)

type PolicyId string
type PolicyVersion string
type PolicyFamily string
type Severity string
type EnforcementOutcome string

const (
	FamilyCleanCode             PolicyFamily = "clean-code"
	FamilyTesting               PolicyFamily = "testing"
	FamilyArchitecture          PolicyFamily = "architecture"
	FamilySecurity              PolicyFamily = "security"
	FamilyDependencyManagement  PolicyFamily = "dependency-management"
	FamilyChangeSurface         PolicyFamily = "change-surface"
	FamilyBackwardCompatibility PolicyFamily = "backward-compatibility"
	FamilyPerformance           PolicyFamily = "performance"
	FamilyObservability         PolicyFamily = "observability"
	FamilyErrorHandling         PolicyFamily = "error-handling"
	FamilyAPIContracts          PolicyFamily = "api-contracts"
	FamilyDatabase              PolicyFamily = "database"

	SeverityInfo     Severity = "INFO"
	SeverityLow      Severity = "LOW"
	SeverityMedium   Severity = "MEDIUM"
	SeverityHigh     Severity = "HIGH"
	SeverityCritical Severity = "CRITICAL"

	OutcomeAuto      EnforcementOutcome = "AUTO"
	OutcomeReview    EnforcementOutcome = "REVIEW"
	OutcomeApproval  EnforcementOutcome = "APPROVAL"
	OutcomeForbidden EnforcementOutcome = "FORBIDDEN"
)

var identityPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,127}$`)
var versionPattern = regexp.MustCompile(`^[0-9]+(?:\.[0-9]+){0,2}$`)

// Policy is one immutable generic evidence requirement and its governance consequence.
type Policy struct {
	id                        PolicyId
	version                   PolicyVersion
	family                    PolicyFamily
	description               string
	severity                  Severity
	outcome                   EnforcementOutcome
	requiredEvidenceKind      verification.StepKind
	nonOverridable            bool
	exceptionCandidateAllowed bool
}

func NewPolicy(id PolicyId, version PolicyVersion, family PolicyFamily, description string, severity Severity, outcome EnforcementOutcome, requiredEvidenceKind verification.StepKind, nonOverridable, exceptionCandidateAllowed bool) (Policy, error) {
	if !identityPattern.MatchString(string(id)) {
		return Policy{}, fmt.Errorf("PolicyId %q is invalid", id)
	}
	if !versionPattern.MatchString(string(version)) {
		return Policy{}, fmt.Errorf("PolicyVersion %q is invalid", version)
	}
	if !validFamily(family) {
		return Policy{}, fmt.Errorf("PolicyFamily %q is invalid", family)
	}
	if strings.TrimSpace(description) == "" || strings.TrimSpace(description) != description || len(description) > 512 {
		return Policy{}, fmt.Errorf("policy description is invalid")
	}
	if !validSeverity(severity) {
		return Policy{}, fmt.Errorf("Severity %q is invalid", severity)
	}
	if !validOutcome(outcome) {
		return Policy{}, fmt.Errorf("EnforcementOutcome %q is invalid", outcome)
	}
	if !validEvidenceKind(requiredEvidenceKind) {
		return Policy{}, fmt.Errorf("required evidence kind %q is invalid", requiredEvidenceKind)
	}
	return Policy{id: id, version: version, family: family, description: description, severity: severity, outcome: outcome, requiredEvidenceKind: requiredEvidenceKind, nonOverridable: nonOverridable, exceptionCandidateAllowed: exceptionCandidateAllowed}, nil
}

func (value Policy) Id() PolicyId                                { return value.id }
func (value Policy) Version() PolicyVersion                      { return value.version }
func (value Policy) Family() PolicyFamily                        { return value.family }
func (value Policy) Description() string                         { return value.description }
func (value Policy) Severity() Severity                          { return value.severity }
func (value Policy) Outcome() EnforcementOutcome                 { return value.outcome }
func (value Policy) RequiredEvidenceKind() verification.StepKind { return value.requiredEvidenceKind }
func (value Policy) NonOverridable() bool                        { return value.nonOverridable }
func (value Policy) ExceptionCandidateAllowed() bool             { return value.exceptionCandidateAllowed }

type PolicyBundle struct {
	id       PolicyId
	version  PolicyVersion
	digest   string
	policies []Policy
}

func NewBundle(id PolicyId, version PolicyVersion, digest string, policies []Policy) (PolicyBundle, error) {
	if !identityPattern.MatchString(string(id)) {
		return PolicyBundle{}, fmt.Errorf("PolicyBundle identity %q is invalid", id)
	}
	if !versionPattern.MatchString(string(version)) {
		return PolicyBundle{}, fmt.Errorf("PolicyBundle version %q is invalid", version)
	}
	decodedDigest, digestError := hex.DecodeString(digest)
	if digestError != nil || len(decodedDigest) != sha256.Size || strings.ToLower(digest) != digest {
		return PolicyBundle{}, fmt.Errorf("PolicyBundle digest is invalid")
	}
	if len(policies) == 0 || len(policies) > 128 {
		return PolicyBundle{}, fmt.Errorf("PolicyBundle requires between 1 and 128 policies")
	}
	copyPolicies := append([]Policy(nil), policies...)
	seen := map[PolicyId]struct{}{}
	for _, item := range copyPolicies {
		if item.id == "" {
			return PolicyBundle{}, fmt.Errorf("PolicyBundle contains an invalid policy")
		}
		if _, exists := seen[item.id]; exists {
			return PolicyBundle{}, fmt.Errorf("duplicate PolicyId %q", item.id)
		}
		seen[item.id] = struct{}{}
	}
	sort.Slice(copyPolicies, func(i, j int) bool { return copyPolicies[i].id < copyPolicies[j].id })
	return PolicyBundle{id: id, version: version, digest: digest, policies: copyPolicies}, nil
}

func (value PolicyBundle) Id() PolicyId           { return value.id }
func (value PolicyBundle) Version() PolicyVersion { return value.version }
func (value PolicyBundle) Digest() string         { return value.digest }
func (value PolicyBundle) Policies() []Policy     { return append([]Policy(nil), value.policies...) }

// ComposeBundle applies later configuration while preserving non-overridable governance authority.
func ComposeBundle(id PolicyId, version PolicyVersion, digest string, authoritative, configured PolicyBundle) (PolicyBundle, error) {
	combined := map[PolicyId]Policy{}
	for _, rule := range authoritative.policies {
		combined[rule.id] = rule
	}
	for _, rule := range configured.policies {
		if existing, found := combined[rule.id]; found && existing.nonOverridable && existing != rule {
			return PolicyBundle{}, fmt.Errorf("configuration cannot override non-overridable PolicyId %q", rule.id)
		}
		combined[rule.id] = rule
	}
	values := make([]Policy, 0, len(combined))
	for _, rule := range combined {
		values = append(values, rule)
	}
	return NewBundle(id, version, digest, values)
}

type AggregateRequirements struct {
	Denied           bool
	RequiresReview   bool
	RequiresApproval bool
}

func (value AggregateRequirements) AllowsPositiveDisposition() bool {
	return !value.Denied && !value.RequiresReview
}

type PolicyDecision struct {
	policy      Policy
	outcome     EnforcementOutcome
	reason      string
	evidenceIds []string
}

func (value PolicyDecision) Policy() Policy              { return value.policy }
func (value PolicyDecision) Outcome() EnforcementOutcome { return value.outcome }
func (value PolicyDecision) Reason() string              { return value.reason }
func (value PolicyDecision) EvidenceIds() []string {
	return append([]string(nil), value.evidenceIds...)
}

type BundleDecision struct {
	id                    string
	projectId             project.ProjectId
	changeId              change.ChangeId
	workspaceId           proposal.WorkspaceId
	verificationAttemptId verification.VerificationAttemptId
	evidenceSetId         string
	patchDigest           string
	sourceStateDigest     source.SourceStateDigest
	bundle                PolicyBundle
	decisions             []PolicyDecision
	aggregate             AggregateRequirements
	evaluatedAt           time.Time
}

type DurablePolicy struct {
	Id                        PolicyId
	Version                   PolicyVersion
	Family                    PolicyFamily
	Description               string
	Severity                  Severity
	Outcome                   EnforcementOutcome
	RequiredEvidenceKind      verification.StepKind
	NonOverridable            bool
	ExceptionCandidateAllowed bool
}

type DurablePolicyDecision struct {
	PolicyId      PolicyId
	PolicyVersion PolicyVersion
	Outcome       EnforcementOutcome
	Reason        string
	EvidenceIds   []string
}

type DurableBundleDecision struct {
	Id                    string
	ProjectId             project.ProjectId
	ChangeId              change.ChangeId
	WorkspaceId           proposal.WorkspaceId
	VerificationAttemptId verification.VerificationAttemptId
	EvidenceSetId         string
	PatchDigest           string
	SourceStateDigest     source.SourceStateDigest
	BundleId              PolicyId
	BundleVersion         PolicyVersion
	BundleDigest          string
	Policies              []DurablePolicy
	Decisions             []DurablePolicyDecision
	EvaluatedAt           time.Time
}

// RehydrateBundleDecision validates persisted policy authority without
// consulting the current Project Policy Manifest.
func RehydrateBundleDecision(value DurableBundleDecision) (BundleDecision, error) {
	policies := make([]Policy, len(value.Policies))
	byId := make(map[PolicyId]Policy, len(value.Policies))
	for index, stored := range value.Policies {
		item, err := NewPolicy(stored.Id, stored.Version, stored.Family, stored.Description, stored.Severity, stored.Outcome, stored.RequiredEvidenceKind, stored.NonOverridable, stored.ExceptionCandidateAllowed)
		if err != nil {
			return BundleDecision{}, err
		}
		policies[index] = item
		byId[item.Id()] = item
	}
	bundle, err := NewBundle(value.BundleId, value.BundleVersion, value.BundleDigest, policies)
	if err != nil {
		return BundleDecision{}, err
	}
	if len(value.Decisions) != len(policies) {
		return BundleDecision{}, fmt.Errorf("durable policy decision count does not match bundle")
	}
	decisions := make([]PolicyDecision, len(value.Decisions))
	seen := make(map[PolicyId]bool, len(value.Decisions))
	for index, stored := range value.Decisions {
		rule, ok := byId[stored.PolicyId]
		if !ok || seen[stored.PolicyId] || stored.PolicyVersion != rule.Version() || !validOutcome(stored.Outcome) || strings.TrimSpace(stored.Reason) == "" {
			return BundleDecision{}, fmt.Errorf("durable policy decision %d is invalid", index+1)
		}
		seen[stored.PolicyId] = true
		decisions[index] = PolicyDecision{policy: rule, outcome: stored.Outcome, reason: stored.Reason, evidenceIds: append([]string(nil), stored.EvidenceIds...)}
	}
	aggregate, err := AggregateOutcomes(decisionOutcomes(decisions))
	if err != nil {
		return BundleDecision{}, err
	}
	decision := BundleDecision{id: value.Id, projectId: value.ProjectId, changeId: value.ChangeId, workspaceId: value.WorkspaceId, verificationAttemptId: value.VerificationAttemptId, evidenceSetId: value.EvidenceSetId, patchDigest: value.PatchDigest, sourceStateDigest: value.SourceStateDigest, bundle: bundle, decisions: decisions, aggregate: aggregate, evaluatedAt: value.EvaluatedAt.UTC()}
	if !decision.IsValid() || decision.Id() != decisionIdentity(bundle.Digest(), value.EvidenceSetId) {
		return BundleDecision{}, fmt.Errorf("durable PolicyDecision identity or linkage is invalid")
	}
	return decision, nil
}

func (value BundleDecision) Id() string                   { return value.id }
func (value BundleDecision) ProjectId() project.ProjectId { return value.projectId }
func (value BundleDecision) ChangeId() change.ChangeId    { return value.changeId }
func (value BundleDecision) WorkspaceId() proposal.WorkspaceId {
	return value.workspaceId
}
func (value BundleDecision) VerificationAttemptId() verification.VerificationAttemptId {
	return value.verificationAttemptId
}
func (value BundleDecision) EvidenceSetId() string { return value.evidenceSetId }
func (value BundleDecision) PatchDigest() string   { return value.patchDigest }
func (value BundleDecision) SourceStateDigest() source.SourceStateDigest {
	return value.sourceStateDigest
}
func (value BundleDecision) Bundle() PolicyBundle { return value.bundle }
func (value BundleDecision) Decisions() []PolicyDecision {
	return append([]PolicyDecision(nil), value.decisions...)
}
func (value BundleDecision) Aggregate() AggregateRequirements { return value.aggregate }
func (value BundleDecision) EvaluatedAt() time.Time           { return value.evaluatedAt }
func (value BundleDecision) IsValid() bool {
	return value.id != "" && value.projectId.IsValid() && value.changeId != "" &&
		value.workspaceId != "" && value.verificationAttemptId != "" &&
		value.evidenceSetId != "" && value.patchDigest != "" &&
		value.sourceStateDigest != "" && value.bundle.digest != "" &&
		!value.evaluatedAt.IsZero()
}

type PolicyExceptionCandidate struct {
	id                       string
	evaluationId             string
	policyId                 PolicyId
	projectId                project.ProjectId
	changeId                 change.ChangeId
	reason, scope, authority string
	expiresAt                *time.Time
	createdAt                time.Time
}

func (value PolicyExceptionCandidate) Id() string                   { return value.id }
func (value PolicyExceptionCandidate) EvaluationId() string         { return value.evaluationId }
func (value PolicyExceptionCandidate) PolicyId() PolicyId           { return value.policyId }
func (value PolicyExceptionCandidate) ProjectId() project.ProjectId { return value.projectId }
func (value PolicyExceptionCandidate) ChangeId() change.ChangeId    { return value.changeId }
func (value PolicyExceptionCandidate) Reason() string               { return value.reason }
func (value PolicyExceptionCandidate) Scope() string                { return value.scope }
func (value PolicyExceptionCandidate) RequestedAuthority() string   { return value.authority }
func (value PolicyExceptionCandidate) ExpiresAt() (time.Time, bool) {
	if value.expiresAt == nil {
		return time.Time{}, false
	}
	return *value.expiresAt, true
}
func (value PolicyExceptionCandidate) CreatedAt() time.Time { return value.createdAt }

func decisionIdentity(bundleDigest, evidenceSetId string) string {
	sum := sha256.Sum256([]byte(bundleDigest + "\x00" + evidenceSetId))
	return "policy-eval-" + hex.EncodeToString(sum[:16])
}
func candidateIdentity(evaluationId string, policyId PolicyId, reason, scope, authority string) string {
	sum := sha256.Sum256([]byte(strings.Join([]string{evaluationId, string(policyId), reason, scope, authority}, "\x00")))
	return "policy-exception-" + hex.EncodeToString(sum[:16])
}

func validSeverity(value Severity) bool {
	switch value {
	case SeverityInfo, SeverityLow, SeverityMedium, SeverityHigh, SeverityCritical:
		return true
	}
	return false
}
func validOutcome(value EnforcementOutcome) bool {
	switch value {
	case OutcomeAuto, OutcomeReview, OutcomeApproval, OutcomeForbidden:
		return true
	}
	return false
}
func validFamily(value PolicyFamily) bool {
	switch value {
	case FamilyCleanCode, FamilyTesting, FamilyArchitecture, FamilySecurity, FamilyDependencyManagement, FamilyChangeSurface, FamilyBackwardCompatibility, FamilyPerformance, FamilyObservability, FamilyErrorHandling, FamilyAPIContracts, FamilyDatabase:
		return true
	}
	return false
}
func validEvidenceKind(value verification.StepKind) bool {
	switch value {
	case verification.KindBuild, verification.KindTest, verification.KindLint, verification.KindTypeCheck, verification.KindRepositoryCheck, verification.KindPatchIntegrity:
		return true
	}
	return false
}
