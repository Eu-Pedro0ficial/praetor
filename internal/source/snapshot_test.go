package source_test

import (
	"testing"

	"github.com/Eu-Pedro0ficial/praetor/internal/project"
	"github.com/Eu-Pedro0ficial/praetor/internal/source"
)

const sourceProjectId project.ProjectId = "01890c29-7a78-7abc-8def-0123456789ab"

const sourceDigest source.SourceStateDigest = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestNewSourceSnapshotPreservesSourceFactsAndImmutableInventory(t *testing.T) {
	snapshot, err := source.NewSourceSnapshot(
		sourceProjectId,
		"/tmp/repository",
		"0123456789abcdef",
		source.WorkingTreeClean,
		[]string{"internal/z.go", "README.md", "internal/a.go", "README.md"},
		sourceDigest,
	)
	if err != nil {
		t.Fatalf("NewSourceSnapshot() error = %v", err)
	}
	if snapshot.ProjectId() != sourceProjectId || snapshot.RepositoryRoot() != "/tmp/repository" {
		t.Fatalf("snapshot association = project %q, root %q", snapshot.ProjectId(), snapshot.RepositoryRoot())
	}
	if snapshot.HeadRevision() != "0123456789abcdef" || snapshot.WorkingTreeState() != source.WorkingTreeClean {
		t.Fatalf("snapshot Git state = HEAD %q, tree %q", snapshot.HeadRevision(), snapshot.WorkingTreeState())
	}
	if snapshot.SourceStateDigest() != sourceDigest {
		t.Fatalf("SourceStateDigest() = %q", snapshot.SourceStateDigest())
	}
	wantPaths := []source.RepositoryPath{"README.md", "internal/a.go", "internal/z.go"}
	assertRepositoryPaths(t, snapshot.TrackedPaths(), wantPaths)

	returned := snapshot.TrackedPaths()
	returned[0] = "mutated"
	assertRepositoryPaths(t, snapshot.TrackedPaths(), wantPaths)
	if !snapshot.ContainsTrackedPath("internal/a.go") || snapshot.ContainsTrackedPath("missing.go") {
		t.Fatal("ContainsTrackedPath() returned inconsistent inventory membership")
	}
}

func TestNewSourceSnapshotRejectsInvalidFacts(t *testing.T) {
	tests := []struct {
		name             string
		projectId        project.ProjectId
		repositoryRoot   string
		headRevision     string
		workingTreeState source.WorkingTreeState
		trackedPaths     []string
		digest           source.SourceStateDigest
	}{
		{name: "ProjectId", repositoryRoot: "/tmp/repo", headRevision: "abc", workingTreeState: source.WorkingTreeClean, digest: sourceDigest},
		{name: "RepositoryRoot", projectId: sourceProjectId, headRevision: "abc", workingTreeState: source.WorkingTreeClean, digest: sourceDigest},
		{name: "HEAD", projectId: sourceProjectId, repositoryRoot: "/tmp/repo", workingTreeState: source.WorkingTreeClean, digest: sourceDigest},
		{name: "working tree state", projectId: sourceProjectId, repositoryRoot: "/tmp/repo", headRevision: "abc", workingTreeState: "unknown", digest: sourceDigest},
		{name: "tracked path", projectId: sourceProjectId, repositoryRoot: "/tmp/repo", headRevision: "abc", workingTreeState: source.WorkingTreeClean, trackedPaths: []string{"../escape"}, digest: sourceDigest},
		{name: "digest prefix", projectId: sourceProjectId, repositoryRoot: "/tmp/repo", headRevision: "abc", workingTreeState: source.WorkingTreeClean, digest: "0123"},
		{name: "digest length", projectId: sourceProjectId, repositoryRoot: "/tmp/repo", headRevision: "abc", workingTreeState: source.WorkingTreeClean, digest: "sha256:0123"},
		{name: "digest encoding", projectId: sourceProjectId, repositoryRoot: "/tmp/repo", headRevision: "abc", workingTreeState: source.WorkingTreeClean, digest: "sha256:zzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzzz"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := source.NewSourceSnapshot(
				test.projectId,
				test.repositoryRoot,
				test.headRevision,
				test.workingTreeState,
				test.trackedPaths,
				test.digest,
			); err == nil {
				t.Fatalf("NewSourceSnapshot() accepted invalid %s", test.name)
			}
		})
	}
}

func assertRepositoryPaths(t *testing.T, got []source.RepositoryPath, want []source.RepositoryPath) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("paths = %#v, want %#v", got, want)
	}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("paths = %#v, want %#v", got, want)
		}
	}
}
