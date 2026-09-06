package verification

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"github.com/Eu-Pedro0ficial/praetor/internal/project"
	"github.com/Eu-Pedro0ficial/praetor/internal/proposal"
	"github.com/Eu-Pedro0ficial/praetor/internal/source"
)

const verificationTestProjectId project.ProjectId = "01890f47-9f20-7cc1-98c8-1123456789ab"

type verificationFixtureAdapter struct {
	workspaceRoot string
	extracted     proposal.ExtractedPatch
}

func (adapter *verificationFixtureAdapter) Create(request proposal.WorkspaceRequest) (proposal.ProposalWorkspace, error) {
	return proposal.NewProposalWorkspace(
		"proposal-11223344556677889900aabbccddeeff",
		request.ProjectId,
		request.ChangeId,
		request.CanonicalRoot,
		adapter.workspaceRoot,
		request.BaseRevision,
		request.SourceStateDigest,
	)
}

func (*verificationFixtureAdapter) Remove(proposal.ProposalWorkspace) error { return nil }

func (adapter *verificationFixtureAdapter) Extract(proposal.ProposalWorkspace) (proposal.ExtractedPatch, error) {
	return proposal.ExtractedPatch{
		Content:      append([]byte(nil), adapter.extracted.Content...),
		ChangedPaths: append([]string(nil), adapter.extracted.ChangedPaths...),
	}, nil
}

type verificationFixture struct {
	currentChange   change.Change
	currentProposal proposal.Proposal
	proposalService *proposal.Service
	adapter         *verificationFixtureAdapter
	canonicalRoot   string
	workspaceRoot   string
}

func newVerificationFixture(t *testing.T, evidenceFiles map[string]string) verificationFixture {
	t.Helper()
	canonicalRoot := t.TempDir()
	workspaceRoot := t.TempDir()
	files := map[string]string{
		"service.go": "package service\n\nconst Value = 1\n",
	}
	for path, contents := range evidenceFiles {
		files[path] = contents
	}
	tracked := make([]string, 0, len(files))
	for name, contents := range files {
		tracked = append(tracked, name)
		for _, root := range []string{canonicalRoot, workspaceRoot} {
			path := filepath.Join(root, filepath.FromSlash(name))
			if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
				t.Fatalf("create fixture path: %v", err)
			}
			if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
				t.Fatalf("write fixture %s: %v", name, err)
			}
		}
	}
	now := time.Date(2026, time.September, 2, 12, 0, 0, 0, time.UTC)
	currentChange, err := change.New("change-verification", verificationTestProjectId, "verify service change", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := currentChange.Transition(change.StatePlanned, now.Add(time.Second), "planned"); err != nil {
		t.Fatal(err)
	}
	snapshot, err := source.NewSourceSnapshot(
		verificationTestProjectId,
		canonicalRoot,
		"1123456789abcdef0123456789abcdef01234567",
		source.WorkingTreeClean,
		tracked,
		source.SourceStateDigest("sha256:"+strings.Repeat("1", 64)),
	)
	if err != nil {
		t.Fatal(err)
	}
	analysis, err := source.AnalyzeImpact(currentChange, snapshot, source.ScopeRequest{Expected: []string{"service.go"}})
	if err != nil {
		t.Fatal(err)
	}
	scope, err := source.EstablishApprovedScope(analysis)
	if err != nil {
		t.Fatal(err)
	}
	adapter := &verificationFixtureAdapter{
		workspaceRoot: workspaceRoot,
		extracted: proposal.ExtractedPatch{
			Content:      []byte("diff --git a/service.go b/service.go\n+const Value = 2\n"),
			ChangedPaths: []string{"service.go"},
		},
	}
	proposalService, err := proposal.New(
		adapter,
		adapter,
		func(project.ProjectId, string) (source.SourceSnapshot, error) { return snapshot, nil },
		func(proposal.LifecycleEvent) error { return nil },
		func() time.Time { return now.Add(2 * time.Second) },
	)
	if err != nil {
		t.Fatal(err)
	}
	currentProposal, err := proposalService.CreateWorkspace(currentChange, snapshot, scope)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(
		filepath.Join(workspaceRoot, "service.go"),
		[]byte("package service\n\nconst Value = 2\n"),
		0o600,
	); err != nil {
		t.Fatal(err)
	}
	currentProposal, _, err = proposalService.ExtractPatch(currentProposal)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := currentChange.Transition(change.StateIsolated, now.Add(3*time.Second), "isolated"); err != nil {
		t.Fatal(err)
	}
	return verificationFixture{
		currentChange:   currentChange,
		currentProposal: currentProposal,
		proposalService: proposalService,
		adapter:         adapter,
		canonicalRoot:   canonicalRoot,
		workspaceRoot:   workspaceRoot,
	}
}

type fakeStepRunner struct {
	results      []ProcessResult
	errors       []error
	resolvePath  string
	resolveError error
	events       []string
	runCalls     int
	maximumBytes int
	run          func(context.Context, ProcessInvocation) (ProcessResult, error)
}

func (runner *fakeStepRunner) Resolve(executable string) (string, error) {
	runner.events = append(runner.events, "resolve:"+executable)
	if runner.resolveError != nil {
		return "", runner.resolveError
	}
	if runner.resolvePath != "" {
		return runner.resolvePath, nil
	}
	return os.Executable()
}

func (runner *fakeStepRunner) Run(ctx context.Context, invocation ProcessInvocation) (ProcessResult, error) {
	runner.events = append(runner.events, "run:"+filepath.Base(invocation.ResolvedExecutable))
	index := runner.runCalls
	runner.runCalls++
	if runner.run != nil {
		return runner.run(ctx, invocation)
	}
	var result ProcessResult
	var err error
	if index < len(runner.results) {
		result = runner.results[index]
	}
	if index < len(runner.errors) {
		err = runner.errors[index]
	}
	return result, err
}

func (runner *fakeStepRunner) MaximumOutputBytes() int {
	if runner.maximumBytes == 0 {
		return 64 << 10
	}
	return runner.maximumBytes
}

func advancingVerificationClock() Clock {
	now := time.Date(2026, time.September, 2, 13, 0, 0, 0, time.UTC)
	return func() time.Time {
		value := now
		now = now.Add(time.Second)
		return value
	}
}

func mustCandidate(
	t *testing.T,
	kind StepKind,
	executable string,
	arguments []string,
	origin CandidateOrigin,
	evidence string,
) VerificationCandidate {
	t.Helper()
	candidate, err := NewCandidate(kind, executable, arguments, ".", origin, []string{evidence})
	if err != nil {
		t.Fatalf("NewCandidate() error = %v", err)
	}
	return candidate
}

func TestEngineValidatesWholePlanAndCollectsCompleteBoundedEvidence(t *testing.T) {
	fixture := newVerificationFixture(t, map[string]string{"go.mod": "module fixture\n"})
	runner := &fakeStepRunner{
		maximumBytes: 32,
		results: []ProcessResult{
			NewProcessResult(1, true, []byte("token=do-not-store\n"), []byte("test failed\n"), false),
			NewProcessResult(0, true, []byte(strings.Repeat("x", 64)), nil, true),
		},
		errors: []error{errors.New("exit status 1"), nil},
	}
	integrityCalls := 0
	engine, err := NewEngine(runner, func(proposal.Proposal) error {
		integrityCalls++
		return nil
	}, advancingVerificationClock())
	if err != nil {
		t.Fatal(err)
	}
	plan, err := BuildPlan([]VerificationCandidate{
		mustCandidate(t, KindTest, "go", []string{"test", "./..."}, OriginDeterministicallyInferred, "go.mod"),
		mustCandidate(t, KindLint, "go", []string{"vet", "./..."}, OriginDeterministicallyInferred, "go.mod"),
	}, nil, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	evidence, err := engine.Execute(context.Background(), plan, fixture.currentProposal)
	if err != nil {
		t.Fatalf("Execute() error = %v", err)
	}
	if len(evidence) != 3 || evidence[0].Outcome() != OutcomeFail ||
		evidence[1].Outcome() != OutcomePass || evidence[2].Kind() != KindPatchIntegrity ||
		evidence[2].Outcome() != OutcomePass {
		t.Fatalf("evidence outcomes = %#v", evidence)
	}
	if runner.runCalls != 2 || integrityCalls != 2 {
		t.Fatalf("run/integrity calls = %d/%d", runner.runCalls, integrityCalls)
	}
	if len(runner.events) != 4 || !strings.HasPrefix(runner.events[0], "resolve:") ||
		!strings.HasPrefix(runner.events[1], "resolve:") || !strings.HasPrefix(runner.events[2], "run:") {
		t.Fatalf("validation/execution order = %v", runner.events)
	}
	if evidence[0].StandardOutput() != "[REDACTED]\n" {
		t.Fatalf("secret-bearing output was retained: %q", evidence[0].StandardOutput())
	}
	if !evidence[1].OutputTruncated() || len(evidence[1].StandardOutput()) != 32 {
		t.Fatalf("bounded output = %d/%t", len(evidence[1].StandardOutput()), evidence[1].OutputTruncated())
	}
	arguments := evidence[1].Arguments()
	arguments[0] = "changed"
	if evidence[1].Arguments()[0] != "test" {
		t.Fatal("Evidence exposed mutable arguments")
	}
}

func TestEngineRejectsPathEscapeAndWorkspaceExecutableBeforeRunning(t *testing.T) {
	fixture := newVerificationFixture(t, map[string]string{"go.mod": "module fixture\n"})
	if err := os.Symlink(fixture.canonicalRoot, filepath.Join(fixture.workspaceRoot, "escape")); err != nil {
		t.Fatal(err)
	}
	candidate := mustCandidate(t, KindTest, "go", []string{"test", "./..."}, OriginDeterministicallyInferred, "go.mod")
	candidate.workingDirectory = "escape"
	plan, err := BuildPlan([]VerificationCandidate{candidate}, nil, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	runner := &fakeStepRunner{}
	engine, _ := NewEngine(runner, func(proposal.Proposal) error { return nil }, advancingVerificationClock())
	if _, err := engine.Execute(context.Background(), plan, fixture.currentProposal); err == nil ||
		!strings.Contains(err.Error(), "escapes ProposalWorkspace") && !strings.Contains(err.Error(), "canonical source") {
		t.Fatalf("path escape error = %v", err)
	}
	if runner.runCalls != 0 {
		t.Fatal("path-unsafe plan executed")
	}

	toolPath := filepath.Join(fixture.workspaceRoot, "go")
	if err := os.Symlink(os.Args[0], toolPath); err != nil {
		t.Fatal(err)
	}
	runner = &fakeStepRunner{resolvePath: toolPath}
	engine, _ = NewEngine(runner, func(proposal.Proposal) error { return nil }, advancingVerificationClock())
	validPlan, _ := BuildPlan([]VerificationCandidate{
		mustCandidate(t, KindTest, "go", []string{"test", "./..."}, OriginDeterministicallyInferred, "go.mod"),
	}, nil, time.Second)
	if _, err := engine.Execute(context.Background(), validPlan, fixture.currentProposal); err == nil ||
		!strings.Contains(err.Error(), "ProposalWorkspace") {
		t.Fatalf("workspace executable error = %v", err)
	}
	if runner.runCalls != 0 {
		t.Fatal("repository-controlled executable ran")
	}
}

func TestEngineNormalizesTimeoutAndCancellation(t *testing.T) {
	fixture := newVerificationFixture(t, map[string]string{"go.mod": "module fixture\n"})
	plan, _ := BuildPlan([]VerificationCandidate{
		mustCandidate(t, KindTest, "go", []string{"test", "./..."}, OriginDeterministicallyInferred, "go.mod"),
	}, nil, 10*time.Millisecond)
	runner := &fakeStepRunner{run: func(ctx context.Context, _ ProcessInvocation) (ProcessResult, error) {
		<-ctx.Done()
		return ProcessResult{}, ctx.Err()
	}}
	engine, _ := NewEngine(runner, func(proposal.Proposal) error { return nil }, advancingVerificationClock())
	evidence, err := engine.Execute(context.Background(), plan, fixture.currentProposal)
	if err != nil || len(evidence) != 2 || evidence[0].Outcome() != OutcomeTimeout {
		t.Fatalf("timeout evidence = %#v/%v", evidence, err)
	}

	cancelledContext, cancel := context.WithCancel(context.Background())
	cancelPlan, err := BuildPlan([]VerificationCandidate{
		mustCandidate(t, KindTest, "go", []string{"test", "./..."}, OriginDeterministicallyInferred, "go.mod"),
		mustCandidate(t, KindLint, "go", []string{"vet", "./..."}, OriginDeterministicallyInferred, "go.mod"),
	}, nil, time.Second)
	if err != nil {
		t.Fatal(err)
	}
	runner = &fakeStepRunner{run: func(ctx context.Context, _ ProcessInvocation) (ProcessResult, error) {
		cancel()
		<-ctx.Done()
		return ProcessResult{}, ctx.Err()
	}}
	engine, _ = NewEngine(runner, func(proposal.Proposal) error { return nil }, advancingVerificationClock())
	evidence, err = engine.Execute(cancelledContext, cancelPlan, fixture.currentProposal)
	if err != nil || len(evidence) != 2 || evidence[0].Outcome() != OutcomeCancelled || runner.runCalls != 1 {
		t.Fatalf("cancellation evidence = %#v/%v", evidence, err)
	}
}

func TestEngineRejectsMissingExecutableBeforeAnyProcessRuns(t *testing.T) {
	fixture := newVerificationFixture(t, map[string]string{"go.mod": "module fixture\n"})
	plan, _ := BuildPlan([]VerificationCandidate{
		mustCandidate(t, KindTest, "go", []string{"test", "./..."}, OriginDeterministicallyInferred, "go.mod"),
	}, nil, time.Second)
	runner := &fakeStepRunner{resolveError: os.ErrNotExist}
	engine, _ := NewEngine(runner, func(proposal.Proposal) error { return nil }, advancingVerificationClock())
	if _, err := engine.Execute(context.Background(), plan, fixture.currentProposal); err == nil ||
		!errors.Is(err, os.ErrNotExist) {
		t.Fatalf("missing executable error = %v", err)
	}
	if runner.runCalls != 0 {
		t.Fatal("plan with missing executable ran a process")
	}
}

func newVerificationTestService(
	t *testing.T,
	fixture verificationFixture,
	runner *fakeStepRunner,
	events *[]LifecycleEvent,
	attemptIds AttemptIdGenerator,
) *Service {
	t.Helper()
	engine, err := NewEngine(runner, fixture.proposalService.VerifyIntegrity, advancingVerificationClock())
	if err != nil {
		t.Fatal(err)
	}
	service, err := New(
		engine,
		func(event LifecycleEvent) error {
			*events = append(*events, event)
			return nil
		},
		attemptIds,
		advancingVerificationClock(),
	)
	if err != nil {
		t.Fatal(err)
	}
	return service
}

func fixedVerificationAttemptId() (VerificationAttemptId, error) {
	return VerificationAttemptId("verification-00112233445566778899aabbccddeeff"), nil
}

func TestServicePassesDeterministicPlanWithoutAIAndRecordsEvidence(t *testing.T) {
	fixture := newVerificationFixture(t, map[string]string{"go.mod": "module fixture\n"})
	runner := &fakeStepRunner{results: []ProcessResult{NewProcessResult(0, true, []byte("ok\n"), nil, false)}}
	var events []LifecycleEvent
	service := newVerificationTestService(t, fixture, runner, &events, fixedVerificationAttemptId)
	plannerCalls := 0
	result, err := service.Verify(context.Background(), fixture.currentChange, fixture.currentProposal,
		func(context.Context, PlanningRequest) (PlanningResult, error) {
			plannerCalls++
			return PlanningResult{}, errors.New("must not run")
		})
	if err != nil {
		t.Fatalf("Verify() error = %v", err)
	}
	if !result.Passed() || plannerCalls != 0 || runner.runCalls != 1 {
		t.Fatalf("verification result/planner/runs = %#v/%d/%d", result, plannerCalls, runner.runCalls)
	}
	if len(result.EvidenceSet().Evidence()) != 2 ||
		result.EvidenceSet().PatchDigest() == "" ||
		result.EvidenceSet().ChangeId() != fixture.currentChange.ChangeId() {
		t.Fatalf("EvidenceSet = %#v", result.EvidenceSet())
	}
	returnedEvidence := result.EvidenceSet().Evidence()
	returnedEvidence[0].arguments[0] = "mutated"
	returnedEvidence[0] = Evidence{}
	if result.EvidenceSet().Evidence()[0].Arguments()[0] != "test" {
		t.Fatal("EvidenceSet exposed mutable evidence storage")
	}
	wantEventTypes := []string{
		EventVerificationStarted,
		EventVerificationStepCompleted,
		EventVerificationStepCompleted,
		EventVerificationCompleted,
	}
	if len(events) != len(wantEventTypes) {
		t.Fatalf("events = %#v", events)
	}
	for index, eventType := range wantEventTypes {
		if events[index].EventType != eventType {
			t.Fatalf("event %d = %q, want %q", index, events[index].EventType, eventType)
		}
	}
}

func TestSafeFailureRedactsSensitiveTextAndGovernedPaths(t *testing.T) {
	fixture := newVerificationFixture(t, map[string]string{"go.mod": "module fixture\n"})
	message := safeFailure(fmt.Errorf(
		"failure at %s while comparing %s: token=do-not-retain",
		fixture.workspaceRoot,
		fixture.canonicalRoot,
	), fixture.currentProposal)
	if strings.Contains(message, fixture.workspaceRoot) || strings.Contains(message, fixture.canonicalRoot) ||
		strings.Contains(message, "do-not-retain") || message != "[REDACTED]" {
		t.Fatalf("safeFailure() = %q", message)
	}
}

func TestServicePlannerFailureUsesSufficientDeterministicEvidenceOnly(t *testing.T) {
	fixture := newVerificationFixture(t, map[string]string{
		"go.mod":         "module fixture\n",
		"pyproject.toml": "[project]\nname='fixture'\n",
	})
	runner := &fakeStepRunner{results: []ProcessResult{NewProcessResult(0, true, nil, nil, false)}}
	var events []LifecycleEvent
	service := newVerificationTestService(t, fixture, runner, &events, fixedVerificationAttemptId)
	result, err := service.Verify(context.Background(), fixture.currentChange, fixture.currentProposal,
		func(context.Context, PlanningRequest) (PlanningResult, error) {
			return PlanningResult{}, errors.New("provider unavailable")
		})
	if err != nil || !result.Passed() || result.PlanningFailure() == "" || runner.runCalls != 1 {
		t.Fatalf("deterministic fallback = %#v/%v/runs=%d", result, err, runner.runCalls)
	}

	insufficient := newVerificationFixture(t, map[string]string{
		"pyproject.toml": "[project]\nname='fixture'\n",
	})
	runner = &fakeStepRunner{}
	events = nil
	service = newVerificationTestService(t, insufficient, runner, &events, fixedVerificationAttemptId)
	result, err = service.Verify(context.Background(), insufficient.currentChange, insufficient.currentProposal,
		func(context.Context, PlanningRequest) (PlanningResult, error) {
			return PlanningResult{}, errors.New("provider unavailable")
		})
	if err == nil || result.Passed() || runner.runCalls != 0 ||
		!strings.Contains(err.Error(), "insufficient deterministic repository evidence") {
		t.Fatalf("insufficient planning result = %#v/%v/runs=%d", result, err, runner.runCalls)
	}
}

func TestServiceUsesAIOnlyForPlanCandidatesAndFailureKeepsProposalRetained(t *testing.T) {
	fixture := newVerificationFixture(t, map[string]string{
		"pyproject.toml": "[tool.pytest.ini_options]\n",
	})
	runner := &fakeStepRunner{
		results: []ProcessResult{NewProcessResult(1, true, nil, []byte("assertion failed\n"), false)},
		errors:  []error{errors.New("exit status 1")},
	}
	var events []LifecycleEvent
	service := newVerificationTestService(t, fixture, runner, &events, fixedVerificationAttemptId)
	result, err := service.Verify(context.Background(), fixture.currentChange, fixture.currentProposal,
		func(_ context.Context, request PlanningRequest) (PlanningResult, error) {
			candidate := mustCandidate(t, KindTest, "pytest", nil, OriginAIAssisted, "pyproject.toml")
			return NewPlanningResult("attempt-00112233445566778899aabbccddeeff", "codex-cli", "model-x", []VerificationCandidate{candidate})
		})
	if err == nil || result.Passed() || fixture.currentChange.State() != change.StateIsolated ||
		fixture.currentProposal.Workspace().State() != proposal.WorkspaceRetained {
		t.Fatalf("failed verification state = %#v/%v/%s/%s", result, err, fixture.currentChange.State(), fixture.currentProposal.Workspace().State())
	}
	planning, used := result.PlanningResult()
	if !used || planning.Provider() != "codex-cli" || len(result.EvidenceSet().Evidence()) != 2 {
		t.Fatalf("planning/evidence provenance = %#v/%t/%#v", planning, used, result.EvidenceSet())
	}
	if events[len(events)-1].EventType != EventVerificationFailed {
		t.Fatalf("last event = %#v", events[len(events)-1])
	}
}

func TestServiceDetectsProposalMutationAfterPlanning(t *testing.T) {
	fixture := newVerificationFixture(t, map[string]string{"pyproject.toml": "[tool.pytest.ini_options]\n"})
	runner := &fakeStepRunner{}
	var events []LifecycleEvent
	service := newVerificationTestService(t, fixture, runner, &events, fixedVerificationAttemptId)
	_, err := service.Verify(context.Background(), fixture.currentChange, fixture.currentProposal,
		func(context.Context, PlanningRequest) (PlanningResult, error) {
			fixture.adapter.extracted.Content = append(fixture.adapter.extracted.Content, []byte("unexpected")...)
			return PlanningResult{}, errors.New("planner failure")
		})
	if err == nil || !strings.Contains(err.Error(), "changed protected source state") || runner.runCalls != 0 {
		t.Fatalf("planner mutation result = %v/runs=%d", err, runner.runCalls)
	}
}

func TestServiceEmptyPlannerResultAndExecutionMutationFailClosed(t *testing.T) {
	t.Run("empty planner result", func(t *testing.T) {
		fixture := newVerificationFixture(t, nil)
		runner := &fakeStepRunner{}
		var events []LifecycleEvent
		service := newVerificationTestService(t, fixture, runner, &events, fixedVerificationAttemptId)
		result, err := service.Verify(context.Background(), fixture.currentChange, fixture.currentProposal,
			func(context.Context, PlanningRequest) (PlanningResult, error) {
				return NewPlanningResult(
					"attempt-00112233445566778899aabbccddeeff",
					"codex-cli",
					"",
					nil,
				)
			})
		if err == nil || result.Passed() || runner.runCalls != 0 ||
			fixture.currentChange.State() != change.StateIsolated {
			t.Fatalf("empty planner verification = %#v/%v/runs=%d/state=%s",
				result, err, runner.runCalls, fixture.currentChange.State())
		}
	})

	t.Run("proposal mutation during deterministic execution", func(t *testing.T) {
		fixture := newVerificationFixture(t, map[string]string{"go.mod": "module fixture\n"})
		runner := &fakeStepRunner{run: func(context.Context, ProcessInvocation) (ProcessResult, error) {
			fixture.adapter.extracted.Content = append(fixture.adapter.extracted.Content, []byte("unexpected")...)
			return NewProcessResult(0, true, nil, nil, false), nil
		}}
		var events []LifecycleEvent
		service := newVerificationTestService(t, fixture, runner, &events, fixedVerificationAttemptId)
		result, err := service.Verify(context.Background(), fixture.currentChange, fixture.currentProposal, nil)
		if err == nil || result.Passed() || len(result.EvidenceSet().Evidence()) != 2 ||
			result.EvidenceSet().Evidence()[1].Kind() != KindPatchIntegrity ||
			result.EvidenceSet().Evidence()[1].Outcome() != OutcomeFail {
			t.Fatalf("execution mutation verification = %#v/%v", result, err)
		}
		if fixture.currentProposal.Workspace().State() != proposal.WorkspaceRetained ||
			fixture.currentChange.State() != change.StateIsolated {
			t.Fatalf("execution mutation changed retained workflow state")
		}
	})
}
