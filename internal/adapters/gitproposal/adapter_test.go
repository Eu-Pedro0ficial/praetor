package gitproposal_test

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Eu-Pedro0ficial/praetor/internal/adapters/gitproposal"
	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"github.com/Eu-Pedro0ficial/praetor/internal/project"
	"github.com/Eu-Pedro0ficial/praetor/internal/proposal"
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
