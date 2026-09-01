package intelligence_test

import (
	"errors"
	"testing"
	"time"

	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"github.com/Eu-Pedro0ficial/praetor/internal/intelligence"
	"github.com/Eu-Pedro0ficial/praetor/internal/project"
	"github.com/Eu-Pedro0ficial/praetor/internal/source"
)

const intelligenceProjectId project.ProjectId = "01890c29-7a78-7abc-8def-0123456789ab"

const intelligenceDigest source.SourceStateDigest = "sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef"

func TestServiceEstablishesAuditableSurfaceWithoutChangingLifecycle(t *testing.T) {
	currentChange := intelligenceChange(t)
	snapshot := intelligenceSnapshot(t, "/tmp/repository")
	var events []intelligence.LifecycleEvent
	service, err := intelligence.New(
		func(projectId project.ProjectId, repositoryRoot string) (source.SourceSnapshot, error) {
			if projectId != intelligenceProjectId || repositoryRoot != "/tmp/repository" {
				t.Fatalf("inspector input = Project %q, root %q", projectId, repositoryRoot)
			}
			return snapshot, nil
		},
		func(event intelligence.LifecycleEvent) error {
			events = append(events, event)
			return nil
		},
	)
	if err != nil {
		t.Fatalf("intelligence.New() error = %v", err)
	}

	prepared, err := service.EstablishSurface(currentChange, "/tmp/repository", intelligenceScopeRequest())
	if err != nil {
		t.Fatalf("EstablishSurface() error = %v", err)
	}
	if currentChange.State() != change.StateCreated {
		t.Fatalf("repository intelligence changed M0.2 lifecycle state to %q", currentChange.State())
	}
	if prepared.Snapshot().SourceStateDigest() != intelligenceDigest {
		t.Fatalf("prepared SourceSnapshot digest = %q", prepared.Snapshot().SourceStateDigest())
	}
	if prepared.ImpactAnalysis().ChangeId() != currentChange.ChangeId() || prepared.ApprovedScope().ChangeId() != currentChange.ChangeId() {
		t.Fatal("prepared surface lost ChangeId linkage")
	}
	if len(events) != 3 {
		t.Fatalf("recorded %d establishment events, want 3", len(events))
	}
	wantTypes := []string{
		intelligence.EventSourceSnapshotCaptured,
		intelligence.EventImpactAnalysisProduced,
		intelligence.EventChangeSurfaceEstablished,
	}
	for index, event := range events {
		if event.EventType != wantTypes[index] || event.ChangeId != currentChange.ChangeId() || event.ProjectId != intelligenceProjectId {
			t.Fatalf("event %d = %#v", index, event)
		}
	}
}

func TestServiceRecordsAllowedAndRejectedSurfaceValidation(t *testing.T) {
	currentChange := intelligenceChange(t)
	var events []intelligence.LifecycleEvent
	service := intelligenceService(t, &events, nil)
	prepared, err := service.EstablishSurface(currentChange, "/tmp/repository", intelligenceScopeRequest())
	if err != nil {
		t.Fatalf("EstablishSurface() error = %v", err)
	}

	allowed, err := service.ValidateActualSurface(
		prepared.ApprovedScope(),
		[]string{"internal/service/service.go", "internal/service/service_test.go"},
	)
	if err != nil || !allowed.Allowed() {
		t.Fatalf("allowed validation = %#v, error = %v", allowed, err)
	}
	if events[len(events)-1].EventType != intelligence.EventChangeSurfaceValidated {
		t.Fatalf("allowed event type = %q", events[len(events)-1].EventType)
	}

	rejected, err := service.ValidateActualSurface(
		prepared.ApprovedScope(),
		[]string{"internal/service/service.go", "go.mod"},
	)
	var validationError *source.SurfaceValidationError
	if !errors.As(err, &validationError) || rejected.Allowed() {
		t.Fatalf("rejected validation = %#v, error = %v", rejected, err)
	}
	if events[len(events)-1].EventType != intelligence.EventChangeSurfaceViolation {
		t.Fatalf("rejected event type = %q", events[len(events)-1].EventType)
	}
	lastEvent := events[len(events)-1]
	if lastEvent.ChangeId != currentChange.ChangeId() || lastEvent.ProjectId != intelligenceProjectId {
		t.Fatalf("violation event identity linkage = %#v", lastEvent)
	}
	if len(lastEvent.Validation.Violations()) != 1 || lastEvent.Validation.Violations()[0].Path() != "go.mod" {
		t.Fatalf("violation event details = %#v", lastEvent.Validation.Violations())
	}
}

func TestServicePropagatesRecorderFailure(t *testing.T) {
	recorderFailure := errors.New("audit unavailable")
	currentChange := intelligenceChange(t)
	service := intelligenceService(t, nil, recorderFailure)
	if _, err := service.EstablishSurface(currentChange, "/tmp/repository", intelligenceScopeRequest()); !errors.Is(err, recorderFailure) {
		t.Fatalf("EstablishSurface() error = %v, want wrapped recorder failure", err)
	}
}

func TestServiceRejectsInspectorRootMismatch(t *testing.T) {
	service, err := intelligence.New(
		func(project.ProjectId, string) (source.SourceSnapshot, error) {
			return intelligenceSnapshot(t, "/tmp/other"), nil
		},
		func(intelligence.LifecycleEvent) error { return nil },
	)
	if err != nil {
		t.Fatalf("intelligence.New() error = %v", err)
	}
	if _, err := service.EstablishSurface(intelligenceChange(t), "/tmp/repository", intelligenceScopeRequest()); err == nil {
		t.Fatal("EstablishSurface() accepted mismatched RepositoryRoot")
	}
}

func TestNewServiceRequiresExplicitDependencies(t *testing.T) {
	inspector := func(project.ProjectId, string) (source.SourceSnapshot, error) {
		return source.SourceSnapshot{}, nil
	}
	recorder := func(intelligence.LifecycleEvent) error { return nil }
	if _, err := intelligence.New(nil, recorder); err == nil {
		t.Fatal("intelligence.New() accepted nil inspector")
	}
	if _, err := intelligence.New(inspector, nil); err == nil {
		t.Fatal("intelligence.New() accepted nil recorder")
	}
}

func intelligenceService(
	t *testing.T,
	events *[]intelligence.LifecycleEvent,
	recorderFailure error,
) *intelligence.Service {
	t.Helper()
	service, err := intelligence.New(
		func(project.ProjectId, string) (source.SourceSnapshot, error) {
			return intelligenceSnapshot(t, "/tmp/repository"), nil
		},
		func(event intelligence.LifecycleEvent) error {
			if recorderFailure != nil {
				return recorderFailure
			}
			if events != nil {
				*events = append(*events, event)
			}
			return nil
		},
	)
	if err != nil {
		t.Fatalf("intelligence.New() error = %v", err)
	}
	return service
}

func intelligenceChange(t *testing.T) change.Change {
	t.Helper()
	currentChange, err := change.New(
		"change-intelligence-test",
		intelligenceProjectId,
		"bound repository changes",
		time.Date(2026, time.August, 31, 12, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("change.New() error = %v", err)
	}
	return currentChange
}

func intelligenceSnapshot(t *testing.T, repositoryRoot string) source.SourceSnapshot {
	t.Helper()
	snapshot, err := source.NewSourceSnapshot(
		intelligenceProjectId,
		repositoryRoot,
		"0123456789abcdef",
		source.WorkingTreeClean,
		[]string{"go.mod", "internal/service/service.go", "internal/service/service_test.go"},
		intelligenceDigest,
	)
	if err != nil {
		t.Fatalf("source.NewSourceSnapshot() error = %v", err)
	}
	return snapshot
}

func intelligenceScopeRequest() source.ScopeRequest {
	return source.ScopeRequest{
		Expected:  []string{"internal/service/service.go"},
		Possible:  []string{"internal/service/service_test.go"},
		Protected: []string{"go.mod"},
	}
}
