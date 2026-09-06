package integration_test

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Eu-Pedro0ficial/praetor/internal/adapters/gitproposal"
	"github.com/Eu-Pedro0ficial/praetor/internal/approval"
	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"github.com/Eu-Pedro0ficial/praetor/internal/integration"
	"github.com/Eu-Pedro0ficial/praetor/internal/project"
	"github.com/Eu-Pedro0ficial/praetor/internal/proposal"
	"github.com/Eu-Pedro0ficial/praetor/internal/repository"
	"github.com/Eu-Pedro0ficial/praetor/internal/source"
	"github.com/Eu-Pedro0ficial/praetor/internal/verification"
	"github.com/Eu-Pedro0ficial/praetor/internal/workflow"
)

const integrationProjectId project.ProjectId = "01890f47-9f20-7cc1-98c8-abcdef012345"

var errIntegrationTestFailure = errors.New("injected integration test failure")

type integrationFixture struct {
	root              string
	baseRevision      string
	clock             func() time.Time
	workflow          *workflow.Service
	proposalService   *proposal.Service
	adapter           *gitproposal.Adapter
	currentChange     change.Change
	currentProposal   proposal.Proposal
	verification      verification.Result
	decision          approval.HumanDecision
	events            *[]string
	failAuditLock     *bool
	canonicalApplyOps *int
}

type passingRunner struct{}

func (passingRunner) Resolve(string) (string, error) { return os.Executable() }
func (passingRunner) Run(context.Context, verification.ProcessInvocation) (verification.ProcessResult, error) {
	return verification.NewProcessResult(0, true, []byte("pass\n"), nil, false), nil
}
func (passingRunner) MaximumOutputBytes() int { return 4096 }

type failingRunner struct{}

func (failingRunner) Resolve(string) (string, error) { return os.Executable() }
func (failingRunner) Run(context.Context, verification.ProcessInvocation) (verification.ProcessResult, error) {
	return verification.NewProcessResult(1, true, nil, []byte("failed\n"), false), errors.New("exit status 1")
}
func (failingRunner) MaximumOutputBytes() int { return 4096 }

type recordingCanonical struct {
	delegate     integration.CanonicalSourcePort
	events       *[]string
	applyCalls   *int
	preflightErr error
	applyErr     error
	mutated      bool
	wrongProof   bool
}

func (canonical recordingCanonical) Preflight(request integration.ApplicationRequest) error {
	if canonical.preflightErr != nil {
		return canonical.preflightErr
	}
	return canonical.delegate.Preflight(request)
}

func (canonical recordingCanonical) Apply(request integration.ApplicationRequest) (integration.CanonicalProof, bool, error) {
	*canonical.applyCalls++
	*canonical.events = append(*canonical.events, "canonical-apply")
	if canonical.applyErr != nil {
		return integration.CanonicalProof{}, canonical.mutated, canonical.applyErr
	}
	proof, mutated, err := canonical.delegate.Apply(request)
	if err != nil || !canonical.wrongProof {
		return proof, mutated, err
	}
	artifact, _ := request.Proposal.PatchArtifact()
	wrong, proofError := integration.NewCanonicalProof(
		artifact.BaseRevision(),
		"sha256:"+strings.Repeat("0", 64),
		artifact.ChangedPaths(),
		true,
	)
	return wrong, mutated, proofError
}

func TestIntegrationServiceApprovedAndRejectedTerminalPaths(t *testing.T) {
	t.Run("approved applies after durable start and locks after completion", func(t *testing.T) {
		fixture := newIntegrationFixture(t, approval.DecisionApprove)
		service := fixture.newService(t, recordingCanonical{
			delegate:   fixture.adapter,
			events:     fixture.events,
			applyCalls: fixture.canonicalApplyOps,
		}, repository.Inspect, "")

		result, terminal, err := service.Apply(
			context.Background(),
			fixture.currentChange,
			fixture.currentProposal,
			fixture.verification,
			fixture.decision,
		)
		if err != nil {
			t.Fatalf("Apply() error = %v", err)
		}
		artifact, _ := fixture.currentProposal.PatchArtifact()
		if terminal.State() != change.StateAuditLocked || !result.CanonicalApplicationProven() ||
			!result.CanonicalMutationOccurred() || !result.IndexUnchanged() ||
			result.PatchDigest() != artifact.PatchDigest() || result.CanonicalHead() != fixture.baseRevision {
			t.Fatalf("terminal/result = %q/%#v", terminal.State(), result)
		}
		assertEventOrder(t, *fixture.events,
			integration.EventCanonicalApplicationStarted,
			"canonical-apply",
			integration.EventCanonicalApplicationCompleted,
			"transition:audit-locked",
		)
		if *fixture.canonicalApplyOps != 1 {
			t.Fatalf("canonical Apply calls = %d", *fixture.canonicalApplyOps)
		}
		if head := strings.TrimSpace(string(runIntegrationGit(t, fixture.root, "rev-parse", "HEAD"))); head != fixture.baseRevision {
			t.Fatalf("canonical HEAD = %q", head)
		}
		if staged := runIntegrationGit(t, fixture.root, "diff", "--cached", "--name-only"); len(staged) != 0 {
			t.Fatalf("canonical index changed: %q", staged)
		}
		if contents, err := os.ReadFile(filepath.Join(fixture.root, "service.go")); err != nil || !bytes.Contains(contents, []byte("return 2")) {
			t.Fatalf("canonical contents = %q/%v", contents, err)
		}
	})

	t.Run("rejected proves unchanged and never applies", func(t *testing.T) {
		fixture := newIntegrationFixture(t, approval.DecisionReject)
		service := fixture.newService(t, recordingCanonical{
			delegate:   fixture.adapter,
			events:     fixture.events,
			applyCalls: fixture.canonicalApplyOps,
		}, repository.Inspect, "")

		terminal, err := service.CloseRejected(
			context.Background(),
			fixture.currentChange,
			fixture.currentProposal,
			fixture.verification,
			fixture.decision,
		)
		if err != nil {
			t.Fatalf("CloseRejected() error = %v", err)
		}
		if terminal.State() != change.StateAuditLocked || *fixture.canonicalApplyOps != 0 {
			t.Fatalf("terminal/apply calls = %q/%d", terminal.State(), *fixture.canonicalApplyOps)
		}
		assertEventOrder(t, *fixture.events,
			integration.EventChangeClosureRecorded,
			"transition:audit-locked",
		)
		assertCleanIntegrationCanonical(t, fixture)
	})
}

func TestIntegrationServiceRejectsMissingOrInvalidAuthorizationBeforeMutation(t *testing.T) {
	fixture := newIntegrationFixture(t, approval.DecisionApprove)
	service := fixture.newService(t, recordingCanonical{
		delegate:   fixture.adapter,
		events:     fixture.events,
		applyCalls: fixture.canonicalApplyOps,
	}, repository.Inspect, "")

	states := []change.ChangeState{
		change.StateCreated,
		change.StatePlanned,
		change.StateIsolated,
		change.StateValidated,
	}
	for _, state := range states {
		t.Run(string(state), func(t *testing.T) {
			candidate := changeAtState(t, state)
			if _, _, err := service.Apply(context.Background(), candidate, fixture.currentProposal, fixture.verification, fixture.decision); err == nil {
				t.Fatalf("Apply() accepted %q Change", state)
			}
		})
	}
	if _, _, err := service.Apply(context.Background(), fixture.currentChange, fixture.currentProposal, fixture.verification, approval.HumanDecision{}); err == nil {
		t.Fatal("Apply() accepted missing HumanDecision")
	}
	if _, _, err := service.Apply(context.Background(), fixture.currentChange, fixture.currentProposal, verification.Result{}, fixture.decision); err == nil {
		t.Fatal("Apply() accepted missing EvidenceSet")
	}
	if _, err := service.CloseRejected(context.Background(), fixture.currentChange, fixture.currentProposal, fixture.verification, fixture.decision); err == nil {
		t.Fatal("CloseRejected() accepted approved Change")
	}
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := service.Apply(cancelled, fixture.currentChange, fixture.currentProposal, fixture.verification, fixture.decision); !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled Apply() error = %v", err)
	}
	if *fixture.canonicalApplyOps != 0 {
		t.Fatalf("invalid authorization attempted %d canonical applications", *fixture.canonicalApplyOps)
	}
	assertCleanIntegrationCanonical(t, fixture)

	rejected := newIntegrationFixture(t, approval.DecisionReject)
	rejectedService := rejected.newService(t, recordingCanonical{
		delegate:   rejected.adapter,
		events:     rejected.events,
		applyCalls: rejected.canonicalApplyOps,
	}, repository.Inspect, "")
	if _, _, err := rejectedService.Apply(context.Background(), rejected.currentChange, rejected.currentProposal, rejected.verification, rejected.decision); err == nil {
		t.Fatal("Apply() accepted rejected Change")
	}
	assertCleanIntegrationCanonical(t, rejected)

	if _, _, err := service.Apply(context.Background(), fixture.currentChange, fixture.currentProposal, fixture.verification, rejected.decision); err == nil {
		t.Fatal("Apply() accepted opposite HumanDecision")
	}
	if _, _, err := service.Apply(context.Background(), fixture.currentChange, rejected.currentProposal, rejected.verification, fixture.decision); err == nil {
		t.Fatal("Apply() accepted substituted ProposalWorkspace/PatchArtifact")
	}
	if _, _, err := service.Apply(context.Background(), fixture.currentChange, fixture.currentProposal, rejected.verification, fixture.decision); err == nil {
		t.Fatal("Apply() accepted substituted EvidenceSet/VerificationAttempt")
	}
	foreignChange := customChangeAtState(
		t,
		"change-foreign",
		project.ProjectId("01890f47-9f20-7cc1-98c8-fedcba987654"),
		change.StateApproved,
	)
	if _, _, err := service.Apply(context.Background(), foreignChange, fixture.currentProposal, fixture.verification, fixture.decision); err == nil {
		t.Fatal("Apply() accepted mismatched ChangeId/ProjectId")
	}

	failedVerification := failedVerificationResult(t, fixture)
	if failedVerification.Passed() {
		t.Fatal("failed verification fixture unexpectedly passed")
	}
	if _, _, err := service.Apply(context.Background(), fixture.currentChange, fixture.currentProposal, failedVerification, fixture.decision); err == nil ||
		!strings.Contains(err.Error(), "passing deterministic EvidenceSet") {
		t.Fatalf("Apply() failed-evidence error = %v", err)
	}
}

func TestIntegrationServiceFailureOrderingAndReplaySafety(t *testing.T) {
	tests := []struct {
		name          string
		canonical     func(*integrationFixture) recordingCanonical
		inspector     integration.RepositoryInspector
		failEvent     string
		failAuditLock bool
		wantMutated   bool
		wantStage     string
	}{
		{
			name: "start audit failure",
			canonical: func(f *integrationFixture) recordingCanonical {
				return f.recordingCanonical()
			},
			failEvent:   integration.EventCanonicalApplicationStarted,
			wantMutated: false,
		},
		{
			name: "preflight failure",
			canonical: func(f *integrationFixture) recordingCanonical {
				candidate := f.recordingCanonical()
				candidate.preflightErr = errIntegrationTestFailure
				return candidate
			},
			wantMutated: false,
		},
		{
			name: "apply failure before mutation",
			canonical: func(f *integrationFixture) recordingCanonical {
				candidate := f.recordingCanonical()
				candidate.applyErr = errIntegrationTestFailure
				return candidate
			},
			wantMutated: false,
			wantStage:   "atomic-application",
		},
		{
			name: "inspection failure after mutation",
			canonical: func(f *integrationFixture) recordingCanonical {
				return f.recordingCanonical()
			},
			inspector: func(project.ProjectId, string) (source.SourceSnapshot, error) {
				return source.SourceSnapshot{}, errIntegrationTestFailure
			},
			wantMutated: true,
			wantStage:   "post-application-inspection",
		},
		{
			name: "proof failure after mutation",
			canonical: func(f *integrationFixture) recordingCanonical {
				candidate := f.recordingCanonical()
				candidate.wrongProof = true
				return candidate
			},
			wantMutated: true,
			wantStage:   "post-application-proof",
		},
		{
			name: "completion audit failure after mutation",
			canonical: func(f *integrationFixture) recordingCanonical {
				return f.recordingCanonical()
			},
			failEvent:   integration.EventCanonicalApplicationCompleted,
			wantMutated: true,
			wantStage:   "completion-audit",
		},
		{
			name: "terminal transition failure preserves proof",
			canonical: func(f *integrationFixture) recordingCanonical {
				return f.recordingCanonical()
			},
			failAuditLock: true,
			wantMutated:   true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newIntegrationFixture(t, approval.DecisionApprove)
			*fixture.failAuditLock = test.failAuditLock
			inspector := test.inspector
			if inspector == nil {
				inspector = repository.Inspect
			}
			service := fixture.newService(t, test.canonical(&fixture), inspector, test.failEvent)
			result, terminal, err := service.Apply(
				context.Background(), fixture.currentChange, fixture.currentProposal, fixture.verification, fixture.decision,
			)
			if err == nil {
				t.Fatal("Apply() unexpectedly succeeded")
			}
			if result.CanonicalMutationOccurred() != test.wantMutated || terminal.State() != change.StateApproved {
				t.Fatalf("failure result/state = mutated %t/state %q/error %v", result.CanonicalMutationOccurred(), terminal.State(), err)
			}
			if test.failAuditLock && !result.CanonicalApplicationProven() {
				t.Fatal("terminal transition failure lost completed deterministic proof")
			}
			if test.wantStage != "" && eventIndex(*fixture.events, "failed:"+test.wantStage) < 0 {
				t.Fatalf("events %v lack failure stage %q", *fixture.events, test.wantStage)
			}
			if !test.wantMutated {
				assertCleanIntegrationCanonical(t, fixture)
			}
		})
	}
}

func TestIntegrationServiceJoinsPrimaryAndFailureAuditErrors(t *testing.T) {
	fixture := newIntegrationFixture(t, approval.DecisionApprove)
	primaryFailure := errors.New("injected canonical application failure")
	failureAuditFailure := errors.New("injected failure audit append failure")
	canonical := fixture.recordingCanonical()
	canonical.applyErr = primaryFailure
	service, err := integration.New(
		fixture.workflow,
		fixture.proposalService.VerifyIntegrity,
		canonical,
		repository.Inspect,
		func(event integration.LifecycleEvent) error {
			*fixture.events = append(*fixture.events, event.EventType)
			if event.EventType == integration.EventCanonicalApplicationFailed {
				return failureAuditFailure
			}
			return nil
		},
		fixture.clock,
	)
	if err != nil {
		t.Fatal(err)
	}

	result, terminal, err := service.Apply(
		context.Background(), fixture.currentChange, fixture.currentProposal, fixture.verification, fixture.decision,
	)
	if !errors.Is(err, primaryFailure) || !errors.Is(err, failureAuditFailure) {
		t.Fatalf("Apply() error = %v, want joined primary and failure-audit errors", err)
	}
	if result.CanonicalMutationOccurred() || terminal.State() != change.StateApproved {
		t.Fatalf("failure result/state = mutated %t/state %q", result.CanonicalMutationOccurred(), terminal.State())
	}
	assertEventOrder(t, *fixture.events,
		integration.EventCanonicalApplicationStarted,
		"canonical-apply",
		integration.EventCanonicalApplicationFailed,
	)
	assertCleanIntegrationCanonical(t, fixture)
}

func TestIntegrationServiceCancellationAfterStartAndLateFailureReplay(t *testing.T) {
	t.Run("cancellation after start prevents mutation", func(t *testing.T) {
		fixture := newIntegrationFixture(t, approval.DecisionApprove)
		ctx, cancel := context.WithCancel(context.Background())
		service, err := integration.New(
			fixture.workflow,
			fixture.proposalService.VerifyIntegrity,
			fixture.recordingCanonical(),
			repository.Inspect,
			func(event integration.LifecycleEvent) error {
				*fixture.events = append(*fixture.events, event.EventType)
				if event.EventType == integration.EventCanonicalApplicationStarted {
					cancel()
				}
				return nil
			},
			fixture.clock,
		)
		if err != nil {
			t.Fatal(err)
		}
		result, _, err := service.Apply(ctx, fixture.currentChange, fixture.currentProposal, fixture.verification, fixture.decision)
		if !errors.Is(err, context.Canceled) || result.CanonicalMutationOccurred() || *fixture.canonicalApplyOps != 0 {
			t.Fatalf("cancelled Apply() = result %#v, calls %d, error %v", result, *fixture.canonicalApplyOps, err)
		}
		assertCleanIntegrationCanonical(t, fixture)
	})

	t.Run("completion audit failure cannot double apply", func(t *testing.T) {
		fixture := newIntegrationFixture(t, approval.DecisionApprove)
		service := fixture.newService(t, fixture.recordingCanonical(), repository.Inspect, integration.EventCanonicalApplicationCompleted)
		first, _, firstError := service.Apply(context.Background(), fixture.currentChange, fixture.currentProposal, fixture.verification, fixture.decision)
		if firstError == nil || !first.CanonicalMutationOccurred() || *fixture.canonicalApplyOps != 1 {
			t.Fatalf("first Apply() = %#v, calls %d, error %v", first, *fixture.canonicalApplyOps, firstError)
		}
		second, _, secondError := service.Apply(context.Background(), fixture.currentChange, fixture.currentProposal, fixture.verification, fixture.decision)
		if secondError == nil || second.CanonicalMutationOccurred() || *fixture.canonicalApplyOps != 1 {
			t.Fatalf("replay Apply() = %#v, calls %d, error %v", second, *fixture.canonicalApplyOps, secondError)
		}
		if !strings.Contains(secondError.Error(), "integrity") && !strings.Contains(secondError.Error(), "preflight") {
			t.Fatalf("replay error = %v", secondError)
		}
	})
}

func (fixture integrationFixture) recordingCanonical() recordingCanonical {
	return recordingCanonical{
		delegate:   fixture.adapter,
		events:     fixture.events,
		applyCalls: fixture.canonicalApplyOps,
	}
}

func (fixture integrationFixture) newService(
	t *testing.T,
	canonical integration.CanonicalSourcePort,
	inspector integration.RepositoryInspector,
	failEvent string,
) *integration.Service {
	t.Helper()
	service, err := integration.New(
		fixture.workflow,
		fixture.proposalService.VerifyIntegrity,
		canonical,
		inspector,
		func(event integration.LifecycleEvent) error {
			*fixture.events = append(*fixture.events, event.EventType)
			if event.EventType == integration.EventCanonicalApplicationFailed {
				*fixture.events = append(*fixture.events, "failed:"+event.FailureStage)
			}
			if event.EventType == failEvent {
				return errIntegrationTestFailure
			}
			return nil
		},
		fixture.clock,
	)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func newIntegrationFixture(t *testing.T, decisionKind approval.DecisionKind) integrationFixture {
	t.Helper()
	root := t.TempDir()
	runIntegrationGit(t, root, "init", "--quiet")
	writeIntegrationFile(t, root, "go.mod", "module example.invalid/integrationfixture\n\ngo 1.25\n")
	writeIntegrationFile(t, root, "service.go", "package fixture\n\nfunc Value() int { return 1 }\n")
	runIntegrationGit(t, root, "add", ".")
	runIntegrationGit(t, root,
		"-c", "user.name=Praetor Test", "-c", "user.email=praetor@example.invalid",
		"commit", "--quiet", "-m", "baseline",
	)
	baseRevision := strings.TrimSpace(string(runIntegrationGit(t, root, "rev-parse", "HEAD")))
	next := time.Date(2026, time.September, 6, 12, 0, 0, 0, time.UTC)
	clock := func() time.Time {
		value := next
		next = next.Add(time.Second)
		return value
	}
	events := make([]string, 0, 32)
	failAuditLock := false
	store := workflow.NewMemoryStore()
	changeWorkflow, err := workflow.New(
		store,
		integrationProjectId,
		func(event workflow.LifecycleEvent) error {
			if event.EventType == workflow.EventChangeTransition {
				events = append(events, "transition:"+string(event.ResultingState))
				if failAuditLock && event.ResultingState == change.StateAuditLocked {
					return errIntegrationTestFailure
				}
			}
			return nil
		},
		clock,
	)
	if err != nil {
		t.Fatal(err)
	}
	currentChange, err := changeWorkflow.Create("change-integration", "apply exact retained patch", "created")
	if err != nil {
		t.Fatal(err)
	}
	currentChange, err = changeWorkflow.Transition(currentChange.ChangeId(), change.StatePlanned, "planned")
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := repository.Inspect(integrationProjectId, root)
	if err != nil {
		t.Fatal(err)
	}
	analysis, err := source.AnalyzeImpact(currentChange, snapshot, source.ScopeRequest{
		Expected:  []string{"service.go"},
		Protected: []string{"go.mod"},
	})
	if err != nil {
		t.Fatal(err)
	}
	scope, err := source.EstablishApprovedScope(analysis)
	if err != nil {
		t.Fatal(err)
	}
	adapter, err := gitproposal.New(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	proposalService, err := proposal.New(
		adapter,
		adapter,
		repository.Inspect,
		func(proposal.LifecycleEvent) error { return nil },
		clock,
	)
	if err != nil {
		t.Fatal(err)
	}
	currentProposal, err := proposalService.CreateWorkspace(currentChange, snapshot, scope)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = adapter.Remove(currentProposal.Workspace()) })
	writeIntegrationFile(t, currentProposal.Workspace().Root(), "service.go", "package fixture\n\nfunc Value() int { return 2 }\n")
	currentProposal, validation, err := proposalService.ExtractPatch(currentProposal)
	if err != nil || !validation.Allowed() {
		t.Fatalf("ExtractPatch() = allowed %t, error %v", validation.Allowed(), err)
	}
	currentChange, err = changeWorkflow.Transition(currentChange.ChangeId(), change.StateIsolated, "isolated")
	if err != nil {
		t.Fatal(err)
	}
	engine, err := verification.NewEngine(passingRunner{}, proposalService.VerifyIntegrity, clock)
	if err != nil {
		t.Fatal(err)
	}
	verificationService, err := verification.New(
		engine,
		func(verification.LifecycleEvent) error { return nil },
		func() (verification.VerificationAttemptId, error) {
			return "verification-abcdef0123456789abcdef0123456789", nil
		},
		clock,
	)
	if err != nil {
		t.Fatal(err)
	}
	verificationResult, err := verificationService.Verify(context.Background(), currentChange, currentProposal, nil)
	if err != nil {
		t.Fatal(err)
	}
	currentChange, err = changeWorkflow.Transition(currentChange.ChangeId(), change.StateValidated, "validated")
	if err != nil {
		t.Fatal(err)
	}
	approvalService, err := approval.New(
		changeWorkflow,
		proposalService.VerifyIntegrity,
		func(approval.LifecycleEvent) error { return nil },
		clock,
	)
	if err != nil {
		t.Fatal(err)
	}
	decision, currentChange, err := approvalService.Decide(
		context.Background(), currentChange, currentProposal, verificationResult, decisionKind, "fixture decision",
	)
	if err != nil {
		t.Fatal(err)
	}
	canonicalApplyOps := 0
	return integrationFixture{
		root:              root,
		baseRevision:      baseRevision,
		clock:             clock,
		workflow:          changeWorkflow,
		proposalService:   proposalService,
		adapter:           adapter,
		currentChange:     currentChange,
		currentProposal:   currentProposal,
		verification:      verificationResult,
		decision:          decision,
		events:            &events,
		failAuditLock:     &failAuditLock,
		canonicalApplyOps: &canonicalApplyOps,
	}
}

func changeAtState(t *testing.T, target change.ChangeState) change.Change {
	return customChangeAtState(t, "change-integration", integrationProjectId, target)
}

func customChangeAtState(
	t *testing.T,
	changeId change.ChangeId,
	projectId project.ProjectId,
	target change.ChangeState,
) change.Change {
	t.Helper()
	now := time.Date(2026, time.September, 6, 9, 0, 0, 0, time.UTC)
	candidate, err := change.New(changeId, projectId, "state fixture", now)
	if err != nil {
		t.Fatal(err)
	}
	sequence := []change.ChangeState{change.StatePlanned, change.StateIsolated, change.StateValidated}
	if target == change.StateApproved {
		sequence = append(sequence, change.StateApproved)
	}
	for index, state := range sequence {
		if target == change.StateCreated {
			break
		}
		if _, err := candidate.Transition(state, now.Add(time.Duration(index+1)*time.Second), "fixture"); err != nil {
			t.Fatal(err)
		}
		if state == target {
			break
		}
	}
	return candidate
}

func failedVerificationResult(t *testing.T, fixture integrationFixture) verification.Result {
	t.Helper()
	isolated := changeAtState(t, change.StateIsolated)
	engine, err := verification.NewEngine(failingRunner{}, fixture.proposalService.VerifyIntegrity, fixture.clock)
	if err != nil {
		t.Fatal(err)
	}
	service, err := verification.New(
		engine,
		func(verification.LifecycleEvent) error { return nil },
		func() (verification.VerificationAttemptId, error) {
			return "verification-00112233445566778899aabbccddeeff", nil
		},
		fixture.clock,
	)
	if err != nil {
		t.Fatal(err)
	}
	result, verifyError := service.Verify(context.Background(), isolated, fixture.currentProposal, nil)
	if verifyError == nil {
		t.Fatal("failing verification unexpectedly succeeded")
	}
	return result
}

func assertEventOrder(t *testing.T, events []string, expected ...string) {
	t.Helper()
	previous := -1
	for _, event := range expected {
		index := eventIndex(events, event)
		if index <= previous {
			t.Fatalf("event %q is absent or out of order in %v", event, events)
		}
		previous = index
	}
}

func eventIndex(events []string, expected string) int {
	for index, event := range events {
		if event == expected {
			return index
		}
	}
	return -1
}

func assertCleanIntegrationCanonical(t *testing.T, fixture integrationFixture) {
	t.Helper()
	if status := runIntegrationGit(t, fixture.root, "status", "--porcelain=v1", "--untracked-files=all"); len(status) != 0 {
		t.Fatalf("canonical source changed: %q", status)
	}
	if head := strings.TrimSpace(string(runIntegrationGit(t, fixture.root, "rev-parse", "HEAD"))); head != fixture.baseRevision {
		t.Fatalf("canonical HEAD = %q, want %q", head, fixture.baseRevision)
	}
}

func writeIntegrationFile(t *testing.T, root, relativePath, contents string) {
	t.Helper()
	absolutePath := filepath.Join(root, filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(absolutePath), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(absolutePath, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
}

func runIntegrationGit(t *testing.T, root string, arguments ...string) []byte {
	t.Helper()
	commandArguments := append([]string{"-C", root}, arguments...)
	command := exec.Command("git", commandArguments...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", arguments, err, output)
	}
	return output
}
