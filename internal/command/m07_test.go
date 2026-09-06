package command_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Eu-Pedro0ficial/praetor/internal/aiprovider"
	"github.com/Eu-Pedro0ficial/praetor/internal/approval"
	"github.com/Eu-Pedro0ficial/praetor/internal/audit"
	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"github.com/Eu-Pedro0ficial/praetor/internal/command"
	"github.com/Eu-Pedro0ficial/praetor/internal/composition"
	"github.com/Eu-Pedro0ficial/praetor/internal/proposal"
	"github.com/Eu-Pedro0ficial/praetor/internal/repository"
)

func TestHumanDecisionCommandsExplicitlyApproveOrRejectValidatedProposal(t *testing.T) {
	tests := []struct {
		name       string
		contextual bool
		kind       approval.DecisionKind
		command    string
		wantState  change.ChangeState
		rationale  string
	}{
		{
			name:      "direct approval",
			kind:      approval.DecisionApprove,
			command:   "change approve",
			wantState: change.StateApproved,
		},
		{
			name:      "direct rejection with rationale",
			kind:      approval.DecisionReject,
			command:   `change reject "verification passed but this patch is not desired"`,
			wantState: change.StateRejected,
			rationale: "verification passed but this patch is not desired",
		},
		{
			name:       "contextual approval",
			contextual: true,
			kind:       approval.DecisionApprove,
			command:    "approve",
			wantState:  change.StateApproved,
		},
		{
			name:       "contextual rejection with rationale",
			contextual: true,
			kind:       approval.DecisionReject,
			command:    `reject "verification passed but this patch is not desired"`,
			wantState:  change.StateRejected,
			rationale:  "verification passed but this patch is not desired",
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repositoryRoot, dataDirectory, session, registry, providerCalls := prepareValidatedDecisionCommandTest(t, nil)
			beforeEvents := readCommandAudit(t, dataDirectory)
			beforeSource, err := repository.Inspect(session.Registration().ProjectId, repositoryRoot)
			if err != nil {
				t.Fatal(err)
			}
			currentProposal, _ := session.CurrentProposal()
			artifact, _ := currentProposal.PatchArtifact()
			verificationResult, _ := session.LastVerification()
			evidenceSet := verificationResult.EvidenceSet()
			validatedChange, _ := session.CurrentChange()
			callsBeforeDecision := *providerCalls
			if test.contextual {
				if _, err := registry.Dispatch(session, "change", io.Discard); err != nil {
					t.Fatalf("enter Change mode: %v", err)
				}
			}

			var output bytes.Buffer
			if _, err := registry.Dispatch(session, test.command, &output); err != nil {
				t.Fatalf("%s error = %v\n%s", test.command, err, output.String())
			}
			for _, expected := range []string{
				"Decision summary:",
				"Change: change-human-decision",
				"State: validated",
				"Patch: " + artifact.PatchDigest(),
				"Changed files: 1",
				"Verification: PASS",
				"Human decision: " + string(test.kind),
				"Actor provenance: local-interactive-human",
				"Change state: " + string(test.wantState),
				"Canonical source: unchanged; decision authorizes or rejects later application",
			} {
				if !strings.Contains(output.String(), expected) {
					t.Fatalf("decision output %q lacks %q", output.String(), expected)
				}
			}
			if *providerCalls != callsBeforeDecision {
				t.Fatalf("human decision called AI provider: calls %d -> %d", callsBeforeDecision, *providerCalls)
			}
			currentChange, _ := session.CurrentChange()
			retainedProposal, hasProposal := session.CurrentProposal()
			decision, hasDecision := session.LastDecision()
			if currentChange.State() != test.wantState || !hasProposal ||
				retainedProposal.Workspace().State() != proposal.WorkspaceRetained ||
				!hasDecision || decision.Kind() != test.kind ||
				string(decision.Rationale()) != test.rationale ||
				decision.Actor() != approval.ActorLocalInteractiveHuman {
				t.Fatalf("decision session state = %s/%t/%s/%t/%#v",
					currentChange.State(), hasProposal, retainedProposal.Workspace().State(), hasDecision, decision)
			}
			if decision.ProjectId() != validatedChange.ProjectId() ||
				decision.ChangeId() != validatedChange.ChangeId() ||
				decision.WorkspaceId() != currentProposal.Workspace().WorkspaceId() ||
				decision.RequestedState() != test.wantState ||
				decision.BaseRevision() != artifact.BaseRevision() ||
				decision.PatchDigest() != artifact.PatchDigest() ||
				decision.EvidenceSetId() != evidenceSet.Id() ||
				decision.VerificationAttemptId() != evidenceSet.VerificationAttemptId() ||
				decision.SourceStateDigest() != artifact.SourceStateDigest() ||
				decision.EvidenceCount() != len(evidenceSet.Evidence()) ||
				decision.ChangedPathCount() != len(artifact.ChangedPaths()) ||
				decision.OccurredAt().IsZero() || decision.OccurredAt().Before(validatedChange.UpdatedAt()) ||
				decision.OccurredAt().Location() != time.UTC {
				t.Fatalf("decision linkage = %#v", decision)
			}
			if _, err := registry.Dispatch(session, test.command, io.Discard); err == nil ||
				!strings.Contains(err.Error(), "must be validated") {
				t.Fatalf("duplicate/replayed decision error = %v", err)
			}
			opposite := "reject"
			if test.kind == approval.DecisionReject {
				opposite = "approve"
			}
			if !test.contextual {
				opposite = "change " + opposite
			}
			if _, err := registry.Dispatch(session, opposite, io.Discard); err == nil ||
				!strings.Contains(err.Error(), "must be validated") {
				t.Fatalf("opposite decision after disposition error = %v", err)
			}
			afterEvents := readCommandAudit(t, dataDirectory)
			if !reflect.DeepEqual(afterEvents[:len(beforeEvents)], beforeEvents) {
				t.Fatal("human decision rewrote prior audit history")
			}
			assertHumanDecisionAudit(t, afterEvents, test.kind, test.wantState, test.rationale)
			assertM07LifecycleCoverage(t, afterEvents)

			afterSource, err := repository.Inspect(session.Registration().ProjectId, repositoryRoot)
			if err != nil {
				t.Fatal(err)
			}
			if beforeSource.HeadRevision() != afterSource.HeadRevision() ||
				beforeSource.WorkingTreeState() != afterSource.WorkingTreeState() ||
				beforeSource.SourceStateDigest() != afterSource.SourceStateDigest() {
				t.Fatalf("canonical source changed: before=%#v after=%#v", beforeSource, afterSource)
			}
			assertCommandCanonicalSourceUnchanged(t, repositoryRoot)
		})
	}
}

func TestSessionCloseCleansOwnedProposalWithoutChangingHumanDispositionOrAuditLocking(t *testing.T) {
	tests := []struct {
		command   string
		wantState change.ChangeState
	}{
		{command: "change approve", wantState: change.StateApproved},
		{command: `change reject "not desired"`, wantState: change.StateRejected},
	}
	for _, test := range tests {
		t.Run(string(test.wantState), func(t *testing.T) {
			repositoryRoot, dataDirectory, session, registry, _ := prepareValidatedDecisionCommandTest(t, nil)
			if _, err := registry.Dispatch(session, test.command, io.Discard); err != nil {
				t.Fatal(err)
			}
			if err := session.Close(); err != nil {
				t.Fatalf("Session.Close() error = %v", err)
			}
			currentChange, _ := session.CurrentChange()
			if currentChange.State() != test.wantState {
				t.Fatalf("Session.Close() changed disposition to %q", currentChange.State())
			}
			if _, retained := session.CurrentProposal(); retained {
				t.Fatal("Session.Close() retained its owned proposal workspace")
			}
			for _, event := range readCommandAudit(t, dataDirectory) {
				if event.EventType == audit.EventChangeTransition &&
					event.Metadata["resulting_state"] == string(change.StateAuditLocked) {
					t.Fatalf("Session.Close() automatically audit-locked the Change: %#v", event)
				}
			}
			assertCommandCanonicalSourceUnchanged(t, repositoryRoot)
		})
	}
}

func TestHumanDecisionHasNoImplicitOrProviderDrivenApprovalPath(t *testing.T) {
	_, dataDirectory, session, registry, providerCalls := prepareValidatedDecisionCommandTest(t, nil)
	currentChange, _ := session.CurrentChange()
	if currentChange.State() != change.StateValidated {
		t.Fatalf("verification did not stop at validated: %s", currentChange.State())
	}
	callsAfterVerification := *providerCalls
	for _, line := range []string{"", "?", "help change approve", "status"} {
		if _, err := registry.Dispatch(session, line, io.Discard); err != nil {
			t.Fatalf("non-decision command %q error = %v", line, err)
		}
	}
	currentChange, _ = session.CurrentChange()
	if currentChange.State() != change.StateValidated || *providerCalls != callsAfterVerification {
		t.Fatalf("non-decision input changed authority: state=%s calls=%d", currentChange.State(), *providerCalls)
	}
	if _, available := session.LastDecision(); available {
		t.Fatal("verification/provider success manufactured a human decision")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := registry.DispatchContext(cancelled, session, "change approve", io.Discard); err == nil ||
		!errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled approval error = %v", err)
	}
	timedOut, stopTimeout := context.WithDeadline(context.Background(), time.Now().Add(-time.Second))
	defer stopTimeout()
	if _, err := registry.DispatchContext(timedOut, session, "change approve", io.Discard); err == nil ||
		!errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timed-out approval error = %v", err)
	}
	currentChange, _ = session.CurrentChange()
	if currentChange.State() != change.StateValidated {
		t.Fatalf("cancelled/timed-out input changed state to %q", currentChange.State())
	}
	for _, event := range readCommandAudit(t, dataDirectory) {
		if event.EventType == audit.EventHumanDecisionRecorded {
			t.Fatalf("non-decision input recorded human authority: %#v", event)
		}
	}
	if _, err := registry.Dispatch(session, "change approve first second", io.Discard); err == nil ||
		err.Error() != "usage: approve [<rationale>]" {
		t.Fatalf("ambiguous approval arguments error = %v", err)
	}
}

func TestMalformedExistingAuditPreventsHumanDecision(t *testing.T) {
	_, dataDirectory, session, registry, _ := prepareValidatedDecisionCommandTest(t, nil)
	ledgerPath, err := audit.LedgerPath()
	if err != nil {
		t.Fatal(err)
	}
	original, err := os.ReadFile(ledgerPath)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		if err := os.WriteFile(ledgerPath, original, 0o600); err != nil {
			t.Errorf("restore malformed audit fixture: %v", err)
		}
	})
	ledger, err := os.OpenFile(ledgerPath, os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := ledger.WriteString("{"); err != nil {
		_ = ledger.Close()
		t.Fatal(err)
	}
	if err := ledger.Close(); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Dispatch(session, "change approve", io.Discard); err == nil ||
		!strings.Contains(err.Error(), "incomplete trailing event") {
		t.Fatalf("approval with malformed ledger error = %v", err)
	}
	currentChange, _ := session.CurrentChange()
	if currentChange.State() != change.StateValidated {
		t.Fatalf("malformed ledger allowed state %q", currentChange.State())
	}
	if _, available := session.LastDecision(); available {
		t.Fatal("malformed ledger produced a successful decision")
	}
	if err := os.WriteFile(ledgerPath, original, 0o600); err != nil {
		t.Fatal(err)
	}
	for _, event := range readCommandAudit(t, dataDirectory) {
		if event.EventType == audit.EventHumanDecisionRecorded {
			t.Fatalf("malformed-ledger decision reached audit: %#v", event)
		}
	}
}

func TestHumanDecisionCommandsShareCentralMetadataHelpAndCompletion(t *testing.T) {
	_, _, session, registry, _ := prepareValidatedDecisionCommandTest(t, nil)
	assertSuggestions(t, registry.Complete(session, "change app"), []string{"approve"})
	assertSuggestions(t, registry.ContextualHelp(session, "change rej"), []string{"reject"})
	if _, err := registry.Dispatch(session, "change", io.Discard); err != nil {
		t.Fatal(err)
	}
	assertMetadataNames(t, registry.ContextCommands(session), []string{
		"new", "isolate", "implement", "patch", "verify", "approve", "reject", "discard", "help", "?", "end",
	})
	var output bytes.Buffer
	if _, err := registry.Dispatch(session, "?", &output); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"approve", "reject", "Explicitly authorize", "Explicitly reject"} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("Change contextual help %q lacks %q", output.String(), expected)
		}
	}
	for _, forbidden := range []string{"apply", "merge", "commit", "audit-lock"} {
		for _, metadata := range registry.Commands() {
			if metadata.Name == forbidden {
				t.Fatalf("M0.8 command %q leaked into M0.7", forbidden)
			}
		}
	}
}

func TestHumanDecisionRequiresValidationAndSafeRationale(t *testing.T) {
	provider := decisionCommandProvider(t, new(int))
	repositoryRoot, dataDirectory, session, registry := prepareProviderCommandTest(t, provider, &commandVerificationRunner{})
	if _, err := registry.Dispatch(session,
		`change isolate change-decision-gate "Prove the evidence gate." --expected internal/service/service.go --protected go.mod`,
		io.Discard,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Dispatch(session, "change implement", io.Discard); err != nil {
		t.Fatal(err)
	}
	currentChange, _ := session.CurrentChange()
	if currentChange.State() != change.StateIsolated {
		t.Fatalf("provider success changed state to %q", currentChange.State())
	}
	if _, err := registry.Dispatch(session, "change approve", io.Discard); err == nil ||
		!strings.Contains(err.Error(), "no deterministic EvidenceSet") {
		t.Fatalf("approval without EvidenceSet error = %v", err)
	}
	for _, event := range readCommandAudit(t, dataDirectory) {
		if event.EventType == audit.EventHumanDecisionRecorded {
			t.Fatal("approval without deterministic evidence reached audit")
		}
	}
	assertCommandCanonicalSourceUnchanged(t, repositoryRoot)

	_, _, validatedSession, validatedRegistry, _ := prepareValidatedDecisionCommandTest(t, nil)
	unsafeRationales := []string{
		"line\nbreak",
		"terminal\x1b[2Jcontrol",
		strings.Repeat("a", 1025),
	}
	for _, rationale := range unsafeRationales {
		line := `change reject "` + rationale + `"`
		if _, err := validatedRegistry.Dispatch(validatedSession, line, io.Discard); err == nil {
			t.Fatalf("unsafe rationale was accepted: %q", rationale)
		}
		currentChange, _ := validatedSession.CurrentChange()
		if currentChange.State() != change.StateValidated {
			t.Fatalf("unsafe rationale changed state to %q", currentChange.State())
		}
	}
}

func TestHumanDecisionAuditFailuresLeaveChangeValidated(t *testing.T) {
	auditFailure := errors.New("audit unavailable")
	tests := []struct {
		name            string
		failDecision    bool
		failTransition  bool
		wantDecisionLog bool
	}{
		{name: "decision event failure", failDecision: true},
		{name: "transition event failure", failTransition: true, wantDecisionLog: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			failureEnabled := false
			configure := func(container *composition.Container) {
				baseLogger := container.ChangeAuditLogger
				container.ChangeAuditLogger = func(
					dataDirectory, eventType, projectID, changeID, repositoryRoot string,
					metadata map[string]any,
				) (audit.Event, error) {
					if failureEnabled && test.failDecision && eventType == audit.EventHumanDecisionRecorded {
						return audit.Event{}, auditFailure
					}
					if failureEnabled && test.failTransition && eventType == audit.EventChangeTransition &&
						metadata["resulting_state"] == string(change.StateApproved) {
						return audit.Event{}, auditFailure
					}
					return baseLogger(dataDirectory, eventType, projectID, changeID, repositoryRoot, metadata)
				}
			}
			_, dataDirectory, session, registry, _ := prepareValidatedDecisionCommandTest(t, configure)
			beforeEvents := readCommandAudit(t, dataDirectory)
			failureEnabled = true
			var output bytes.Buffer
			if _, err := registry.Dispatch(session, "change approve", &output); !errors.Is(err, auditFailure) {
				t.Fatalf("change approve error = %v, want audit failure", err)
			}
			currentChange, _ := session.CurrentChange()
			if currentChange.State() != change.StateValidated {
				t.Fatalf("audit failure committed state %q", currentChange.State())
			}
			if _, available := session.LastDecision(); available {
				t.Fatal("audit-failed decision became successful session state")
			}
			afterEvents := readCommandAudit(t, dataDirectory)
			if !reflect.DeepEqual(afterEvents[:len(beforeEvents)], beforeEvents) {
				t.Fatal("audit failure rewrote prior events")
			}
			decisionCount := 0
			for _, event := range afterEvents[len(beforeEvents):] {
				if event.EventType == audit.EventHumanDecisionRecorded {
					decisionCount++
				}
				if event.EventType == audit.EventChangeTransition &&
					event.Metadata["resulting_state"] == string(change.StateApproved) {
					t.Fatal("audit-failed approval persisted an approved transition")
				}
			}
			if (decisionCount == 1) != test.wantDecisionLog {
				t.Fatalf("decision audit count = %d, want logged=%t", decisionCount, test.wantDecisionLog)
			}
			if test.wantDecisionLog {
				if !strings.Contains(output.String(), "Human decision: APPROVE RECORDED; state transition failed") ||
					!strings.Contains(output.String(), "Change state: validated") {
					t.Fatalf("transition-failed decision output = %q", output.String())
				}
			} else if !strings.Contains(output.String(), "Human decision: NOT RECORDED") {
				t.Fatalf("audit-failed decision output = %q", output.String())
			}
		})
	}
}

func TestApprovalFailsClosedOnProposalOrCanonicalDrift(t *testing.T) {
	t.Run("proposal drift", func(t *testing.T) {
		repositoryRoot, dataDirectory, session, registry, _ := prepareValidatedDecisionCommandTest(t, nil)
		currentProposal, _ := session.CurrentProposal()
		if err := osWriteDecisionFile(currentProposal.Workspace().Root(), "internal/service/service.go", "package service\n\nconst Drifted = true\n"); err != nil {
			t.Fatal(err)
		}
		if _, err := registry.Dispatch(session, "change approve", io.Discard); err == nil ||
			!strings.Contains(err.Error(), "proposal integrity") {
			t.Fatalf("proposal-drift approval error = %v", err)
		}
		assertDecisionNotRecorded(t, dataDirectory, session)
		assertCommandCanonicalSourceUnchanged(t, repositoryRoot)
	})

	t.Run("canonical drift", func(t *testing.T) {
		repositoryRoot, dataDirectory, session, registry, _ := prepareValidatedDecisionCommandTest(t, nil)
		defer func() {
			if err := osWriteDecisionFile(repositoryRoot, "README.md", "# Fixture\n"); err != nil {
				t.Errorf("restore canonical fixture for cleanup: %v", err)
			}
		}()
		if err := osWriteDecisionFile(repositoryRoot, "README.md", "# Drifted canonical fixture\n"); err != nil {
			t.Fatal(err)
		}
		if _, err := registry.Dispatch(session, "change reject", io.Discard); err == nil ||
			!strings.Contains(err.Error(), "canonical source drift") {
			t.Fatalf("canonical-drift rejection error = %v", err)
		}
		assertDecisionNotRecorded(t, dataDirectory, session)
	})
}

func prepareValidatedDecisionCommandTest(
	t *testing.T,
	configure func(*composition.Container),
) (string, string, *command.Session, command.Registry, *int) {
	t.Helper()
	providerCalls := new(int)
	provider := decisionCommandProvider(t, providerCalls)
	repositoryRoot, dataDirectory, session, registry := prepareProviderCommandTestWithContainer(
		t,
		provider,
		&commandVerificationRunner{},
		configure,
	)
	if _, err := registry.Dispatch(session,
		`change isolate change-human-decision "Change Greeting() for M0.7." --expected internal/service/service.go --protected go.mod`,
		io.Discard,
	); err != nil {
		t.Fatalf("change isolate: %v", err)
	}
	if _, err := registry.Dispatch(session, "change implement", io.Discard); err != nil {
		t.Fatalf("change implement: %v", err)
	}
	if _, err := registry.Dispatch(session, "change verify", io.Discard); err != nil {
		t.Fatalf("change verify: %v", err)
	}
	return repositoryRoot, dataDirectory, session, registry, providerCalls
}

func decisionCommandProvider(t *testing.T, calls *int) aiprovider.Provider {
	t.Helper()
	return newCommandFakeProvider(t, func(_ context.Context, request aiprovider.ExecutionRequest) (aiprovider.ProviderResponse, error) {
		(*calls)++
		switch request.RoleContract().Role() {
		case aiprovider.RoleImplementation:
			if err := osWriteDecisionFile(
				request.Workspace().Root(),
				"internal/service/service.go",
				"package service\n\nfunc Greeting() string { return \"approved-candidate\" }\n",
			); err != nil {
				return aiprovider.ProviderResponse{}, err
			}
			return newCommandProviderResponse(t, request, "implementation-thread", "implemented"), nil
		case aiprovider.RoleVerificationPlanning:
			return planningCommandResponse(t, request, `{"candidates":[]}`), nil
		default:
			return aiprovider.ProviderResponse{}, fmt.Errorf("unexpected provider role %q", request.RoleContract().Role())
		}
	})
}

func osWriteDecisionFile(root string, relativePath string, contents string) error {
	return os.WriteFile(filepathFromSlash(root, relativePath), []byte(contents), 0o600)
}

func assertDecisionNotRecorded(t *testing.T, dataDirectory string, session *command.Session) {
	t.Helper()
	currentChange, _ := session.CurrentChange()
	if currentChange.State() != change.StateValidated {
		t.Fatalf("failed decision changed state to %q", currentChange.State())
	}
	if _, available := session.LastDecision(); available {
		t.Fatal("failed decision became session state")
	}
	for _, event := range readCommandAudit(t, dataDirectory) {
		if event.EventType == audit.EventHumanDecisionRecorded {
			t.Fatalf("failed decision reached audit: %#v", event)
		}
	}
}

func assertHumanDecisionAudit(
	t *testing.T,
	events []audit.Event,
	kind approval.DecisionKind,
	wantState change.ChangeState,
	rationale string,
) {
	t.Helper()
	decisionIndex := -1
	for index, event := range events {
		if event.EventType == audit.EventHumanDecisionRecorded {
			if decisionIndex >= 0 {
				t.Fatal("human decision was recorded more than once")
			}
			decisionIndex = index
			metadataText := strings.ToLower(fmt.Sprintf("%v", event.Metadata))
			if event.ProjectID == "" || event.ChangeID != "change-human-decision" ||
				event.Metadata["decision"] != string(kind) ||
				event.Metadata["requested_state"] != string(wantState) ||
				event.Metadata["actor_type"] != "human" ||
				event.Metadata["actor_provenance"] != string(approval.ActorLocalInteractiveHuman) ||
				event.Metadata["identity_assurance"] != "local-process-interaction-only" ||
				event.Metadata["decision_timestamp"] == nil ||
				event.Metadata["workspace_id"] == nil ||
				event.Metadata["base_revision"] == nil ||
				event.Metadata["patch_digest"] == nil ||
				event.Metadata["source_state_digest"] == nil ||
				event.Metadata["verification_attempt_id"] == nil ||
				event.Metadata["evidence_set_id"] == nil ||
				event.Metadata["evidence_count"] == nil ||
				event.Metadata["changed_path_count"] == nil ||
				strings.Contains(metadataText, "package service") ||
				strings.Contains(metadataText, "approved-candidate") ||
				strings.Contains(metadataText, "implementation-thread") {
				t.Fatalf("human decision audit = %#v", event)
			}
			if rationale == "" {
				if _, present := event.Metadata["rationale"]; present {
					t.Fatalf("absent rationale was persisted: %#v", event.Metadata)
				}
			} else if event.Metadata["rationale"] != rationale {
				t.Fatalf("rationale metadata = %#v", event.Metadata["rationale"])
			}
		}
	}
	if decisionIndex < 0 || decisionIndex+1 >= len(events) {
		t.Fatalf("decision event missing or lacks following transition: %#v", events)
	}
	transition := events[decisionIndex+1]
	if transition.EventType != audit.EventChangeTransition ||
		transition.Metadata["previous_state"] != string(change.StateValidated) ||
		transition.Metadata["resulting_state"] != string(wantState) {
		t.Fatalf("decision transition ordering = %#v then %#v", events[decisionIndex], transition)
	}
}

func assertM07LifecycleCoverage(t *testing.T, events []audit.Event) {
	t.Helper()
	required := []string{
		audit.EventInitialization,
		audit.EventProjectAttach,
		audit.EventConfiguration,
		audit.EventChangeCreated,
		audit.EventChangeTransition,
		audit.EventSourceSnapshotCaptured,
		audit.EventImpactAnalysisProduced,
		audit.EventChangeSurfaceEstablished,
		audit.EventProposalWorkspaceCreated,
		audit.EventProviderExecutionStarted,
		audit.EventProviderExecutionCompleted,
		audit.EventPatchExtracted,
		audit.EventPatchSurfaceValidated,
		audit.EventVerificationPlanningStarted,
		audit.EventVerificationPlanningCompleted,
		audit.EventVerificationStarted,
		audit.EventVerificationStepCompleted,
		audit.EventVerificationCompleted,
		audit.EventHumanDecisionRecorded,
	}
	seen := make(map[string]bool, len(events))
	for _, event := range events {
		seen[event.EventType] = true
	}
	for _, eventType := range required {
		if !seen[eventType] {
			t.Fatalf("M0.7 lifecycle audit lacks %s: %#v", eventType, seen)
		}
	}
}
