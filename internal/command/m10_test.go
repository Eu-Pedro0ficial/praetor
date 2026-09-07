package command_test

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"

	"github.com/Eu-Pedro0ficial/praetor/internal/audit"
	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"github.com/Eu-Pedro0ficial/praetor/internal/composition"
	"github.com/Eu-Pedro0ficial/praetor/internal/policy"
	"github.com/Eu-Pedro0ficial/praetor/internal/verification"
)

type fixedPolicySource struct{ bundle policy.PolicyBundle }

func (source fixedPolicySource) Load(string) (policy.PolicyBundle, error) { return source.bundle, nil }

func TestPolicyCommandsUseSharedHierarchyAndInspectManifest(t *testing.T) {
	_, _, session, registry := prepareCommittedCommandTest(t)
	var output bytes.Buffer
	if _, err := registry.Dispatch(session, "policy show", &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "engineering/policies/praetor.yaml") || !strings.Contains(output.String(), "fixture-policy") {
		t.Fatalf("show output = %q", output.String())
	}
	output.Reset()
	if _, err := registry.Dispatch(session, "policy", &output); err != nil {
		t.Fatal(err)
	}
	if session.CurrentMode().Identity != "policy" {
		t.Fatalf("mode = %q", session.CurrentMode().Identity)
	}
	if _, err := registry.Dispatch(session, "?", &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "exception") || !strings.Contains(output.String(), "evaluate") {
		t.Fatalf("context help = %q", output.String())
	}
	if _, err := registry.Dispatch(session, "list", &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "test-required@1.0") {
		t.Fatalf("list output = %q", output.String())
	}
}

func TestPolicyRequirementsGovernPositiveDispositionWithoutChangingWorkflowStates(t *testing.T) {
	for _, outcome := range []policy.EnforcementOutcome{policy.OutcomeAuto, policy.OutcomeReview, policy.OutcomeApproval, policy.OutcomeForbidden} {
		t.Run(string(outcome), func(t *testing.T) {
			rule, err := policy.NewPolicy("gate", "1.0", policy.FamilyTesting, "Test policy gate.", policy.SeverityHigh, outcome, verification.KindTest, true, true)
			if err != nil {
				t.Fatal(err)
			}
			bundle, err := policy.NewBundle("gate-bundle", "1.0", strings.Repeat("d", 64), []policy.Policy{rule})
			if err != nil {
				t.Fatal(err)
			}
			_, _, session, registry, _ := prepareValidatedDecisionCommandTest(t, func(container *composition.Container) { container.PolicySource = fixedPolicySource{bundle: bundle} })
			current, _ := session.CurrentChange()
			if current.State() != change.StateValidated {
				t.Fatalf("policy changed workflow state to %q", current.State())
			}
			_, approveError := registry.Dispatch(session, "change approve", io.Discard)
			if outcome == policy.OutcomeReview || outcome == policy.OutcomeForbidden {
				if approveError == nil {
					t.Fatalf("%s allowed positive disposition", outcome)
				}
				if _, rejectError := registry.Dispatch(session, "change reject", io.Discard); rejectError != nil {
					t.Fatalf("%s blocked rejection: %v", outcome, rejectError)
				}
			} else if approveError != nil {
				t.Fatalf("%s approval failed: %v", outcome, approveError)
			}
		})
	}
}

func TestPolicyEvaluationRetainsCombinedRequirementsAndFailsClosedOnMissingEvidence(t *testing.T) {
	review, _ := policy.NewPolicy("review", "1.0", policy.FamilyArchitecture, "Require independent review.", policy.SeverityMedium, policy.OutcomeReview, verification.KindTest, true, false)
	approvalRule, _ := policy.NewPolicy("approval", "1.0", policy.FamilyTesting, "Require human approval.", policy.SeverityHigh, policy.OutcomeApproval, verification.KindTest, true, true)
	missing, _ := policy.NewPolicy("missing", "1.0", policy.FamilySecurity, "Require unavailable lint evidence.", policy.SeverityCritical, policy.OutcomeAuto, verification.KindLint, true, false)

	t.Run("review and approval remain independent", func(t *testing.T) {
		bundle, err := policy.NewBundle("combined", "1.0", strings.Repeat("e", 64), []policy.Policy{review, approvalRule})
		if err != nil {
			t.Fatal(err)
		}
		_, _, session, registry, _ := prepareValidatedDecisionCommandTest(t, func(container *composition.Container) { container.PolicySource = fixedPolicySource{bundle: bundle} })
		decision, ok := session.LastPolicyDecision()
		if !ok || len(decision.Decisions()) != 2 || decision.Aggregate() != (policy.AggregateRequirements{RequiresReview: true, RequiresApproval: true}) {
			t.Fatalf("combined decision = %#v", decision)
		}
		if _, err := registry.Dispatch(session, "change approve", io.Discard); err == nil {
			t.Fatal("combined REVIEW + APPROVAL requirements allowed positive disposition")
		}
	})

	t.Run("missing required evidence becomes forbidden", func(t *testing.T) {
		bundle, err := policy.NewBundle("missing-evidence", "1.0", strings.Repeat("f", 64), []policy.Policy{approvalRule, missing})
		if err != nil {
			t.Fatal(err)
		}
		_, _, session, _, _ := prepareValidatedDecisionCommandTest(t, func(container *composition.Container) { container.PolicySource = fixedPolicySource{bundle: bundle} })
		decision, ok := session.LastPolicyDecision()
		if !ok || !decision.Aggregate().Denied || !decision.Aggregate().RequiresApproval || len(decision.Decisions()) != 2 {
			t.Fatalf("missing-evidence decision = %#v", decision)
		}
		if decision.Decisions()[1].Outcome() != policy.OutcomeForbidden {
			t.Fatalf("missing evidence outcome = %q", decision.Decisions()[1].Outcome())
		}
	})
}

func TestFailedVerificationCannotProduceUsablePolicyAuthority(t *testing.T) {
	providerCalls := new(int)
	provider := decisionCommandProvider(t, providerCalls)
	repositoryRoot, dataDirectory, session, registry := prepareProviderCommandTest(t, provider, &commandVerificationRunner{exitCodes: []int{1}})
	if _, err := registry.Dispatch(session, `change isolate change-policy-failed-evidence "Reject failed evidence." --expected internal/service/service.go --protected go.mod`, io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Dispatch(session, "change implement", io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Dispatch(session, "change verify", io.Discard); err == nil {
		t.Fatal("failed deterministic verification was accepted")
	}
	if _, ok := session.LastPolicyDecision(); ok {
		t.Fatal("failed deterministic evidence produced retained policy authority")
	}
	current, _ := session.CurrentChange()
	if current.State() != change.StateIsolated {
		t.Fatalf("failed verification changed workflow state to %q", current.State())
	}
	for _, event := range readCommandAudit(t, dataDirectory) {
		if event.EventType == audit.EventPolicyDecisionRecorded {
			t.Fatal("failed deterministic evidence produced a policy decision audit")
		}
	}
	assertCommandCanonicalSourceUnchanged(t, repositoryRoot)
}

func TestApprovedPolicySnapshotIsNotReplacedByLaterManifestContent(t *testing.T) {
	repositoryRoot, _, session, registry, _ := prepareValidatedDecisionCommandTest(t, nil)
	retained, _ := session.LastPolicyDecision()
	manifestPath := "engineering/policies/praetor.yaml"
	originalManifest, err := os.ReadFile(filepathFromSlash(repositoryRoot, manifestPath))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.WriteFile(filepathFromSlash(repositoryRoot, manifestPath), originalManifest, 0o600); err != nil {
			t.Errorf("restore policy manifest: %v", err)
		}
	})
	if _, err := registry.Dispatch(session, "change approve", io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := osWriteDecisionFile(repositoryRoot, manifestPath, "schema_version: 2\n"); err != nil {
		t.Fatal(err)
	}
	after, _ := session.LastPolicyDecision()
	if after.Id() != retained.Id() || after.Bundle().Digest() != retained.Bundle().Digest() {
		t.Fatal("later policy source content replaced the retained approved authority chain")
	}
	if _, err := registry.Dispatch(session, "change apply", io.Discard); err == nil {
		t.Fatal("canonical application ignored governed-source drift")
	}
	if err := os.WriteFile(filepathFromSlash(repositoryRoot, manifestPath), originalManifest, 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestPolicyEvaluationAndExceptionCandidateAreAuditedWithoutGrant(t *testing.T) {
	_, dataDirectory, session, registry, _ := prepareValidatedDecisionCommandTest(t, nil)
	decision, ok := session.LastPolicyDecision()
	if !ok || !decision.IsValid() {
		t.Fatal("automatic post-verification policy decision missing")
	}
	if !decision.Aggregate().RequiresApproval || decision.Aggregate().Denied || decision.Aggregate().RequiresReview {
		t.Fatalf("aggregate = %#v", decision.Aggregate())
	}
	var output bytes.Buffer
	if _, err := registry.Dispatch(session, "policy evaluate", &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Policy evaluation:") {
		t.Fatalf("evaluate output = %q", output.String())
	}
	output.Reset()
	if _, err := registry.Dispatch(session, `policy exception test-required change local-human "temporary rationale"`, &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "no exception was granted or consumed") {
		t.Fatalf("exception output = %q", output.String())
	}
	if _, err := registry.Dispatch(session, `policy exception test-required change local-human "temporary rationale"`, io.Discard); err == nil {
		t.Fatal("duplicate exception candidate was accepted")
	}
	output.Reset()
	if _, err := registry.Dispatch(session, "policy show", &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "Latest retained policy evaluation:") || !strings.Contains(output.String(), "Exception candidate:") {
		t.Fatalf("policy inspection output = %q", output.String())
	}
	events, err := audit.Read(dataDirectory)
	if err != nil {
		t.Fatal(err)
	}
	decisionCount, candidateCount := 0, 0
	for _, event := range events {
		if event.EventType == audit.EventPolicyDecisionRecorded {
			decisionCount++
			if event.Metadata["policy_bundle_digest"] == "" || event.Metadata["workspace_id"] == "" || event.Metadata["evidence_set_id"] == "" || event.Metadata["verification_attempt_id"] == "" || event.Metadata["policy_decisions"] == nil {
				t.Fatalf("policy decision audit linkage is incomplete: %#v", event.Metadata)
			}
		}
		if event.EventType == audit.EventPolicyExceptionCandidateRecorded {
			candidateCount++
			if event.Metadata["exception_candidate_id"] == "" || event.Metadata["requested_scope"] != "change" || event.Metadata["requested_authority"] != "local-human" {
				t.Fatalf("policy exception audit linkage is incomplete: %#v", event.Metadata)
			}
		}
		if event.EventType == audit.EventPolicyDecisionRecorded || event.EventType == audit.EventPolicyExceptionCandidateRecorded {
			metadata := fmt.Sprintf("%v", event.Metadata)
			if strings.Contains(metadata, "approved-candidate") || strings.Contains(metadata, "implementation-thread") {
				t.Fatalf("policy audit leaked source or provider contents: %q", metadata)
			}
		}
	}
	if decisionCount != 2 || candidateCount != 1 {
		t.Fatalf("policy audit events missing: %#v", events)
	}
}
