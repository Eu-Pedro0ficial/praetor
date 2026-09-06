package command_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/Eu-Pedro0ficial/praetor/internal/adapters/verification/localexec"
	"github.com/Eu-Pedro0ficial/praetor/internal/aiprovider"
	"github.com/Eu-Pedro0ficial/praetor/internal/audit"
	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"github.com/Eu-Pedro0ficial/praetor/internal/command"
	"github.com/Eu-Pedro0ficial/praetor/internal/proposal"
	"github.com/Eu-Pedro0ficial/praetor/internal/verification"
)

type commandVerificationRunner struct {
	exitCodes   []int
	invocations []verification.ProcessInvocation
}

func (*commandVerificationRunner) Resolve(string) (string, error) { return os.Executable() }
func (runner *commandVerificationRunner) Run(
	_ context.Context,
	invocation verification.ProcessInvocation,
) (verification.ProcessResult, error) {
	runner.invocations = append(runner.invocations, invocation)
	index := len(runner.invocations) - 1
	exitCode := 0
	if index < len(runner.exitCodes) {
		exitCode = runner.exitCodes[index]
	}
	if exitCode != 0 {
		return verification.NewProcessResult(exitCode, true, nil, []byte("deterministic check failed\n"), false), errors.New("exit status 1")
	}
	return verification.NewProcessResult(0, true, []byte("deterministic check passed\n"), nil, false), nil
}
func (*commandVerificationRunner) MaximumOutputBytes() int { return 4096 }

func planningCommandResponse(
	t *testing.T,
	request aiprovider.ExecutionRequest,
	summary string,
) aiprovider.ProviderResponse {
	t.Helper()
	startedAt := time.Date(2026, time.September, 2, 15, 0, 0, 0, time.UTC)
	response, err := aiprovider.NewProviderResponse(
		request.AttemptId(),
		request.Selection(),
		"codex-cli test-1.0",
		"thread-verification-planner",
		summary,
		false,
		aiprovider.ProviderUsage{},
		startedAt,
		startedAt.Add(time.Second),
	)
	if err != nil {
		t.Fatalf("NewProviderResponse() error = %v", err)
	}
	return response
}

func TestChangeVerifyPassesRequiredChecksAndTransitionsIsolatedToValidated(t *testing.T) {
	var requests []aiprovider.ExecutionRequest
	provider := newCommandFakeProvider(t, func(_ context.Context, request aiprovider.ExecutionRequest) (aiprovider.ProviderResponse, error) {
		requests = append(requests, request)
		switch request.RoleContract().Role() {
		case aiprovider.RoleImplementation:
			if err := os.WriteFile(
				filepathFromSlash(request.Workspace().Root(), "internal/service/service.go"),
				[]byte("package service\n\nfunc Greeting() string { return \"hello-praetor\" }\n"),
				0o600,
			); err != nil {
				return aiprovider.ProviderResponse{}, err
			}
			return newCommandProviderResponse(t, request, "thread-implementation", "implemented"), nil
		case aiprovider.RoleVerificationPlanning:
			return planningCommandResponse(t, request,
				`{"candidates":[{"kind":"lint","executable":"ruff","arguments":[],"working_directory":".","supporting_evidence":["pyproject.toml"]}]}`), nil
		default:
			return aiprovider.ProviderResponse{}, errors.New("unexpected provider role")
		}
	})
	runner := &commandVerificationRunner{}
	repositoryRoot, dataDirectory, session, registry := prepareProviderCommandTest(t, provider, runner)
	if _, err := registry.Dispatch(session, "provider model planner-model", io.Discard); err != nil {
		t.Fatalf("select provider-scoped model: %v", err)
	}
	if _, err := registry.Dispatch(session,
		`change isolate change-verify "Change Greeting() to return hello-praetor." --expected internal/service/service.go --possible internal/service/service_test.go --protected go.mod`,
		io.Discard,
	); err != nil {
		t.Fatalf("change isolate: %v", err)
	}
	if _, err := registry.Dispatch(session, "change implement", io.Discard); err != nil {
		t.Fatalf("change implement: %v", err)
	}
	var output bytes.Buffer
	if _, err := registry.Dispatch(session, "change verify", &output); err != nil {
		t.Fatalf("change verify: %v\n%s", err, output.String())
	}
	for _, expected := range []string{
		"Verification attempt: verification-abcdef0123456789abcdef0123456789",
		"Verification result: PASS",
		"AI planning provenance: provider=codex-cli",
		"Check: lint [PASS] ruff",
		"Check: test [PASS] go test ./...",
		"Check: patch-integrity [PASS] praetor-patch-integrity",
		"Evidence set:",
		"(3 items)",
		"Change state: validated",
	} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("verify output %q lacks %q", output.String(), expected)
		}
	}
	currentChange, _ := session.CurrentChange()
	currentProposal, hasProposal := session.CurrentProposal()
	lastVerification, hasVerification := session.LastVerification()
	if currentChange.State() != change.StateValidated || !hasProposal ||
		currentProposal.Workspace().State() != proposal.WorkspaceRetained ||
		!hasVerification || !lastVerification.Passed() {
		t.Fatalf("validated session state = %s/%t/%s/%t/%t",
			currentChange.State(), hasProposal, currentProposal.Workspace().State(), hasVerification, lastVerification.Passed())
	}
	if len(requests) != 2 || requests[0].RoleContract().Role() != aiprovider.RoleImplementation ||
		requests[1].RoleContract().Role() != aiprovider.RoleVerificationPlanning ||
		requests[1].RoleContract().WorkspaceAccess() != aiprovider.WorkspaceAccessReadOnly ||
		requests[0].AttemptId() == requests[1].AttemptId() {
		t.Fatalf("provider requests = %#v", requests)
	}
	if model, selected := requests[1].Selection().ModelIdentifier(); !selected || model != "planner-model" {
		t.Fatalf("planner model selection = %#v", requests[1].Selection())
	}
	if len(runner.invocations) != 2 {
		t.Fatalf("deterministic invocations = %#v", runner.invocations)
	}
	if _, err := registry.Dispatch(session, "change verify", io.Discard); err == nil ||
		!strings.Contains(err.Error(), "must be isolated") {
		t.Fatalf("repeat verification from validated state error = %v", err)
	}
	assertM06Audit(t, dataDirectory, true, false, true)
	assertCommandCanonicalSourceUnchanged(t, repositoryRoot)
}

func TestChangeVerifyFailureRemainsIsolatedAndRetainsProposal(t *testing.T) {
	provider := newCommandFakeProvider(t, func(_ context.Context, request aiprovider.ExecutionRequest) (aiprovider.ProviderResponse, error) {
		if request.RoleContract().Role() == aiprovider.RoleImplementation {
			if err := os.WriteFile(
				filepathFromSlash(request.Workspace().Root(), "internal/service/service.go"),
				[]byte("package service\n\nconst Changed = true\n"),
				0o600,
			); err != nil {
				return aiprovider.ProviderResponse{}, err
			}
			return newCommandProviderResponse(t, request, "", "implemented"), nil
		}
		return planningCommandResponse(t, request,
			`{"candidates":[{"kind":"lint","executable":"ruff","arguments":[],"working_directory":".","supporting_evidence":["pyproject.toml"]}]}`), nil
	})
	runner := &commandVerificationRunner{exitCodes: []int{1, 0}}
	repositoryRoot, dataDirectory, session, registry := prepareProviderCommandTest(t, provider, runner)
	if _, err := registry.Dispatch(session,
		`change isolate change-verify-failure "Exercise verification failure." --expected internal/service/service.go --protected go.mod`,
		io.Discard,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Dispatch(session, "change implement", io.Discard); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if _, err := registry.Dispatch(session, "change verify", &output); err == nil {
		t.Fatal("failing verification returned no error")
	}
	if !strings.Contains(output.String(), "Verification result: FAIL") ||
		!strings.Contains(output.String(), "Check: lint [FAIL]") ||
		!strings.Contains(output.String(), "Change state: isolated") {
		t.Fatalf("failure output = %q", output.String())
	}
	currentChange, _ := session.CurrentChange()
	currentProposal, hasProposal := session.CurrentProposal()
	lastVerification, hasVerification := session.LastVerification()
	if currentChange.State() != change.StateIsolated || !hasProposal ||
		currentProposal.Workspace().State() != proposal.WorkspaceRetained ||
		!hasVerification || lastVerification.Passed() {
		t.Fatalf("failure session state = %s/%t/%s/%t/%t",
			currentChange.State(), hasProposal, currentProposal.Workspace().State(), hasVerification, lastVerification.Passed())
	}
	assertM06Audit(t, dataDirectory, false, true, true)
	assertCommandCanonicalSourceUnchanged(t, repositoryRoot)
	if _, err := registry.Dispatch(session, "change discard", io.Discard); err != nil {
		t.Fatalf("discard retained failed proposal: %v", err)
	}
	currentChange, _ = session.CurrentChange()
	if currentChange.State() != change.StateRejected {
		t.Fatalf("discarded Change state = %q", currentChange.State())
	}
}

func TestChangeVerifyContinuesWhenOptionalPlannerFailsButGoEvidenceIsSufficient(t *testing.T) {
	provider := newCommandFakeProvider(t, func(_ context.Context, request aiprovider.ExecutionRequest) (aiprovider.ProviderResponse, error) {
		if request.RoleContract().Role() == aiprovider.RoleImplementation {
			if err := os.WriteFile(
				filepathFromSlash(request.Workspace().Root(), "internal/service/service.go"),
				[]byte("package service\n\nconst Changed = true\n"),
				0o600,
			); err != nil {
				return aiprovider.ProviderResponse{}, err
			}
			return newCommandProviderResponse(t, request, "", "implemented"), nil
		}
		return aiprovider.ProviderResponse{}, aiprovider.NewExecutionError(
			aiprovider.FailureUnavailable,
			request.Selection().ProviderIdentifier(),
			"",
			errors.New("planner unavailable"),
		)
	})
	runner := &commandVerificationRunner{}
	_, dataDirectory, session, registry := prepareProviderCommandTest(t, provider, runner)
	if _, err := registry.Dispatch(session,
		`change isolate change-planner-fallback "Exercise deterministic fallback." --expected internal/service/service.go --protected go.mod`,
		io.Discard,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Dispatch(session, "change implement", io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Dispatch(session, "change", io.Discard); err != nil {
		t.Fatalf("enter change context: %v", err)
	}
	var output bytes.Buffer
	if _, err := registry.Dispatch(session, "verify", &output); err != nil {
		t.Fatalf("deterministic fallback verify: %v", err)
	}
	if !strings.Contains(output.String(), "AI planning provenance: failed; deterministic candidates retained") ||
		!strings.Contains(output.String(), "Verification result: PASS") || len(runner.invocations) != 1 {
		t.Fatalf("deterministic fallback output/runs = %q/%d", output.String(), len(runner.invocations))
	}
	if _, err := registry.Dispatch(session, "end", io.Discard); err != nil || session.CurrentMode().Identity != command.ModeRoot {
		t.Fatalf("leave change context: mode=%q error=%v", session.CurrentMode(), err)
	}
	assertM06Audit(t, dataDirectory, true, false, false)
}

func TestChangeVerifyRunsRealDirectProcessAgainstProposalWorkspace(t *testing.T) {
	provider := newCommandFakeProvider(t, func(_ context.Context, request aiprovider.ExecutionRequest) (aiprovider.ProviderResponse, error) {
		if request.RoleContract().Role() == aiprovider.RoleImplementation {
			if err := os.WriteFile(
				filepathFromSlash(request.Workspace().Root(), "internal/service/service.go"),
				[]byte("package service\n\nfunc Greeting() string { return \"verified\" }\n"),
				0o600,
			); err != nil {
				return aiprovider.ProviderResponse{}, err
			}
			return newCommandProviderResponse(t, request, "", "implemented"), nil
		}
		return aiprovider.ProviderResponse{}, aiprovider.NewExecutionError(
			aiprovider.FailureUnavailable,
			request.Selection().ProviderIdentifier(),
			"",
			errors.New("planner intentionally unavailable for deterministic smoke"),
		)
	})
	repositoryRoot, _, session, registry := prepareProviderCommandTest(t, provider, localexec.NewDefault())
	if _, err := registry.Dispatch(session,
		`change isolate change-real-verification "Run direct Go verification." --expected internal/service/service.go --protected go.mod`,
		io.Discard,
	); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Dispatch(session, "change implement", io.Discard); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if _, err := registry.Dispatch(session, "change verify", &output); err != nil {
		t.Fatalf("real direct-process verification: %v\n%s", err, output.String())
	}
	result, available := session.LastVerification()
	if !available || !result.Passed() {
		t.Fatalf("real verification result = %#v/%t", result, available)
	}
	evidence := result.EvidenceSet().Evidence()
	if len(evidence) != 2 || evidence[0].Executable() != "go" || evidence[0].Outcome() != verification.OutcomePass ||
		!strings.Contains(evidence[0].ResolvedExecutable(), "go") {
		t.Fatalf("real process evidence = %#v", evidence)
	}
	assertCommandCanonicalSourceUnchanged(t, repositoryRoot)
}

func TestChangeVerifyRequiresRetainedProposalAndNoArguments(t *testing.T) {
	provider := newCommandFakeProvider(t, func(context.Context, aiprovider.ExecutionRequest) (aiprovider.ProviderResponse, error) {
		return aiprovider.ProviderResponse{}, errors.New("not used")
	})
	_, _, session, registry := prepareProviderCommandTest(t, provider, &commandVerificationRunner{})
	if _, err := registry.Dispatch(session, "change verify", io.Discard); err == nil ||
		!strings.Contains(err.Error(), "no retained proposal") {
		t.Fatalf("verification without proposal error = %v", err)
	}
	if _, err := registry.Dispatch(session, "change verify extra", io.Discard); err == nil ||
		err.Error() != "usage: verify" {
		t.Fatalf("verification arguments error = %v", err)
	}
}

func filepathFromSlash(root string, relative string) string {
	return root + string(os.PathSeparator) + strings.ReplaceAll(relative, "/", string(os.PathSeparator))
}

func assertM06Audit(t *testing.T, dataDirectory string, completed, failed, planningCompleted bool) {
	t.Helper()
	events := readCommandAudit(t, dataDirectory)
	counts := make(map[string]int)
	for _, event := range events {
		counts[event.EventType]++
		metadataText := strings.ToLower(fmt.Sprintf("%v", event.Metadata))
		if strings.Contains(metadataText, "package service") || strings.Contains(metadataText, "deterministic check passed") {
			t.Fatalf("M0.6 audit leaked source or process output: %#v", event.Metadata)
		}
		if _, leaked := event.Metadata["resolved_executable"]; leaked {
			t.Fatalf("M0.6 audit retained an unnecessary host executable path: %#v", event.Metadata)
		}
	}
	if counts[audit.EventVerificationPlanningStarted] != 1 {
		t.Fatalf("planning start audit count = %d", counts[audit.EventVerificationPlanningStarted])
	}
	if planningCompleted {
		if counts[audit.EventVerificationPlanningCompleted] != 1 || counts[audit.EventVerificationPlanningFailed] != 0 {
			t.Fatalf("planning completion audit counts = %#v", counts)
		}
	} else if counts[audit.EventVerificationPlanningFailed] != 1 {
		t.Fatalf("planning failure audit counts = %#v", counts)
	}
	if counts[audit.EventVerificationStarted] != 1 || counts[audit.EventVerificationStepCompleted] < 2 {
		t.Fatalf("verification audit counts = %#v", counts)
	}
	if completed {
		if counts[audit.EventVerificationCompleted] != 1 || counts[audit.EventVerificationFailed] != 0 {
			t.Fatalf("successful verification audit counts = %#v", counts)
		}
	} else if failed {
		if counts[audit.EventVerificationFailed] != 1 || counts[audit.EventVerificationCompleted] != 0 {
			t.Fatalf("failed verification audit counts = %#v", counts)
		}
	}
	var completedEvent audit.Event
	for _, event := range events {
		if event.EventType == audit.EventVerificationCompleted {
			completedEvent = event
		}
		if event.EventType == audit.EventVerificationPlanningCompleted ||
			event.EventType == audit.EventVerificationPlanningFailed {
			if event.Metadata["role"] != string(aiprovider.RoleVerificationPlanning) || event.Metadata["workspace_id"] == nil {
				t.Fatalf("planning audit linkage = %#v", event)
			}
		}
	}
	if completed && (completedEvent.ChangeID == "" || completedEvent.Metadata["evidence_set_id"] == nil ||
		completedEvent.Metadata["patch_digest"] == nil || completedEvent.Metadata["source_state_digest"] == nil ||
		completedEvent.Metadata["passed"] != true) {
		t.Fatalf("verification completion linkage = %#v", completedEvent)
	}
}
