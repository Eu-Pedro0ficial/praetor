package command_test

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Eu-Pedro0ficial/praetor/internal/aiprovider"
	"github.com/Eu-Pedro0ficial/praetor/internal/audit"
	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"github.com/Eu-Pedro0ficial/praetor/internal/command"
	"github.com/Eu-Pedro0ficial/praetor/internal/composition"
	"github.com/Eu-Pedro0ficial/praetor/internal/proposal"
	"github.com/Eu-Pedro0ficial/praetor/internal/source"
	"github.com/Eu-Pedro0ficial/praetor/internal/verification"
)

const commandExecutionAttemptId aiprovider.ExecutionAttemptId = "attempt-abcdef0123456789abcdef0123456789"

type commandFakeProvider struct {
	descriptor aiprovider.ProviderDescriptor
	execute    func(context.Context, aiprovider.ExecutionRequest) (aiprovider.ProviderResponse, error)
}

func newCommandFakeProvider(
	t *testing.T,
	execute func(context.Context, aiprovider.ExecutionRequest) (aiprovider.ProviderResponse, error),
) *commandFakeProvider {
	t.Helper()
	descriptor, err := aiprovider.NewProviderDescriptor(
		"codex-cli",
		"OpenAI",
		"OpenAI Codex CLI",
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
	return &commandFakeProvider{descriptor: descriptor, execute: execute}
}

func (provider *commandFakeProvider) Descriptor() aiprovider.ProviderDescriptor {
	return provider.descriptor
}

func (provider *commandFakeProvider) Execute(
	ctx context.Context,
	request aiprovider.ExecutionRequest,
) (aiprovider.ProviderResponse, error) {
	return provider.execute(ctx, request)
}

func TestProviderCommandsMaintainExplicitSessionSelection(t *testing.T) {
	provider := newCommandFakeProvider(t, func(context.Context, aiprovider.ExecutionRequest) (aiprovider.ProviderResponse, error) {
		t.Fatal("provider selection command executed the provider")
		return aiprovider.ProviderResponse{}, nil
	})
	_, _, session, registry := prepareM05CommandTest(t, provider)
	registrationBefore := session.Registration()
	if _, err := registry.Dispatch(
		session,
		`change new change-provider-selection "Keep selection independent."`,
		io.Discard,
	); err != nil {
		t.Fatalf("create selection test Change: %v", err)
	}
	changeBefore, _ := session.CurrentChange()

	var output bytes.Buffer
	if _, err := registry.Dispatch(session, "provider show", &output); err != nil {
		t.Fatalf("provider show error = %v", err)
	}
	if got := output.String(); !strings.Contains(got, "AI provider adapter: codex-cli") ||
		!strings.Contains(got, "AI provider vendor: OpenAI") ||
		!strings.Contains(got, "AI model: provider default") {
		t.Fatalf("provider show output = %q", got)
	}

	output.Reset()
	if _, err := registry.Dispatch(session, "provider list", &output); err != nil {
		t.Fatalf("provider list error = %v", err)
	}
	if got := output.String(); !strings.Contains(got, "codex-cli (selected)") ||
		!strings.Contains(got, "workspace-mutation") {
		t.Fatalf("provider list output = %q", got)
	}
	if _, err := registry.Dispatch(session, "provider select codex-cli", io.Discard); err != nil {
		t.Fatalf("direct provider select error = %v", err)
	}
	if _, err := registry.Dispatch(session, "provider model direct-model", io.Discard); err != nil {
		t.Fatalf("direct provider model error = %v", err)
	}
	output.Reset()
	if _, err := registry.Dispatch(session, "provider show", &output); err != nil {
		t.Fatalf("direct provider show after selection error = %v", err)
	}
	if !strings.Contains(output.String(), "AI model: direct-model") {
		t.Fatalf("direct provider model output = %q", output.String())
	}

	if _, err := registry.Dispatch(session, "provider", io.Discard); err != nil {
		t.Fatalf("enter provider mode: %v", err)
	}
	if session.CurrentMode().Identity != command.ModeProvider {
		t.Fatalf("provider mode = %#v", session.CurrentMode())
	}
	assertMetadataNames(t, registry.ContextCommands(session), []string{"list", "show", "select", "model", "help", "?", "end"})
	assertSuggestions(t, registry.ContextualHelp(session, ""), []string{"list", "show", "select", "model", "help", "?", "end"})
	assertSuggestions(t, registry.ContextualHelp(session, "select "), []string{"codex-cli"})
	output.Reset()
	if _, err := registry.Dispatch(session, "?", &output); err != nil {
		t.Fatalf("provider ? error = %v", err)
	}
	for _, commandName := range []string{"list", "show", "select", "model", "end", "help"} {
		if !strings.Contains(output.String(), commandName) {
			t.Fatalf("provider ? output %q lacks %q", output.String(), commandName)
		}
	}
	if _, err := registry.Dispatch(session, "list", io.Discard); err != nil {
		t.Fatalf("contextual provider list error = %v", err)
	}
	if _, err := registry.Dispatch(session, "show", io.Discard); err != nil {
		t.Fatalf("contextual provider show error = %v", err)
	}

	output.Reset()
	if _, err := registry.Dispatch(session, "select ?", &output); err != nil {
		t.Fatalf("contextual select help error = %v", err)
	}
	if !strings.Contains(output.String(), "codex-cli") {
		t.Fatalf("select ? output = %q", output.String())
	}
	output.Reset()
	if _, err := registry.Dispatch(session, "model ?", &output); err != nil {
		t.Fatalf("contextual model help error = %v", err)
	}
	if !strings.Contains(output.String(), "No contextual commands") {
		t.Fatalf("model ? unexpectedly enumerated a hard-coded model catalog: %q", output.String())
	}

	output.Reset()
	if _, err := registry.Dispatch(session, "select codex-cli", &output); err != nil {
		t.Fatalf("contextual provider selection error = %v", err)
	}
	if _, err := registry.Dispatch(session, "model model-a", &output); err != nil {
		t.Fatalf("contextual provider model error = %v", err)
	}
	if got := output.String(); !strings.Contains(got, "AI model: model-a") {
		t.Fatalf("provider selection output = %q", got)
	}
	selection := session.ProviderSelection()
	model, hasModel := selection.ModelIdentifier()
	if selection.ProviderIdentifier() != "codex-cli" || !hasModel || model != "model-a" {
		t.Fatalf("active provider selection = %#v", selection)
	}

	if _, err := registry.Dispatch(session, "select unknown-provider", io.Discard); err == nil {
		t.Fatal("unknown manual selection unexpectedly routed or fell back")
	}
	afterFailure := session.ProviderSelection()
	afterModel, afterHasModel := afterFailure.ModelIdentifier()
	if afterFailure.ProviderIdentifier() != "codex-cli" || !afterHasModel || afterModel != "model-a" {
		t.Fatalf("failed selection mutated active selection = %#v", afterFailure)
	}
	if _, err := registry.Dispatch(session, "end", io.Discard); err != nil {
		t.Fatalf("leave provider mode: %v", err)
	}
	if session.CurrentMode().Identity != command.ModeRoot {
		t.Fatalf("mode after provider end = %#v", session.CurrentMode())
	}
	changeAfter, _ := session.CurrentChange()
	if session.Registration() != registrationBefore ||
		changeAfter.ChangeId() != changeBefore.ChangeId() ||
		changeAfter.State() != changeBefore.State() {
		t.Fatalf("provider/model selection mutated Project or Change: registration=%#v change=%#v", session.Registration(), changeAfter)
	}
}

func TestProviderModelSelectionIsSessionLocal(t *testing.T) {
	provider := newCommandFakeProvider(t, func(context.Context, aiprovider.ExecutionRequest) (aiprovider.ProviderResponse, error) {
		t.Fatal("session selection executed provider")
		return aiprovider.ProviderResponse{}, nil
	})
	_, _, firstSession, firstRegistry := prepareM05CommandTest(t, provider)
	_, _, secondSession, _ := prepareM05CommandTest(t, provider)
	if _, err := firstRegistry.Dispatch(firstSession, "provider model first-session-model", io.Discard); err != nil {
		t.Fatalf("first session model selection error = %v", err)
	}
	firstModel, firstSelected := firstSession.ProviderSelection().ModelIdentifier()
	_, secondSelected := secondSession.ProviderSelection().ModelIdentifier()
	if !firstSelected || firstModel != "first-session-model" || secondSelected {
		t.Fatalf("session-local selections = %#v / %#v", firstSession.ProviderSelection(), secondSession.ProviderSelection())
	}
}

func TestChangeImplementDirectAndContextualUseProviderPipeline(t *testing.T) {
	for _, invocation := range []struct {
		name                string
		contextual          bool
		model               string
		externalExecutionId string
	}{
		{name: "direct", contextual: false},
		{name: "change context", contextual: true, model: "model-a", externalExecutionId: "thread-command"},
	} {
		t.Run(invocation.name, func(t *testing.T) {
			provider := newCommandFakeProvider(t, func(_ context.Context, request aiprovider.ExecutionRequest) (aiprovider.ProviderResponse, error) {
				if err := os.WriteFile(
					filepath.Join(request.Workspace().Root(), "internal/service/service.go"),
					[]byte("package service\n\nfunc Greeting() string { return \"hello-praetor\" }\n"),
					0o600,
				); err != nil {
					return aiprovider.ProviderResponse{}, err
				}
				return newCommandProviderResponse(t, request, invocation.externalExecutionId, "bounded-provider-summary"), nil
			})
			repositoryRoot, dataDirectory, session, registry := prepareM05CommandTest(t, provider)
			if invocation.model != "" {
				if _, err := registry.Dispatch(
					session,
					"provider model "+invocation.model,
					io.Discard,
				); err != nil {
					t.Fatalf("provider select error = %v", err)
				}
			}

			isolate := `change isolate change-implement "Change Greeting() to return hello-praetor." --expected internal/service/service.go --possible internal/service/service_test.go --protected go.mod`
			if _, err := registry.Dispatch(session, isolate, io.Discard); err != nil {
				t.Fatalf("change isolate error = %v", err)
			}
			if invocation.contextual {
				if _, err := registry.Dispatch(session, "change", io.Discard); err != nil {
					t.Fatalf("enter change mode: %v", err)
				}
				assertSuggestions(t, registry.ContextualHelp(session, ""), []string{
					"list", "show", "select", "artifacts", "history", "diagnose", "recover", "content", "new", "isolate", "implement", "patch", "verify", "approve", "reject", "apply", "close", "discard", "help", "?", "end",
				})
			}

			var output bytes.Buffer
			line := "change implement"
			if invocation.contextual {
				line = "implement"
			}
			if _, err := registry.Dispatch(session, line, &output); err != nil {
				t.Fatalf("%s error = %v", line, err)
			}
			wantModel := "provider default"
			if invocation.model != "" {
				wantModel = invocation.model
			}
			for _, want := range []string{
				"Execution attempt: " + string(commandExecutionAttemptId),
				"Provider: codex-cli",
				"Model: " + wantModel,
				"Provider outcome: completed",
				"Changed paths: internal/service/service.go",
				"Surface valid: true",
			} {
				if !strings.Contains(output.String(), want) {
					t.Fatalf("implement output %q lacks %q", output.String(), want)
				}
			}
			if invocation.externalExecutionId == "" {
				if strings.Contains(output.String(), "External execution ID:") {
					t.Fatalf("implement output rendered absent external provenance: %q", output.String())
				}
			} else if !strings.Contains(output.String(), "External execution ID: "+invocation.externalExecutionId) {
				t.Fatalf("implement output %q lacks external execution identity", output.String())
			}
			if strings.Contains(output.String(), "bounded-provider-summary") {
				t.Fatalf("implement output leaked provider summary: %q", output.String())
			}

			currentChange, hasChange := session.CurrentChange()
			currentProposal, hasProposal := session.CurrentProposal()
			if !hasChange || currentChange.State() != change.StateIsolated {
				t.Fatalf("successful implementation Change = %#v/%t", currentChange, hasChange)
			}
			if !hasProposal || currentProposal.Workspace().State() != proposal.WorkspaceRetained {
				t.Fatalf("successful implementation proposal = %#v/%t", currentProposal, hasProposal)
			}
			artifact, hasArtifact := currentProposal.PatchArtifact()
			if !hasArtifact || !reflect.DeepEqual(artifact.ChangedPaths(), []string{"internal/service/service.go"}) {
				t.Fatalf("Git-derived patch artifact = %#v/%t", artifact, hasArtifact)
			}
			assertCommandCanonicalSourceUnchanged(t, repositoryRoot)
			assertCommandProviderAudit(t, dataDirectory, true, false, invocation.model)
		})
	}
}

func TestChangeImplementFailureRejectsAndCleansPartialWorkspace(t *testing.T) {
	provider := newCommandFakeProvider(t, func(_ context.Context, request aiprovider.ExecutionRequest) (aiprovider.ProviderResponse, error) {
		if err := os.WriteFile(
			filepath.Join(request.Workspace().Root(), "internal/service/service.go"),
			[]byte("package service\n\nconst Partial = true\n"),
			0o600,
		); err != nil {
			return aiprovider.ProviderResponse{}, err
		}
		return aiprovider.ProviderResponse{}, aiprovider.NewExecutionErrorWithDiagnostic(
			aiprovider.FailureProcess,
			"codex-cli",
			"thread-partial",
			"exit_code=7 stderr=safe-provider-diagnostic",
			errors.New("secret-provider-stderr"),
		)
	})
	repositoryRoot, dataDirectory, session, registry := prepareM05CommandTest(t, provider)
	if _, err := registry.Dispatch(
		session,
		`change isolate change-partial "Exercise partial failure." --expected internal/service/service.go --protected go.mod`,
		io.Discard,
	); err != nil {
		t.Fatalf("change isolate error = %v", err)
	}

	if _, err := registry.Dispatch(session, "change implement", io.Discard); err == nil ||
		aiprovider.FailureKindOf(err) != aiprovider.FailureProcess {
		t.Fatalf("change implement partial failure = %v", err)
	}
	currentChange, hasChange := session.CurrentChange()
	if !hasChange || currentChange.State() != change.StateRejected {
		t.Fatalf("failed implementation Change = %#v/%t", currentChange, hasChange)
	}
	if _, hasProposal := session.CurrentProposal(); hasProposal {
		t.Fatal("failed implementation retained a process-owned workspace")
	}
	assertCommandCanonicalSourceUnchanged(t, repositoryRoot)
	assertCommandProviderAudit(t, dataDirectory, false, true, "")
	var discardReason string
	for _, event := range readCommandAudit(t, dataDirectory) {
		if event.EventType == audit.EventProposalWorkspaceDiscarded {
			discardReason, _ = event.Metadata["reason"].(string)
		}
	}
	if discardReason != "provider execution failed: kind=process-failure" {
		t.Fatalf("provider failure discard reason = %q", discardReason)
	}
}

func prepareM05CommandTest(
	t *testing.T,
	provider aiprovider.Provider,
) (string, string, *command.Session, command.Registry) {
	return prepareProviderCommandTest(t, provider, nil)
}

func prepareProviderCommandTest(
	t *testing.T,
	provider aiprovider.Provider,
	verificationRunner verification.StepRunner,
) (string, string, *command.Session, command.Registry) {
	return prepareProviderCommandTestWithContainer(t, provider, verificationRunner, nil)
}

func prepareProviderCommandTestWithContainer(
	t *testing.T,
	provider aiprovider.Provider,
	verificationRunner verification.StepRunner,
	configure func(*composition.Container),
) (string, string, *command.Session, command.Registry) {
	t.Helper()
	repositoryRoot := t.TempDir()
	runCommandGit(t, repositoryRoot, "init", "--quiet")
	files := map[string]string{
		"cmd/app/main.go":                   "package main\n",
		"internal/service/service.go":       "package service\n\nfunc Greeting() string { return \"hello\" }\n",
		"internal/service/service_test.go":  "package service_test\n",
		"go.mod":                            "module example.invalid/fixture\n\ngo 1.25.1\n",
		"pyproject.toml":                    "[tool.pytest.ini_options]\n",
		"README.md":                         "# Fixture\n",
		"engineering/policies/praetor.yaml": "schema_version: 1\nbundle:\n  id: fixture-policy\n  version: \"1.0\"\n  policies:\n    - id: test-required\n      version: \"1.0\"\n      family: testing\n      description: Require passing tests.\n      severity: HIGH\n      outcome: APPROVAL\n      required_evidence: test\n      non_overridable: true\n      exception_candidate_allowed: true\n    - id: patch-integrity\n      version: \"1.0\"\n      family: change-surface\n      description: Require patch integrity.\n      severity: CRITICAL\n      outcome: AUTO\n      required_evidence: patch-integrity\n      non_overridable: true\n      exception_candidate_allowed: false\n",
	}
	for relativePath, contents := range files {
		writeCommandFile(t, repositoryRoot, relativePath, contents)
	}
	runCommandGit(t, repositoryRoot, "add", ".")
	runCommandGit(
		t,
		repositoryRoot,
		"-c", "user.name=Praetor Test",
		"-c", "user.email=praetor@example.invalid",
		"commit", "--quiet", "-m", "baseline",
	)
	t.Chdir(repositoryRoot)
	xdgDataHome := filepath.Join(t.TempDir(), "xdg")
	t.Setenv("XDG_DATA_HOME", xdgDataHome)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(t.TempDir(), "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	container := composition.New()
	container.AIProviders = []aiprovider.Provider{provider}
	container.ConfiguredProvider = "codex-cli"
	container.ConfiguredModel = ""
	executionAttemptIndex := 0
	container.ExecutionAttemptIds = func() (aiprovider.ExecutionAttemptId, error) {
		identities := []aiprovider.ExecutionAttemptId{
			commandExecutionAttemptId,
			"attempt-fedcba9876543210fedcba9876543210",
		}
		if executionAttemptIndex < len(identities) {
			identity := identities[executionAttemptIndex]
			executionAttemptIndex++
			return identity, nil
		}
		return aiprovider.GenerateExecutionAttemptId()
	}
	nextTime := time.Date(2026, time.September, 1, 15, 0, 0, 0, time.UTC)
	container.ExecutionClock = func() time.Time {
		nextTime = nextTime.Add(time.Second)
		return nextTime
	}
	if verificationRunner != nil {
		container.VerificationRunner = verificationRunner
	}
	container.VerificationAttemptIds = func() (verification.VerificationAttemptId, error) {
		return "verification-abcdef0123456789abcdef0123456789", nil
	}
	if configure != nil {
		configure(&container)
	}
	session, err := container.NewInteractiveSession(".")
	if err != nil {
		t.Fatalf("NewInteractiveSession() error = %v", err)
	}
	t.Cleanup(func() {
		if err := session.Close(); err != nil {
			t.Errorf("Session.Close() error = %v", err)
		}
	})
	return repositoryRoot, filepath.Join(xdgDataHome, "praetor"), session, newTestRegistry(t)
}

func newCommandProviderResponse(
	t *testing.T,
	request aiprovider.ExecutionRequest,
	externalId string,
	summary string,
) aiprovider.ProviderResponse {
	t.Helper()
	startedAt := time.Date(2026, time.September, 1, 15, 0, 2, 0, time.UTC)
	response, err := aiprovider.NewProviderResponse(
		request.AttemptId(),
		request.Selection(),
		"codex-cli test-1.0",
		externalId,
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

func assertCommandCanonicalSourceUnchanged(t *testing.T, repositoryRoot string) {
	t.Helper()
	want := "package service\n\nfunc Greeting() string { return \"hello\" }\n"
	contents, err := os.ReadFile(filepath.Join(repositoryRoot, "internal/service/service.go"))
	if err != nil || string(contents) != want {
		t.Fatalf("canonical source = %q/%v, want unchanged", contents, err)
	}
	if status := runCommandGitOutput(t, repositoryRoot, "status", "--porcelain=v1", "--untracked-files=all"); len(status) != 0 {
		t.Fatalf("canonical Git status = %q", status)
	}
	if _, err := os.Stat(filepath.Join(repositoryRoot, ".praetor")); !os.IsNotExist(err) {
		t.Fatalf("runtime metadata appeared in canonical source: %v", err)
	}
}

func assertCommandProviderAudit(
	t *testing.T,
	dataDirectory string,
	completed bool,
	failed bool,
	wantModel string,
) {
	t.Helper()
	events := readCommandAudit(t, dataDirectory)
	var providerEvents []audit.Event
	var executionPatchEvents []audit.Event
	for _, event := range events {
		switch event.EventType {
		case audit.EventProviderExecutionStarted,
			audit.EventProviderExecutionCompleted,
			audit.EventProviderExecutionFailed:
			providerEvents = append(providerEvents, event)
		case audit.EventPatchExtracted,
			audit.EventPatchSurfaceValidated,
			audit.EventPatchRejected:
			if event.Metadata["execution_attempt_id"] != nil {
				executionPatchEvents = append(executionPatchEvents, event)
			}
		}
	}
	wantTypes := []string{audit.EventProviderExecutionStarted}
	if completed {
		wantTypes = append(wantTypes, audit.EventProviderExecutionCompleted)
	}
	if failed {
		wantTypes = append(wantTypes, audit.EventProviderExecutionFailed)
	}
	if len(providerEvents) != len(wantTypes) {
		t.Fatalf("provider audit events = %#v, want %v", providerEvents, wantTypes)
	}
	if completed {
		if len(executionPatchEvents) != 2 {
			t.Fatalf("attempt-linked patch audit events = %#v", executionPatchEvents)
		}
		for _, event := range executionPatchEvents {
			if event.Metadata["execution_attempt_id"] != string(commandExecutionAttemptId) {
				t.Fatalf("patch audit attempt linkage = %#v", event)
			}
		}
	} else if len(executionPatchEvents) != 0 {
		t.Fatalf("failed provider emitted patch success audit = %#v", executionPatchEvents)
	}
	for index, event := range providerEvents {
		if event.EventType != wantTypes[index] || event.ChangeID == "" ||
			event.Metadata["execution_attempt_id"] != string(commandExecutionAttemptId) ||
			event.Metadata["workspace_id"] == "" ||
			event.Metadata["provider"] != "codex-cli" ||
			event.Metadata["provider_vendor"] != "OpenAI" ||
			event.Metadata["role"] != string(aiprovider.RoleImplementation) {
			t.Fatalf("provider audit linkage %d = %#v", index, event)
		}
		if wantModel == "" {
			if _, hasModel := event.Metadata["model"]; hasModel {
				t.Fatalf("provider-default audit unexpectedly has model = %#v", event.Metadata)
			}
		} else if event.Metadata["model"] != wantModel {
			t.Fatalf("provider audit model = %#v, want %q", event.Metadata["model"], wantModel)
		}
		if event.EventType == audit.EventProviderExecutionCompleted {
			if event.Metadata["provider_version"] != "codex-cli test-1.0" ||
				event.Metadata["provider_summary"] != "bounded-provider-summary" {
				t.Fatalf("provider completion metadata = %#v", event.Metadata)
			}
		}
		metadata := fmt.Sprintf("%v", event.Metadata)
		if strings.Contains(metadata, "secret-provider-stderr") ||
			strings.Contains(metadata, "package service") {
			t.Fatalf("provider audit leaked unbounded provider/source data: %#v", event.Metadata)
		}
	}
	if failed {
		failure := providerEvents[len(providerEvents)-1]
		if failure.Metadata["failure_kind"] != string(aiprovider.FailureProcess) ||
			failure.Metadata["external_execution_id"] != "thread-partial" ||
			failure.Metadata["provider_diagnostic"] != "exit_code=7 stderr=safe-provider-diagnostic" ||
			failure.Metadata["workspace_may_be_changed"] != true {
			t.Fatalf("provider failure audit = %#v", failure.Metadata)
		}
		paths, ok := failure.Metadata["changed_paths"].([]any)
		if !ok || len(paths) != 1 || paths[0] != "internal/service/service.go" {
			t.Fatalf("provider failure changed paths = %#v", failure.Metadata["changed_paths"])
		}
	}
}

func TestChangeImplementCompletedProviderWithoutPatchRetainsDiagnosticsThenCleans(t *testing.T) {
	providerExplanation := "I inspected the approved files but produced no source changes. " + strings.Repeat("detail ", 800)
	retainedExplanation := providerExplanation[:aiprovider.MaximumProviderSummaryBytes]
	var workspaceRoot string
	provider := newCommandFakeProvider(t, func(_ context.Context, request aiprovider.ExecutionRequest) (aiprovider.ProviderResponse, error) {
		workspaceRoot = request.Workspace().Root()
		usage, err := aiprovider.NewProviderUsage(120, 20, 35, 8)
		if err != nil {
			return aiprovider.ProviderResponse{}, err
		}
		startedAt := time.Date(2026, time.September, 1, 15, 0, 2, 0, time.UTC)
		return aiprovider.NewProviderResponse(
			request.AttemptId(),
			request.Selection(),
			"codex-cli test-1.0",
			"thread-no-patch",
			providerExplanation,
			false,
			usage,
			startedAt,
			startedAt.Add(time.Second),
		)
	})
	repositoryRoot, dataDirectory, session, registry := prepareM05CommandTest(t, provider)
	if _, err := registry.Dispatch(
		session,
		`change isolate change-no-patch "Change Greeting without guessing." --expected internal/service/service.go --possible internal/service/service_test.go --protected go.mod`,
		io.Discard,
	); err != nil {
		t.Fatalf("change isolate error = %v", err)
	}

	var output bytes.Buffer
	_, implementationError := registry.Dispatch(session, "change implement", &output)
	var emptyPatch proposal.EmptyPatchError
	if !errors.As(implementationError, &emptyPatch) {
		t.Fatalf("change implement error = %T %v, want EmptyPatchError", implementationError, implementationError)
	}
	for _, expected := range []string{
		"Provider execution completed; implementation did not succeed.",
		"Execution attempt: " + string(commandExecutionAttemptId),
		"Provider: codex-cli",
		"Model: provider default",
		"Provider outcome: completed (protocol completion only)",
		"Token usage: input=120 cached_input=20 output=35 reasoning_output=8",
		"Provider summary (bounded, untrusted, truncated=true): " + retainedExplanation,
		"Git-visible changes: 0",
		"Implementation result: rejected; no patch was produced.",
		"Rejection reason: " + emptyPatch.Error(),
	} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("no-patch output %q lacks %q", output.String(), expected)
		}
	}
	currentChange, hasChange := session.CurrentChange()
	if !hasChange || currentChange.State() != change.StateRejected {
		t.Fatalf("no-patch Change = %#v/%t", currentChange, hasChange)
	}
	if _, hasProposal := session.CurrentProposal(); hasProposal {
		t.Fatal("no-patch implementation retained a process-owned proposal")
	}
	if workspaceRoot == "" {
		t.Fatal("provider did not receive a workspace")
	}
	if _, err := os.Stat(workspaceRoot); !os.IsNotExist(err) {
		t.Fatalf("discarded workspace still exists: %v", err)
	}
	assertCommandCanonicalSourceUnchanged(t, repositoryRoot)

	events := readCommandAudit(t, dataDirectory)
	completedIndex := eventIndex(events, audit.EventProviderExecutionCompleted)
	rejectedIndex := eventIndex(events, audit.EventPatchRejected)
	transitionIndex := -1
	discardedIndex := -1
	for index := rejectedIndex + 1; index < len(events); index++ {
		if transitionIndex < 0 && events[index].EventType == audit.EventChangeTransition {
			transitionIndex = index
		}
		if events[index].EventType == audit.EventProposalWorkspaceDiscarded {
			discardedIndex = index
			break
		}
	}
	if !(completedIndex >= 0 && completedIndex < rejectedIndex && rejectedIndex < transitionIndex && transitionIndex < discardedIndex) {
		t.Fatalf("no-patch audit ordering completed=%d rejected=%d transition=%d discarded=%d", completedIndex, rejectedIndex, transitionIndex, discardedIndex)
	}
	completed := events[completedIndex]
	if completed.Metadata["provider_summary"] != retainedExplanation ||
		completed.Metadata["summary_present"] != true ||
		completed.Metadata["summary_truncated"] != true {
		t.Fatalf("completed provider diagnostics = %#v", completed.Metadata)
	}
	rejected := events[rejectedIndex]
	if rejected.Metadata["reason"] != emptyPatch.Error() ||
		fmt.Sprint(rejected.Metadata["approved_expected_paths"]) == "[]" ||
		fmt.Sprint(rejected.Metadata["approved_possible_paths"]) == "[]" ||
		fmt.Sprint(rejected.Metadata["approved_protected_paths"]) == "[]" ||
		fmt.Sprint(rejected.Metadata["actual_expected_changes"]) != "[]" ||
		fmt.Sprint(rejected.Metadata["actual_possible_changes"]) != "[]" {
		t.Fatalf("no-patch scope diagnostics = %#v", rejected.Metadata)
	}
	if events[transitionIndex].Metadata["context"] != emptyPatch.Error() ||
		events[discardedIndex].Metadata["reason"] != emptyPatch.Error() {
		t.Fatalf("precise cleanup reasons transition=%#v discard=%#v", events[transitionIndex].Metadata, events[discardedIndex].Metadata)
	}
}

func TestChangeImplementScopeViolationIsDistinctFromNoPatch(t *testing.T) {
	provider := newCommandFakeProvider(t, func(_ context.Context, request aiprovider.ExecutionRequest) (aiprovider.ProviderResponse, error) {
		if err := os.WriteFile(filepath.Join(request.Workspace().Root(), "go.mod"), []byte("module forbidden.invalid/change\n"), 0o600); err != nil {
			return aiprovider.ProviderResponse{}, err
		}
		return newCommandProviderResponse(t, request, "thread-scope", "I changed a protected file."), nil
	})
	repositoryRoot, dataDirectory, session, registry := prepareM05CommandTest(t, provider)
	if _, err := registry.Dispatch(session, `change isolate change-scope-diagnostic "Exercise scope rejection." --expected internal/service/service.go --protected go.mod`, io.Discard); err != nil {
		t.Fatalf("change isolate error = %v", err)
	}
	var output bytes.Buffer
	_, implementationError := registry.Dispatch(session, "change implement", &output)
	var surfaceError *source.SurfaceValidationError
	if !errors.As(implementationError, &surfaceError) {
		t.Fatalf("change implement error = %T %v, want SurfaceValidationError", implementationError, implementationError)
	}
	if strings.Contains(output.String(), "no patch") || strings.Contains(output.String(), "Git-visible changes: 0") {
		t.Fatalf("scope violation was presented as no-patch: %q", output.String())
	}
	if current, ok := session.CurrentChange(); !ok || current.State() != change.StateRejected {
		t.Fatalf("scope-violating Change = %#v/%t", current, ok)
	}
	if _, ok := session.CurrentProposal(); ok {
		t.Fatal("scope-violating proposal was not discarded")
	}
	assertCommandCanonicalSourceUnchanged(t, repositoryRoot)
	wantReason := "provider produced a patch outside ApprovedScope: " + surfaceError.Error()
	var rejected, discarded audit.Event
	for _, event := range readCommandAudit(t, dataDirectory) {
		switch event.EventType {
		case audit.EventPatchRejected:
			rejected = event
		case audit.EventProposalWorkspaceDiscarded:
			discarded = event
		}
	}
	if rejected.Metadata["reason"] != surfaceError.Error() || discarded.Metadata["reason"] != wantReason {
		t.Fatalf("scope rejection reasons rejected=%#v discarded=%#v", rejected.Metadata, discarded.Metadata)
	}
}

func TestChangeImplementUsesOnlyCurrentBoundedImpactReportContext(t *testing.T) {
	var receivedContext aiprovider.ImplementationContext
	provider := newCommandFakeProvider(t, func(_ context.Context, request aiprovider.ExecutionRequest) (aiprovider.ProviderResponse, error) {
		receivedContext = request.ImplementationContext()
		if err := os.WriteFile(filepath.Join(request.Workspace().Root(), "internal/service/service.go"), []byte("package service\n\nfunc Greeting() string { return \"hello-context\" }\n"), 0o600); err != nil {
			return aiprovider.ProviderResponse{}, err
		}
		return newCommandProviderResponse(t, request, "thread-context", "Implemented using bounded impact evidence."), nil
	})
	_, _, session, registry := prepareM05CommandTest(t, provider)
	const intent = "Change Greeting using repository impact evidence."
	if _, err := registry.Dispatch(session, `change new change-context "`+intent+`"`, io.Discard); err != nil {
		t.Fatalf("change new error = %v", err)
	}
	if _, err := registry.Dispatch(session, "analysis report change-context --expected internal/service/service.go --possible internal/service/service_test.go --protected go.mod", io.Discard); err != nil {
		t.Fatalf("analysis report error = %v", err)
	}
	if _, err := registry.Dispatch(session, `change isolate change-context "`+intent+`" --expected internal/service/service.go --possible internal/service/service_test.go --protected go.mod`, io.Discard); err != nil {
		t.Fatalf("change isolate error = %v", err)
	}
	if _, err := registry.Dispatch(session, "change implement", io.Discard); err != nil {
		t.Fatalf("change implement error = %v", err)
	}
	if !receivedContext.Available() || !strings.Contains(receivedContext.Source(), "current ImpactReport") || len(receivedContext.Entries()) == 0 {
		t.Fatalf("provider implementation context = source %q entries %#v", receivedContext.Source(), receivedContext.Entries())
	}
	if len(receivedContext.Entries()) > aiprovider.MaximumImplementationContextEntries {
		t.Fatalf("provider implementation context entries = %d", len(receivedContext.Entries()))
	}
}
