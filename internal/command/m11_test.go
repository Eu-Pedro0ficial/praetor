package command_test

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Eu-Pedro0ficial/praetor/internal/adapters/gitproposal"
	sqliteadapter "github.com/Eu-Pedro0ficial/praetor/internal/adapters/persistence/sqlite"
	"github.com/Eu-Pedro0ficial/praetor/internal/aiprovider"
	"github.com/Eu-Pedro0ficial/praetor/internal/approval"
	"github.com/Eu-Pedro0ficial/praetor/internal/artifact"
	"github.com/Eu-Pedro0ficial/praetor/internal/audit"
	"github.com/Eu-Pedro0ficial/praetor/internal/authority"
	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"github.com/Eu-Pedro0ficial/praetor/internal/command"
	"github.com/Eu-Pedro0ficial/praetor/internal/composition"
	"github.com/Eu-Pedro0ficial/praetor/internal/project"
	"github.com/Eu-Pedro0ficial/praetor/internal/proposal"
	"github.com/Eu-Pedro0ficial/praetor/internal/source"
)

type injectedCommitFailureStore struct {
	authority.Store
	fail bool
}

func (store *injectedCommitFailureStore) CommitAuthority(commit authority.AuthorityCommit) error {
	if store.fail {
		return errors.New("injected authority commit failure")
	}
	return store.Store.CommitAuthority(commit)
}

func wrapAuthorityCommits(target **injectedCommitFailureStore) func(*composition.Container) {
	return func(container *composition.Container) {
		base := container.DurableStoreFactory
		container.DurableStoreFactory = func(dataDirectory string, registration project.Registration) (authority.Store, error) {
			store, err := base(dataDirectory, registration)
			if err != nil {
				return nil, err
			}
			wrapped := &injectedCommitFailureStore{Store: store}
			*target = wrapped
			return wrapped, nil
		}
	}
}

type injectedOperationCompletionFailureStore struct {
	authority.Store
	fail bool
}

func (store *injectedOperationCompletionFailureStore) CompleteOperation(id authority.OperationId, digest string, result []byte, events []audit.Event) (authority.Operation, error) {
	if store.fail {
		return authority.Operation{}, errors.New("injected operation completion failure")
	}
	return store.Store.CompleteOperation(id, digest, result, events)
}

func wrapOperationCompletion(target **injectedOperationCompletionFailureStore) func(*composition.Container) {
	return func(container *composition.Container) {
		base := container.DurableStoreFactory
		container.DurableStoreFactory = func(dataDirectory string, registration project.Registration) (authority.Store, error) {
			store, err := base(dataDirectory, registration)
			if err != nil {
				return nil, err
			}
			wrapped := &injectedOperationCompletionFailureStore{Store: store}
			*target = wrapped
			return wrapped, nil
		}
	}
}

func TestM11NormalLifecycleAuthorityCommitFailuresLeaveNoPartialAuthority(t *testing.T) {

	t.Run("proposal workspace foundation", func(t *testing.T) {
		var store *injectedCommitFailureStore
		repositoryRoot, _, session, registry := prepareProviderCommandTestWithContainer(t, decisionCommandProvider(t, new(int)), &commandVerificationRunner{}, wrapAuthorityCommits(&store))
		store.fail = true
		_, isolateError := registry.Dispatch(session, `change isolate atomic-workspace "atomic workspace" --expected internal/service/service.go --protected go.mod`, io.Discard)
		if isolateError == nil || !strings.Contains(isolateError.Error(), "injected authority commit failure") {
			t.Fatalf("workspace foundation error=%v", isolateError)
		}
		current, ok := session.CurrentChange()
		if !ok || current.State() != change.StatePlanned {
			t.Fatalf("failed workspace foundation Change=%#v available=%t", current, ok)
		}
		operations, err := store.ListOperations(current.ChangeId())
		if err != nil || len(operations) != 1 || operations[0].Kind != "proposal-workspace-create" || operations[0].State != authority.OperationFailed {
			t.Fatalf("workspace creation operations=%#v err=%v", operations, err)
		}
		worktrees := string(runCommandGitOutput(t, repositoryRoot, "worktree", "list", "--porcelain"))
		if strings.Count(worktrees, "worktree ") != 1 {
			t.Fatalf("failed foundation left an external worktree: %s", worktrees)
		}
		events := mustAuditHistory(t, store, current.ChangeId())
		if events[len(events)-1].EventType != audit.EventOperationRecovered {
			t.Fatalf("workspace compensation audit tail=%#v", events)
		}
		store.fail = false
	})

	t.Run("verification", func(t *testing.T) {
		var store *injectedCommitFailureStore
		provider := decisionCommandProvider(t, new(int))
		_, _, session, registry := prepareProviderCommandTestWithContainer(t, provider, &commandVerificationRunner{}, wrapAuthorityCommits(&store))
		if _, err := registry.Dispatch(session, `change isolate atomic-verification "atomic verification" --expected internal/service/service.go --protected go.mod`, io.Discard); err != nil {
			t.Fatal(err)
		}
		if _, err := registry.Dispatch(session, "change implement", io.Discard); err != nil {
			t.Fatal(err)
		}
		before, _ := session.CurrentChange()
		store.fail = true
		if _, err := registry.Dispatch(session, "change verify", io.Discard); err == nil || !strings.Contains(err.Error(), "injected authority commit failure") {
			t.Fatalf("verification commit error=%v", err)
		}
		assertDurableStateAndAbsentRoles(t, store, before, "verification-plan", "evidence-set", "policy-decision")
		for _, event := range mustAuditHistory(t, store, before.ChangeId()) {
			if event.EventType == audit.EventChangeTransition && event.Metadata["resulting_state"] == string(change.StateValidated) {
				t.Fatalf("failed verification published validated transition: %#v", event)
			}
		}
		store.fail = false
	})

	t.Run("human decision", func(t *testing.T) {
		var store *injectedCommitFailureStore
		_, _, session, registry, _ := prepareValidatedDecisionCommandTest(t, wrapAuthorityCommits(&store))
		before, _ := session.CurrentChange()
		store.fail = true
		if _, err := registry.Dispatch(session, "change approve", io.Discard); err == nil || !strings.Contains(err.Error(), "injected authority commit failure") {
			t.Fatalf("decision commit error=%v", err)
		}
		assertDurableStateAndAbsentRoles(t, store, before, "human-decision")
		for _, event := range mustAuditHistory(t, store, before.ChangeId()) {
			if event.EventType == approval.EventHumanDecisionRecorded {
				t.Fatalf("failed human decision published audit: %#v", event)
			}
		}
		store.fail = false
	})

	t.Run("canonical completion", func(t *testing.T) {
		var store *injectedCommitFailureStore
		repositoryRoot, _, session, registry, _ := prepareValidatedDecisionCommandTest(t, wrapAuthorityCommits(&store))
		if _, err := registry.Dispatch(session, "change approve", io.Discard); err != nil {
			t.Fatal(err)
		}
		before, _ := session.CurrentChange()
		store.fail = true
		if _, err := registry.Dispatch(session, "change apply", io.Discard); err == nil || !strings.Contains(err.Error(), "injected authority commit failure") {
			t.Fatalf("canonical completion commit error=%v", err)
		}
		assertDurableStateAndAbsentRoles(t, store, before, "application-result")
		operations, err := store.ListIncompleteOperations()
		if err != nil || len(operations) != 1 || operations[0].State != authority.OperationReserved {
			t.Fatalf("recoverable operation=%#v err=%v", operations, err)
		}
		contents, err := os.ReadFile(filepath.Join(repositoryRoot, "internal/service/service.go"))
		if err != nil || strings.Count(string(contents), "approved-candidate") != 1 {
			t.Fatalf("canonical mutation count/content=%q err=%v", contents, err)
		}
		store.fail = false
		if _, err := registry.Dispatch(session, "change recover "+string(operations[0].Id), io.Discard); err != nil {
			t.Fatalf("recover atomic completion: %v", err)
		}
		terminal, _, err := store.GetChange(before.ChangeId())
		if err != nil || terminal.State() != change.StateAuditLocked {
			t.Fatalf("recovered terminal=%#v err=%v", terminal, err)
		}
	})
}

func TestM11ReservedWorkspaceCreationRecoversAfterProcessEquivalentInterruption(t *testing.T) {
	var store *injectedCommitFailureStore
	var workspaceAdapter *gitproposal.Adapter
	repositoryRoot, _, first, registry := prepareProviderCommandTestWithContainer(
		t,
		decisionCommandProvider(t, new(int)),
		&commandVerificationRunner{},
		func(container *composition.Container) {
			workspaceAdapter = container.ProposalWorkspaces.(*gitproposal.Adapter)
			wrapAuthorityCommits(&store)(container)
		},
	)
	if _, err := registry.Dispatch(first, "change new orphan-workspace \"recover reserved worktree\" planned", io.Discard); err != nil {
		t.Fatal(err)
	}
	current, _ := first.CurrentChange()
	operationId, err := authority.GenerateOperationId()
	if err != nil {
		t.Fatal(err)
	}
	now := time.Now().UTC()
	operation := authority.Operation{
		Id:               operationId,
		ProjectId:        current.ProjectId(),
		ChangeId:         current.ChangeId(),
		Kind:             proposal.OperationProposalWorkspaceCreate,
		RequestDigest:    "sha256:" + strings.Repeat("a", 64),
		ExpectedRevision: current.Revision(),
		State:            authority.OperationReserved,
		CreatedAt:        now,
		UpdatedAt:        now,
	}
	if _, _, err := store.ReserveOperation(operation); err != nil {
		t.Fatal(err)
	}
	workspaceId := proposal.WorkspaceId("proposal-" + strings.TrimPrefix(string(operationId), "op-"))
	baseRevision := strings.TrimSpace(string(runCommandGitOutput(t, repositoryRoot, "rev-parse", "HEAD")))
	workspace, err := workspaceAdapter.Create(proposal.WorkspaceRequest{
		WorkspaceId:       workspaceId,
		ProjectId:         current.ProjectId(),
		ChangeId:          current.ChangeId(),
		CanonicalRoot:     repositoryRoot,
		BaseRevision:      baseRevision,
		SourceStateDigest: source.SourceStateDigest("sha256:" + strings.Repeat("b", 64)),
	})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(workspace.Root()); err != nil {
		t.Fatal(err)
	}

	restarted, err := composition.New().NewInteractiveSession(repositoryRoot)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restarted.Close() })
	restartedRegistry, err := command.DefaultRegistry()
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if _, err := restartedRegistry.Dispatch(restarted, "change recover "+string(operationId), &output); err != nil {
		t.Fatalf("recover reserved workspace: %v", err)
	}
	if !strings.Contains(output.String(), "workspace cleanup proven") {
		t.Fatalf("recovery output=%q", output.String())
	}
	if _, err := os.Stat(workspace.Root()); !os.IsNotExist(err) {
		t.Fatalf("orphan workspace remains after explicit recovery: %v", err)
	}
	recovered, err := store.GetOperation(operationId)
	if err != nil || recovered.State != authority.OperationFailed {
		t.Fatalf("recovered operation=%#v err=%v", recovered, err)
	}
	if incomplete, err := store.ListIncompleteOperations(); err != nil || len(incomplete) != 0 {
		t.Fatalf("incomplete operations after recovery=%#v err=%v", incomplete, err)
	}
	assertCommandCanonicalSourceUnchanged(t, repositoryRoot)
}

func TestM11WorkspaceRecoveryPreservesPersistedFoundationAfterCompletionFailure(t *testing.T) {
	var store *injectedOperationCompletionFailureStore
	repositoryRoot, _, first, registry := prepareProviderCommandTestWithContainer(
		t,
		decisionCommandProvider(t, new(int)),
		&commandVerificationRunner{},
		wrapOperationCompletion(&store),
	)
	store.fail = true
	_, isolateError := registry.Dispatch(first, "change isolate recover-foundation \"recover durable foundation\" --expected internal/service/service.go --protected go.mod", io.Discard)
	if isolateError == nil || !strings.Contains(isolateError.Error(), "injected operation completion failure") {
		t.Fatalf("workspace completion error=%v", isolateError)
	}
	current, _, err := store.GetChange(change.ChangeId("recover-foundation"))
	if err != nil || current.State() != change.StateIsolated {
		t.Fatalf("durable Change=%#v err=%v", current, err)
	}
	operations, err := store.ListIncompleteOperations()
	if err != nil || len(operations) != 1 || operations[0].Kind != proposal.OperationProposalWorkspaceCreate {
		t.Fatalf("incomplete workspace operation=%#v err=%v", operations, err)
	}
	bindings, err := store.ListBindings(current.ChangeId())
	if err != nil {
		t.Fatal(err)
	}
	var workspaceRoot string
	for _, binding := range bindings {
		if binding.Role != "source-snapshot" {
			continue
		}
		item, loadError := store.GetArtifact(current.ChangeId(), binding.ArtifactId, true)
		if loadError != nil {
			t.Fatal(loadError)
		}
		payload, decodeError := artifact.DecodeSourceSnapshotPayload(item.Payload())
		if decodeError != nil {
			t.Fatal(decodeError)
		}
		workspaceRoot = payload.WorkspaceRoot
	}
	if workspaceRoot == "" {
		t.Fatal("durable source foundation missing")
	}
	store.fail = false

	restarted, err := composition.New().NewInteractiveSession(repositoryRoot)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restarted.Close() })
	restartedRegistry, err := command.DefaultRegistry()
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if _, err := restartedRegistry.Dispatch(restarted, "change recover "+string(operations[0].Id), &output); err != nil {
		t.Fatalf("recover persisted workspace foundation: %v", err)
	}
	if !strings.Contains(output.String(), "workspace preserved") {
		t.Fatalf("recovery output=%q", output.String())
	}
	if _, err := os.Stat(workspaceRoot); err != nil {
		t.Fatalf("legitimate workspace was removed: %v", err)
	}
	recovered, err := store.GetOperation(operations[0].Id)
	if err != nil || recovered.State != authority.OperationCompleted {
		t.Fatalf("recovered operation=%#v err=%v", recovered, err)
	}
	if _, err := restartedRegistry.Dispatch(restarted, "change select recover-foundation", io.Discard); err != nil {
		t.Fatalf("hydrate recovered workspace: %v", err)
	}
	assertCommandCanonicalSourceUnchanged(t, repositoryRoot)
}

func TestM11RestartFailsClosedWhenDurableWorkspaceIsExternallyAbsent(t *testing.T) {
	var workspaceAdapter *gitproposal.Adapter
	repositoryRoot, _, first, registry := prepareProviderCommandTestWithContainer(
		t,
		decisionCommandProvider(t, new(int)),
		&commandVerificationRunner{},
		func(container *composition.Container) {
			workspaceAdapter = container.ProposalWorkspaces.(*gitproposal.Adapter)
		},
	)
	const changeId = "missing-durable-workspace"
	if _, err := registry.Dispatch(first, `change isolate `+changeId+` "prove absent workspace fail closed" --expected internal/service/service.go`, io.Discard); err != nil {
		t.Fatal(err)
	}
	currentProposal, ok := first.CurrentProposal()
	if !ok {
		t.Fatal("isolated proposal is missing")
	}
	workspace := currentProposal.Workspace()
	if err := workspaceAdapter.RemoveReservedWorkspace(workspace.WorkspaceId(), repositoryRoot); err != nil {
		t.Fatalf("simulate completed cleanup before evidence: %v", err)
	}
	if _, err := os.Stat(workspace.Root()); !os.IsNotExist(err) {
		t.Fatalf("workspace still exists after simulated cleanup: %v", err)
	}

	restarted, err := composition.New().NewInteractiveSession(repositoryRoot)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = restarted.Close() })
	restartedRegistry, err := command.DefaultRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restartedRegistry.Dispatch(restarted, "change select "+changeId, io.Discard); err == nil || !strings.Contains(err.Error(), "rehydrate durable ProposalWorkspace") {
		t.Fatalf("missing external workspace selection error = %v", err)
	}
	if _, ok := restarted.CurrentProposal(); ok {
		t.Fatal("absent external workspace was promoted into live proposal authority")
	}
	if _, err := workspaceAdapter.Create(proposal.WorkspaceRequest{
		WorkspaceId:       workspace.WorkspaceId(),
		ProjectId:         workspace.ProjectId(),
		ChangeId:          workspace.ChangeId(),
		CanonicalRoot:     workspace.CanonicalRoot(),
		BaseRevision:      workspace.BaseRevision(),
		SourceStateDigest: workspace.SourceStateDigest(),
	}); err != nil {
		t.Fatalf("restore test-owned workspace for fixture cleanup: %v", err)
	}
	assertCommandCanonicalSourceUnchanged(t, repositoryRoot)
}

func assertDurableStateAndAbsentRoles(t *testing.T, store authority.Store, before change.Change, absentRoles ...string) {
	t.Helper()
	current, _, err := store.GetChange(before.ChangeId())
	if err != nil {
		t.Fatal(err)
	}
	if current.State() != before.State() || current.Revision() != before.Revision() {
		t.Fatalf("failed authority commit changed state/revision from %s/%d to %s/%d", before.State(), before.Revision(), current.State(), current.Revision())
	}
	bindings, err := store.ListBindings(before.ChangeId())
	if err != nil {
		t.Fatal(err)
	}
	for _, binding := range bindings {
		for _, role := range absentRoles {
			if binding.Role == role {
				t.Fatalf("failed authority commit published %s binding: %#v", role, binding)
			}
		}
	}
}

func mustAuditHistory(t *testing.T, store authority.Store, id change.ChangeId) []audit.Event {
	t.Helper()
	events, err := store.AuditHistory(id)
	if err != nil {
		t.Fatal(err)
	}
	return events
}

func TestM11DurableChangeInspectionSurvivesSessionRestart(t *testing.T) {
	repositoryRoot, _, first, registry := prepareCommittedCommandTest(t)
	if _, err := registry.Dispatch(first, `change new durable-restart "survive process restart" planned`, io.Discard); err != nil {
		t.Fatal(err)
	}
	before, _ := first.CurrentChange()
	if before.Revision() != 2 {
		t.Fatalf("revision=%d", before.Revision())
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := composition.New().NewInteractiveSession(repositoryRoot)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Close() })
	secondRegistry, err := command.DefaultRegistry()
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if _, err := secondRegistry.Dispatch(second, "change list", &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "durable-restart state=planned revision=2") {
		t.Fatalf("list output=%q", output.String())
	}
	output.Reset()
	if _, err := secondRegistry.Dispatch(second, "change show durable-restart", &output); err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"State: planned", "Revision: 2", "Workflow: core-v0@1.0.0", "Audit events: 2"} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("show output lacks %q: %q", want, output.String())
		}
	}
	if _, err := secondRegistry.Dispatch(second, "change select durable-restart", io.Discard); err != nil {
		t.Fatal(err)
	}
	selected, ok := second.CurrentChange()
	if !ok || selected.ChangeId() != "durable-restart" || selected.Revision() != 2 {
		t.Fatalf("selected=%#v %t", selected, ok)
	}
}

func TestM11FreshProcessHydratesGovernedAuthorityAndContinues(t *testing.T) {
	if os.Getenv("PRAETOR_M11_RESTART_HELPER") == "1" {
		runM11RestartHelper(t, os.Getenv("PRAETOR_M11_RESTART_REPOSITORY"))
		os.Exit(0)
	}
	repositoryRoot := prepareM11Repository(t)
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "xdg"))
	t.Setenv("XDG_CACHE_HOME", filepath.Join(t.TempDir(), "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	process := exec.Command(executable, "-test.run=^TestM11FreshProcessHydratesGovernedAuthorityAndContinues$")
	process.Env = append(os.Environ(), "PRAETOR_M11_RESTART_HELPER=1", "PRAETOR_M11_RESTART_REPOSITORY="+repositoryRoot)
	if output, err := process.CombinedOutput(); err != nil {
		t.Fatalf("first runtime failed: %v: %s", err, output)
	}

	container := composition.New()
	container.AIProviders = []aiprovider.Provider{decisionCommandProvider(t, new(int))}
	container.ConfiguredProvider = "codex-cli"
	container.VerificationRunner = &commandVerificationRunner{}
	second, err := container.NewInteractiveSession(repositoryRoot)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Close() })
	registry, err := command.DefaultRegistry()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Dispatch(second, "change select restart-governed", io.Discard); err != nil {
		t.Fatalf("fresh runtime selection/hydration failed: %v", err)
	}
	if _, ok := second.CurrentProposal(); !ok {
		t.Fatal("fresh runtime lacks hydrated Proposal authority")
	}
	if _, ok := second.LastVerification(); !ok {
		t.Fatal("fresh runtime lacks hydrated Verification/Evidence authority")
	}
	if _, ok := second.LastPolicyDecision(); !ok {
		t.Fatal("fresh runtime lacks hydrated PolicyDecision authority")
	}
	if decision, ok := second.LastDecision(); !ok || decision.Kind() != approval.DecisionApprove {
		t.Fatalf("fresh runtime HumanDecision=%#v available=%t", decision, ok)
	}
	if _, err := registry.Dispatch(second, "change apply", io.Discard); err != nil {
		t.Fatalf("fresh runtime could not continue canonical application: %v", err)
	}
	terminal, _ := second.CurrentChange()
	if terminal.State() != change.StateAuditLocked {
		t.Fatalf("fresh runtime terminal state=%s", terminal.State())
	}
	contents, err := os.ReadFile(filepath.Join(repositoryRoot, "internal/service/service.go"))
	if err != nil || strings.Count(string(contents), "approved-candidate") != 1 {
		t.Fatalf("fresh runtime canonical result=%q err=%v", contents, err)
	}
}

func TestM11CompletedOperationCrashRecoveryAcrossProcess(t *testing.T) {
	if os.Getenv("PRAETOR_M11_COMPLETED_CRASH_HELPER") == "1" {
		runM11CompletedCrashHelper(t, os.Getenv("PRAETOR_M11_RESTART_REPOSITORY"), os.Getenv("PRAETOR_M11_OPERATION_FILE"), os.Getenv("PRAETOR_M11_LEAVE_RESERVED") == "1")
		os.Exit(0)
	}
	for _, test := range []struct {
		name                 string
		drift                bool
		corruptResult        string
		incompatibleWorkflow bool
	}{{name: "exact POST terminalizes without reapply"}, {name: "drift blocks terminalization", drift: true}, {name: "contradictory completed result blocks terminalization", corruptResult: "contradictory"}, {name: "malformed completed result blocks terminalization", corruptResult: "malformed"}, {name: "historical workflow blocks reserved POST recovery", incompatibleWorkflow: true}} {
		t.Run(test.name, func(t *testing.T) {
			repositoryRoot := prepareM11Repository(t)
			t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "xdg"))
			t.Setenv("XDG_CACHE_HOME", filepath.Join(t.TempDir(), "cache"))
			t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
			t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
			operationFile := filepath.Join(t.TempDir(), "operation-id")
			executable, err := os.Executable()
			if err != nil {
				t.Fatal(err)
			}
			process := exec.Command(executable, "-test.run=^TestM11CompletedOperationCrashRecoveryAcrossProcess$")
			process.Env = append(os.Environ(), "PRAETOR_M11_COMPLETED_CRASH_HELPER=1", "PRAETOR_M11_RESTART_REPOSITORY="+repositoryRoot, "PRAETOR_M11_OPERATION_FILE="+operationFile)
			if test.incompatibleWorkflow {
				process.Env = append(process.Env, "PRAETOR_M11_LEAVE_RESERVED=1")
			}
			if output, err := process.CombinedOutput(); err != nil {
				t.Fatalf("crash fixture process failed: %v: %s", err, output)
			}
			operationBytes, err := os.ReadFile(operationFile)
			if err != nil {
				t.Fatal(err)
			}
			operationId := strings.TrimSpace(string(operationBytes))
			if test.drift {
				if err := os.WriteFile(filepath.Join(repositoryRoot, "README.md"), []byte("# unrelated post-crash drift\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			container := composition.New()
			var restartedStore *injectedCommitFailureStore
			wrapAuthorityCommits(&restartedStore)(&container)
			container.AIProviders = []aiprovider.Provider{decisionCommandProvider(t, new(int))}
			container.ConfiguredProvider = "codex-cli"
			container.VerificationRunner = &commandVerificationRunner{}
			session, err := container.NewInteractiveSession(repositoryRoot)
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { _ = session.Close() })
			registry, _ := command.DefaultRegistry()
			var persistedResultBefore []byte
			var sourceBefore []byte
			if test.incompatibleWorkflow {
				historical := []byte("schema_version: 99\nworkflow_id: historical-v99\nworkflow_version: 9.0.0\nstates: []\ntransitions: []\n")
				sum := sha256.Sum256(historical)
				digest := "sha256:" + hex.EncodeToString(sum[:])
				database := restartedStore.Store.(*sqliteadapter.Store)
				raw, err := sql.Open("sqlite", database.Path())
				if err != nil {
					t.Fatal(err)
				}
				if _, err := raw.Exec(`INSERT INTO workflow_snapshots(workflow_digest,workflow_id,workflow_version,schema_version,exact_yaml) VALUES(?,?,?,?,?)`, digest, "historical-v99", "9.0.0", 99, historical); err != nil {
					raw.Close()
					t.Fatal(err)
				}
				if _, err := raw.Exec(`UPDATE changes SET workflow_digest=? WHERE change_id=?`, digest, "change-human-decision"); err != nil {
					raw.Close()
					t.Fatal(err)
				}
				if err := raw.Close(); err != nil {
					t.Fatal(err)
				}
				if err := restartedStore.ValidateAttachment(); err != nil {
					t.Fatalf("valid historical snapshot did not remain attachable: %v", err)
				}
				var inspected bytes.Buffer
				if _, err := registry.Dispatch(session, "change show change-human-decision", &inspected); err != nil || !strings.Contains(inspected.String(), "schema=99") || !strings.Contains(inspected.String(), digest) {
					t.Fatalf("historical Change inspection=%q err=%v", inspected.String(), err)
				}
				sourceBefore, err = os.ReadFile(filepath.Join(repositoryRoot, "internal/service/service.go"))
				if err != nil {
					t.Fatal(err)
				}
			}
			if test.corruptResult != "" {
				operation, err := restartedStore.GetOperation(authority.OperationId(operationId))
				if err != nil {
					t.Fatal(err)
				}
				var replacement []byte
				if test.corruptResult == "malformed" {
					replacement = []byte(`{"post":true}`)
				} else {
					result, err := authority.DecodeCanonicalResult(operation.Result)
					if err != nil {
						t.Fatal(err)
					}
					result.Patch = "sha256:" + strings.Repeat("f", 64)
					replacement, err = result.Encode()
					if err != nil {
						t.Fatal(err)
					}
				}
				database := restartedStore.Store.(*sqliteadapter.Store)
				raw, err := sql.Open("sqlite", database.Path())
				if err != nil {
					t.Fatal(err)
				}
				if _, err := raw.Exec(`UPDATE operations SET result=? WHERE operation_id=?`, replacement, operationId); err != nil {
					raw.Close()
					t.Fatal(err)
				}
				if err := raw.Close(); err != nil {
					t.Fatal(err)
				}
				persistedResultBefore = append([]byte(nil), replacement...)
				sourceBefore, err = os.ReadFile(filepath.Join(repositoryRoot, "internal/service/service.go"))
				if err != nil {
					t.Fatal(err)
				}
			}
			var unqualified bytes.Buffer
			if _, err := registry.Dispatch(session, "change diagnose", &unqualified); err != nil || !strings.Contains(unqualified.String(), "No Change selected") || strings.Contains(unqualified.String(), "clean") {
				t.Fatalf("unqualified diagnosis=%q err=%v", unqualified.String(), err)
			}
			var diagnosis bytes.Buffer
			if _, err := registry.Dispatch(session, "change diagnose change-human-decision", &diagnosis); err != nil {
				t.Fatal(err)
			}
			wantCondition := authority.RecoveryExternalUncertain
			if test.incompatibleWorkflow {
				wantCondition = authority.RecoveryIncomplete
			}
			if !strings.Contains(diagnosis.String(), string(wantCondition)) || !strings.Contains(diagnosis.String(), operationId) {
				t.Fatalf("completed crash diagnosis=%q", diagnosis.String())
			}
			if _, err := registry.Dispatch(session, "change select change-human-decision", io.Discard); err != nil {
				t.Fatal(err)
			}
			_, recoveryError := registry.Dispatch(session, "change recover "+operationId, io.Discard)
			if test.incompatibleWorkflow {
				if !errors.Is(recoveryError, authority.ErrIncompatible) {
					t.Fatalf("incompatible workflow recovery error=%v", recoveryError)
				}
				current, snapshot, err := restartedStore.GetChange("change-human-decision")
				if err != nil || current.State() != change.StateApproved || current.Revision() != 5 || snapshot.Executable() || snapshot.SchemaVersion() != 99 {
					t.Fatalf("incompatible recovery changed Change or workflow: %#v %#v %v", current, snapshot, err)
				}
				operation, err := restartedStore.GetOperation(authority.OperationId(operationId))
				if err != nil || operation.State != authority.OperationReserved || len(operation.Result) != 0 {
					t.Fatalf("incompatible recovery completed operation: %#v %v", operation, err)
				}
				bindings, err := restartedStore.ListBindings(current.ChangeId())
				if err != nil {
					t.Fatal(err)
				}
				for _, binding := range bindings {
					if binding.Role == "application-result" {
						t.Fatal("incompatible recovery published ApplicationResult")
					}
				}
				for _, event := range mustAuditHistory(t, restartedStore, current.ChangeId()) {
					if event.EventType == audit.EventOperationRecovered {
						t.Fatal("incompatible recovery recorded successful recovery")
					}
				}
				contents, err := os.ReadFile(filepath.Join(repositoryRoot, "internal/service/service.go"))
				if err != nil || !bytes.Equal(contents, sourceBefore) {
					t.Fatalf("incompatible recovery modified Git source: %q err=%v", contents, err)
				}
				return
			}
			if test.drift {
				if recoveryError == nil || !strings.Contains(recoveryError.Error(), "AMBIGUOUS") {
					t.Fatalf("drifted completed recovery error=%v", recoveryError)
				}
				current, _ := session.CurrentChange()
				if current.State() != change.StateApproved {
					t.Fatalf("drifted recovery changed state=%s", current.State())
				}
				return
			}
			if test.corruptResult != "" {
				if !errors.Is(recoveryError, authority.ErrCorrupt) {
					t.Fatalf("corrupt completed result recovery error=%v", recoveryError)
				}
				current, _, err := restartedStore.GetChange("change-human-decision")
				persistedOperation, operationError := restartedStore.GetOperation(authority.OperationId(operationId))
				if operationError != nil || !bytes.Equal(persistedOperation.Result, persistedResultBefore) {
					t.Fatalf("recovery changed completed operation result: %#v err=%v", persistedOperation, operationError)
				}
				if err != nil || current.State() != change.StateApproved {
					t.Fatalf("corrupt result advanced Change=%#v err=%v", current, err)
				}
				bindings, err := restartedStore.ListBindings(current.ChangeId())
				if err != nil {
					t.Fatal(err)
				}
				for _, binding := range bindings {
					if binding.Role == "application-result" {
						t.Fatal("corrupt result published ApplicationResult")
					}
				}
				for _, event := range mustAuditHistory(t, restartedStore, current.ChangeId()) {
					if event.EventType == audit.EventOperationRecovered {
						t.Fatal("corrupt result recorded successful recovery")
					}
				}
				contents, err := os.ReadFile(filepath.Join(repositoryRoot, "internal/service/service.go"))
				if err != nil || !bytes.Equal(contents, sourceBefore) {
					t.Fatalf("corrupt result reapplied source: %q err=%v", contents, err)
				}
				return
			}
			if recoveryError != nil {
				t.Fatalf("completed POST recovery failed: %v", recoveryError)
			}
			current, _ := session.CurrentChange()
			if current.State() != change.StateAuditLocked {
				t.Fatalf("completed POST recovery state=%s", current.State())
			}
			var artifactsOutput bytes.Buffer
			if _, err := registry.Dispatch(session, "change artifacts change-human-decision", &artifactsOutput); err != nil {
				t.Fatal(err)
			}
			if !strings.Contains(artifactsOutput.String(), "binding role=application-result") || !strings.Contains(artifactsOutput.String(), "state=completed") {
				t.Fatalf("recovered authority=%q", artifactsOutput.String())
			}
			contents, err := os.ReadFile(filepath.Join(repositoryRoot, "internal/service/service.go"))
			if err != nil || strings.Count(string(contents), "approved-candidate") != 1 {
				t.Fatalf("recovery reapplied canonical mutation: %q err=%v", contents, err)
			}
		})
	}
}

func runM11CompletedCrashHelper(t *testing.T, repositoryRoot, operationFile string, leaveReserved bool) {
	t.Helper()
	var store *injectedCommitFailureStore
	container := composition.New()
	container.AIProviders = []aiprovider.Provider{decisionCommandProvider(t, new(int))}
	container.ConfiguredProvider = "codex-cli"
	container.VerificationRunner = &commandVerificationRunner{}
	wrapAuthorityCommits(&store)(&container)
	session, err := container.NewInteractiveSession(repositoryRoot)
	if err != nil {
		t.Fatal(err)
	}
	registry, _ := command.DefaultRegistry()
	commands := []string{
		`change isolate change-human-decision "completed operation crash" --expected internal/service/service.go --protected go.mod`,
		"change implement",
		"change verify",
		"change approve",
	}
	for _, line := range commands {
		if _, err := registry.Dispatch(session, line, io.Discard); err != nil {
			t.Fatalf("%s: %v", line, err)
		}
	}
	store.fail = true
	if _, err := registry.Dispatch(session, "change apply", io.Discard); err == nil {
		t.Fatal("injected final authority failure did not expose crash boundary")
	}
	operations, err := store.ListIncompleteOperations()
	if err != nil || len(operations) != 1 {
		t.Fatalf("post-mutation reserved operations=%#v err=%v", operations, err)
	}
	operation := operations[0]
	store.fail = false
	currentProposal, ok := session.CurrentProposal()
	if !ok {
		t.Fatal("completed crash boundary lost proposal")
	}
	patch, ok := currentProposal.PatchArtifact()
	if !ok {
		t.Fatal("completed crash boundary lost patch")
	}
	if !leaveReserved {
		completedResult, err := (authority.CanonicalResult{Head: patch.BaseRevision(), Patch: patch.PatchDigest(), Paths: patch.ChangedPaths(), Index: true}).Encode()
		if err != nil {
			t.Fatal(err)
		}
		if _, err := store.CompleteOperation(operation.Id, operation.RequestDigest, completedResult, nil); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.WriteFile(operationFile, []byte(operation.Id), 0o600); err != nil {
		t.Fatal(err)
	}
	// os.Exit in the caller models death after the Git effect and before
	// any Change/application-result terminal authority commit.
}

func runM11RestartHelper(t *testing.T, repositoryRoot string) {
	t.Helper()
	container := composition.New()
	container.AIProviders = []aiprovider.Provider{decisionCommandProvider(t, new(int))}
	container.ConfiguredProvider = "codex-cli"
	container.VerificationRunner = &commandVerificationRunner{}
	first, err := container.NewInteractiveSession(repositoryRoot)
	if err != nil {
		t.Fatal(err)
	}
	registry, err := command.DefaultRegistry()
	if err != nil {
		t.Fatal(err)
	}
	commands := []string{
		`change isolate restart-governed "survive a real process restart" --expected internal/service/service.go --protected go.mod`,
		"change implement",
		"change verify",
		"change approve restart-authority",
	}
	for _, line := range commands {
		if _, err := registry.Dispatch(first, line, io.Discard); err != nil {
			t.Fatalf("%s: %v", line, err)
		}
	}
	// Intentionally do not call Session.Close: os.Exit below models abrupt
	// process death after durable approval while the worktree remains retained.
}

func prepareM11Repository(t *testing.T) string {
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
	for path, contents := range files {
		writeCommandFile(t, repositoryRoot, path, contents)
	}
	runCommandGit(t, repositoryRoot, "add", ".")
	runCommandGit(t, repositoryRoot, "-c", "user.name=Praetor Test", "-c", "user.email=praetor@example.invalid", "commit", "--quiet", "-m", "baseline")
	return repositoryRoot
}

func TestM11GovernedArtifactsAndDecisionSurviveRestart(t *testing.T) {
	repositoryRoot, _, first, registry, _ := prepareValidatedDecisionCommandTest(t, nil)
	if _, err := registry.Dispatch(first, "change approve durable-approval", io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := composition.New().NewInteractiveSession(repositoryRoot)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Close() })
	secondRegistry, _ := command.DefaultRegistry()
	var output bytes.Buffer
	if _, err := secondRegistry.Dispatch(second, "change artifacts change-human-decision", &output); err != nil {
		t.Fatal(err)
	}
	for _, role := range []string{"source-snapshot", "approved-scope", "patch", "verification-plan", "evidence-set", "policy-decision", "human-decision"} {
		if !strings.Contains(output.String(), "binding role="+role) {
			t.Fatalf("artifact output lacks %s: %q", role, output.String())
		}
	}
	output.Reset()
	if _, err := secondRegistry.Dispatch(second, "change history change-human-decision", &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "HUMAN_DECISION_RECORDED") || !strings.Contains(output.String(), "ARTIFACT_COMMITTED") {
		t.Fatalf("history=%q", output.String())
	}
}

func TestM11CanonicalOperationAndResultSurviveRestart(t *testing.T) {
	repositoryRoot, _, first, registry, _ := prepareValidatedDecisionCommandTest(t, nil)
	if _, err := registry.Dispatch(first, "change approve", io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Dispatch(first, "change apply", io.Discard); err != nil {
		t.Fatal(err)
	}
	if err := first.Close(); err != nil {
		t.Fatal(err)
	}
	second, err := composition.New().NewInteractiveSession(repositoryRoot)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Close() })
	secondRegistry, _ := command.DefaultRegistry()
	var output bytes.Buffer
	if _, err := secondRegistry.Dispatch(second, "change show change-human-decision", &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "State: audit-locked") || !strings.Contains(output.String(), "Revision: 6") {
		t.Fatalf("show=%q", output.String())
	}
	output.Reset()
	if _, err := secondRegistry.Dispatch(second, "change artifacts change-human-decision", &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "binding role=application-result") || !strings.Contains(output.String(), "state=completed") {
		t.Fatalf("artifacts=%q", output.String())
	}
}
