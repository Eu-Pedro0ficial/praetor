package policy

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"github.com/Eu-Pedro0ficial/praetor/internal/project"
	"github.com/Eu-Pedro0ficial/praetor/internal/verification"
)

type policySourceStub struct{ bundle PolicyBundle }

func (stub policySourceStub) Load(string) (PolicyBundle, error) { return stub.bundle, nil }

type policyEngineStub struct{ decision BundleDecision }

func (stub policyEngineStub) Evaluate(project.ProjectId, change.ChangeId, verification.EvidenceSet, PolicyBundle, time.Time) (BundleDecision, error) {
	return stub.decision, nil
}

func TestServiceDoesNotRetainUnauditedPolicyArtifacts(t *testing.T) {
	now := time.Date(2026, 9, 7, 12, 0, 0, 0, time.UTC)
	rule, _ := NewPolicy("gate", "1.0", FamilyTesting, "Require test evidence.", SeverityHigh, OutcomeForbidden, verification.KindTest, true, true)
	bundle, _ := NewBundle("bundle", "1.0", strings.Repeat("a", 64), []Policy{rule})
	decision := BundleDecision{
		id: "policy-eval-test", projectId: "01890f47-9f20-7cc1-98c8-abcdef012345", changeId: "change-test",
		workspaceId: "workspace-test", verificationAttemptId: "attempt-test", evidenceSetId: "evidence-test", patchDigest: strings.Repeat("b", 64),
		sourceStateDigest: "source-test", bundle: bundle,
		decisions: []PolicyDecision{{policy: rule, outcome: OutcomeForbidden, reason: "required test evidence failed"}},
		aggregate: AggregateRequirements{Denied: true}, evaluatedAt: now,
	}
	auditFailure := errors.New("audit unavailable")

	t.Run("decision", func(t *testing.T) {
		service, err := New(policySourceStub{bundle}, policyEngineStub{decision}, func(LifecycleEvent) error { return auditFailure }, func() time.Time { return now })
		if err != nil {
			t.Fatal(err)
		}
		if _, err := service.Evaluate(".", decision.ProjectId(), decision.ChangeId(), verification.EvidenceSet{}); !errors.Is(err, auditFailure) {
			t.Fatalf("Evaluate() error = %v", err)
		}
		if _, retained := service.Decision(decision.ChangeId()); retained {
			t.Fatal("unaudited policy decision was retained")
		}
	})

	t.Run("exception candidate", func(t *testing.T) {
		service, err := New(policySourceStub{bundle}, policyEngineStub{decision}, func(event LifecycleEvent) error {
			if event.EventType == EventPolicyExceptionCandidateRecorded {
				return auditFailure
			}
			return nil
		}, func() time.Time { return now })
		if err != nil {
			t.Fatal(err)
		}
		retained, err := service.Evaluate(".", decision.ProjectId(), decision.ChangeId(), verification.EvidenceSet{})
		if err != nil {
			t.Fatal(err)
		}
		if _, err := service.CreateExceptionCandidate(retained, rule.Id(), "bounded reason", "change", "local-human", nil); !errors.Is(err, auditFailure) {
			t.Fatalf("CreateExceptionCandidate() error = %v", err)
		}
		if _, retained := service.Candidate(decision.ChangeId()); retained {
			t.Fatal("unaudited exception candidate was retained")
		}
		if retained.Decisions()[0].Outcome() != OutcomeForbidden || !retained.Aggregate().Denied {
			t.Fatal("failed candidate creation changed the original policy decision")
		}
	})
}
