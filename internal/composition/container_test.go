package composition

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/Eu-Pedro0ficial/praetor/internal/audit"
	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"github.com/Eu-Pedro0ficial/praetor/internal/project"
)

const compositionProjectId project.ProjectId = "01890c29-7a78-7abc-8def-0123456789ab"

func TestNewChangeWorkflowPersistsChangeAuditLinkage(t *testing.T) {
	xdgDataHome := filepath.Join(t.TempDir(), "xdg")
	t.Setenv("XDG_DATA_HOME", xdgDataHome)
	repositoryRoot := filepath.Join(t.TempDir(), "repository")
	registration := project.Registration{
		ProjectId:      compositionProjectId,
		RepositoryRoot: repositoryRoot,
		SchemaVersion:  1,
		CreatedAt:      time.Date(2026, time.August, 31, 12, 0, 0, 0, time.UTC),
	}

	container := New()
	nextTime := registration.CreatedAt
	container.WorkflowClock = func() time.Time {
		nextTime = nextTime.Add(time.Second)
		return nextTime
	}
	changeWorkflow, err := container.NewChangeWorkflow(registration)
	if err != nil {
		t.Fatalf("NewChangeWorkflow() error = %v", err)
	}
	createdChange, err := changeWorkflow.Create("change-composed", "prove composition", "developer request")
	if err != nil {
		t.Fatalf("Create() error = %v", err)
	}
	plannedChange, err := changeWorkflow.Transition("change-composed", change.StatePlanned, "planning established")
	if err != nil {
		t.Fatalf("Transition() error = %v", err)
	}

	dataDirectory, err := audit.ResolveDataDir()
	if err != nil {
		t.Fatalf("audit.ResolveDataDir() error = %v", err)
	}
	events, err := audit.Read(dataDirectory)
	if err != nil {
		t.Fatalf("audit.Read() error = %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("audit.Read() returned %d events, want 2", len(events))
	}

	createdEvent := events[0]
	if createdEvent.EventType != audit.EventChangeCreated || createdEvent.ChangeID != "change-composed" || createdEvent.ProjectID != string(compositionProjectId) {
		t.Fatalf("creation audit linkage = %#v", createdEvent)
	}
	if createdEvent.RepositoryRoot != repositoryRoot || createdEvent.Metadata["resulting_state"] != string(change.StateCreated) {
		t.Fatalf("creation audit context = %#v", createdEvent)
	}
	if createdEvent.Metadata["intent"] != "prove composition" || createdEvent.Metadata["context"] != "developer request" {
		t.Fatalf("creation audit provenance = %#v", createdEvent.Metadata)
	}
	if createdEvent.Metadata["transition_timestamp"] != createdChange.CreatedAt().Format(time.RFC3339Nano) {
		t.Fatalf("creation transition_timestamp = %#v", createdEvent.Metadata["transition_timestamp"])
	}

	transitionEvent := events[1]
	if transitionEvent.EventType != audit.EventChangeTransition || transitionEvent.ChangeID != "change-composed" || transitionEvent.ProjectID != string(compositionProjectId) {
		t.Fatalf("transition audit linkage = %#v", transitionEvent)
	}
	if transitionEvent.Metadata["previous_state"] != string(change.StateCreated) || transitionEvent.Metadata["resulting_state"] != string(change.StatePlanned) {
		t.Fatalf("transition audit state provenance = %#v", transitionEvent.Metadata)
	}
	if transitionEvent.Metadata["context"] != "planning established" || transitionEvent.Metadata["transition_timestamp"] != plannedChange.UpdatedAt().Format(time.RFC3339Nano) {
		t.Fatalf("transition audit context/time = %#v", transitionEvent.Metadata)
	}
}

func TestNewChangeWorkflowRequiresM02Dependencies(t *testing.T) {
	registration := project.Registration{
		ProjectId:      compositionProjectId,
		RepositoryRoot: "/tmp/repository",
	}
	tests := []struct {
		name   string
		mutate func(*Container)
	}{
		{name: "Change audit logger", mutate: func(container *Container) { container.ChangeAuditLogger = nil }},
		{name: "Change store", mutate: func(container *Container) { container.ChangeStore = nil }},
		{name: "workflow clock", mutate: func(container *Container) { container.WorkflowClock = nil }},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			container := New()
			test.mutate(&container)
			if _, err := container.NewChangeWorkflow(registration); err == nil {
				t.Fatalf("NewChangeWorkflow() accepted missing %s", test.name)
			}
		})
	}
}

func TestNewChangeWorkflowPropagatesAuditFailureWithoutStateMutation(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "xdg"))
	registration := project.Registration{
		ProjectId:      compositionProjectId,
		RepositoryRoot: "/tmp/repository",
	}
	auditFailure := errors.New("audit unavailable")
	container := New()
	container.ChangeAuditLogger = func(string, string, string, string, string, map[string]any) (audit.Event, error) {
		return audit.Event{}, auditFailure
	}

	changeWorkflow, err := container.NewChangeWorkflow(registration)
	if err != nil {
		t.Fatalf("NewChangeWorkflow() error = %v", err)
	}
	if _, err := changeWorkflow.Create("change-failed-audit", "test failure", "developer request"); !errors.Is(err, auditFailure) {
		t.Fatalf("Create() error = %v, want wrapped audit failure", err)
	}
	if _, err := changeWorkflow.Get("change-failed-audit"); err == nil {
		t.Fatal("audit-failed Change was committed to the store")
	}
}
