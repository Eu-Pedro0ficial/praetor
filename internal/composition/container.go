package composition

import (
	"fmt"
	"time"

	"github.com/Eu-Pedro0ficial/praetor/internal/audit"
	"github.com/Eu-Pedro0ficial/praetor/internal/project"
	"github.com/Eu-Pedro0ficial/praetor/internal/repository"
	"github.com/Eu-Pedro0ficial/praetor/internal/workflow"
)

// RepositoryDiscoveryFunc is the concrete dependency used by the C01 composition boundary.
type RepositoryDiscoveryFunc func(string) (repository.Context, error)

// ProjectRegistrationFunc is the concrete dependency used to ensure a local registration exists.
type ProjectRegistrationFunc func(string) (project.Registration, error)

// AuditLoggerFunc records a minimal M0.1 audit event in the local durable data directory.
type AuditLoggerFunc func(dataDirectory string, eventType string, projectID string, repositoryRoot string, metadata map[string]any) (audit.Event, error)

// ChangeAuditLoggerFunc records a Change lifecycle event with top-level audit linkage.
type ChangeAuditLoggerFunc func(dataDirectory string, eventType string, projectID string, changeID string, repositoryRoot string, metadata map[string]any) (audit.Event, error)

// Container assembles only the dependencies required by the current M0.2 runtime scope.
type Container struct {
	RepositoryDiscovery RepositoryDiscoveryFunc
	ProjectRegistration ProjectRegistrationFunc
	AuditLogger         AuditLoggerFunc
	ChangeAuditLogger   ChangeAuditLoggerFunc
	ChangeStore         *workflow.MemoryStore
	WorkflowClock       workflow.Clock
}

// New creates the explicit composition root for the current runtime boundary.
func New() Container {
	return Container{
		RepositoryDiscovery: repository.Discover,
		ProjectRegistration: project.EnsureRegistration,
		AuditLogger: func(dataDirectory string, eventType string, projectID string, repositoryRoot string, metadata map[string]any) (audit.Event, error) {
			return audit.Append(dataDirectory, eventType, projectID, repositoryRoot, metadata)
		},
		ChangeAuditLogger: func(dataDirectory string, eventType string, projectID string, changeID string, repositoryRoot string, metadata map[string]any) (audit.Event, error) {
			return audit.AppendChange(dataDirectory, eventType, projectID, changeID, repositoryRoot, metadata)
		},
		ChangeStore: workflow.NewMemoryStore(),
		WorkflowClock: func() time.Time {
			return time.Now().UTC()
		},
	}
}

// DiscoverRepository resolves the current repository context through the explicitly assembled dependency.
func (container Container) DiscoverRepository(path string) (repository.Context, error) {
	if container.RepositoryDiscovery == nil {
		return repository.Context{}, fmt.Errorf("repository discovery dependency is not configured")
	}
	return container.RepositoryDiscovery(path)
}

// EnsureProjectRegistration resolves the repository context and ensures a local project registration exists.
func (container Container) EnsureProjectRegistration(path string) (project.Registration, error) {
	if container.RepositoryDiscovery == nil {
		return project.Registration{}, fmt.Errorf("repository discovery dependency is not configured")
	}
	if container.ProjectRegistration == nil {
		return project.Registration{}, fmt.Errorf("project registration dependency is not configured")
	}

	repositoryContext, err := container.RepositoryDiscovery(path)
	if err != nil {
		return project.Registration{}, err
	}
	return container.ProjectRegistration(repositoryContext.Root)
}

// RecordEvent writes a single audit event for the current project registration.
func (container Container) RecordEvent(registration project.Registration, eventType string, metadata map[string]any) (audit.Event, error) {
	if container.AuditLogger == nil {
		return audit.Event{}, fmt.Errorf("audit logger dependency is not configured")
	}

	dataDirectory, err := audit.ResolveDataDir()
	if err != nil {
		return audit.Event{}, err
	}
	return container.AuditLogger(dataDirectory, eventType, string(registration.ProjectId), registration.RepositoryRoot, metadata)
}

// RecordInitialization persists the minimal initialization event for a successful runtime start.
func (container Container) RecordInitialization(registration project.Registration, metadata map[string]any) (audit.Event, error) {
	return container.RecordEvent(registration, audit.EventInitialization, metadata)
}

// RecordProjectAttach persists the project-registration event for the resolved project.
func (container Container) RecordProjectAttach(registration project.Registration, metadata map[string]any) (audit.Event, error) {
	return container.RecordEvent(registration, audit.EventProjectAttach, metadata)
}

// RecordConfiguration persists the minimal configuration resolution event.
func (container Container) RecordConfiguration(registration project.Registration, metadata map[string]any) (audit.Event, error) {
	return container.RecordEvent(registration, audit.EventConfiguration, metadata)
}

// NewChangeWorkflow assembles the minimal M0.2 application service for one
// registered repository runtime.
func (container Container) NewChangeWorkflow(registration project.Registration) (*workflow.Service, error) {
	if container.ChangeAuditLogger == nil {
		return nil, fmt.Errorf("Change audit logger dependency is not configured")
	}
	if container.ChangeStore == nil {
		return nil, fmt.Errorf("Change store dependency is not configured")
	}
	if container.WorkflowClock == nil {
		return nil, fmt.Errorf("workflow clock dependency is not configured")
	}

	dataDirectory, err := audit.ResolveDataDir()
	if err != nil {
		return nil, err
	}
	recorder := func(event workflow.LifecycleEvent) error {
		metadata := map[string]any{
			"resulting_state":      string(event.ResultingState),
			"transition_timestamp": event.OccurredAt.Format(time.RFC3339Nano),
			"context":              event.Context,
		}
		if event.PreviousState != "" {
			metadata["previous_state"] = string(event.PreviousState)
		}
		if event.Intent != "" {
			metadata["intent"] = string(event.Intent)
		}
		_, recordError := container.ChangeAuditLogger(
			dataDirectory,
			event.EventType,
			string(event.ProjectId),
			string(event.ChangeId),
			registration.RepositoryRoot,
			metadata,
		)
		return recordError
	}
	return workflow.New(
		container.ChangeStore,
		registration.ProjectId,
		recorder,
		container.WorkflowClock,
	)
}
