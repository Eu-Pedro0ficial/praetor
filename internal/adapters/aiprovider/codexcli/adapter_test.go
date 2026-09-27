package codexcli

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/Eu-Pedro0ficial/praetor/internal/aiprovider"
	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"github.com/Eu-Pedro0ficial/praetor/internal/project"
	"github.com/Eu-Pedro0ficial/praetor/internal/proposal"
	"github.com/Eu-Pedro0ficial/praetor/internal/source"
)

const adapterTestProjectId project.ProjectId = "01890c29-7a78-7abc-8def-0123456789ab"

type runnerFunction func(context.Context, processInvocation) (processResult, error)

func (function runnerFunction) Run(ctx context.Context, invocation processInvocation) (processResult, error) {
	return function(ctx, invocation)
}

func withSuccessfulPreflight(execution runnerFunction) runnerFunction {
	return func(ctx context.Context, invocation processInvocation) (processResult, error) {
		arguments := strings.Join(invocation.arguments, " ")
		switch arguments {
		case "--version":
			return processResult{standardOutput: []byte("codex-cli 1.2.3\n")}, nil
		case "exec --help":
			return processResult{standardOutput: []byte(strings.Join([]string{
				"--model",
				"--sandbox workspace-write read-only",
				"--cd",
				"--ephemeral",
				"--ignore-user-config",
				"--approve-for-me",
				"--color",
				"--json",
			}, "\n"))}, nil
		default:
			return execution(ctx, invocation)
		}
	}
}

func TestShapeImplementationRequestExplainsRepositoryWideAuthorization(t *testing.T) {
	request, _ := adapterRequestFixture(t, "", false, source.ScopeRequest{
		AuthorizationMode: source.AuthorizationRepositoryWide,
		Protected:         []string{"go.mod"},
	})
	prompt, _, err := serializeImplementationRequest(request)
	if err != nil {
		t.Fatalf("serializeImplementationRequest() error = %v", err)
	}
	for _, expected := range []string{
		"Mode: repository-wide",
		"Repository scope: .",
		"Protected paths:\n- go.mod",
		"Write constraint: any repository-relative path is authorized except protected paths and their descendants.",
	} {
		if !strings.Contains(prompt, expected) {
			t.Fatalf("repository-wide prompt lacks %q:\n%s", expected, prompt)
		}
	}
	if strings.Contains(prompt, "only expected or possible paths are authorized") {
		t.Fatalf("repository-wide prompt retained strict-only instruction:\n%s", prompt)
	}
}

func TestAdapterShapesCodexExecRequestAndNormalizesResponse(t *testing.T) {
	request, canonicalRoot := adapterTestRequest(t, "gpt-test-1")
	var captured processInvocation
	runner := runnerFunction(func(_ context.Context, invocation processInvocation) (processResult, error) {
		captured = invocation
		return processResult{standardOutput: []byte(strings.Join([]string{
			`{"type":"thread.started","thread_id":"thread-123"}`,
			`{"type":"turn.started"}`,
			`{"type":"item.completed","item":{"id":"item-1","type":"agent_message","text":"Implemented greeting."}}`,
			`{"type":"turn.completed","usage":{"input_tokens":100,"cached_input_tokens":20,"output_tokens":30,"reasoning_output_tokens":4}}`,
		}, "\n") + "\n")}, nil
	})
	times := []time.Time{
		time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC),
		time.Date(2026, time.September, 1, 12, 0, 2, 0, time.UTC),
	}
	clockIndex := 0
	adapter, err := newWithRunner(Config{
		Binary:  "/opt/codex",
		Timeout: time.Minute,
		Clock: func() time.Time {
			value := times[clockIndex]
			clockIndex++
			return value
		},
	}, withSuccessfulPreflight(runner))
	if err != nil {
		t.Fatalf("newWithRunner() error = %v", err)
	}
	if adapter.Descriptor().Identifier() != Identifier || adapter.Descriptor().Vendor() != "OpenAI" {
		t.Fatalf("adapter identity = %#v", adapter.Descriptor())
	}

	response, err := adapter.Execute(context.Background(), request)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	wantArguments := []string{
		"exec",
		"--ephemeral",
		"--ignore-user-config",
		"--approve-for-me",
		"--json",
		"--color", "never",
		"--cd", request.Workspace().Root(),
		"--model", "gpt-test-1",
		"-",
	}
	if captured.binary != "/opt/codex" || !reflect.DeepEqual(captured.arguments, wantArguments) {
		t.Fatalf("process invocation = %q %#v", captured.binary, captured.arguments)
	}
	if captured.directory != request.Workspace().Root() {
		t.Fatalf("process directory = %q, want ProposalWorkspace %q", captured.directory, request.Workspace().Root())
	}
	for _, expected := range []string{
		"TASK INTENT\nChange Greeting() to return hello-praetor.",
		"Expected paths:\n- service.go",
		"Possible paths:\n- service_test.go",
		"Protected paths:\n- go.mod",
		"ADVISORY IMPACT CONTEXT",
		"Advisory only; it does not authorize writes.",
		"impact=EXPECTED kind=file location=service.go basis=manifest confidence=HIGH",
		"Inspect the isolated ProposalWorkspace at the current working directory.",
	} {
		if !strings.Contains(captured.standardInput, expected) {
			t.Fatalf("shaped request lacks %q:\n%s", expected, captured.standardInput)
		}
	}
	if strings.Contains(captured.standardInput, canonicalRoot) || strings.Contains(strings.Join(captured.arguments, " "), canonicalRoot) {
		t.Fatalf("canonical source path leaked into provider request: %q", captured.standardInput)
	}
	if response.ExternalExecutionId() != "thread-123" || response.Summary() != "Implemented greeting." {
		t.Fatalf("normalized response = %#v", response)
	}
	if response.ProviderVersion() != "codex-cli 1.2.3" {
		t.Fatalf("provider version = %q", response.ProviderVersion())
	}
	if !response.Usage().Available() || response.Usage().InputTokens() != 100 || response.Usage().ReasoningOutputTokens() != 4 {
		t.Fatalf("normalized usage = %#v", response.Usage())
	}
	if response.CompletedAt().Sub(response.StartedAt()) != 2*time.Second {
		t.Fatalf("response duration = %s", response.CompletedAt().Sub(response.StartedAt()))
	}
}

func TestAdapterOmitsModelFlagForProviderDefault(t *testing.T) {
	request, _ := adapterTestRequest(t, "")
	var captured processInvocation
	adapter, err := newWithRunner(Config{}, withSuccessfulPreflight(runnerFunction(func(_ context.Context, invocation processInvocation) (processResult, error) {
		captured = invocation
		return successfulProcessOutput("thread-default"), nil
	})))
	if err != nil {
		t.Fatalf("newWithRunner() error = %v", err)
	}
	if _, err := adapter.Execute(context.Background(), request); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if strings.Contains(strings.Join(captured.arguments, " "), "--model") {
		t.Fatalf("provider-default invocation hard-coded a model: %#v", captured.arguments)
	}
}

func TestAdapterShapesReadOnlyVerificationPlanningRequest(t *testing.T) {
	request, canonicalRoot := adapterPlanningTestRequest(t, "gpt-planner")
	var captured processInvocation
	output := strings.Join([]string{
		`{"type":"thread.started","thread_id":"thread-planner"}`,
		`{"type":"item.completed","item":{"type":"agent_message","text":"{\"candidates\":[{\"kind\":\"test\",\"executable\":\"go\",\"arguments\":[\"test\",\"./...\"],\"working_directory\":\".\",\"supporting_evidence\":[\"go.mod\"]}]}"}}`,
		`{"type":"turn.completed"}`,
	}, "\n") + "\n"
	adapter, err := newWithRunner(Config{}, withSuccessfulPreflight(runnerFunction(func(_ context.Context, invocation processInvocation) (processResult, error) {
		captured = invocation
		return processResult{standardOutput: []byte(output)}, nil
	})))
	if err != nil {
		t.Fatalf("newWithRunner() error = %v", err)
	}
	response, err := adapter.Execute(context.Background(), request)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	arguments := strings.Join(captured.arguments, " ")
	if !strings.Contains(arguments, "--sandbox read-only") ||
		strings.Contains(arguments, "--sandbox workspace-write") ||
		strings.Contains(arguments, "--approve-for-me") {
		t.Fatalf("verification-planning arguments = %q", captured.arguments)
	}
	for _, expected := range []string{
		"read-only verification-planning agent",
		"Do not modify any file",
		"Return exactly one JSON object",
		"Patch digest:",
		"Changed paths:\n- service.go",
		"evidence path=go.mod kind=manifest",
	} {
		if !strings.Contains(captured.standardInput, expected) {
			t.Fatalf("planner request lacks %q:\n%s", expected, captured.standardInput)
		}
	}
	if strings.Contains(captured.standardInput, canonicalRoot) || strings.Contains(arguments, canonicalRoot) {
		t.Fatalf("planner request leaked canonical source path")
	}
	if response.ExternalExecutionId() != "thread-planner" || !strings.HasPrefix(response.Summary(), `{"candidates"`) {
		t.Fatalf("planner response = %#v", response)
	}
}

func TestAdapterNormalizesFailuresWithoutExposingRawOutput(t *testing.T) {
	request, _ := adapterTestRequest(t, "")
	tests := []struct {
		name     string
		result   processResult
		runError error
		wantKind aiprovider.FailureKind
	}{
		{
			name: "authentication",
			result: processResult{
				standardOutput: []byte(`{"type":"thread.started","thread_id":"thread-auth"}` + "\n" + `{"type":"error","message":"authentication required SECRET-TOKEN"}` + "\n"),
				standardError:  []byte("login required SECRET-TOKEN"),
			},
			runError: errors.New("exit status 1"),
			wantKind: aiprovider.FailureAuthentication,
		},
		{
			name:     "unavailable",
			runError: exec.ErrNotFound,
			wantKind: aiprovider.FailureUnavailable,
		},
		{
			name: "rate limited",
			result: processResult{
				standardOutput: []byte(`{"type":"thread.started","thread_id":"thread-rate"}` + "\n" + `{"type":"turn.failed","message":"HTTP 429 rate limit"}` + "\n"),
			},
			wantKind: aiprovider.FailureRateLimited,
		},
		{
			name: "transport",
			result: processResult{
				standardOutput: []byte(`{"type":"thread.started","thread_id":"thread-network"}` + "\n" + `{"type":"turn.failed","message":"network connection failed"}` + "\n"),
			},
			wantKind: aiprovider.FailureTransport,
		},
		{
			name: "refusal",
			result: processResult{
				standardOutput: []byte(`{"type":"thread.started","thread_id":"thread-refusal"}` + "\n" + `{"type":"turn.failed","message":"provider refusal"}` + "\n"),
			},
			wantKind: aiprovider.FailureRefused,
		},
		{
			name: "bounded output",
			result: processResult{
				standardOutput: []byte(`{"type":"thread.started","thread_id":"thread-large"}` + "\n"),
				outputExceeded: true,
			},
			wantKind: aiprovider.FailureMalformedOutput,
		},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			adapter, err := newWithRunner(Config{}, withSuccessfulPreflight(runnerFunction(func(context.Context, processInvocation) (processResult, error) {
				return test.result, test.runError
			})))
			if err != nil {
				t.Fatalf("newWithRunner() error = %v", err)
			}
			_, executionError := adapter.Execute(context.Background(), request)
			var normalized *aiprovider.ExecutionError
			if !errors.As(executionError, &normalized) || normalized.Kind() != test.wantKind {
				t.Fatalf("Execute() error = %T %v, want %s", executionError, executionError, test.wantKind)
			}
			if strings.Contains(executionError.Error(), "SECRET-TOKEN") {
				t.Fatalf("normalized error leaked provider output: %v", executionError)
			}
		})
	}
}

func TestAdapterBoundsUntrustedTextualSummary(t *testing.T) {
	request, _ := adapterTestRequest(t, "")
	largeSummary := "Authorization: Bearer SECRET-TOKEN " + strings.Repeat("a", maximumSummaryBytes+100)
	output := strings.Join([]string{
		`{"type":"thread.started","thread_id":"thread-summary"}`,
		`{"type":"item.completed","item":{"type":"agent_message","text":` + strconv.Quote(largeSummary) + `}}`,
		`{"type":"turn.completed"}`,
	}, "\n") + "\n"
	adapter, err := newWithRunner(Config{}, withSuccessfulPreflight(runnerFunction(func(context.Context, processInvocation) (processResult, error) {
		return processResult{standardOutput: []byte(output)}, nil
	})))
	if err != nil {
		t.Fatalf("newWithRunner() error = %v", err)
	}
	response, err := adapter.Execute(context.Background(), request)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if !response.SummaryTruncated() || len(response.Summary()) != maximumSummaryBytes {
		t.Fatalf("bounded summary length/truncation = %d/%t", len(response.Summary()), response.SummaryTruncated())
	}
	if strings.Contains(response.Summary(), "SECRET-TOKEN") || !strings.Contains(response.Summary(), "[REDACTED]") {
		t.Fatalf("provider summary was not sanitized: %q", response.Summary())
	}
}

func TestAdapterRejectsMalformedOrIncompleteJSONL(t *testing.T) {
	request, _ := adapterTestRequest(t, "")
	outputs := [][]byte{
		[]byte("not-json\n"),
		[]byte(`{"type":"thread.started","thread_id":"thread-incomplete"}` + "\n"),
		[]byte(`{"type":"turn.completed"}` + "\n"),
		[]byte(`{"thread_id":"missing-type"}` + "\n"),
	}
	for index, output := range outputs {
		t.Run(string(rune('a'+index)), func(t *testing.T) {
			adapter, err := newWithRunner(Config{}, withSuccessfulPreflight(runnerFunction(func(context.Context, processInvocation) (processResult, error) {
				return processResult{standardOutput: output}, nil
			})))
			if err != nil {
				t.Fatalf("newWithRunner() error = %v", err)
			}
			_, executionError := adapter.Execute(context.Background(), request)
			var normalized *aiprovider.ExecutionError
			if !errors.As(executionError, &normalized) || normalized.Kind() != aiprovider.FailureMalformedOutput {
				t.Fatalf("Execute() error = %T %v", executionError, executionError)
			}
		})
	}
}

func TestAdapterPreflightIsBoundedAndCapabilityOriented(t *testing.T) {
	request, _ := adapterTestRequest(t, "")
	tests := []struct {
		name     string
		runner   runnerFunction
		wantKind aiprovider.FailureKind
	}{
		{
			name: "executable missing",
			runner: func(context.Context, processInvocation) (processResult, error) {
				return processResult{}, exec.ErrNotFound
			},
			wantKind: aiprovider.FailureUnavailable,
		},
		{
			name: "version invocation failure",
			runner: func(context.Context, processInvocation) (processResult, error) {
				return processResult{standardError: []byte("SECRET-HOME-PATH")}, errors.New("exit status 1")
			},
			wantKind: aiprovider.FailureProcess,
		},
		{
			name: "invalid version output",
			runner: func(context.Context, processInvocation) (processResult, error) {
				return processResult{standardOutput: []byte("codex 1\nunbounded detail")}, nil
			},
			wantKind: aiprovider.FailureUnsupported,
		},
		{
			name: "unsupported capability",
			runner: func(_ context.Context, invocation processInvocation) (processResult, error) {
				if reflect.DeepEqual(invocation.arguments, []string{"--version"}) {
					return processResult{standardOutput: []byte("codex-cli 1.0\n")}, nil
				}
				return processResult{standardOutput: []byte("--json only")}, nil
			},
			wantKind: aiprovider.FailureUnsupported,
		},
		{
			name: "bounded help output",
			runner: func(_ context.Context, invocation processInvocation) (processResult, error) {
				if reflect.DeepEqual(invocation.arguments, []string{"--version"}) {
					return processResult{standardOutput: []byte("codex-cli 1.0\n")}, nil
				}
				return processResult{outputExceeded: true}, nil
			},
			wantKind: aiprovider.FailureUnsupported,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			adapter, err := newWithRunner(Config{}, test.runner)
			if err != nil {
				t.Fatalf("newWithRunner() error = %v", err)
			}
			_, executionError := adapter.Execute(context.Background(), request)
			var normalized *aiprovider.ExecutionError
			if !errors.As(executionError, &normalized) || normalized.Kind() != test.wantKind {
				t.Fatalf("Execute() error = %T %v, want %s", executionError, executionError, test.wantKind)
			}
			if strings.Contains(executionError.Error(), "SECRET-HOME-PATH") {
				t.Fatalf("preflight error leaked raw process output: %v", executionError)
			}
		})
	}
}

func TestAdapterPreflightUsesOnlyBoundedLocalProcessProbes(t *testing.T) {
	request, _ := adapterTestRequest(t, "")
	var invocations []processInvocation
	runner := runnerFunction(func(ctx context.Context, invocation processInvocation) (processResult, error) {
		invocations = append(invocations, invocation)
		return withSuccessfulPreflight(func(context.Context, processInvocation) (processResult, error) {
			return successfulProcessOutput("thread-preflight"), nil
		})(ctx, invocation)
	})
	adapter, err := newWithRunner(Config{}, runner)
	if err != nil {
		t.Fatalf("newWithRunner() error = %v", err)
	}
	if _, err := adapter.Execute(context.Background(), request); err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(invocations) != 3 ||
		!reflect.DeepEqual(invocations[0].arguments, []string{"--version"}) ||
		!reflect.DeepEqual(invocations[1].arguments, []string{"exec", "--help"}) ||
		invocations[2].arguments[0] != "exec" {
		t.Fatalf("preflight/execution invocations = %#v", invocations)
	}
	for _, invocation := range invocations {
		if invocation.directory != request.Workspace().Root() {
			t.Fatalf("preflight directory = %q, want ProposalWorkspace", invocation.directory)
		}
	}
}

func TestAdapterPropagatesCancellationAndTimeout(t *testing.T) {
	for _, test := range []struct {
		name    string
		request func(*testing.T, string) (aiprovider.ExecutionRequest, string)
	}{
		{name: "implementation", request: adapterTestRequest},
		{name: "verification-planning", request: adapterPlanningTestRequest},
	} {
		t.Run(test.name, func(t *testing.T) {
			request, _ := test.request(t, "")
			blockingRunner := runnerFunction(func(ctx context.Context, _ processInvocation) (processResult, error) {
				<-ctx.Done()
				return processResult{}, ctx.Err()
			})

			cancelledContext, cancel := context.WithCancel(context.Background())
			cancel()
			cancelledAdapter, _ := newWithRunner(Config{Timeout: time.Minute}, withSuccessfulPreflight(blockingRunner))
			_, cancellationError := cancelledAdapter.Execute(cancelledContext, request)
			var normalized *aiprovider.ExecutionError
			if !errors.As(cancellationError, &normalized) || normalized.Kind() != aiprovider.FailureCancelled {
				t.Fatalf("cancellation error = %v", cancellationError)
			}

			timedAdapter, _ := newWithRunner(Config{Timeout: 10 * time.Millisecond}, withSuccessfulPreflight(blockingRunner))
			_, timeoutError := timedAdapter.Execute(context.Background(), request)
			if !errors.As(timeoutError, &normalized) || normalized.Kind() != aiprovider.FailureTimeout {
				t.Fatalf("timeout error = %v", timeoutError)
			}
		})
	}
}

func TestAdapterRealProcessHarnessUsesProposalWorkspaceOnly(t *testing.T) {
	request, canonicalRoot := adapterTestRequest(t, "")
	binary := filepath.Join(t.TempDir(), "fake-codex")
	script := `#!/bin/sh
if [ "$1" = "--version" ]; then
  printf '%s\n' 'codex-cli 1.2.3'
  exit 0
fi
if [ "$1" = "exec" ] && [ "$2" = "--help" ]; then
  printf '%s\n' '--model --sandbox workspace-write read-only --cd --ephemeral --ignore-user-config --approve-for-me --color --json'
  exit 0
fi
printf '%s\n' "$PWD" > process-directory.txt
printf '%s\n' "$@" > process-arguments.txt
cat > process-prompt.txt
printf 'package service\n\nfunc Greeting() string { return "hello-praetor" }\n' > service.go
printf '%s\n' '{"type":"thread.started","thread_id":"thread-harness"}'
printf '%s\n' '{"type":"item.completed","item":{"type":"agent_message","text":"done"}}'
printf '%s\n' '{"type":"turn.completed","usage":{"input_tokens":1,"cached_input_tokens":0,"output_tokens":1,"reasoning_output_tokens":0}}'
`
	if err := os.WriteFile(binary, []byte(script), 0o700); err != nil {
		t.Fatalf("write fake executable: %v", err)
	}
	adapter, err := New(Config{Binary: binary, Timeout: time.Minute})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	response, err := adapter.Execute(context.Background(), request)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if response.ExternalExecutionId() != "thread-harness" {
		t.Fatalf("response = %#v", response)
	}
	workspaceRoot := request.Workspace().Root()
	directory, err := os.ReadFile(filepath.Join(workspaceRoot, "process-directory.txt"))
	if err != nil {
		t.Fatalf("read process directory: %v", err)
	}
	if strings.TrimSpace(string(directory)) != workspaceRoot {
		t.Fatalf("process directory = %q, want %q", directory, workspaceRoot)
	}
	arguments, _ := os.ReadFile(filepath.Join(workspaceRoot, "process-arguments.txt"))
	if !strings.Contains(string(arguments), workspaceRoot) || strings.Contains(string(arguments), canonicalRoot) {
		t.Fatalf("process arguments = %q", arguments)
	}
	prompt, _ := os.ReadFile(filepath.Join(workspaceRoot, "process-prompt.txt"))
	if strings.Contains(string(prompt), canonicalRoot) {
		t.Fatalf("prompt leaked canonical root: %q", prompt)
	}
	canonicalService, err := os.ReadFile(filepath.Join(canonicalRoot, "service.go"))
	if err != nil {
		t.Fatalf("read canonical service: %v", err)
	}
	if strings.Contains(string(canonicalService), "hello-praetor") {
		t.Fatal("fake provider mutated canonical source")
	}
}

func TestAdapterRealProcessHarnessLeavesPartialMutationAsFailedProvenance(t *testing.T) {
	request, canonicalRoot := adapterTestRequest(t, "")
	binary := filepath.Join(t.TempDir(), "fake-codex-partial")
	script := `#!/bin/sh
if [ "$1" = "--version" ]; then
  printf '%s\n' 'codex-cli 1.2.3'
  exit 0
fi
if [ "$1" = "exec" ] && [ "$2" = "--help" ]; then
  printf '%s\n' '--model --sandbox workspace-write read-only --cd --ephemeral --ignore-user-config --approve-for-me --color --json'
  exit 0
fi
printf 'package service\n\nconst Partial = true\n' > service.go
printf '%s\n' '{"type":"thread.started","thread_id":"thread-partial"}'
printf '%s\n' '{"type":"turn.failed","message":"process failed after mutation"}'
exit 1
`
	if err := os.WriteFile(binary, []byte(script), 0o700); err != nil {
		t.Fatalf("write fake executable: %v", err)
	}
	adapter, err := New(Config{Binary: binary, Timeout: time.Minute})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	_, executionError := adapter.Execute(context.Background(), request)
	var normalized *aiprovider.ExecutionError
	if !errors.As(executionError, &normalized) || normalized.Kind() != aiprovider.FailureProcess ||
		normalized.ExternalExecutionId() != "thread-partial" {
		t.Fatalf("partial execution error = %T %v", executionError, executionError)
	}
	workspaceContents, err := os.ReadFile(filepath.Join(request.Workspace().Root(), "service.go"))
	if err != nil || !strings.Contains(string(workspaceContents), "const Partial") {
		t.Fatalf("partial workspace mutation = %q/%v", workspaceContents, err)
	}
	canonicalContents, err := os.ReadFile(filepath.Join(canonicalRoot, "service.go"))
	if err != nil || strings.Contains(string(canonicalContents), "const Partial") {
		t.Fatalf("canonical source changed = %q/%v", canonicalContents, err)
	}
}

func successfulProcessOutput(threadId string) processResult {
	return processResult{standardOutput: []byte(
		`{"type":"thread.started","thread_id":"` + threadId + `"}` + "\n" +
			`{"type":"turn.completed","usage":{"input_tokens":0,"cached_input_tokens":0,"output_tokens":0,"reasoning_output_tokens":0}}` + "\n",
	)}
}

type adapterWorkspacePort struct{ root string }

func (port adapterWorkspacePort) Create(request proposal.WorkspaceRequest) (proposal.ProposalWorkspace, error) {
	return proposal.NewProposalWorkspace(
		"proposal-0123456789abcdef0123456789abcdef",
		request.ProjectId,
		request.ChangeId,
		request.CanonicalRoot,
		port.root,
		request.BaseRevision,
		request.SourceStateDigest,
	)
}
func (adapterWorkspacePort) Remove(proposal.ProposalWorkspace) error { return nil }

type adapterPatchPort struct {
	extracted proposal.ExtractedPatch
}

func (port *adapterPatchPort) Extract(proposal.ProposalWorkspace) (proposal.ExtractedPatch, error) {
	return port.extracted, nil
}

func TestImplementationRequestAccountingMatchesSerializedComponents(t *testing.T) {
	request, _ := adapterTestRequest(t, "")
	prompt, accounting, err := serializeImplementationRequest(request)
	if err != nil {
		t.Fatalf("serializeImplementationRequest() error = %v", err)
	}
	t.Logf("strict implementation request: bytes=%d characters=%d", accounting.TotalBytes(), accounting.TotalCharacters())
	if accounting.TotalBytes() != len(prompt) ||
		accounting.TotalCharacters() != len([]rune(prompt)) ||
		len(accounting.Components()) != aiprovider.MaximumRequestContextComponents {
		t.Fatalf("request accounting = bytes:%d/%d chars:%d/%d components:%d",
			accounting.TotalBytes(), len(prompt),
			accounting.TotalCharacters(), len([]rune(prompt)),
			len(accounting.Components()))
	}
	componentBytes := 0
	componentCharacters := 0
	for _, component := range accounting.Components() {
		t.Logf("component=%s bytes=%d characters=%d items=%d omitted=%d truncated=%t", component.Kind(), component.ByteCount(), component.CharacterCount(), component.ItemCount(), component.OmittedItems(), component.Truncated())
		componentBytes += component.ByteCount()
		componentCharacters += component.CharacterCount()
	}
	if componentBytes != len(prompt) || componentCharacters != len([]rune(prompt)) {
		t.Fatalf("component totals = bytes:%d chars:%d", componentBytes, componentCharacters)
	}
	intent := string(request.Intent())
	if strings.Count(prompt, intent) != 1 {
		t.Fatalf("intent occurrence count = %d in prompt:\n%s", strings.Count(prompt, intent), prompt)
	}
	if strings.Count(prompt, "service_test.go") != 1 || strings.Count(prompt, "go.mod") != 1 {
		t.Fatalf("possible/protected scope was duplicated:\n%s", prompt)
	}
	if strings.Contains(prompt, "func Greeting()") {
		t.Fatalf("source body was eagerly serialized:\n%s", prompt)
	}
	sourceComponent := requestContextComponent(t, accounting, aiprovider.RequestContextSource)
	if sourceComponent.ByteCount() != 0 || sourceComponent.ItemCount() != 0 {
		t.Fatalf("source context accounting = %#v", sourceComponent)
	}
	adapter := NewDefault()
	adapterAccounting, err := adapter.AccountRequest(request)
	if err != nil {
		t.Fatalf("AccountRequest() error = %v", err)
	}
	if adapterAccounting.TotalBytes() != accounting.TotalBytes() {
		t.Fatalf("adapter accounting bytes = %d, serialized = %d", adapterAccounting.TotalBytes(), accounting.TotalBytes())
	}
}

func TestRepositoryWideLargeInventoryIsAuthorizationNotMaterialization(t *testing.T) {
	trackedPaths := []string{"go.mod", "service.go", "service_test.go"}
	for index := 0; index < 10_000; index++ {
		trackedPaths = append(trackedPaths, "bulk/large-repository-file-"+strconv.Itoa(index)+".go")
	}
	slices.Sort(trackedPaths)
	scope := source.ScopeRequest{
		AuthorizationMode: source.AuthorizationRepositoryWide,
		Protected:         []string{"go.mod"},
	}
	request, _ := adapterRequestFixtureWithOptions(t, "", adapterFixtureOptions{
		scope:        &scope,
		trackedPaths: trackedPaths,
	})
	prompt, accounting, err := serializeImplementationRequest(request)
	if err != nil {
		t.Fatalf("serializeImplementationRequest() error = %v", err)
	}
	t.Logf("repository-wide implementation request with %d tracked paths: bytes=%d characters=%d", len(trackedPaths), accounting.TotalBytes(), accounting.TotalCharacters())
	if strings.Contains(prompt, "bulk/large-repository-file-9999.go") ||
		strings.Contains(prompt, "func Greeting()") {
		t.Fatalf("large repository inventory or source body was materialized:\n%s", prompt)
	}
	if !strings.Contains(prompt, "Mode: repository-wide") ||
		!strings.Contains(prompt, "Repository scope: .") ||
		!strings.Contains(prompt, "Protected paths:\n- go.mod") {
		t.Fatalf("repository-wide authority is incomplete:\n%s", prompt)
	}
	if accounting.TotalBytes() > 2<<10 {
		t.Fatalf("repository-wide request unexpectedly scales with inventory: %d bytes", accounting.TotalBytes())
	}
	repositoryComponent := requestContextComponent(t, accounting, aiprovider.RequestContextRepository)
	if repositoryComponent.ItemCount() != 1 {
		t.Fatalf("repository context item count = %d", repositoryComponent.ItemCount())
	}
	sourceComponent := requestContextComponent(t, accounting, aiprovider.RequestContextSource)
	if sourceComponent.ByteCount() != 0 || sourceComponent.ItemCount() != 0 {
		t.Fatalf("large source context accounting = %#v", sourceComponent)
	}
}

func TestImplementationRequestReportsAdvisoryContextOmission(t *testing.T) {
	implementationContext, err := aiprovider.NewImplementationContextWithDiagnostics(
		"current ImpactReport",
		[]string{"impact=EXPECTED kind=file location=service.go"},
		100,
		true,
	)
	if err != nil {
		t.Fatalf("NewImplementationContextWithDiagnostics() error = %v", err)
	}
	request, _ := adapterRequestFixtureWithOptions(t, "", adapterFixtureOptions{
		implementationContext: &implementationContext,
	})
	prompt, accounting, err := serializeImplementationRequest(request)
	if err != nil {
		t.Fatalf("serializeImplementationRequest() error = %v", err)
	}
	if !strings.Contains(prompt, "omitted by context budget: 99 entries") ||
		!strings.Contains(prompt, "inspect the ProposalWorkspace for additional evidence") {
		t.Fatalf("advisory omission is not actionable:\n%s", prompt)
	}
	impact := requestContextComponent(t, accounting, aiprovider.RequestContextImpactReport)
	if impact.ItemCount() != 1 || impact.OmittedItems() != 99 || !impact.Truncated() || !accounting.Truncated() {
		t.Fatalf("impact accounting = %#v, aggregate truncated = %t", impact, accounting.Truncated())
	}
}

func TestImplementationRequestFailsClosedAtGovernanceCriticalBudgets(t *testing.T) {
	t.Run("intent", func(t *testing.T) {
		request, _ := adapterRequestFixtureWithOptions(t, "", adapterFixtureOptions{
			intent: strings.Repeat("i", maximumImplementationIntentBytes+1),
		})
		if _, _, err := serializeImplementationRequest(request); err == nil ||
			!strings.Contains(err.Error(), "intent exceeds") {
			t.Fatalf("intent budget error = %v", err)
		}
	})

	t.Run("approved scope path count", func(t *testing.T) {
		paths := make([]string, maximumImplementationScopePaths+1)
		for index := range paths {
			paths[index] = "scope/path-" + strconv.Itoa(index) + ".go"
		}
		slices.Sort(paths)
		scope := source.ScopeRequest{Expected: paths}
		request, _ := adapterRequestFixtureWithOptions(t, "", adapterFixtureOptions{
			scope:        &scope,
			trackedPaths: paths,
		})
		if _, _, err := serializeImplementationRequest(request); err == nil ||
			!strings.Contains(err.Error(), "approved scope exceeds") {
			t.Fatalf("scope budget error = %v", err)
		}
	})

	t.Run("approved scope bytes", func(t *testing.T) {
		paths := make([]string, 1000)
		for index := range paths {
			paths[index] = "scope/" + strings.Repeat("long-segment-", 25) + strconv.Itoa(index) + ".go"
		}
		slices.Sort(paths)
		scope := source.ScopeRequest{Expected: paths}
		request, _ := adapterRequestFixtureWithOptions(t, "", adapterFixtureOptions{
			scope:        &scope,
			trackedPaths: paths,
		})
		if _, _, err := serializeImplementationRequest(request); err == nil ||
			!strings.Contains(err.Error(), "scope serialization exceeds") {
			t.Fatalf("scope byte budget error = %v", err)
		}
	})
}

func requestContextComponent(
	t *testing.T,
	accounting aiprovider.RequestContextAccounting,
	kind aiprovider.RequestContextComponentKind,
) aiprovider.RequestContextComponent {
	t.Helper()
	for _, component := range accounting.Components() {
		if component.Kind() == kind {
			return component
		}
	}
	t.Fatalf("request context component %q is absent", kind)
	return aiprovider.RequestContextComponent{}
}

func adapterTestRequest(t *testing.T, model string) (aiprovider.ExecutionRequest, string) {
	return adapterRequestFixture(t, model, false)
}

func adapterPlanningTestRequest(t *testing.T, model string) (aiprovider.ExecutionRequest, string) {
	return adapterRequestFixture(t, model, true)
}

type adapterFixtureOptions struct {
	planning              bool
	scope                 *source.ScopeRequest
	trackedPaths          []string
	intent                string
	implementationContext *aiprovider.ImplementationContext
}

func adapterRequestFixture(t *testing.T, model string, planning bool, scopeRequests ...source.ScopeRequest) (aiprovider.ExecutionRequest, string) {
	t.Helper()
	options := adapterFixtureOptions{planning: planning}
	if len(scopeRequests) > 0 {
		options.scope = &scopeRequests[0]
	}
	return adapterRequestFixtureWithOptions(t, model, options)
}

func adapterRequestFixtureWithOptions(t *testing.T, model string, options adapterFixtureOptions) (aiprovider.ExecutionRequest, string) {
	t.Helper()
	canonicalRoot := t.TempDir()
	workspaceRoot := t.TempDir()
	for name, content := range map[string]string{
		"service.go":      "package service\n\nfunc Greeting() string { return \"hello\" }\n",
		"service_test.go": "package service\n",
		"go.mod":          "module fixture\n",
	} {
		if err := os.WriteFile(filepath.Join(canonicalRoot, name), []byte(content), 0o600); err != nil {
			t.Fatalf("write fixture %s: %v", name, err)
		}
		if err := os.WriteFile(filepath.Join(workspaceRoot, name), []byte(content), 0o600); err != nil {
			t.Fatalf("write workspace fixture %s: %v", name, err)
		}
	}
	createdAt := time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC)
	intent := options.intent
	if intent == "" {
		intent = "Change Greeting() to return hello-praetor."
	}
	currentChange, err := change.New(
		"change-codex-adapter",
		adapterTestProjectId,
		change.ChangeIntent(intent),
		createdAt,
	)
	if err != nil {
		t.Fatalf("change.New() error = %v", err)
	}
	if _, err := currentChange.Transition(change.StatePlanned, createdAt.Add(time.Second), "planned"); err != nil {
		t.Fatalf("planned transition error = %v", err)
	}
	trackedPaths := options.trackedPaths
	if trackedPaths == nil {
		trackedPaths = []string{"go.mod", "service.go", "service_test.go"}
	}
	snapshot, err := source.NewSourceSnapshot(
		adapterTestProjectId,
		canonicalRoot,
		"0123456789abcdef0123456789abcdef01234567",
		source.WorkingTreeClean,
		trackedPaths,
		source.SourceStateDigest("sha256:"+strings.Repeat("b", 64)),
	)
	if err != nil {
		t.Fatalf("NewSourceSnapshot() error = %v", err)
	}
	scopeRequest := source.ScopeRequest{
		Expected:  []string{"service.go"},
		Possible:  []string{"service_test.go"},
		Protected: []string{"go.mod"},
	}
	if options.scope != nil {
		scopeRequest = *options.scope
	}
	analysis, err := source.AnalyzeImpact(currentChange, snapshot, scopeRequest)
	if err != nil {
		t.Fatalf("AnalyzeImpact() error = %v", err)
	}
	approvedScope, err := source.EstablishApprovedScope(analysis)
	if err != nil {
		t.Fatalf("EstablishApprovedScope() error = %v", err)
	}
	patchPort := &adapterPatchPort{}
	proposalService, err := proposal.New(
		adapterWorkspacePort{root: workspaceRoot},
		patchPort,
		func(project.ProjectId, string) (source.SourceSnapshot, error) { return snapshot, nil },
		func(proposal.LifecycleEvent) error { return nil },
		func() time.Time { return createdAt.Add(3 * time.Second) },
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
	attemptId, _ := aiprovider.GenerateExecutionAttemptId()
	selection, err := aiprovider.NewSelection(Identifier, model)
	if err != nil {
		t.Fatalf("NewSelection() error = %v", err)
	}
	var request aiprovider.ExecutionRequest
	if options.planning {
		changedContents := "package service\n\nfunc Greeting() string { return \"hello-praetor\" }\n"
		if err := os.WriteFile(filepath.Join(workspaceRoot, "service.go"), []byte(changedContents), 0o600); err != nil {
			t.Fatalf("write planned fixture change: %v", err)
		}
		patchPort.extracted = proposal.ExtractedPatch{
			Content:      []byte("diff --git a/service.go b/service.go\n+hello-praetor\n"),
			ChangedPaths: []string{"service.go"},
		}
		currentProposal, _, err = proposalService.ExtractPatch(currentProposal)
		if err != nil {
			t.Fatalf("ExtractPatch() error = %v", err)
		}
		evidence, evidenceError := aiprovider.NewPlanningEvidence("go.mod", "manifest", "module fixture\n")
		if evidenceError != nil {
			t.Fatalf("NewPlanningEvidence() error = %v", evidenceError)
		}
		request, err = aiprovider.NewVerificationPlanningRequest(
			attemptId,
			currentChange,
			currentProposal,
			selection,
			[]aiprovider.PlanningEvidence{evidence},
		)
	} else {
		implementationContext, contextError := aiprovider.NewImplementationContext(
			"current ImpactReport art-0123456789abcdef0123456789abcdef digest=sha256:test risk=LOW",
			[]string{"impact=EXPECTED kind=file location=service.go basis=manifest confidence=HIGH"},
		)
		if contextError != nil {
			t.Fatalf("NewImplementationContext() error = %v", contextError)
		}
		if options.implementationContext != nil {
			implementationContext = *options.implementationContext
		}
		request, err = aiprovider.NewExecutionRequestWithContext(
			attemptId,
			currentChange,
			currentProposal,
			aiprovider.ImplementationRoleContract(),
			selection,
			implementationContext,
		)
	}
	if err != nil {
		t.Fatalf("NewExecutionRequest() error = %v", err)
	}
	return request, canonicalRoot
}

func TestAdapterRetainsBoundedSanitizedFailureDiagnostic(t *testing.T) {
	request, _ := adapterTestRequest(t, "")
	secret := "SUPER-SECRET-P6"

	adapter, err := newWithRunner(
		Config{},
		withSuccessfulPreflight(runnerFunction(func(context.Context, processInvocation) (processResult, error) {
			return processResult{
				standardOutput: []byte(
					`{"type":"thread.started","thread_id":"thread-diagnostic"}` + "\n" +
						`{"type":"turn.failed","message":"provider failed password=` + secret + `"}` + "\n",
				),
				standardError: []byte(
					"request failed Authorization: Bearer " + secret + " " +
						strings.Repeat("detail ", 400),
				),
				exitCode: 7,
			}, errors.New("exit status 7")
		})),
	)
	if err != nil {
		t.Fatalf("newWithRunner() error = %v", err)
	}

	_, executionError := adapter.Execute(context.Background(), request)

	var normalized *aiprovider.ExecutionError
	if !errors.As(executionError, &normalized) {
		t.Fatalf("Execute() error = %T %v", executionError, executionError)
	}

	diagnostic := normalized.Diagnostic()
	if diagnostic == "" {
		t.Fatal("failure diagnostic is empty")
	}
	if len(diagnostic) > maximumFailureDiagnosticBytes {
		t.Fatalf("failure diagnostic length = %d", len(diagnostic))
	}
	if strings.Contains(diagnostic, secret) {
		t.Fatalf("failure diagnostic leaked secret: %q", diagnostic)
	}
	if !strings.Contains(diagnostic, "[REDACTED]") {
		t.Fatalf("failure diagnostic did not redact secret: %q", diagnostic)
	}
	if !strings.Contains(diagnostic, "exit_code=7") {
		t.Fatalf("failure diagnostic lacks exit code: %q", diagnostic)
	}
	if strings.Contains(executionError.Error(), diagnostic) ||
		strings.Contains(executionError.Error(), secret) {
		t.Fatalf("normalized public error leaked diagnostic: %q", executionError.Error())
	}
}

func TestAdapterNeverCombinesAutomaticApprovalWithExplicitSandbox(t *testing.T) {
	implementationRequest, _ := adapterTestRequest(t, "")
	planningRequest, _ := adapterPlanningTestRequest(t, "")
	adapter := NewDefault()

	implementationArguments := adapter.arguments(implementationRequest)
	if !slices.Contains(implementationArguments, "--approve-for-me") || slices.Contains(implementationArguments, "--sandbox") {
		t.Fatalf("implementation arguments must use automatic review without explicit sandbox: %#v", implementationArguments)
	}
	planningArguments := adapter.arguments(planningRequest)
	if slices.Contains(planningArguments, "--approve-for-me") || !slices.Contains(planningArguments, "--sandbox") {
		t.Fatalf("planning arguments must use an explicit read-only sandbox without automatic review: %#v", planningArguments)
	}
	for _, arguments := range [][]string{implementationArguments, planningArguments} {
		if slices.Contains(arguments, "--dangerously-bypass-approvals-and-sandbox") || slices.Contains(arguments, "danger-full-access") {
			t.Fatalf("unsafe Codex argument generated: %#v", arguments)
		}
	}
}
