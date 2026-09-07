package policy

import (
	"reflect"
	"strings"
	"testing"

	"github.com/Eu-Pedro0ficial/praetor/internal/verification"
)

func TestSeverityAndOutcomeVocabulariesAreExact(t *testing.T) {
	for _, severity := range []Severity{SeverityInfo, SeverityLow, SeverityMedium, SeverityHigh, SeverityCritical} {
		if _, err := NewPolicy("rule", "1.0", FamilyTesting, "bounded rule", severity, OutcomeAuto, verification.KindTest, true, false); err != nil {
			t.Fatalf("severity %q: %v", severity, err)
		}
	}
	for _, invalid := range []Severity{"", "WARNING", "critical"} {
		if _, err := NewPolicy("rule", "1.0", FamilyTesting, "bounded rule", invalid, OutcomeAuto, verification.KindTest, true, false); err == nil {
			t.Fatalf("accepted invalid severity %q", invalid)
		}
	}
	for _, outcome := range []EnforcementOutcome{OutcomeAuto, OutcomeReview, OutcomeApproval, OutcomeForbidden} {
		if _, err := NewPolicy("rule", "1.0", FamilyTesting, "bounded rule", SeverityHigh, outcome, verification.KindTest, true, false); err != nil {
			t.Fatalf("outcome %q: %v", outcome, err)
		}
	}
	invalidPolicies := []struct {
		id       PolicyId
		version  PolicyVersion
		family   PolicyFamily
		evidence verification.StepKind
	}{
		{id: "", version: "1.0", family: FamilyTesting, evidence: verification.KindTest},
		{id: "Uppercase", version: "1.0", family: FamilyTesting, evidence: verification.KindTest},
		{id: "rule", version: "v1", family: FamilyTesting, evidence: verification.KindTest},
		{id: "rule", version: "1.0", family: "unknown", evidence: verification.KindTest},
		{id: "rule", version: "1.0", family: FamilyTesting, evidence: "provider-completion"},
	}
	for _, invalid := range invalidPolicies {
		if _, err := NewPolicy(invalid.id, invalid.version, invalid.family, "bounded rule", SeverityHigh, OutcomeAuto, invalid.evidence, true, false); err == nil {
			t.Fatalf("accepted invalid policy: %#v", invalid)
		}
	}
}

func TestBundleRejectsDuplicatesAndReturnsImmutableSortedPolicies(t *testing.T) {
	a, _ := NewPolicy("z-rule", "1.0", FamilyTesting, "z rule", SeverityLow, OutcomeAuto, verification.KindTest, false, false)
	b, _ := NewPolicy("a-rule", "1.0", FamilySecurity, "a rule", SeverityHigh, OutcomeForbidden, verification.KindPatchIntegrity, true, true)
	bundle, err := NewBundle("bundle", "1.0", strings.Repeat("a", 64), []Policy{a, b})
	if err != nil {
		t.Fatal(err)
	}
	values := bundle.Policies()
	if values[0].Id() != "a-rule" {
		t.Fatalf("order = %#v", values)
	}
	values[0] = a
	if bundle.Policies()[0].Id() != "a-rule" {
		t.Fatal("bundle exposed mutable policies")
	}
	if _, err := NewBundle("bundle", "1.0", strings.Repeat("a", 64), []Policy{a, a}); err == nil {
		t.Fatal("accepted duplicate policy")
	}
	decision := PolicyDecision{policy: b, evidenceIds: []string{"step-a"}}
	returnedEvidence := decision.EvidenceIds()
	returnedEvidence[0] = "mutated"
	if decision.EvidenceIds()[0] != "step-a" {
		t.Fatal("PolicyDecision exposed mutable evidence identities")
	}
	bundleDecision := BundleDecision{decisions: []PolicyDecision{decision}}
	returnedDecisions := bundleDecision.Decisions()
	returnedDecisions[0] = PolicyDecision{}
	if bundleDecision.Decisions()[0].Policy().Id() != b.Id() {
		t.Fatal("BundleDecision exposed mutable individual decisions")
	}
}

func TestAggregateRequirementsAreOrderIndependentAndConjunctive(t *testing.T) {
	singles := map[EnforcementOutcome]AggregateRequirements{
		OutcomeAuto:      {},
		OutcomeReview:    {RequiresReview: true},
		OutcomeApproval:  {RequiresApproval: true},
		OutcomeForbidden: {Denied: true},
	}
	for outcome, want := range singles {
		got, err := AggregateOutcomes([]EnforcementOutcome{outcome})
		if err != nil || got != want {
			t.Fatalf("AggregateOutcomes(%q) = %#v, %v", outcome, got, err)
		}
	}
	first, err := AggregateOutcomes([]EnforcementOutcome{OutcomeReview, OutcomeAuto, OutcomeApproval})
	if err != nil {
		t.Fatal(err)
	}
	second, _ := AggregateOutcomes([]EnforcementOutcome{OutcomeApproval, OutcomeReview, OutcomeAuto})
	want := AggregateRequirements{RequiresReview: true, RequiresApproval: true}
	if !reflect.DeepEqual(first, want) || !reflect.DeepEqual(second, want) {
		t.Fatalf("aggregate = %#v/%#v", first, second)
	}
	denied, _ := AggregateOutcomes([]EnforcementOutcome{OutcomeReview, OutcomeForbidden, OutcomeApproval})
	if !denied.Denied || !denied.RequiresReview || !denied.RequiresApproval {
		t.Fatalf("denied aggregate lost findings: %#v", denied)
	}
	if _, err := AggregateOutcomes([]EnforcementOutcome{"REVIEW + APPROVAL"}); err == nil {
		t.Fatal("accepted a fifth outcome")
	}
}

func TestConfigurationPrecedenceCannotWeakenNonOverridableGovernance(t *testing.T) {
	strong, _ := NewPolicy("required", "1.0", FamilySecurity, "strong rule", SeverityCritical, OutcomeForbidden, verification.KindPatchIntegrity, true, false)
	weak, _ := NewPolicy("required", "1.0", FamilySecurity, "strong rule", SeverityCritical, OutcomeAuto, verification.KindPatchIntegrity, true, false)
	base, _ := NewBundle("base", "1.0", strings.Repeat("a", 64), []Policy{strong})
	overlay, _ := NewBundle("overlay", "1.0", strings.Repeat("b", 64), []Policy{weak})
	if _, err := ComposeBundle("effective", "1.0", strings.Repeat("c", 64), base, overlay); err == nil {
		t.Fatal("lower configuration weakened non-overridable governance")
	}
}
