package execution_test

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Eu-Pedro0ficial/praetor/internal/adapters/aiprovider/codexcli"
	"github.com/Eu-Pedro0ficial/praetor/internal/adapters/gitproposal"
	"github.com/Eu-Pedro0ficial/praetor/internal/aiprovider"
	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"github.com/Eu-Pedro0ficial/praetor/internal/execution"
	"github.com/Eu-Pedro0ficial/praetor/internal/project"
	"github.com/Eu-Pedro0ficial/praetor/internal/proposal"
	"github.com/Eu-Pedro0ficial/praetor/internal/repository"
	"github.com/Eu-Pedro0ficial/praetor/internal/source"
	"github.com/Eu-Pedro0ficial/praetor/internal/verification"
)

const executionTestProjectId project.ProjectId = "01890f47-9f20-7cc1-98c8-0123456789ab"
const executionTestAttemptId aiprovider.ExecutionAttemptId = "attempt-0123456789abcdef0123456789abcdef"

type fakeProvider struct {
	descriptor aiprovider.ProviderDescriptor
	account    func(aiprovider.ExecutionRequest) (aiprovider.RequestContextAccounting, error)
	execute    func(context.Context, aiprovider.ExecutionRequest) (aiprovider.ProviderResponse, error)
	readiness  aiprovider.LocalReadiness
}

func newFakeProvider(t *testing.T, execute func(context.Context, aiprovider.ExecutionRequest) (aiprovider.ProviderResponse, error)) *fakeProvider {
	t.Helper()
	descriptor, err := aiprovider.NewProviderDescriptor(
		"test-provider",
		"Test Vendor",
		"Test Provider",
		[]aiprovider.ProviderCapability{
			aiprovider.CapabilityWorkspaceMutation,
			aiprovider.CapabilityWorkspaceReadOnly,
			aiprovider.CapabilityNonInteractive,
			aiprovider.CapabilityStructuredEvents,
			aiprovider.CapabilityContextCancellation,
		},
	)
	if err != nil {
		t.Fatalf("NewProviderDescriptor() error = %v", err)
	}
	readiness, readinessError := aiprovider.NewLocalReadiness(
		aiprovider.ReadinessLocallyReady,
		"/test/bin/provider",
		"test-provider 1.0",
		"the fake provider is locally ready",
		"no local setup action is required",
		"test configuration present",
	)
	if readinessError != nil {
		t.Fatalf("NewLocalReadiness() error = %v", readinessError)
	}
	return &fakeProvider{descriptor: descriptor, execute: execute, readiness: readiness}
}

func (provider *fakeProvider) Descriptor() aiprovider.ProviderDescriptor { return provider.descriptor }
func (provider *fakeProvider) AccountRequest(request aiprovider.ExecutionRequest) (aiprovider.RequestContextAccounting, error) {
	if provider.account != nil {
		return provider.account(request)
	}
	return aiprovider.NewRequestContextAccounting(nil)
}
func (provider *fakeProvider) Execute(ctx context.Context, request aiprovider.ExecutionRequest) (aiprovider.ProviderResponse, error) {
	return provider.execute(ctx, request)
}
func (provider *fakeProvider) InspectReadiness(
	_ context.Context,
	_ aiprovider.ReadinessInspection,
) aiprovider.LocalReadiness {
	return provider.readiness
}

type executionFixture struct {
	currentChange   change.Change
	currentProposal proposal.Proposal
	proposalService *proposal.Service
	canonicalRoot   string
	baseRevision    string
	baseDigest      source.SourceStateDigest
	proposalEvents  *[]proposal.LifecycleEvent
}

func TestImplementationPipelineUsesExistingPatchAndSurfaceLifecycle(t *testing.T) {
	tests := []struct {
		name          string
		mutations     map[string]string
		wantError     bool
		wantState     proposal.WorkspaceState
		wantPaths     []string
		wantAllowed   bool
		wantViolation source.ViolationKind
	}{
		{
			name:        "allowed expected path",
			mutations:   map[string]string{"service.go": "package service\n\nfunc Greeting() string { return \"hello-praetor\" }\n"},
			wantState:   proposal.WorkspaceRetained,
			wantPaths:   []string{"service.go"},
			wantAllowed: true,
		},
		{
			name:        "allowed possible path",
			mutations:   map[string]string{"service_test.go": "package service\n\nconst AddedTest = true\n"},
			wantState:   proposal.WorkspaceRetained,
			wantPaths:   []string{"service_test.go"},
			wantAllowed: true,
		},
		{
			name:          "protected path rejects whole proposal",
			mutations:     map[string]string{"go.mod": "module forbidden\n"},
			wantError:     true,
			wantState:     proposal.WorkspaceRejected,
			wantPaths:     []string{"go.mod"},
			wantViolation: source.ViolationProtected,
		},
		{
			name:          "unexpected path rejects whole proposal",
			mutations:     map[string]string{"README.md": "unexpected\n"},
			wantError:     true,
			wantState:     proposal.WorkspaceRejected,
			wantPaths:     []string{"README.md"},
			wantViolation: source.ViolationUnexpected,
		},
		{
			name: "mixed forbidden path rejects whole proposal",
			mutations: map[string]string{
				"service.go": "package service\n\nconst Allowed = true\n",
				"go.mod":     "module forbidden\n",
			},
			wantError:     true,
			wantState:     proposal.WorkspaceRejected,
			wantPaths:     []string{"go.mod", "service.go"},
			wantViolation: source.ViolationProtected,
		},
		{
			name:      "provider success with no source change",
			mutations: map[string]string{},
			wantError: true,
			wantState: proposal.WorkspaceRejected,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := prepareExecutionFixture(t)
			provider := newFakeProvider(t, func(_ context.Context, request aiprovider.ExecutionRequest) (aiprovider.ProviderResponse, error) {
				for name, contents := range test.mutations {
					if err := os.WriteFile(filepath.Join(request.Workspace().Root(), name), []byte(contents), 0o600); err != nil {
						return aiprovider.ProviderResponse{}, err
					}
				}
				return successfulProviderResponse(t, request, "thread-surface"), nil
			})
			service, lifecycleEvents := prepareExecutionService(t, fixture.proposalService, provider)
			selection, _ := aiprovider.NewSelection("test-provider", "test-model")

			result, err := service.Implement(context.Background(), fixture.currentChange, fixture.currentProposal, selection)
			if (err != nil) != test.wantError {
				t.Fatalf("Implement() error = %v, want error %t", err, test.wantError)
			}
			if result.Proposal().Workspace().State() != test.wantState {
				t.Fatalf("proposal state = %q, want %q", result.Proposal().Workspace().State(), test.wantState)
			}
			artifact, hasArtifact := result.Proposal().PatchArtifact()
			if len(test.wantPaths) == 0 {
				if hasArtifact {
					t.Fatalf("empty provider change produced artifact %#v", artifact)
				}
			} else if !hasArtifact || !reflect.DeepEqual(artifact.ChangedPaths(), test.wantPaths) {
				t.Fatalf("artifact paths = %#v/%t, want %#v", artifact.ChangedPaths(), hasArtifact, test.wantPaths)
			}
			if result.Validation().Allowed() != test.wantAllowed {
				t.Fatalf("validation allowed = %t, want %t", result.Validation().Allowed(), test.wantAllowed)
			}
			if test.wantViolation != "" {
				violations := result.Validation().Violations()
				if len(violations) == 0 || violations[0].Kind() != test.wantViolation {
					t.Fatalf("violations = %#v, want %s", violations, test.wantViolation)
				}
			}
			if fixture.currentChange.State() != change.StateIsolated {
				t.Fatalf("M0.5 changed authoritative Change state to %q", fixture.currentChange.State())
			}
			if len(*lifecycleEvents) != 2 ||
				(*lifecycleEvents)[0].EventType != execution.EventProviderExecutionStarted ||
				(*lifecycleEvents)[1].EventType != execution.EventProviderExecutionCompleted {
				t.Fatalf("execution lifecycle events = %#v", *lifecycleEvents)
			}
			if (*lifecycleEvents)[1].Response.ExternalExecutionId() != "thread-surface" {
				t.Fatalf("completion linkage = %#v", (*lifecycleEvents)[1])
			}
			assertExecutionCanonicalUnchanged(t, fixture)
			assertPatchEventsLinkedToAttempt(t, *fixture.proposalEvents, test.wantPaths)
		})
	}
}

func TestProviderRequestAccountingPropagatesThroughResultAndLifecycle(t *testing.T) {
	fixture := prepareExecutionFixture(t)
	provider := newFakeProvider(t, func(_ context.Context, request aiprovider.ExecutionRequest) (aiprovider.ProviderResponse, error) {
		if err := os.WriteFile(
			filepath.Join(request.Workspace().Root(), "service.go"),
			[]byte("package service\n\nconst Accounted = true\n"),
			0o600,
		); err != nil {
			return aiprovider.ProviderResponse{}, err
		}
		return successfulProviderResponse(t, request, "thread-accounted"), nil
	})
	component, err := aiprovider.MeasureRequestContextComponent(
		aiprovider.RequestContextIntent,
		"TASK INTENT\naccount this request\n",
		1,
		0,
		false,
	)
	if err != nil {
		t.Fatalf("MeasureRequestContextComponent() error = %v", err)
	}
	wantAccounting, err := aiprovider.NewRequestContextAccounting([]aiprovider.RequestContextComponent{component})
	if err != nil {
		t.Fatalf("NewRequestContextAccounting() error = %v", err)
	}
	provider.account = func(aiprovider.ExecutionRequest) (aiprovider.RequestContextAccounting, error) {
		return wantAccounting, nil
	}
	service, lifecycleEvents := prepareExecutionService(t, fixture.proposalService, provider)
	selection, _ := aiprovider.NewSelection("test-provider", "")

	result, err := service.Implement(context.Background(), fixture.currentChange, fixture.currentProposal, selection)
	if err != nil {
		t.Fatalf("Implement() error = %v", err)
	}
	if result.RequestContext().TotalBytes() != wantAccounting.TotalBytes() {
		t.Fatalf("result request accounting = %#v", result.RequestContext())
	}
	if len(*lifecycleEvents) != 2 {
		t.Fatalf("lifecycle event count = %d", len(*lifecycleEvents))
	}
	for _, event := range *lifecycleEvents {
		if event.RequestContext.TotalBytes() != wantAccounting.TotalBytes() ||
			len(event.RequestContext.Components()) != 1 {
			t.Fatalf("event request accounting = %#v", event.RequestContext)
		}
	}
	assertExecutionCanonicalUnchanged(t, fixture)
}

func TestProviderRequestAccountingFailurePreventsInvocation(t *testing.T) {
	fixture := prepareExecutionFixture(t)
	providerInvoked := false
	provider := newFakeProvider(t, func(context.Context, aiprovider.ExecutionRequest) (aiprovider.ProviderResponse, error) {
		providerInvoked = true
		return aiprovider.ProviderResponse{}, errors.New("provider must not run")
	})
	provider.account = func(aiprovider.ExecutionRequest) (aiprovider.RequestContextAccounting, error) {
		return aiprovider.RequestContextAccounting{}, errors.New("request exceeds governed budget")
	}
	service, lifecycleEvents := prepareExecutionService(t, fixture.proposalService, provider)
	selection, _ := aiprovider.NewSelection("test-provider", "")

	result, err := service.Implement(context.Background(), fixture.currentChange, fixture.currentProposal, selection)
	if err == nil || !strings.Contains(err.Error(), "request exceeds governed budget") {
		t.Fatalf("Implement() accounting error = %v", err)
	}
	if providerInvoked {
		t.Fatal("provider ran after request accounting failed")
	}
	if len(*lifecycleEvents) != 0 {
		t.Fatalf("provider lifecycle began before request accounting succeeded: %#v", *lifecycleEvents)
	}
	if result.Proposal().Workspace().State() != proposal.WorkspaceActive {
		t.Fatalf("proposal state = %q", result.Proposal().Workspace().State())
	}
	assertExecutionCanonicalUnchanged(t, fixture)
}

func TestProviderFailureAfterPartialMutationIsAuditedWithoutFalseSuccess(t *testing.T) {
	fixture := prepareExecutionFixture(t)
	provider := newFakeProvider(t, func(_ context.Context, request aiprovider.ExecutionRequest) (aiprovider.ProviderResponse, error) {
		if err := os.WriteFile(
			filepath.Join(request.Workspace().Root(), "service.go"),
			[]byte("package service\n\nconst Partial = true\n"),
			0o600,
		); err != nil {
			return aiprovider.ProviderResponse{}, err
		}
		return aiprovider.ProviderResponse{}, aiprovider.NewExecutionErrorWithDiagnostic(
			aiprovider.FailureProcess,
			"test-provider",
			"thread-partial",
			"exit_code=7 stderr=safe-provider-diagnostic",
			errors.New("provider process failed"),
		)
	})
	service, lifecycleEvents := prepareExecutionService(t, fixture.proposalService, provider)
	selection, _ := aiprovider.NewSelection("test-provider", "")

	result, err := service.Implement(context.Background(), fixture.currentChange, fixture.currentProposal, selection)
	if err == nil {
		t.Fatal("Implement() succeeded after partial provider failure")
	}
	if result.Proposal().Workspace().State() != proposal.WorkspaceFailed || result.Validation().Allowed() {
		t.Fatalf("failed result state/validation = %q/%t", result.Proposal().Workspace().State(), result.Validation().Allowed())
	}
	if len(*lifecycleEvents) != 2 || (*lifecycleEvents)[1].EventType != execution.EventProviderExecutionFailed {
		t.Fatalf("execution lifecycle events = %#v", *lifecycleEvents)
	}
	failureEvent := (*lifecycleEvents)[1]
	if failureEvent.FailureKind != aiprovider.FailureProcess ||
		failureEvent.ExternalExecutionId != "thread-partial" ||
		failureEvent.ProviderDiagnostic != "exit_code=7 stderr=safe-provider-diagnostic" ||
		!failureEvent.WorkspaceMayBeChanged ||
		!reflect.DeepEqual(failureEvent.ChangedPaths, []string{"service.go"}) {
		t.Fatalf("failure provenance = %#v", failureEvent)
	}
	for _, event := range *lifecycleEvents {
		if event.EventType == execution.EventProviderExecutionCompleted {
			t.Fatal("failed provider attempt recorded a false completion")
		}
	}
	assertExecutionCanonicalUnchanged(t, fixture)
}

func TestProviderCancellationIsNormalizedAndAudited(t *testing.T) {
	fixture := prepareExecutionFixture(t)
	provider := newFakeProvider(t, func(ctx context.Context, request aiprovider.ExecutionRequest) (aiprovider.ProviderResponse, error) {
		<-ctx.Done()
		return aiprovider.ProviderResponse{}, aiprovider.NewExecutionError(
			aiprovider.FailureCancelled,
			request.Selection().ProviderIdentifier(),
			"thread-cancelled",
			ctx.Err(),
		)
	})
	service, lifecycleEvents := prepareExecutionService(t, fixture.proposalService, provider)
	selection, _ := aiprovider.NewSelection("test-provider", "")
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	result, err := service.Implement(ctx, fixture.currentChange, fixture.currentProposal, selection)
	if err == nil || aiprovider.FailureKindOf(err) != aiprovider.FailureCancelled {
		t.Fatalf("Implement() cancellation error = %v", err)
	}
	if len(*lifecycleEvents) != 2 || (*lifecycleEvents)[1].FailureKind != aiprovider.FailureCancelled ||
		(*lifecycleEvents)[1].FailureStage != "provider-or-post-guard" {
		t.Fatalf("cancellation events = %#v", *lifecycleEvents)
	}
	if result.Proposal().Workspace().State() != proposal.WorkspaceFailed {
		t.Fatalf("cancelled proposal state = %q", result.Proposal().Workspace().State())
	}
	assertExecutionCanonicalUnchanged(t, fixture)
}

func TestExecutionAttemptCapturesProviderModelAtStart(t *testing.T) {
	fixture := prepareExecutionFixture(t)
	activeSelection, err := aiprovider.NewSelection("test-provider", "model-a")
	if err != nil {
		t.Fatalf("NewSelection() error = %v", err)
	}
	var capturedSelection aiprovider.Selection
	provider := newFakeProvider(t, func(_ context.Context, request aiprovider.ExecutionRequest) (aiprovider.ProviderResponse, error) {
		capturedSelection = request.Selection()
		activeSelection, err = aiprovider.NewSelection("test-provider", "model-b")
		if err != nil {
			return aiprovider.ProviderResponse{}, err
		}
		return aiprovider.ProviderResponse{}, aiprovider.NewExecutionError(
			aiprovider.FailureProcess,
			"test-provider",
			"thread-model-capture",
			errors.New("intentional model-capture failure"),
		)
	})
	service, lifecycleEvents := prepareExecutionService(t, fixture.proposalService, provider)

	_, executionError := service.Implement(
		context.Background(),
		fixture.currentChange,
		fixture.currentProposal,
		activeSelection,
	)
	if executionError == nil {
		t.Fatal("intentional provider failure unexpectedly succeeded")
	}
	capturedModel, captured := capturedSelection.ModelIdentifier()
	futureModel, futureSelected := activeSelection.ModelIdentifier()
	if !captured || capturedModel != "model-a" || !futureSelected || futureModel != "model-b" {
		t.Fatalf("captured/future selections = %#v / %#v", capturedSelection, activeSelection)
	}
	for _, event := range *lifecycleEvents {
		model, selected := event.Request.Selection().ModelIdentifier()
		if !selected || model != "model-a" {
			t.Fatalf("attempt event selection changed mid-flight: %#v", event.Request.Selection())
		}
	}
}

func TestProviderResponseLinkageMismatchFailsAndIsAudited(t *testing.T) {
	fixture := prepareExecutionFixture(t)
	provider := newFakeProvider(t, func(_ context.Context, request aiprovider.ExecutionRequest) (aiprovider.ProviderResponse, error) {
		wrongSelection, err := aiprovider.NewSelection("test-provider", "wrong-model")
		if err != nil {
			return aiprovider.ProviderResponse{}, err
		}
		startedAt := time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC)
		return aiprovider.NewProviderResponse(
			request.AttemptId(),
			wrongSelection,
			"test-provider 1.0",
			"thread-mismatch",
			"",
			false,
			aiprovider.ProviderUsage{},
			startedAt,
			startedAt.Add(time.Second),
		)
	})
	service, lifecycleEvents := prepareExecutionService(t, fixture.proposalService, provider)
	selection, _ := aiprovider.NewSelection("test-provider", "selected-model")

	_, err := service.Implement(context.Background(), fixture.currentChange, fixture.currentProposal, selection)
	if err == nil || aiprovider.FailureKindOf(err) != aiprovider.FailureMalformedOutput {
		t.Fatalf("Implement() response-linkage error = %v", err)
	}
	if len(*lifecycleEvents) != 2 ||
		(*lifecycleEvents)[1].EventType != execution.EventProviderExecutionFailed ||
		(*lifecycleEvents)[1].ExternalExecutionId != "thread-mismatch" {
		t.Fatalf("response-linkage events = %#v", *lifecycleEvents)
	}
	assertExecutionCanonicalUnchanged(t, fixture)
}

func TestCanonicalDriftDuringProviderExecutionFailsClosedWithoutRepair(t *testing.T) {
	fixture := prepareExecutionFixture(t)
	provider := newFakeProvider(t, func(_ context.Context, request aiprovider.ExecutionRequest) (aiprovider.ProviderResponse, error) {
		if err := os.WriteFile(filepath.Join(fixture.canonicalRoot, "README.md"), []byte("external drift\n"), 0o600); err != nil {
			return aiprovider.ProviderResponse{}, err
		}
		return successfulProviderResponse(t, request, "thread-drift"), nil
	})
	service, lifecycleEvents := prepareExecutionService(t, fixture.proposalService, provider)
	selection, _ := aiprovider.NewSelection("test-provider", "")

	result, err := service.Implement(context.Background(), fixture.currentChange, fixture.currentProposal, selection)
	var driftError proposal.CanonicalSourceDriftError
	if !errors.As(err, &driftError) {
		t.Fatalf("Implement() error = %T %v, want canonical drift", err, err)
	}
	if result.Proposal().Workspace().State() != proposal.WorkspaceFailed {
		t.Fatalf("drifted proposal state = %q", result.Proposal().Workspace().State())
	}
	if len(*lifecycleEvents) != 2 || (*lifecycleEvents)[1].EventType != execution.EventProviderExecutionFailed ||
		(*lifecycleEvents)[1].FailureStage != "provider-or-post-guard" {
		t.Fatalf("drift events = %#v", *lifecycleEvents)
	}
	contents, readError := os.ReadFile(filepath.Join(fixture.canonicalRoot, "README.md"))
	if readError != nil || string(contents) != "external drift\n" {
		t.Fatalf("Praetor repaired external drift: %q/%v", contents, readError)
	}
}

func TestExecutionServiceDoesNotFallbackFromManualSelection(t *testing.T) {
	fixture := prepareExecutionFixture(t)
	provider := newFakeProvider(t, func(context.Context, aiprovider.ExecutionRequest) (aiprovider.ProviderResponse, error) {
		t.Fatal("registered provider was called for an unknown manual selection")
		return aiprovider.ProviderResponse{}, nil
	})
	service, lifecycleEvents := prepareExecutionService(t, fixture.proposalService, provider)
	unknownSelection, _ := aiprovider.NewSelection("unknown-provider", "")
	if _, err := service.Implement(context.Background(), fixture.currentChange, fixture.currentProposal, unknownSelection); err == nil {
		t.Fatal("unknown selection unexpectedly fell back")
	}
	if len(*lifecycleEvents) != 0 {
		t.Fatalf("unresolved provider created execution audit: %#v", *lifecycleEvents)
	}
}

type planningVerificationRunner struct{ calls int }

func (*planningVerificationRunner) Resolve(string) (string, error) { return os.Executable() }
func (runner *planningVerificationRunner) Run(context.Context, verification.ProcessInvocation) (verification.ProcessResult, error) {
	runner.calls++
	return verification.NewProcessResult(0, true, []byte("pass\n"), nil, false), nil
}
func (*planningVerificationRunner) MaximumOutputBytes() int { return 4096 }

func TestVerificationPlanningUsesFreshSelectedReadOnlyProviderAttempt(t *testing.T) {
	fixture := prepareExecutionFixture(t)
	if err := os.WriteFile(
		filepath.Join(fixture.currentProposal.Workspace().Root(), "service.go"),
		[]byte("package service\n\nfunc Greeting() string { return \"hello-praetor\" }\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	retainedProposal, validation, err := fixture.proposalService.ExtractPatch(fixture.currentProposal)
	if err != nil || !validation.Allowed() {
		t.Fatalf("ExtractPatch() = %#v/%v", validation, err)
	}
	var capturedRequest aiprovider.ExecutionRequest
	provider := newFakeProvider(t, func(_ context.Context, request aiprovider.ExecutionRequest) (aiprovider.ProviderResponse, error) {
		capturedRequest = request
		started := time.Date(2026, time.September, 2, 12, 0, 0, 0, time.UTC)
		return aiprovider.NewProviderResponse(
			request.AttemptId(),
			request.Selection(),
			"test-provider 1.0",
			"thread-planning",
			`{"candidates":[{"kind":"lint","executable":"ruff","arguments":[],"working_directory":".","supporting_evidence":["pyproject.toml"]}]}`,
			false,
			aiprovider.ProviderUsage{},
			started,
			started.Add(time.Second),
		)
	})
	executionService, executionEvents := prepareExecutionService(t, fixture.proposalService, provider)
	runner := &planningVerificationRunner{}
	clock := func() time.Time { return time.Date(2026, time.September, 2, 13, 0, 0, 0, time.UTC) }
	engine, err := verification.NewEngine(runner, fixture.proposalService.VerifyIntegrity, clock)
	if err != nil {
		t.Fatal(err)
	}
	verificationService, err := verification.New(
		engine,
		func(verification.LifecycleEvent) error { return nil },
		func() (verification.VerificationAttemptId, error) {
			return "verification-11223344556677889900aabbccddeeff", nil
		},
		clock,
	)
	if err != nil {
		t.Fatal(err)
	}
	selection, _ := aiprovider.NewSelection("test-provider", "planner-model")
	result, err := verificationService.Verify(
		context.Background(),
		fixture.currentChange,
		retainedProposal,
		func(ctx context.Context, request verification.PlanningRequest) (verification.PlanningResult, error) {
			return executionService.PlanVerification(ctx, fixture.currentChange, retainedProposal, selection, request)
		},
	)
	if err != nil || !result.Passed() {
		t.Fatalf("Verify() = %#v/%v", result, err)
	}
	if capturedRequest.AttemptId() != executionTestAttemptId ||
		capturedRequest.RoleContract().Role() != aiprovider.RoleVerificationPlanning ||
		capturedRequest.RoleContract().WorkspaceAccess() != aiprovider.WorkspaceAccessReadOnly {
		t.Fatalf("planning request = %#v", capturedRequest)
	}
	if capturedRequest.AttemptId() == aiprovider.ExecutionAttemptId(result.AttemptId()) {
		t.Fatal("AI ExecutionAttemptId reused VerificationAttemptId")
	}
	if len(*executionEvents) != 2 ||
		(*executionEvents)[0].EventType != execution.EventVerificationPlanningStarted ||
		(*executionEvents)[1].EventType != execution.EventVerificationPlanningCompleted ||
		runner.calls != 2 {
		t.Fatalf("planning events / verification calls = %#v / %d", *executionEvents, runner.calls)
	}
	assertExecutionCanonicalUnchanged(t, fixture)
}

func TestVerificationPlanningMutationFailsClosedAndIsAudited(t *testing.T) {
	fixture := prepareExecutionFixture(t)
	workspaceRoot := fixture.currentProposal.Workspace().Root()
	if err := os.WriteFile(
		filepath.Join(workspaceRoot, "service.go"),
		[]byte("package service\n\nfunc Greeting() string { return \"hello-praetor\" }\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	retainedProposal, _, err := fixture.proposalService.ExtractPatch(fixture.currentProposal)
	if err != nil {
		t.Fatal(err)
	}
	provider := newFakeProvider(t, func(_ context.Context, request aiprovider.ExecutionRequest) (aiprovider.ProviderResponse, error) {
		if err := os.WriteFile(
			filepath.Join(request.Workspace().Root(), "service.go"),
			[]byte("package service\n\nconst PlannerMutated = true\n"),
			0o600,
		); err != nil {
			return aiprovider.ProviderResponse{}, err
		}
		started := time.Date(2026, time.September, 2, 12, 0, 0, 0, time.UTC)
		return aiprovider.NewProviderResponse(
			request.AttemptId(), request.Selection(), "test-provider 1.0", "thread-mutating-planner",
			`{"candidates":[]}`, false, aiprovider.ProviderUsage{}, started, started.Add(time.Second),
		)
	})
	service, events := prepareExecutionService(t, fixture.proposalService, provider)
	discovery, err := verification.Discover(workspaceRoot)
	if err != nil {
		t.Fatal(err)
	}
	// Build the otherwise-private planning request through the verification
	// service, whose planner port is its only intended producer boundary.
	runner := &planningVerificationRunner{}
	clock := func() time.Time { return time.Date(2026, time.September, 2, 13, 0, 0, 0, time.UTC) }
	engine, _ := verification.NewEngine(runner, fixture.proposalService.VerifyIntegrity, clock)
	verificationService, _ := verification.New(
		engine,
		func(verification.LifecycleEvent) error { return nil },
		func() (verification.VerificationAttemptId, error) {
			return "verification-11223344556677889900aabbccddeeff", nil
		},
		clock,
	)
	if !discovery.NeedsPlanning() {
		t.Fatal("fixture did not exercise planning")
	}
	selection, _ := aiprovider.NewSelection("test-provider", "")
	_, err = verificationService.Verify(
		context.Background(),
		fixture.currentChange,
		retainedProposal,
		func(ctx context.Context, request verification.PlanningRequest) (verification.PlanningResult, error) {
			return service.PlanVerification(ctx, fixture.currentChange, retainedProposal, selection, request)
		},
	)
	if err == nil || !strings.Contains(err.Error(), "changed protected source state") {
		t.Fatalf("mutating planner error = %v", err)
	}
	if len(*events) != 2 || (*events)[1].EventType != execution.EventVerificationPlanningFailed ||
		!(*events)[1].WorkspaceMayBeChanged {
		t.Fatalf("mutating planner events = %#v", *events)
	}
	assertExecutionCanonicalUnchanged(t, fixture)
}

func TestCodexCLILiveProposalWorkspaceE2E(t *testing.T) {
	if os.Getenv("PRAETOR_CODEX_CLI_E2E") != "1" {
		t.Skip("set PRAETOR_CODEX_CLI_E2E=1 to run the authenticated Codex CLI smoke test")
	}
	fixture := prepareExecutionFixture(t)
	provider, err := codexcli.New(codexcli.Config{Timeout: 3 * time.Minute})
	if err != nil {
		t.Fatalf("codexcli.New() error = %v", err)
	}
	service, lifecycleEvents := prepareExecutionService(t, fixture.proposalService, provider)
	selection, err := aiprovider.NewSelection(codexcli.Identifier, "")
	if err != nil {
		t.Fatalf("NewSelection() error = %v", err)
	}

	result, err := service.Implement(context.Background(), fixture.currentChange, fixture.currentProposal, selection)
	if err != nil {
		t.Fatalf("live Implement() error = %v", err)
	}
	if !result.Validation().Allowed() || result.Proposal().Workspace().State() != proposal.WorkspaceRetained {
		t.Fatalf(
			"live proposal validation/state = %t/%q",
			result.Validation().Allowed(),
			result.Proposal().Workspace().State(),
		)
	}
	artifact, hasArtifact := result.Proposal().PatchArtifact()
	if !hasArtifact || len(artifact.ChangedPaths()) == 0 {
		t.Fatalf("live provider produced no Git patch: %#v/%t", artifact, hasArtifact)
	}
	for _, changedPath := range artifact.ChangedPaths() {
		if changedPath != "service.go" && changedPath != "service_test.go" {
			t.Fatalf("live provider changed out-of-scope path %q", changedPath)
		}
	}
	response, completed := result.Response()
	if !completed || response.ExternalExecutionId() == "" || response.ProviderVersion() == "" ||
		response.Selection().ProviderIdentifier() != codexcli.Identifier ||
		provider.Descriptor().Vendor() != "OpenAI" {
		t.Fatalf("live provider response = %#v/%t", response, completed)
	}
	if len(*lifecycleEvents) != 2 ||
		(*lifecycleEvents)[0].EventType != execution.EventProviderExecutionStarted ||
		(*lifecycleEvents)[1].EventType != execution.EventProviderExecutionCompleted {
		t.Fatalf("live execution events = %#v", *lifecycleEvents)
	}
	if fixture.currentChange.State() != change.StateIsolated {
		t.Fatalf("live provider advanced Change to %q", fixture.currentChange.State())
	}
	assertExecutionCanonicalUnchanged(t, fixture)
	assertPatchEventsLinkedToAttempt(t, *fixture.proposalEvents, artifact.ChangedPaths())
}

func prepareExecutionService(
	t *testing.T,
	proposalService *proposal.Service,
	provider aiprovider.Provider,
) (*execution.Service, *[]execution.LifecycleEvent) {
	t.Helper()
	registry, err := aiprovider.NewRegistry(provider)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	events := new([]execution.LifecycleEvent)
	clockValue := time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC)
	service, err := execution.New(
		registry,
		proposalService,
		func(event execution.LifecycleEvent) error {
			*events = append(*events, event)
			return nil
		},
		func() (aiprovider.ExecutionAttemptId, error) { return executionTestAttemptId, nil },
		func() time.Time {
			clockValue = clockValue.Add(time.Second)
			return clockValue
		},
	)
	if err != nil {
		t.Fatalf("execution.New() error = %v", err)
	}
	return service, events
}

func prepareExecutionFixture(t *testing.T) executionFixture {
	t.Helper()
	canonicalRoot := t.TempDir()
	files := map[string]string{
		"service.go":      "package service\n\nfunc Greeting() string { return \"hello\" }\n",
		"service_test.go": "package service\n",
		"README.md":       "fixture\n",
		"go.mod":          "module fixture\n\ngo 1.25.1\n",
		"pyproject.toml":  "[tool.pytest.ini_options]\n",
	}
	for name, contents := range files {
		if err := os.WriteFile(filepath.Join(canonicalRoot, name), []byte(contents), 0o600); err != nil {
			t.Fatalf("write fixture %s: %v", name, err)
		}
	}
	runExecutionGit(t, canonicalRoot, "init", "--quiet")
	runExecutionGit(t, canonicalRoot, "add", ".")
	runExecutionGit(
		t,
		canonicalRoot,
		"-c", "user.name=Praetor Test",
		"-c", "user.email=praetor@example.invalid",
		"commit", "--quiet", "-m", "baseline",
	)
	baseRevision := strings.TrimSpace(string(runExecutionGit(t, canonicalRoot, "rev-parse", "HEAD")))
	createdAt := time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC)
	currentChange, err := change.New(
		"change-execution",
		executionTestProjectId,
		"Change Greeting() to return hello-praetor.",
		createdAt,
	)
	if err != nil {
		t.Fatalf("change.New() error = %v", err)
	}
	if _, err := currentChange.Transition(change.StatePlanned, createdAt.Add(time.Second), "planned"); err != nil {
		t.Fatalf("planned transition error = %v", err)
	}
	snapshot, err := repository.Inspect(executionTestProjectId, canonicalRoot)
	if err != nil {
		t.Fatalf("repository.Inspect() error = %v", err)
	}
	analysis, err := source.AnalyzeImpact(currentChange, snapshot, source.ScopeRequest{
		Expected:  []string{"service.go"},
		Possible:  []string{"service_test.go"},
		Protected: []string{"go.mod"},
	})
	if err != nil {
		t.Fatalf("AnalyzeImpact() error = %v", err)
	}
	approvedScope, err := source.EstablishApprovedScope(analysis)
	if err != nil {
		t.Fatalf("EstablishApprovedScope() error = %v", err)
	}
	gitAdapter, err := gitproposal.New(t.TempDir())
	if err != nil {
		t.Fatalf("gitproposal.New() error = %v", err)
	}
	proposalEvents := new([]proposal.LifecycleEvent)
	proposalService, err := proposal.New(
		gitAdapter,
		gitAdapter,
		proposal.RepositoryInspector(repository.Inspect),
		func(event proposal.LifecycleEvent) error {
			*proposalEvents = append(*proposalEvents, event)
			return nil
		},
		func() time.Time { return createdAt.Add(4 * time.Second) },
	)
	if err != nil {
		t.Fatalf("proposal.New() error = %v", err)
	}
	currentProposal, err := proposalService.CreateWorkspace(currentChange, snapshot, approvedScope)
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}
	if _, err := currentChange.Transition(change.StateIsolated, createdAt.Add(2*time.Second), "isolated"); err != nil {
		t.Fatalf("isolated transition error = %v", err)
	}
	t.Cleanup(func() {
		if _, statError := os.Stat(currentProposal.Workspace().Root()); statError == nil {
			_, _ = proposalService.Discard(currentProposal, "test cleanup")
		}
	})
	return executionFixture{
		currentChange:   currentChange,
		currentProposal: currentProposal,
		proposalService: proposalService,
		canonicalRoot:   canonicalRoot,
		baseRevision:    baseRevision,
		baseDigest:      snapshot.SourceStateDigest(),
		proposalEvents:  proposalEvents,
	}
}

func successfulProviderResponse(
	t *testing.T,
	request aiprovider.ExecutionRequest,
	externalId string,
) aiprovider.ProviderResponse {
	t.Helper()
	started := time.Date(2026, time.September, 1, 12, 0, 1, 0, time.UTC)
	response, err := aiprovider.NewProviderResponse(
		request.AttemptId(),
		request.Selection(),
		"test-provider 1.0",
		externalId,
		"implementation completed",
		false,
		aiprovider.ProviderUsage{},
		started,
		started.Add(time.Second),
	)
	if err != nil {
		t.Fatalf("NewProviderResponse() error = %v", err)
	}
	return response
}

func assertExecutionCanonicalUnchanged(t *testing.T, fixture executionFixture) {
	t.Helper()
	snapshot, err := repository.Inspect(executionTestProjectId, fixture.canonicalRoot)
	if err != nil {
		t.Fatalf("repository.Inspect() error = %v", err)
	}
	if snapshot.HeadRevision() != fixture.baseRevision ||
		snapshot.SourceStateDigest() != fixture.baseDigest ||
		snapshot.WorkingTreeState() != source.WorkingTreeClean {
		t.Fatalf("canonical source changed: %#v", snapshot)
	}
	if status := runExecutionGit(t, fixture.canonicalRoot, "status", "--porcelain=v1", "--untracked-files=all"); len(status) != 0 {
		t.Fatalf("canonical Git status = %q", status)
	}
	if _, err := os.Stat(filepath.Join(fixture.canonicalRoot, ".praetor")); !os.IsNotExist(err) {
		t.Fatalf("runtime metadata appeared in canonical source: %v", err)
	}
}

func assertPatchEventsLinkedToAttempt(
	t *testing.T,
	events []proposal.LifecycleEvent,
	wantPaths []string,
) {
	t.Helper()
	linked := 0
	for _, event := range events {
		if event.EventType == proposal.EventPatchExtracted ||
			event.EventType == proposal.EventPatchSurfaceValidated ||
			event.EventType == proposal.EventPatchRejected {
			if event.ExecutionAttemptId != string(executionTestAttemptId) {
				t.Fatalf("patch event lacks attempt linkage: %#v", event)
			}
			linked++
		}
	}
	if len(wantPaths) == 0 && linked != 1 {
		t.Fatalf("empty patch linked events = %d, want 1 rejection", linked)
	}
	if len(wantPaths) > 0 && linked != 2 {
		t.Fatalf("patch linked events = %d, want extraction + classification", linked)
	}
}

func runExecutionGit(t *testing.T, repositoryRoot string, arguments ...string) []byte {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", repositoryRoot}, arguments...)...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", arguments, err, output)
	}
	return output
}

func TestProviderReadinessFailurePreventsAttemptAuditAndInvocation(t *testing.T) {
	fixture := prepareExecutionFixture(t)
	providerInvoked := false
	provider := newFakeProvider(t, func(context.Context, aiprovider.ExecutionRequest) (aiprovider.ProviderResponse, error) {
		providerInvoked = true
		return aiprovider.ProviderResponse{}, errors.New("provider must not run")
	})
	readiness, readinessError := aiprovider.NewLocalReadiness(
		aiprovider.ReadinessUnavailable,
		"",
		"",
		"the provider executable is missing",
		"install the provider executable",
		"not inspected",
	)
	if readinessError != nil {
		t.Fatalf("NewLocalReadiness() error = %v", readinessError)
	}
	provider.readiness = readiness
	service, lifecycleEvents := prepareExecutionService(t, fixture.proposalService, provider)
	selection, _ := aiprovider.NewSelection("test-provider", "")

	result, err := service.Implement(context.Background(), fixture.currentChange, fixture.currentProposal, selection)
	var setupError *aiprovider.ReadinessError
	if !errors.As(err, &setupError) || setupError.Readiness().Disposition() != aiprovider.ReadinessUnavailable {
		t.Fatalf("Implement() readiness error = %T %v", err, err)
	}
	if providerInvoked {
		t.Fatal("provider executed after readiness failure")
	}
	if len(*lifecycleEvents) != 0 {
		t.Fatalf("readiness failure created provider attempt audit: %#v", *lifecycleEvents)
	}
	if result.Proposal().Workspace().State() != proposal.WorkspaceActive {
		t.Fatalf("readiness failure proposal state = %q", result.Proposal().Workspace().State())
	}
	assertExecutionCanonicalUnchanged(t, fixture)
}

func TestUnsupportedImplementationCapabilityFailsBeforeAttemptAndInvocation(t *testing.T) {
	fixture := prepareExecutionFixture(t)
	providerInvoked := false
	provider := newFakeProvider(t, func(context.Context, aiprovider.ExecutionRequest) (aiprovider.ProviderResponse, error) {
		providerInvoked = true
		return aiprovider.ProviderResponse{}, errors.New("provider must not run")
	})
	descriptor, err := aiprovider.NewProviderDescriptor(
		"test-provider",
		"Test Vendor",
		"Read Only Test Provider",
		[]aiprovider.ProviderCapability{
			aiprovider.CapabilityWorkspaceReadOnly,
			aiprovider.CapabilityContextCancellation,
		},
	)
	if err != nil {
		t.Fatalf("NewProviderDescriptor() error = %v", err)
	}
	provider.descriptor = descriptor
	service, lifecycleEvents := prepareExecutionService(t, fixture.proposalService, provider)
	selection, _ := aiprovider.NewSelection("test-provider", "")

	result, err := service.Implement(context.Background(), fixture.currentChange, fixture.currentProposal, selection)
	if err == nil || !strings.Contains(err.Error(), "lacks required capability") {
		t.Fatalf("Implement() capability error = %v", err)
	}
	if providerInvoked || len(*lifecycleEvents) != 0 {
		t.Fatalf("unsupported provider invoked/audited = %t/%#v", providerInvoked, *lifecycleEvents)
	}
	if result.Proposal().Workspace().State() != proposal.WorkspaceActive {
		t.Fatalf("unsupported provider proposal state = %q", result.Proposal().Workspace().State())
	}
	assertExecutionCanonicalUnchanged(t, fixture)
}
