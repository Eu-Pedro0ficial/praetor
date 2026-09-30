package gitproposal_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Eu-Pedro0ficial/praetor/internal/adapters/gitproposal"
	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"github.com/Eu-Pedro0ficial/praetor/internal/integration"
	"github.com/Eu-Pedro0ficial/praetor/internal/project"
	"github.com/Eu-Pedro0ficial/praetor/internal/proposal"
	"github.com/Eu-Pedro0ficial/praetor/internal/repository"
	"github.com/Eu-Pedro0ficial/praetor/internal/source"
)

const testProjectId project.ProjectId = "01890f47-9f20-7cc1-98c8-0123456789ab"

func TestAdapterCreatesDetachedOwnedWorktreeWithoutCanonicalMutation(t *testing.T) {
	repositoryRoot, baseRevision := prepareGitRepository(t)
	temporaryRoot := t.TempDir()
	adapter, err := gitproposal.New(temporaryRoot)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	request := proposal.WorkspaceRequest{
		ProjectId:         testProjectId,
		ChangeId:          change.ChangeId("../../path-like/change"),
		CanonicalRoot:     repositoryRoot,
		BaseRevision:      baseRevision,
		SourceStateDigest: testSourceDigest(),
	}

	workspace, err := adapter.Create(request)
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	if workspace.ProjectId() != request.ProjectId || workspace.ChangeId() != request.ChangeId {
		t.Fatalf("workspace linkage = %#v", workspace)
	}
	if workspace.BaseRevision() != baseRevision || workspace.State() != proposal.WorkspaceActive {
		t.Fatalf("workspace base/state = %q/%q", workspace.BaseRevision(), workspace.State())
	}
	if relative, relativeError := filepath.Rel(repositoryRoot, workspace.Root()); relativeError != nil ||
		(relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))) {
		t.Fatalf("workspace %q is not outside canonical %q (relative %q, error %v)", workspace.Root(), repositoryRoot, relative, relativeError)
	}
	if relative, relativeError := filepath.Rel(temporaryRoot, workspace.Root()); relativeError != nil ||
		(relative == ".." || strings.HasPrefix(relative, ".."+string(filepath.Separator))) {
		t.Fatalf("workspace %q escaped temporary root %q", workspace.Root(), temporaryRoot)
	}
	if strings.Contains(workspace.Root(), "path-like") {
		t.Fatalf("ChangeId controlled workspace path %q", workspace.Root())
	}
	if got := strings.TrimSpace(string(runGit(t, workspace.Root(), "rev-parse", "HEAD"))); got != baseRevision {
		t.Fatalf("worktree HEAD = %q, want %q", got, baseRevision)
	}
	assertCleanCanonical(t, repositoryRoot, baseRevision)

	if err := adapter.Remove(workspace); err != nil {
		t.Fatalf("Remove() error = %v", err)
	}
	if _, err := os.Stat(workspace.Root()); !os.IsNotExist(err) {
		t.Fatalf("workspace remains after cleanup: %v", err)
	}
	assertCleanCanonical(t, repositoryRoot, baseRevision)
}

func TestReservedWorkspaceUsesDeterministicOwnedPathAndRestartCleanup(t *testing.T) {
	repositoryRoot, baseRevision := prepareGitRepository(t)
	temporaryRoot := t.TempDir()
	workspaceId := proposal.WorkspaceId("proposal-00112233445566778899aabbccddeeff")
	adapter, err := gitproposal.New(temporaryRoot)
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := adapter.Create(proposal.WorkspaceRequest{
		WorkspaceId:       workspaceId,
		ProjectId:         testProjectId,
		ChangeId:          change.ChangeId("reserved-workspace"),
		CanonicalRoot:     repositoryRoot,
		BaseRevision:      baseRevision,
		SourceStateDigest: testSourceDigest(),
	})
	if err != nil {
		t.Fatal(err)
	}
	wantRoot := filepath.Join(temporaryRoot, "praetor-proposal-00112233445566778899aabbccddeeff", "workspace")
	if workspace.Root() != wantRoot {
		t.Fatalf("reserved workspace root = %q, want %q", workspace.Root(), wantRoot)
	}
	restarted, err := gitproposal.New(temporaryRoot)
	if err != nil {
		t.Fatal(err)
	}
	if root, err := restarted.ReservedWorkspaceRoot(workspaceId); err != nil || root != wantRoot {
		t.Fatalf("ReservedWorkspaceRoot() = %q, %v", root, err)
	}
	if condition, err := restarted.ClassifyReservedWorkspace(workspaceId, repositoryRoot); err != nil || condition != proposal.WorkspaceReservationPresent {
		t.Fatalf("present classification = %s, %v", condition, err)
	}
	if err := restarted.VerifyReservedWorkspace(workspaceId, repositoryRoot, baseRevision); err != nil {
		t.Fatalf("VerifyReservedWorkspace() error = %v", err)
	}
	if err := restarted.VerifyReservedWorkspace(workspaceId, repositoryRoot, strings.Repeat("f", 40)); err == nil {
		t.Fatal("wrong reserved base revision was accepted")
	}
	if err := restarted.RemoveReservedWorkspace(workspaceId, repositoryRoot); err != nil {
		t.Fatal(err)
	}
	if condition, err := restarted.ClassifyReservedWorkspace(workspaceId, repositoryRoot); err != nil || condition != proposal.WorkspaceReservationAbsent {
		t.Fatalf("absent classification = %s, %v", condition, err)
	}
	assertCleanCanonical(t, repositoryRoot, baseRevision)
}

func TestReservedWorkspaceRecoveryPreservesUnknownExternalDirectory(t *testing.T) {
	repositoryRoot, _ := prepareGitRepository(t)
	temporaryRoot := t.TempDir()
	workspaceId := proposal.WorkspaceId("proposal-ffeeddccbbaa99887766554433221100")
	adapter, err := gitproposal.New(temporaryRoot)
	if err != nil {
		t.Fatal(err)
	}
	root, err := adapter.ReservedWorkspaceRoot(workspaceId)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(root, 0o700); err != nil {
		t.Fatal(err)
	}
	marker := filepath.Join(root, "owner-unknown")
	if err := os.WriteFile(marker, []byte("preserve"), 0o600); err != nil {
		t.Fatal(err)
	}
	if condition, err := adapter.ClassifyReservedWorkspace(workspaceId, repositoryRoot); err != nil || condition != proposal.WorkspaceReservationAmbiguous {
		t.Fatalf("unknown directory classification = %s, %v", condition, err)
	}
	if err := adapter.RemoveReservedWorkspace(workspaceId, repositoryRoot); err == nil {
		t.Fatal("ambiguous directory was accepted for cleanup")
	}
	if contents, err := os.ReadFile(marker); err != nil || string(contents) != "preserve" {
		t.Fatalf("unknown directory was modified: %q, %v", contents, err)
	}
}

func TestAdapterExtractsDeterministicGitPatches(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(*testing.T, string)
		wantPaths []string
		wantPatch []byte
	}{
		{
			name: "modified tracked file",
			mutate: func(t *testing.T, root string) {
				writeFile(t, root, "alpha.txt", "alpha changed\n")
			},
			wantPaths: []string{"alpha.txt"},
			wantPatch: []byte("alpha changed"),
		},
		{
			name: "added files with whitespace and control character",
			mutate: func(t *testing.T, root string) {
				writeFile(t, root, "new file.txt", "new\n")
				writeFile(t, root, "line\nbreak.txt", "control\n")
			},
			wantPaths: []string{"line\nbreak.txt", "new file.txt"},
			wantPatch: []byte("new file mode"),
		},
		{
			name: "deleted file",
			mutate: func(t *testing.T, root string) {
				if err := os.Remove(filepath.Join(root, "beta.txt")); err != nil {
					t.Fatalf("remove tracked file: %v", err)
				}
			},
			wantPaths: []string{"beta.txt"},
			wantPatch: []byte("deleted file mode"),
		},
		{
			name: "binary added file",
			mutate: func(t *testing.T, root string) {
				if err := os.WriteFile(filepath.Join(root, "asset.bin"), []byte{0x00, 0xff, 0x01, 0x02}, 0o600); err != nil {
					t.Fatalf("write binary proposal file: %v", err)
				}
			},
			wantPaths: []string{"asset.bin"},
			wantPatch: []byte("GIT binary patch"),
		},
		{
			name: "multiple files are sorted",
			mutate: func(t *testing.T, root string) {
				writeFile(t, root, "beta.txt", "beta changed\n")
				writeFile(t, root, "alpha.txt", "alpha changed\n")
			},
			wantPaths: []string{"alpha.txt", "beta.txt"},
			wantPatch: []byte("diff --git"),
		},
		{
			name:      "empty proposal",
			mutate:    func(*testing.T, string) {},
			wantPaths: nil,
			wantPatch: nil,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repositoryRoot, baseRevision := prepareGitRepository(t)
			adapter, err := gitproposal.New(t.TempDir())
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}
			workspace := createWorkspace(t, adapter, repositoryRoot, baseRevision, "patch-case")
			t.Cleanup(func() { _ = adapter.Remove(workspace) })
			test.mutate(t, workspace.Root())

			first, err := adapter.Extract(workspace)
			if err != nil {
				t.Fatalf("Extract() error = %v", err)
			}
			second, err := adapter.Extract(workspace)
			if err != nil {
				t.Fatalf("second Extract() error = %v", err)
			}
			if !reflect.DeepEqual(first.ChangedPaths, test.wantPaths) {
				t.Fatalf("ChangedPaths = %#v, want %#v", first.ChangedPaths, test.wantPaths)
			}
			if !reflect.DeepEqual(first.ChangedPaths, second.ChangedPaths) || !bytes.Equal(first.Content, second.Content) {
				t.Fatal("patch extraction is not deterministic")
			}
			if len(test.wantPatch) == 0 {
				if len(first.Content) != 0 {
					t.Fatalf("empty patch content = %q", first.Content)
				}
			} else if !bytes.Contains(first.Content, test.wantPatch) {
				t.Fatalf("patch lacks %q:\n%s", test.wantPatch, first.Content)
			}
			assertCleanCanonical(t, repositoryRoot, baseRevision)
		})
	}
}

func TestAdapterSecurelyReattachesDurableWorktreeAfterRestart(t *testing.T) {
	_, currentProposal, _, _ := retainedApplicationProposal(t, nil, func(t *testing.T, root string) {
		writeFile(t, root, "alpha.txt", "alpha after restart\n")
	}, []string{"alpha.txt"})
	workspace := currentProposal.Workspace()
	temporaryRoot := filepath.Dir(filepath.Dir(workspace.Root()))
	restarted, err := gitproposal.New(temporaryRoot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := restarted.Extract(workspace); err == nil || !strings.Contains(err.Error(), "not owned") {
		t.Fatalf("fresh adapter extracted before reattachment: %v", err)
	}
	if err := restarted.Reattach(workspace); err != nil {
		t.Fatalf("reattach durable worktree: %v", err)
	}
	extracted, err := restarted.Extract(workspace)
	if err != nil {
		t.Fatal(err)
	}
	patch, _ := currentProposal.PatchArtifact()
	if !bytes.Equal(extracted.Content, patch.Content()) || !reflect.DeepEqual(extracted.ChangedPaths, patch.ChangedPaths()) {
		t.Fatal("reattached workspace did not reproduce durable patch authority")
	}
}

func TestAdapterFailsSafelyAndCleansOnlyOwnedWorkspace(t *testing.T) {
	repositoryRoot, baseRevision := prepareGitRepository(t)
	temporaryRoot := t.TempDir()
	adapter, err := gitproposal.New(temporaryRoot)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	otherAdapter, err := gitproposal.New(temporaryRoot)
	if err != nil {
		t.Fatalf("other New() error = %v", err)
	}
	workspace := createWorkspace(t, adapter, repositoryRoot, baseRevision, "owned")
	sentinel := filepath.Join(temporaryRoot, "not-owned.txt")
	if err := os.WriteFile(sentinel, []byte("preserve"), 0o600); err != nil {
		t.Fatalf("write sentinel: %v", err)
	}
	if err := otherAdapter.Remove(workspace); err == nil || !strings.Contains(err.Error(), "not owned") {
		t.Fatalf("foreign Remove() error = %v", err)
	}
	if _, err := os.Stat(workspace.Root()); err != nil {
		t.Fatalf("foreign cleanup touched workspace: %v", err)
	}
	if err := adapter.Remove(workspace); err != nil {
		t.Fatalf("owned Remove() error = %v", err)
	}
	if contents, err := os.ReadFile(sentinel); err != nil || string(contents) != "preserve" {
		t.Fatalf("cleanup touched sentinel: %q, %v", contents, err)
	}

	_, err = adapter.Create(proposal.WorkspaceRequest{
		ProjectId:         testProjectId,
		ChangeId:          "bad-base",
		CanonicalRoot:     repositoryRoot,
		BaseRevision:      "not-a-revision",
		SourceStateDigest: testSourceDigest(),
	})
	if err == nil {
		t.Fatal("Create() accepted nonexistent base revision")
	}
	assertCleanCanonical(t, repositoryRoot, baseRevision)

	insideCanonicalAdapter, err := gitproposal.New(repositoryRoot)
	if err != nil {
		t.Fatalf("New(canonical root) error = %v", err)
	}
	_, err = insideCanonicalAdapter.Create(proposal.WorkspaceRequest{
		ProjectId:         testProjectId,
		ChangeId:          "inside-canonical",
		CanonicalRoot:     repositoryRoot,
		BaseRevision:      baseRevision,
		SourceStateDigest: testSourceDigest(),
	})
	if err == nil || !strings.Contains(err.Error(), "outside canonical") {
		t.Fatalf("inside-canonical Create() error = %v", err)
	}
	assertCleanCanonical(t, repositoryRoot, baseRevision)
}

func TestAdapterReportsPatchExtractionFailure(t *testing.T) {
	repositoryRoot, baseRevision := prepareGitRepository(t)
	adapter, err := gitproposal.New(t.TempDir())
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	workspace := createWorkspace(t, adapter, repositoryRoot, baseRevision, "extract-failure")
	if err := os.RemoveAll(workspace.Root()); err != nil {
		t.Fatalf("remove workspace fixture: %v", err)
	}
	if _, err := adapter.Extract(workspace); err == nil {
		t.Fatal("Extract() succeeded after worktree removal")
	}
	if err := adapter.Remove(workspace); err != nil {
		t.Fatalf("Remove() after missing worktree error = %v", err)
	}
	assertCleanCanonical(t, repositoryRoot, baseRevision)
}

func TestAdapterAppliesExactRetainedPatchWorkingTreeOnly(t *testing.T) {
	tests := []struct {
		name       string
		prepare    func(*testing.T, string)
		mutate     func(*testing.T, string)
		paths      []string
		wantStatus string
	}{
		{
			name: "modified file",
			mutate: func(t *testing.T, root string) {
				writeFile(t, root, "alpha.txt", "alpha changed\n")
			},
			paths:      []string{"alpha.txt"},
			wantStatus: " M alpha.txt\n",
		},
		{
			name: "deleted file",
			mutate: func(t *testing.T, root string) {
				if err := os.Remove(filepath.Join(root, "beta.txt")); err != nil {
					t.Fatal(err)
				}
			},
			paths:      []string{"beta.txt"},
			wantStatus: " D beta.txt\n",
		},
		{
			name: "modified binary file",
			prepare: func(t *testing.T, root string) {
				if err := os.WriteFile(filepath.Join(root, "asset.bin"), []byte{0x00, 0x01, 0x02}, 0o600); err != nil {
					t.Fatal(err)
				}
				runGit(t, root, "add", "asset.bin")
				runGit(t, root, "-c", "user.name=Praetor Test", "-c", "user.email=praetor@example.invalid", "commit", "--quiet", "-m", "binary baseline")
			},
			mutate: func(t *testing.T, root string) {
				if err := os.WriteFile(filepath.Join(root, "asset.bin"), []byte{0x00, 0xff, 0x02, 0x03}, 0o600); err != nil {
					t.Fatal(err)
				}
			},
			paths:      []string{"asset.bin"},
			wantStatus: " M asset.bin\n",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			adapter, currentProposal, repositoryRoot, baseRevision := retainedApplicationProposal(
				t,
				test.prepare,
				test.mutate,
				test.paths,
			)
			request := integration.ApplicationRequest{Proposal: currentProposal}
			beforeStatus := runGit(t, repositoryRoot, "status", "--porcelain=v1")
			if err := adapter.Preflight(request); err != nil {
				t.Fatalf("Preflight() error = %v", err)
			}
			if afterPreflight := runGit(t, repositoryRoot, "status", "--porcelain=v1"); !bytes.Equal(afterPreflight, beforeStatus) {
				t.Fatalf("preflight mutated canonical source: %q", afterPreflight)
			}

			proof, mutated, err := adapter.Apply(request)
			if err != nil {
				t.Fatalf("Apply() error = %v", err)
			}
			artifact, _ := currentProposal.PatchArtifact()
			if !mutated || proof.HeadRevision() != baseRevision || !proof.IndexUnchanged() ||
				proof.PatchDigest() != artifact.PatchDigest() || !reflect.DeepEqual(proof.ChangedPaths(), test.paths) {
				t.Fatalf("canonical proof = %#v, mutated=%t", proof, mutated)
			}
			if head := strings.TrimSpace(string(runGit(t, repositoryRoot, "rev-parse", "HEAD"))); head != baseRevision {
				t.Fatalf("HEAD = %q, want %q", head, baseRevision)
			}
			if staged := runGit(t, repositoryRoot, "diff", "--cached", "--quiet", "HEAD", "--"); len(staged) != 0 {
				t.Fatalf("unexpected staged output: %q", staged)
			}
			if status := string(runGit(t, repositoryRoot, "status", "--porcelain=v1")); status != test.wantStatus {
				t.Fatalf("canonical status = %q, want %q", status, test.wantStatus)
			}
			canonicalPatch := runGit(t, repositoryRoot,
				"-c", "core.quotePath=true", "diff", "--binary", "--full-index", "--no-color",
				"--no-ext-diff", "--no-renames", "--no-textconv", "--diff-algorithm=myers", baseRevision, "--",
			)
			if !bytes.Equal(canonicalPatch, artifact.Content()) {
				t.Fatal("canonical diff does not exactly equal retained PatchArtifact")
			}
			if commits := strings.TrimSpace(string(runGit(t, repositoryRoot, "rev-list", "--count", "HEAD"))); commits != "1" && test.prepare == nil {
				t.Fatalf("canonical application created a commit: count=%s", commits)
			}
			if _, err := os.Stat(filepath.Join(repositoryRoot, ".praetor")); !os.IsNotExist(err) {
				t.Fatalf("Praetor metadata appeared in canonical source: %v", err)
			}
			if matches, err := filepath.Glob(filepath.Join(repositoryRoot, "*.patch")); err != nil || len(matches) != 0 {
				t.Fatalf("patch tempfile appeared in canonical source: %v/%v", matches, err)
			}
		})
	}
}

func TestAdapterClassifiesDurableRecoveryWithoutProposalWorkspace(t *testing.T) {
	adapter, currentProposal, repositoryRoot, _ := retainedApplicationProposal(t, nil, func(t *testing.T, root string) {
		writeFile(t, root, "alpha.txt", "alpha recovered\n")
	}, []string{"alpha.txt"})
	patch, ok := currentProposal.PatchArtifact()
	if !ok {
		t.Fatal("fixture has no PatchArtifact")
	}
	snapshot := currentProposal.CanonicalSource()
	tracked := make([]string, 0, len(snapshot.TrackedPaths()))
	for _, path := range snapshot.TrackedPaths() {
		tracked = append(tracked, string(path))
	}
	recovery := integration.RecoveryRequest{ProjectId: testProjectId, ChangeId: currentProposal.Workspace().ChangeId(), RepositoryRoot: repositoryRoot, BaseRevision: patch.BaseRevision(), SourceStateDigest: string(patch.SourceStateDigest()), TrackedPaths: tracked, ChangedPaths: patch.ChangedPaths(), PatchDigest: patch.PatchDigest(), PatchContent: patch.Content()}
	if condition, err := adapter.ClassifyDurable(recovery); err != nil || condition != integration.ConditionPRE {
		t.Fatalf("initial classification = %s, %v", condition, err)
	}
	if _, mutated, err := adapter.Apply(integration.ApplicationRequest{Proposal: currentProposal}); err != nil || !mutated {
		t.Fatalf("Apply() mutated=%t error=%v", mutated, err)
	}
	if err := adapter.Remove(currentProposal.Workspace()); err != nil {
		t.Fatal(err)
	}
	if condition, err := adapter.ClassifyDurable(recovery); err != nil || condition != integration.ConditionPOST {
		t.Fatalf("restart classification = %s, %v", condition, err)
	}
	proof, err := adapter.PostProofDurable(recovery)
	if err != nil || proof.PatchDigest() != patch.PatchDigest() {
		t.Fatalf("durable proof=%#v error=%v", proof, err)
	}
	writeFile(t, repositoryRoot, "beta.txt", "unrelated drift\n")
	if condition, err := adapter.ClassifyDurable(recovery); err != nil || condition != integration.ConditionAmbiguous {
		t.Fatalf("drift classification = %s, %v", condition, err)
	}
}

func TestAdapterPreflightRejectsCanonicalAndProposalDriftWithoutMutation(t *testing.T) {
	tests := []struct {
		name   string
		drift  func(*testing.T, string, proposal.Proposal)
		needle string
	}{
		{
			name: "dirty tracked canonical",
			drift: func(t *testing.T, root string, _ proposal.Proposal) {
				writeFile(t, root, "beta.txt", "developer change\n")
			},
			needle: "working tree or index changed",
		},
		{
			name: "untracked canonical",
			drift: func(t *testing.T, root string, _ proposal.Proposal) {
				writeFile(t, root, "unexpected.txt", "developer file\n")
			},
			needle: "working tree or index changed",
		},
		{
			name: "staged canonical",
			drift: func(t *testing.T, root string, _ proposal.Proposal) {
				writeFile(t, root, "beta.txt", "staged\n")
				runGit(t, root, "add", "beta.txt")
			},
			needle: "working tree or index changed",
		},
		{
			name: "HEAD drift",
			drift: func(t *testing.T, root string, _ proposal.Proposal) {
				writeFile(t, root, "beta.txt", "new commit\n")
				runGit(t, root, "add", "beta.txt")
				runGit(t, root, "-c", "user.name=Praetor Test", "-c", "user.email=praetor@example.invalid", "commit", "--quiet", "-m", "drift")
			},
			needle: "HEAD changed",
		},
		{
			name: "proposal workspace drift",
			drift: func(t *testing.T, _ string, currentProposal proposal.Proposal) {
				writeFile(t, currentProposal.Workspace().Root(), "alpha.txt", "substituted\n")
			},
			needle: "retained proposal changed",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			adapter, currentProposal, repositoryRoot, _ := retainedApplicationProposal(t, nil, func(t *testing.T, root string) {
				writeFile(t, root, "alpha.txt", "approved\n")
			}, []string{"alpha.txt"})
			test.drift(t, repositoryRoot, currentProposal)
			statusBefore := runGit(t, repositoryRoot, "status", "--porcelain=v1", "--untracked-files=all")
			err := adapter.Preflight(integration.ApplicationRequest{Proposal: currentProposal})
			if err == nil || !strings.Contains(err.Error(), test.needle) {
				t.Fatalf("Preflight() error = %v, want containing %q", err, test.needle)
			}
			statusAfter := runGit(t, repositoryRoot, "status", "--porcelain=v1", "--untracked-files=all")
			if !bytes.Equal(statusAfter, statusBefore) {
				t.Fatalf("rejected preflight mutated canonical state: before=%q after=%q", statusBefore, statusAfter)
			}
		})
	}
}

func retainedApplicationProposal(
	t *testing.T,
	prepare func(*testing.T, string),
	mutate func(*testing.T, string),
	paths []string,
) (*gitproposal.Adapter, proposal.Proposal, string, string) {
	t.Helper()
	repositoryRoot, _ := prepareGitRepository(t)
	if prepare != nil {
		prepare(t, repositoryRoot)
	}
	snapshot, err := repository.Inspect(testProjectId, repositoryRoot)
	if err != nil {
		t.Fatal(err)
	}
	baseRevision := snapshot.HeadRevision()
	now := time.Date(2026, time.September, 6, 10, 0, 0, 0, time.UTC)
	currentChange, err := change.New("change-application", testProjectId, "apply exact patch", now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := currentChange.Transition(change.StatePlanned, now.Add(time.Second), "planned"); err != nil {
		t.Fatal(err)
	}
	analysis, err := source.AnalyzeImpact(currentChange, snapshot, source.ScopeRequest{Expected: paths})
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
		func() time.Time { return now.Add(2 * time.Second) },
	)
	if err != nil {
		t.Fatal(err)
	}
	currentProposal, err := proposalService.CreateWorkspace(currentChange, snapshot, scope)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = adapter.Remove(currentProposal.Workspace()) })
	mutate(t, currentProposal.Workspace().Root())
	currentProposal, validation, err := proposalService.ExtractPatch(currentProposal)
	if err != nil || !validation.Allowed() {
		t.Fatalf("ExtractPatch() = allowed %t, error %v", validation.Allowed(), err)
	}
	return adapter, currentProposal, repositoryRoot, baseRevision
}

func prepareGitRepository(t *testing.T) (string, string) {
	t.Helper()
	repositoryRoot := t.TempDir()
	runGit(t, repositoryRoot, "init", "--quiet")
	writeFile(t, repositoryRoot, "alpha.txt", "alpha\n")
	writeFile(t, repositoryRoot, "beta.txt", "beta\n")
	runGit(t, repositoryRoot, "add", ".")
	runGit(t, repositoryRoot, "-c", "user.name=Praetor Test", "-c", "user.email=praetor@example.invalid", "commit", "--quiet", "-m", "baseline")
	baseRevision := strings.TrimSpace(string(runGit(t, repositoryRoot, "rev-parse", "HEAD")))
	return repositoryRoot, baseRevision
}

func createWorkspace(t *testing.T, adapter *gitproposal.Adapter, repositoryRoot string, baseRevision string, changeId change.ChangeId) proposal.ProposalWorkspace {
	t.Helper()
	workspace, err := adapter.Create(proposal.WorkspaceRequest{
		ProjectId:         testProjectId,
		ChangeId:          changeId,
		CanonicalRoot:     repositoryRoot,
		BaseRevision:      baseRevision,
		SourceStateDigest: testSourceDigest(),
	})
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	return workspace
}

func testSourceDigest() source.SourceStateDigest {
	return source.SourceStateDigest("sha256:" + strings.Repeat("0", 64))
}

func writeFile(t *testing.T, root string, relativePath string, contents string) {
	t.Helper()
	absolutePath := filepath.Join(root, filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(absolutePath), 0o755); err != nil {
		t.Fatalf("create parent for %q: %v", relativePath, err)
	}
	if err := os.WriteFile(absolutePath, []byte(contents), 0o600); err != nil {
		t.Fatalf("write %q: %v", relativePath, err)
	}
}

func runGit(t *testing.T, repositoryRoot string, arguments ...string) []byte {
	t.Helper()
	commandArguments := append([]string{"-C", repositoryRoot}, arguments...)
	process := exec.Command("git", commandArguments...)
	output, err := process.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", arguments, err, output)
	}
	return output
}

func assertCleanCanonical(t *testing.T, repositoryRoot string, baseRevision string) {
	t.Helper()
	if got := strings.TrimSpace(string(runGit(t, repositoryRoot, "rev-parse", "HEAD"))); got != baseRevision {
		t.Fatalf("canonical HEAD = %q, want %q", got, baseRevision)
	}
	if status := runGit(t, repositoryRoot, "status", "--porcelain=v1", "--untracked-files=all"); len(status) != 0 {
		t.Fatalf("canonical repository changed: %s", status)
	}
	if _, err := os.Stat(filepath.Join(repositoryRoot, ".praetor")); !os.IsNotExist(err) {
		t.Fatalf("Praetor metadata appeared in canonical source: %v", err)
	}
}
