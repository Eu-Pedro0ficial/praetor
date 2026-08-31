package composition

import (
	"fmt"

	"github.com/Eu-Pedro0ficial/praetor/internal/audit"
	"github.com/Eu-Pedro0ficial/praetor/internal/project"
	"github.com/Eu-Pedro0ficial/praetor/internal/repository"
)

// RepositoryDiscoveryFunc is the concrete dependency used by the C01 composition boundary.
type RepositoryDiscoveryFunc func(string) (repository.Context, error)

// ProjectRegistrationFunc is the concrete dependency used to ensure a local registration exists.
type ProjectRegistrationFunc func(string) (project.Registration, error)

// AuditLoggerFunc records a minimal M0.1 audit event in the local durable data directory.
type AuditLoggerFunc func(dataDirectory string, eventType string, projectID string, repositoryRoot string, metadata map[string]any) (audit.Event, error)

// Container assembles only the dependencies required by the current M0.1 runtime scope.
type Container struct {
	RepositoryDiscovery RepositoryDiscoveryFunc
	ProjectRegistration ProjectRegistrationFunc
	AuditLogger         AuditLoggerFunc
}

// New creates the explicit composition root for the current runtime boundary.
func New() Container {
	return Container{
		RepositoryDiscovery: repository.Discover,
		ProjectRegistration: project.EnsureRegistration,
		AuditLogger: func(dataDirectory string, eventType string, projectID string, repositoryRoot string, metadata map[string]any) (audit.Event, error) {
			return audit.Append(dataDirectory, eventType, projectID, repositoryRoot, metadata)
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
