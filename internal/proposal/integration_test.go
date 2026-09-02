package proposal_test

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Eu-Pedro0ficial/praetor/internal/adapters/gitproposal"
	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"github.com/Eu-Pedro0ficial/praetor/internal/project"
	"github.com/Eu-Pedro0ficial/praetor/internal/proposal"
	"github.com/Eu-Pedro0ficial/praetor/internal/repository"
	"github.com/Eu-Pedro0ficial/praetor/internal/source"
)

const integrationProjectId project.ProjectId = "01890f47-9f20-7cc1-98c8-0123456789ab"

func TestRealGitProposalMutationTargetsOnlyIsolatedWorkspace(t *testing.T) {
	canonicalRoot := t.TempDir()
	runIntegrationGit(t, canonicalRoot, "init", "--quiet")
	canonicalPath := filepath.Join(canonicalRoot, "service.go")
	if err := os.WriteFile(canonicalPath, []byte("package service\n"), 0o600); err != nil {
		t.Fatalf("write canonical fixture: %v", err)
	}
	runIntegrationGit(t, canonicalRoot, "add", ".")
	runIntegrationGit(t, canonicalRoot, "-c", "user.name=Praetor Test", "-c", "user.email=praetor@example.invalid", "commit", "--quiet", "-m", "baseline")
	baseRevision := strings.TrimSpace(string(runIntegrationGit(t, canonicalRoot, "rev-parse", "HEAD")))

	currentChange, err := change.New("change-real-mutation", integrationProjectId, "prove isolated mutation", time.Now().UTC())
	if err != nil {
		t.Fatalf("change.New() error = %v", err)
	}
	if _, err := currentChange.Transition(change.StatePlanned, time.Now().UTC(), "planned"); err != nil {
		t.Fatalf("Change.Transition() error = %v", err)
	}
	snapshot, err := repository.Inspect(integrationProjectId, canonicalRoot)
	if err != nil {
		t.Fatalf("repository.Inspect() error = %v", err)
	}
	analysis, err := source.AnalyzeImpact(currentChange, snapshot, source.ScopeRequest{Expected: []string{"service.go"}})
	if err != nil {
		t.Fatalf("source.AnalyzeImpact() error = %v", err)
	}
	approvedScope, err := source.EstablishApprovedScope(analysis)
	if err != nil {
		t.Fatalf("source.EstablishApprovedScope() error = %v", err)
	}
	adapter, err := gitproposal.New(t.TempDir())
	if err != nil {
		t.Fatalf("gitproposal.New() error = %v", err)
	}
	var events []proposal.LifecycleEvent
	service, err := proposal.New(
		adapter,
		adapter,
		proposal.RepositoryInspector(repository.Inspect),
		func(event proposal.LifecycleEvent) error {
			events = append(events, event)
			return nil
		},
		func() time.Time { return time.Now().UTC() },
	)
	if err != nil {
		t.Fatalf("proposal.New() error = %v", err)
	}
	currentProposal, err := service.CreateWorkspace(currentChange, snapshot, approvedScope)
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}
	workspaceRoot := currentProposal.Workspace().Root()
	if err := service.Mutate(currentProposal, func(workspace proposal.ProposalWorkspace) error {
		if workspace.Root() == canonicalRoot {
			t.Fatal("mutation callback received canonical repository")
		}
		return os.WriteFile(filepath.Join(workspace.Root(), "service.go"), []byte("package service\n\nconst Isolated = true\n"), 0o600)
	}); err != nil {
		t.Fatalf("Mutate() error = %v", err)
	}
	canonicalContents, err := os.ReadFile(canonicalPath)
	if err != nil || string(canonicalContents) != "package service\n" {
		t.Fatalf("canonical source changed: %q, %v", canonicalContents, err)
	}
	currentProposal, validation, err := service.ExtractPatch(currentProposal)
	if err != nil || !validation.Allowed() || currentProposal.Workspace().State() != proposal.WorkspaceRetained {
		t.Fatalf("ExtractPatch() classification/error = %t/%q/%v", validation.Allowed(), currentProposal.Workspace().State(), err)
	}
	artifact, ok := currentProposal.PatchArtifact()
	if !ok || len(artifact.ChangedPaths()) != 1 || artifact.ChangedPaths()[0] != "service.go" {
		t.Fatalf("PatchArtifact = %#v, %t", artifact, ok)
	}
	currentProposal, err = service.Discard(currentProposal, "integration proof complete")
	if err != nil || currentProposal.Workspace().State() != proposal.WorkspaceCleaned {
		t.Fatalf("Discard() state/error = %q/%v", currentProposal.Workspace().State(), err)
	}
	if _, err := os.Stat(workspaceRoot); !os.IsNotExist(err) {
		t.Fatalf("workspace remains after discard: %v", err)
	}
	if head := strings.TrimSpace(string(runIntegrationGit(t, canonicalRoot, "rev-parse", "HEAD"))); head != baseRevision {
		t.Fatalf("canonical HEAD moved: %q", head)
	}
	if status := runIntegrationGit(t, canonicalRoot, "status", "--porcelain=v1", "--untracked-files=all"); len(status) != 0 {
		t.Fatalf("canonical source is dirty: %s", status)
	}
	if len(events) != 4 || events[0].EventType != proposal.EventProposalWorkspaceCreated ||
		events[1].EventType != proposal.EventPatchExtracted ||
		events[2].EventType != proposal.EventPatchSurfaceValidated ||
		events[3].EventType != proposal.EventProposalWorkspaceDiscarded {
		t.Fatalf("proposal lifecycle events = %#v", events)
	}
}

func runIntegrationGit(t *testing.T, repositoryRoot string, arguments ...string) []byte {
	t.Helper()
	commandArguments := append([]string{"-C", repositoryRoot}, arguments...)
	process := exec.Command("git", commandArguments...)
	output, err := process.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", arguments, err, output)
	}
	return output
}
