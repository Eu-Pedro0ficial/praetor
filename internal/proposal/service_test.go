package proposal

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"github.com/Eu-Pedro0ficial/praetor/internal/project"
	"github.com/Eu-Pedro0ficial/praetor/internal/source"
)

const proposalTestProjectId project.ProjectId = "01890f47-9f20-7cc1-98c8-0123456789ab"

var proposalTestTime = time.Date(2026, 8, 31, 12, 0, 0, 0, time.UTC)

type fakeProposalAdapter struct {
	workspaceRoot string
	extracted     ExtractedPatch
	createError   error
	extractError  error
	removeError   error
	createCalls   int
	removeCalls   int
}

func (adapter *fakeProposalAdapter) Create(request WorkspaceRequest) (ProposalWorkspace, error) {
	adapter.createCalls++
	if adapter.createError != nil {
		return ProposalWorkspace{}, adapter.createError
	}
	return NewProposalWorkspace(
		"proposal-00112233445566778899aabbccddeeff",
		request.ProjectId,
		request.ChangeId,
		request.CanonicalRoot,
		adapter.workspaceRoot,
		request.BaseRevision,
		request.SourceStateDigest,
	)
}

func (adapter *fakeProposalAdapter) Extract(ProposalWorkspace) (ExtractedPatch, error) {
	if adapter.extractError != nil {
		return ExtractedPatch{}, adapter.extractError
	}
	return ExtractedPatch{
		Content:      append([]byte(nil), adapter.extracted.Content...),
		ChangedPaths: append([]string(nil), adapter.extracted.ChangedPaths...),
	}, nil
}

func (adapter *fakeProposalAdapter) Remove(ProposalWorkspace) error {
	adapter.removeCalls++
	return adapter.removeError
}

func TestServiceCreatesLinkedWorkspaceAndRecordsProvenance(t *testing.T) {
	fixture := newProposalFixture(t)
	currentProposal, err := fixture.service.CreateWorkspace(fixture.currentChange, fixture.snapshot, fixture.scope)
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}
	workspace := currentProposal.Workspace()
	if workspace.ProjectId() != fixture.currentChange.ProjectId() || workspace.ChangeId() != fixture.currentChange.ChangeId() {
		t.Fatalf("workspace linkage = %#v", workspace)
	}
	if workspace.BaseRevision() != fixture.snapshot.HeadRevision() ||
		workspace.SourceStateDigest() != fixture.snapshot.SourceStateDigest() ||
		currentProposal.ApprovedScope().SourceStateDigest() != fixture.snapshot.SourceStateDigest() {
		t.Fatal("proposal lost source/base/scope linkage")
	}
	if fixture.adapter.createCalls != 1 || fixture.adapter.removeCalls != 0 {
		t.Fatalf("adapter calls create/remove = %d/%d", fixture.adapter.createCalls, fixture.adapter.removeCalls)
	}
	if len(fixture.events) != 1 || fixture.events[0].EventType != EventProposalWorkspaceCreated {
		t.Fatalf("events = %#v", fixture.events)
	}
}

func TestServiceFailsClosedForDirtyOrDriftingCanonicalSource(t *testing.T) {
	t.Run("dirty source", func(t *testing.T) {
		fixture := newProposalFixture(t)
		dirtySnapshot := newSnapshot(t, fixture.canonicalRoot, source.WorkingTreeDirty, testDigest("1"))
		_, err := fixture.service.CreateWorkspace(fixture.currentChange, dirtySnapshot, approvedScope(t, fixture.currentChange, dirtySnapshot))
		var dirtyError DirtyCanonicalSourceError
		if !errors.As(err, &dirtyError) {
			t.Fatalf("dirty CreateWorkspace() error = %v", err)
		}
		if fixture.adapter.createCalls != 0 {
			t.Fatal("dirty source created a workspace")
		}
	})

	t.Run("drift during creation", func(t *testing.T) {
		fixture := newProposalFixture(t)
		inspectionCount := 0
		drifted := newSnapshot(t, fixture.canonicalRoot, source.WorkingTreeDirty, testDigest("2"))
		fixture.service.inspector = func(project.ProjectId, string) (source.SourceSnapshot, error) {
			inspectionCount++
			if inspectionCount == 1 {
				return fixture.snapshot, nil
			}
			return drifted, nil
		}
		_, err := fixture.service.CreateWorkspace(fixture.currentChange, fixture.snapshot, fixture.scope)
		var driftError CanonicalSourceDriftError
		if !errors.As(err, &driftError) {
			t.Fatalf("drifting CreateWorkspace() error = %v", err)
		}
		if fixture.adapter.removeCalls != 1 {
			t.Fatalf("drifting creation cleanup calls = %d", fixture.adapter.removeCalls)
		}
	})

	t.Run("external drift after mutation is not repaired", func(t *testing.T) {
		fixture := newProposalFixture(t)
		currentProposal := fixture.create(t)
		canonicalFile := filepath.Join(fixture.canonicalRoot, "service.go")
		if err := os.WriteFile(canonicalFile, []byte("external developer edit\n"), 0o600); err != nil {
			t.Fatalf("write external edit: %v", err)
		}
		drifted := newSnapshot(t, fixture.canonicalRoot, source.WorkingTreeDirty, testDigest("3"))
		fixture.service.inspector = func(project.ProjectId, string) (source.SourceSnapshot, error) {
			return drifted, nil
		}
		err := fixture.service.Mutate(currentProposal, func(ProposalWorkspace) error { return nil })
		var driftError CanonicalSourceDriftError
		if !errors.As(err, &driftError) {
			t.Fatalf("Mutate() drift error = %v", err)
		}
		contents, readError := os.ReadFile(canonicalFile)
		if readError != nil || string(contents) != "external developer edit\n" {
			t.Fatalf("canonical drift was repaired or overwritten: %q, %v", contents, readError)
		}
	})
}

func TestServiceMutationIsNarrowAndPropagatesFailure(t *testing.T) {
	fixture := newProposalFixture(t)
	currentProposal := fixture.create(t)
	wantError := errors.New("controlled mutation failed")
	var target string
	err := fixture.service.Mutate(currentProposal, func(workspace ProposalWorkspace) error {
		target = workspace.Root()
		return wantError
	})
	if !errors.Is(err, wantError) {
		t.Fatalf("Mutate() error = %v", err)
	}
	if target != fixture.workspaceRoot || target == fixture.canonicalRoot {
		t.Fatalf("mutation target = %q", target)
	}
}

func TestServiceClassifiesCompletePatchSurface(t *testing.T) {
	tests := []struct {
		name           string
		changedPaths   []string
		wantAllowed    bool
		wantState      WorkspaceState
		wantViolations []source.ViolationKind
	}{
		{name: "expected only", changedPaths: []string{"service.go"}, wantAllowed: true, wantState: WorkspaceRetained},
		{name: "possible only", changedPaths: []string{"service_test.go"}, wantAllowed: true, wantState: WorkspaceRetained},
		{name: "protected precedence", changedPaths: []string{"protected.txt"}, wantState: WorkspaceRejected, wantViolations: []source.ViolationKind{source.ViolationProtected}},
		{name: "unexpected", changedPaths: []string{"README.md"}, wantState: WorkspaceRejected, wantViolations: []source.ViolationKind{source.ViolationUnexpected}},
		{name: "mixed fails whole patch", changedPaths: []string{"service.go", "protected.txt"}, wantState: WorkspaceRejected, wantViolations: []source.ViolationKind{source.ViolationProtected}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			fixture := newProposalFixture(t)
			fixture.adapter.extracted = ExtractedPatch{
				Content:      []byte("deterministic patch\n"),
				ChangedPaths: test.changedPaths,
			}
			currentProposal := fixture.create(t)
			classified, validation, err := fixture.service.ExtractPatch(currentProposal)
			if test.wantAllowed && err != nil {
				t.Fatalf("ExtractPatch() error = %v", err)
			}
			if !test.wantAllowed {
				var surfaceError *source.SurfaceValidationError
				if !errors.As(err, &surfaceError) {
					t.Fatalf("ExtractPatch() error = %v", err)
				}
			}
			if validation.Allowed() != test.wantAllowed || classified.Workspace().State() != test.wantState {
				t.Fatalf("classification allowed/state = %t/%q", validation.Allowed(), classified.Workspace().State())
			}
			var kinds []source.ViolationKind
			for _, violation := range validation.Violations() {
				kinds = append(kinds, violation.Kind())
			}
			if !reflect.DeepEqual(kinds, test.wantViolations) {
				t.Fatalf("violation kinds = %#v, want %#v", kinds, test.wantViolations)
			}
			artifact, ok := classified.PatchArtifact()
			if !ok || !strings.HasPrefix(artifact.PatchDigest(), "sha256:") || artifact.DiffSummary() == "" {
				t.Fatalf("patch artifact = %#v, %t", artifact, ok)
			}
			paths := artifact.ChangedPaths()
			paths[0] = "mutated"
			if artifact.ChangedPaths()[0] == "mutated" {
				t.Fatal("PatchArtifact changed paths are not defensive")
			}
			if len(fixture.events) < 3 || fixture.events[1].EventType != EventPatchExtracted {
				t.Fatalf("patch events = %#v", fixture.events)
			}
			wantFinalEvent := EventPatchRejected
			if test.wantAllowed {
				wantFinalEvent = EventPatchSurfaceValidated
			}
			if fixture.events[len(fixture.events)-1].EventType != wantFinalEvent {
				t.Fatalf("final patch event = %#v", fixture.events[len(fixture.events)-1])
			}
		})
	}
}

func TestServiceRejectsEmptyPatch(t *testing.T) {
	fixture := newProposalFixture(t)
	classified, validation, err := fixture.service.ExtractPatch(fixture.create(t))
	var emptyError EmptyPatchError
	if !errors.As(err, &emptyError) {
		t.Fatalf("ExtractPatch() error = %v", err)
	}
	if validation.Allowed() || classified.Workspace().State() != WorkspaceRejected {
		t.Fatalf("empty classification = %t/%q", validation.Allowed(), classified.Workspace().State())
	}
	if _, ok := classified.PatchArtifact(); ok {
		t.Fatal("empty proposal has a patch artifact")
	}
	if fixture.events[len(fixture.events)-1].EventType != EventPatchRejected {
		t.Fatalf("empty patch events = %#v", fixture.events)
	}
}

func TestServiceFailurePathsRemainExplicit(t *testing.T) {
	t.Run("workspace creation", func(t *testing.T) {
		fixture := newProposalFixture(t)
		fixture.adapter.createError = errors.New("worktree unavailable")
		if _, err := fixture.service.CreateWorkspace(fixture.currentChange, fixture.snapshot, fixture.scope); err == nil || !strings.Contains(err.Error(), "worktree unavailable") {
			t.Fatalf("CreateWorkspace() error = %v", err)
		}
	})

	t.Run("patch extraction", func(t *testing.T) {
		fixture := newProposalFixture(t)
		currentProposal := fixture.create(t)
		fixture.adapter.extractError = errors.New("diff failed")
		classified, _, err := fixture.service.ExtractPatch(currentProposal)
		if err == nil || !strings.Contains(err.Error(), "diff failed") || classified.Workspace().State() != WorkspaceActive {
			t.Fatalf("ExtractPatch() classification/error = %q/%v", classified.Workspace().State(), err)
		}
	})

	t.Run("creation audit cleans workspace", func(t *testing.T) {
		fixture := newProposalFixture(t)
		fixture.service.recorder = func(event LifecycleEvent) error {
			if event.EventType == EventProposalWorkspaceCreated {
				return errors.New("audit unavailable")
			}
			return nil
		}
		if _, err := fixture.service.CreateWorkspace(fixture.currentChange, fixture.snapshot, fixture.scope); err == nil || !strings.Contains(err.Error(), "audit unavailable") {
			t.Fatalf("CreateWorkspace() error = %v", err)
		}
		if fixture.adapter.removeCalls != 1 {
			t.Fatalf("audit failure cleanup calls = %d", fixture.adapter.removeCalls)
		}
	})

	t.Run("cleanup failure preserves state", func(t *testing.T) {
		fixture := newProposalFixture(t)
		currentProposal := fixture.create(t)
		fixture.adapter.removeError = errors.New("worktree busy")
		classified, err := fixture.service.Discard(currentProposal, "test cleanup")
		if err == nil || !strings.Contains(err.Error(), "worktree busy") {
			t.Fatalf("Discard() error = %v", err)
		}
		if classified.Workspace().State() != WorkspaceActive {
			t.Fatalf("failed cleanup state = %q", classified.Workspace().State())
		}
	})
}

type proposalFixture struct {
	canonicalRoot string
	workspaceRoot string
	currentChange change.Change
	snapshot      source.SourceSnapshot
	scope         source.ApprovedScope
	adapter       *fakeProposalAdapter
	service       *Service
	events        []LifecycleEvent
}

func newProposalFixture(t *testing.T) *proposalFixture {
	t.Helper()
	canonicalRoot := t.TempDir()
	workspaceRoot := t.TempDir()
	for _, relativePath := range []string{"service.go", "service_test.go", "protected.txt", "README.md"} {
		if err := os.WriteFile(filepath.Join(canonicalRoot, relativePath), []byte(relativePath+"\n"), 0o600); err != nil {
			t.Fatalf("write fixture path: %v", err)
		}
	}
	currentChange := plannedChange(t, "proposal-change")
	snapshot := newSnapshot(t, canonicalRoot, source.WorkingTreeClean, testDigest("0"))
	scope := approvedScope(t, currentChange, snapshot)
	adapter := &fakeProposalAdapter{workspaceRoot: workspaceRoot}
	fixture := &proposalFixture{
		canonicalRoot: canonicalRoot,
		workspaceRoot: workspaceRoot,
		currentChange: currentChange,
		snapshot:      snapshot,
		scope:         scope,
		adapter:       adapter,
	}
	service, err := New(
		adapter,
		adapter,
		func(projectId project.ProjectId, repositoryRoot string) (source.SourceSnapshot, error) {
			if projectId != proposalTestProjectId || repositoryRoot != canonicalRoot {
				return source.SourceSnapshot{}, fmt.Errorf("unexpected inspection target")
			}
			return snapshot, nil
		},
		func(event LifecycleEvent) error {
			fixture.events = append(fixture.events, event)
			return nil
		},
		func() time.Time { return proposalTestTime.Add(time.Minute) },
	)
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	fixture.service = service
	return fixture
}

func (fixture *proposalFixture) create(t *testing.T) Proposal {
	t.Helper()
	currentProposal, err := fixture.service.CreateWorkspace(fixture.currentChange, fixture.snapshot, fixture.scope)
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}
	return currentProposal
}

func plannedChange(t *testing.T, changeId change.ChangeId) change.Change {
	t.Helper()
	currentChange, err := change.New(changeId, proposalTestProjectId, "test proposal", proposalTestTime)
	if err != nil {
		t.Fatalf("change.New() error = %v", err)
	}
	if _, err := currentChange.Transition(change.StatePlanned, proposalTestTime.Add(time.Second), "test planning"); err != nil {
		t.Fatalf("Change.Transition() error = %v", err)
	}
	return currentChange
}

func newSnapshot(t *testing.T, repositoryRoot string, state source.WorkingTreeState, digest source.SourceStateDigest) source.SourceSnapshot {
	t.Helper()
	snapshot, err := source.NewSourceSnapshot(
		proposalTestProjectId,
		repositoryRoot,
		strings.Repeat("a", 40),
		state,
		[]string{"README.md", "protected.txt", "service.go", "service_test.go"},
		digest,
	)
	if err != nil {
		t.Fatalf("NewSourceSnapshot() error = %v", err)
	}
	return snapshot
}

func approvedScope(t *testing.T, currentChange change.Change, snapshot source.SourceSnapshot) source.ApprovedScope {
	t.Helper()
	analysis, err := source.AnalyzeImpact(currentChange, snapshot, source.ScopeRequest{
		Expected:  []string{"service.go", "protected.txt"},
		Possible:  []string{"service_test.go", "protected.txt"},
		Protected: []string{"protected.txt"},
	})
	if err != nil {
		t.Fatalf("AnalyzeImpact() error = %v", err)
	}
	scope, err := source.EstablishApprovedScope(analysis)
	if err != nil {
		t.Fatalf("EstablishApprovedScope() error = %v", err)
	}
	return scope
}

func testDigest(digitCharacter string) source.SourceStateDigest {
	return source.SourceStateDigest("sha256:" + strings.Repeat(digitCharacter, 64))
}
