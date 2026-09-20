package policy

import (
	"fmt"
	"strings"
	"sync"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"github.com/Eu-Pedro0ficial/praetor/internal/project"
	"github.com/Eu-Pedro0ficial/praetor/internal/verification"
)

const (
	EventPolicyDecisionRecorded           = "POLICY_DECISION_RECORDED"
	EventPolicyExceptionCandidateRecorded = "POLICY_EXCEPTION_CANDIDATE_RECORDED"
)

type SourcePort interface {
	Load(string) (PolicyBundle, error)
}
type LifecycleEvent struct {
	EventType  string
	Decision   BundleDecision
	Candidate  PolicyExceptionCandidate
	OccurredAt time.Time
}
type LifecycleRecorder func(LifecycleEvent) error
type Clock func() time.Time

type Service struct {
	source     SourcePort
	engine     Port
	recorder   LifecycleRecorder
	clock      Clock
	mutex      sync.RWMutex
	decisions  map[change.ChangeId]BundleDecision
	candidates map[change.ChangeId]PolicyExceptionCandidate
}

func New(sourcePort SourcePort, engine Port, recorder LifecycleRecorder, clock Clock) (*Service, error) {
	if sourcePort == nil || engine == nil || recorder == nil || clock == nil {
		return nil, fmt.Errorf("policy service dependencies are incomplete")
	}
	return &Service{source: sourcePort, engine: engine, recorder: recorder, clock: clock, decisions: map[change.ChangeId]BundleDecision{}, candidates: map[change.ChangeId]PolicyExceptionCandidate{}}, nil
}

func (service *Service) Load(repositoryRoot string) (PolicyBundle, error) {
	return service.source.Load(repositoryRoot)
}

func (service *Service) Evaluate(repositoryRoot string, projectId project.ProjectId, changeId change.ChangeId, evidence verification.EvidenceSet) (BundleDecision, error) {
	decision, err := service.EvaluateCandidate(repositoryRoot, projectId, changeId, evidence)
	if err != nil {
		return BundleDecision{}, err
	}
	if err := service.recorder(LifecycleEvent{EventType: EventPolicyDecisionRecorded, Decision: decision, OccurredAt: decision.EvaluatedAt()}); err != nil {
		return BundleDecision{}, fmt.Errorf("record policy decision: %w", err)
	}
	service.RetainDecision(decision)
	return decision, nil
}

// EvaluateCandidate computes policy authority without publishing it. Durable
// callers use this to include the decision, binding, audit, and Change
// transition in one authoritative commit.
func (service *Service) EvaluateCandidate(repositoryRoot string, projectId project.ProjectId, changeId change.ChangeId, evidence verification.EvidenceSet) (BundleDecision, error) {
	bundle, err := service.source.Load(repositoryRoot)
	if err != nil {
		return BundleDecision{}, err
	}
	decision, err := service.engine.Evaluate(projectId, changeId, evidence, bundle, service.clock())
	if err != nil {
		return BundleDecision{}, err
	}
	return decision, nil
}

// RetainDecision caches an already committed durable decision for this
// process. It never establishes authority by itself.
func (service *Service) RetainDecision(decision BundleDecision) {
	service.mutex.Lock()
	service.decisions[decision.ChangeId()] = decision
	delete(service.candidates, decision.ChangeId())
	service.mutex.Unlock()
}

func (service *Service) Decision(changeId change.ChangeId) (BundleDecision, bool) {
	service.mutex.RLock()
	defer service.mutex.RUnlock()
	value, ok := service.decisions[changeId]
	return value, ok
}
func (service *Service) Candidate(changeId change.ChangeId) (PolicyExceptionCandidate, bool) {
	service.mutex.RLock()
	defer service.mutex.RUnlock()
	value, ok := service.candidates[changeId]
	return value, ok
}

func (service *Service) CreateExceptionCandidate(decision BundleDecision, policyId PolicyId, reason, scope, authority string, expiresAt *time.Time) (PolicyExceptionCandidate, error) {
	if !decision.IsValid() {
		return PolicyExceptionCandidate{}, fmt.Errorf("policy decision is required")
	}
	ruleFound, allowed := false, false
	for _, item := range decision.decisions {
		if item.policy.id == policyId {
			ruleFound, allowed = true, item.policy.exceptionCandidateAllowed
			break
		}
	}
	if !ruleFound {
		return PolicyExceptionCandidate{}, fmt.Errorf("PolicyId %q was not evaluated", policyId)
	}
	if !allowed {
		return PolicyExceptionCandidate{}, fmt.Errorf("PolicyId %q does not permit an exception candidate", policyId)
	}
	values := []struct {
		name, value string
		maximum     int
	}{{"reason", reason, 1024}, {"scope", scope, 256}, {"authority", authority, 128}}
	for _, value := range values {
		if err := validateBoundedText(value.name, value.value, value.maximum); err != nil {
			return PolicyExceptionCandidate{}, err
		}
	}
	createdAt := service.clock().UTC()
	if createdAt.IsZero() {
		return PolicyExceptionCandidate{}, fmt.Errorf("exception candidate timestamp is required")
	}
	if expiresAt != nil {
		normalized := expiresAt.UTC()
		if !normalized.After(createdAt) {
			return PolicyExceptionCandidate{}, fmt.Errorf("exception candidate expiry must be in the future")
		}
		expiresAt = &normalized
	}
	candidate := PolicyExceptionCandidate{id: candidateIdentity(decision.id, policyId, reason, scope, authority), evaluationId: decision.id, policyId: policyId, projectId: decision.projectId, changeId: decision.changeId, reason: strings.TrimSpace(reason), scope: strings.TrimSpace(scope), authority: strings.TrimSpace(authority), expiresAt: expiresAt, createdAt: createdAt}
	service.mutex.Lock()
	retainedDecision, retained := service.decisions[decision.changeId]
	if !retained || retainedDecision.id != decision.id || retainedDecision.bundle.digest != decision.bundle.digest {
		service.mutex.Unlock()
		return PolicyExceptionCandidate{}, fmt.Errorf("exception candidate requires the retained policy decision")
	}
	existing, exists := service.candidates[decision.changeId]
	if exists && existing.id == candidate.id {
		service.mutex.Unlock()
		return PolicyExceptionCandidate{}, fmt.Errorf("exception candidate already exists")
	}
	if err := service.recorder(LifecycleEvent{EventType: EventPolicyExceptionCandidateRecorded, Decision: decision, Candidate: candidate, OccurredAt: createdAt}); err != nil {
		service.mutex.Unlock()
		return PolicyExceptionCandidate{}, fmt.Errorf("record policy exception candidate: %w", err)
	}
	service.candidates[decision.changeId] = candidate
	service.mutex.Unlock()
	return candidate, nil
}

func validateBoundedText(name, value string, maximum int) error {
	if !utf8.ValidString(value) || strings.TrimSpace(value) == "" || strings.TrimSpace(value) != value || len(value) > maximum {
		return fmt.Errorf("exception candidate %s is invalid", name)
	}
	for _, character := range value {
		if unicode.IsControl(character) || unicode.In(character, unicode.Zl, unicode.Zp) {
			return fmt.Errorf("exception candidate %s contains control characters", name)
		}
	}
	return nil
}
