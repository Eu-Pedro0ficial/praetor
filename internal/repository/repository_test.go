package repository

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Eu-Pedro0ficial/praetor/internal/project"
	"github.com/Eu-Pedro0ficial/praetor/internal/source"
)

const repositoryTestProjectId project.ProjectId = "01890c29-7a78-7abc-8def-0123456789ab"

func TestDiscoverRepositoryRoot(t *testing.T) {
	repoDir := t.TempDir()
	if err := initGitRepo(repoDir); err != nil {
		t.Fatalf("init git repo: %v", err)
	}

	ctx, err := Discover(repoDir)
	if err != nil {
		t.Fatalf("Discover returned error for repo root: %v", err)
	}
	if ctx.Root != repoDir {
		t.Fatalf("expected repo root %q, got %q", repoDir, ctx.Root)
	}
}

func TestDiscoverRepositoryFromNestedDirectory(t *testing.T) {
	repoDir := t.TempDir()
	if err := initGitRepo(repoDir); err != nil {
		t.Fatalf("init git repo: %v", err)
	}

	nestedDir := filepath.Join(repoDir, "subdir", "deep")
	if err := os.MkdirAll(nestedDir, 0o755); err != nil {
		t.Fatalf("create nested dir: %v", err)
	}

	ctx, err := Discover(nestedDir)
	if err != nil {
		t.Fatalf("Discover returned error from nested directory: %v", err)
	}
	if ctx.Root != repoDir {
		t.Fatalf("expected repo root %q, got %q", repoDir, ctx.Root)
	}
}

func TestDiscoverOutsideRepository(t *testing.T) {
	outsideDir := t.TempDir()

	_, err := Discover(outsideDir)
	if !errors.Is(err, ErrNotGitRepository) {
		t.Fatalf("Discover() error = %v, want ErrNotGitRepository", err)
	}
	if strings.Contains(strings.ToLower(err.Error()), "fatal:") {
		t.Fatalf("Discover() leaked raw Git stderr: %v", err)
	}
}

func TestInspectCapturesHeadRootWorkingTreeAndDeterministicInventory(t *testing.T) {
	repositoryRoot := committedRepositoryFixture(t)
	nested := filepath.Join(repositoryRoot, "internal", "service")

	first, err := Inspect(repositoryTestProjectId, nested)
	if err != nil {
		t.Fatalf("Inspect() error = %v", err)
	}
	second, err := Inspect(repositoryTestProjectId, repositoryRoot)
	if err != nil {
		t.Fatalf("Inspect() repeat error = %v", err)
	}
	headRevision := strings.TrimSpace(runGitTest(t, repositoryRoot, "rev-parse", "HEAD"))
	if first.ProjectId() != repositoryTestProjectId || first.RepositoryRoot() != repositoryRoot {
		t.Fatalf("snapshot association = project %q, root %q", first.ProjectId(), first.RepositoryRoot())
	}
	if first.HeadRevision() != headRevision {
		t.Fatalf("HeadRevision() = %q, want %q", first.HeadRevision(), headRevision)
	}
	if first.WorkingTreeState() != source.WorkingTreeClean {
		t.Fatalf("WorkingTreeState() = %q", first.WorkingTreeState())
	}
	wantPaths := []source.RepositoryPath{
		"README.md",
		"cmd/app/main.go",
		"go.mod",
		"internal/service/service.go",
		"internal/service/service_test.go",
	}
	if !reflect.DeepEqual(first.TrackedPaths(), wantPaths) {
		t.Fatalf("TrackedPaths() = %#v, want %#v", first.TrackedPaths(), wantPaths)
	}
	if first.SourceStateDigest() != second.SourceStateDigest() {
		t.Fatalf("unchanged source produced different digests: %q != %q", first.SourceStateDigest(), second.SourceStateDigest())
	}
	if _, err := os.Stat(filepath.Join(repositoryRoot, ".praetor")); !os.IsNotExist(err) {
		t.Fatalf("Inspect() created source-tree metadata: %v", err)
	}
	if status := runGitTest(t, repositoryRoot, "status", "--short"); status != "" {
		t.Fatalf("Inspect() mutated governed repository: %s", status)
	}
}

func TestInspectSourceStateDigestChangesWithRelevantTrackedState(t *testing.T) {
	repositoryRoot := committedRepositoryFixture(t)
	cleanSnapshot, err := Inspect(repositoryTestProjectId, repositoryRoot)
	if err != nil {
		t.Fatalf("Inspect(clean) error = %v", err)
	}

	servicePath := filepath.Join(repositoryRoot, "internal", "service", "service.go")
	if err := os.WriteFile(servicePath, []byte("package service\n\nfunc Changed() {}\n"), 0o600); err != nil {
		t.Fatalf("modify tracked source: %v", err)
	}
	dirtySnapshot, err := Inspect(repositoryTestProjectId, repositoryRoot)
	if err != nil {
		t.Fatalf("Inspect(dirty) error = %v", err)
	}
	if dirtySnapshot.WorkingTreeState() != source.WorkingTreeDirty {
		t.Fatalf("dirty WorkingTreeState() = %q", dirtySnapshot.WorkingTreeState())
	}
	if dirtySnapshot.HeadRevision() != cleanSnapshot.HeadRevision() {
		t.Fatal("uncommitted source modification changed captured HEAD")
	}
	if dirtySnapshot.SourceStateDigest() == cleanSnapshot.SourceStateDigest() {
		t.Fatal("tracked source modification did not change SourceStateDigest")
	}

	if err := os.WriteFile(servicePath, []byte("package service\n\nfunc ChangedAgain() {}\n"), 0o600); err != nil {
		t.Fatalf("modify tracked source again: %v", err)
	}
	secondDirtySnapshot, err := Inspect(repositoryTestProjectId, repositoryRoot)
	if err != nil {
		t.Fatalf("Inspect(second dirty) error = %v", err)
	}
	if secondDirtySnapshot.SourceStateDigest() == dirtySnapshot.SourceStateDigest() {
		t.Fatal("different tracked content under the same dirty status produced the same SourceStateDigest")
	}
}

func TestInspectRepresentsUntrackedWorkingTreeStateWithoutAddingInventory(t *testing.T) {
	repositoryRoot := committedRepositoryFixture(t)
	cleanSnapshot, err := Inspect(repositoryTestProjectId, repositoryRoot)
	if err != nil {
		t.Fatalf("Inspect(clean) error = %v", err)
	}
	if err := os.WriteFile(filepath.Join(repositoryRoot, "untracked.txt"), []byte("not indexed\n"), 0o600); err != nil {
		t.Fatalf("write untracked file: %v", err)
	}

	dirtySnapshot, err := Inspect(repositoryTestProjectId, repositoryRoot)
	if err != nil {
		t.Fatalf("Inspect(untracked) error = %v", err)
	}
	if dirtySnapshot.WorkingTreeState() != source.WorkingTreeDirty {
		t.Fatalf("WorkingTreeState() = %q", dirtySnapshot.WorkingTreeState())
	}
	if !reflect.DeepEqual(dirtySnapshot.TrackedPaths(), cleanSnapshot.TrackedPaths()) {
		t.Fatalf("untracked file changed tracked inventory: %#v", dirtySnapshot.TrackedPaths())
	}
	if dirtySnapshot.SourceStateDigest() == cleanSnapshot.SourceStateDigest() {
		t.Fatal("untracked working-tree status did not change SourceStateDigest")
	}
}

func TestInspectChangesHeadAndInventoryAfterCommittedSourceChange(t *testing.T) {
	repositoryRoot := committedRepositoryFixture(t)
	before, err := Inspect(repositoryTestProjectId, repositoryRoot)
	if err != nil {
		t.Fatalf("Inspect(before) error = %v", err)
	}
	newPath := filepath.Join(repositoryRoot, "internal", "service", "new.go")
	if err := os.WriteFile(newPath, []byte("package service\n"), 0o600); err != nil {
		t.Fatalf("write new source file: %v", err)
	}
	runGitTest(t, repositoryRoot, "add", "internal/service/new.go")
	runGitTest(t, repositoryRoot, "-c", "user.name=Praetor Test", "-c", "user.email=praetor@example.invalid", "commit", "--quiet", "-m", "add source")

	after, err := Inspect(repositoryTestProjectId, repositoryRoot)
	if err != nil {
		t.Fatalf("Inspect(after) error = %v", err)
	}
	if after.HeadRevision() == before.HeadRevision() || after.SourceStateDigest() == before.SourceStateDigest() {
		t.Fatal("committed source change did not change HEAD and source-state digest")
	}
	if !after.ContainsTrackedPath("internal/service/new.go") {
		t.Fatalf("new tracked path missing from inventory: %#v", after.TrackedPaths())
	}
}

func TestInspectRequiresCommittedHead(t *testing.T) {
	repositoryRoot := t.TempDir()
	if err := initGitRepo(repositoryRoot); err != nil {
		t.Fatalf("init git repo: %v", err)
	}
	if _, err := Inspect(repositoryTestProjectId, repositoryRoot); err == nil || !strings.Contains(err.Error(), "HEAD") {
		t.Fatalf("Inspect() unborn HEAD error = %v", err)
	}
}

func initGitRepo(dir string) error {
	cmd := exec.Command("git", "-C", dir, "init")
	if _, err := cmd.CombinedOutput(); err != nil {
		return err
	}
	return nil
}

func committedRepositoryFixture(t *testing.T) string {
	t.Helper()
	repositoryRoot := t.TempDir()
	if err := initGitRepo(repositoryRoot); err != nil {
		t.Fatalf("init git repo: %v", err)
	}
	files := map[string]string{
		"cmd/app/main.go":                  "package main\n",
		"internal/service/service.go":      "package service\n",
		"internal/service/service_test.go": "package service_test\n",
		"go.mod":                           "module example.invalid/fixture\n\ngo 1.25.1\n",
		"README.md":                        "# Fixture\n",
	}
	for relativePath, contents := range files {
		absolutePath := filepath.Join(repositoryRoot, filepath.FromSlash(relativePath))
		if err := os.MkdirAll(filepath.Dir(absolutePath), 0o755); err != nil {
			t.Fatalf("create fixture directory: %v", err)
		}
		if err := os.WriteFile(absolutePath, []byte(contents), 0o600); err != nil {
			t.Fatalf("write fixture %q: %v", relativePath, err)
		}
	}
	runGitTest(t, repositoryRoot, "add", ".")
	runGitTest(t, repositoryRoot, "-c", "user.name=Praetor Test", "-c", "user.email=praetor@example.invalid", "commit", "--quiet", "-m", "baseline")
	return repositoryRoot
}

func runGitTest(t *testing.T, repositoryRoot string, arguments ...string) string {
	t.Helper()
	commandArguments := append([]string{"-C", repositoryRoot}, arguments...)
	command := exec.Command("git", commandArguments...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", arguments, err, output)
	}
	return strings.TrimSpace(string(output))
}
