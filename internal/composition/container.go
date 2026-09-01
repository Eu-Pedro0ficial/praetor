package composition

import (
	"fmt"
	"time"

	"github.com/Eu-Pedro0ficial/praetor/internal/audit"
	"github.com/Eu-Pedro0ficial/praetor/internal/command"
	"github.com/Eu-Pedro0ficial/praetor/internal/intelligence"
	"github.com/Eu-Pedro0ficial/praetor/internal/project"
	"github.com/Eu-Pedro0ficial/praetor/internal/repository"
	"github.com/Eu-Pedro0ficial/praetor/internal/source"
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

// Container assembles only the dependencies required by the current M0.3 runtime scope.
type Container struct {
	RepositoryDiscovery  RepositoryDiscoveryFunc
	RepositoryInspection intelligence.RepositoryInspector
	ProjectRegistration  ProjectRegistrationFunc
	AuditLogger          AuditLoggerFunc
	ChangeAuditLogger    ChangeAuditLoggerFunc
	ChangeStore          *workflow.MemoryStore
	WorkflowClock        workflow.Clock
}

// New creates the explicit composition root for the current runtime boundary.
func New() Container {
	return Container{
		RepositoryDiscovery:  repository.Discover,
		RepositoryInspection: repository.Inspect,
		ProjectRegistration:  project.EnsureRegistration,
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

// NewInteractiveSession composes one retained shell session around the active
// Project and the current M0.1-M0.3 application capabilities.
func (container Container) NewInteractiveSession(path string) (*command.Session, error) {
	registration, err := container.EnsureProjectRegistration(path)
	if err != nil {
		return nil, err
	}
	changeWorkflow, err := container.NewChangeWorkflow(registration)
	if err != nil {
		return nil, err
	}
	repositoryIntelligence, err := container.NewRepositoryIntelligence(registration)
	if err != nil {
		return nil, err
	}
	session, err := command.NewSession(registration, changeWorkflow, repositoryIntelligence)
	if err != nil {
		return nil, err
	}

	if _, err := container.RecordInitialization(registration, map[string]any{
		"command": "interactive shell",
		"phase":   "startup",
	}); err != nil {
		return nil, err
	}
	if _, err := container.RecordProjectAttach(registration, map[string]any{
		"command":  "interactive shell",
		"attached": true,
	}); err != nil {
		return nil, err
	}
	if _, err := container.RecordConfiguration(registration, map[string]any{
		"command":   "interactive shell",
		"runtime":   "local",
		"directory": "data",
	}); err != nil {
		return nil, err
	}
	return session, nil
}

// NewRepositoryIntelligence assembles M0.3 repository inspection and bounded
// surface orchestration for one registered repository runtime.
func (container Container) NewRepositoryIntelligence(registration project.Registration) (*intelligence.Service, error) {
	if container.RepositoryInspection == nil {
		return nil, fmt.Errorf("repository inspection dependency is not configured")
	}
	if container.ChangeAuditLogger == nil {
		return nil, fmt.Errorf("Change audit logger dependency is not configured")
	}

	dataDirectory, err := audit.ResolveDataDir()
	if err != nil {
		return nil, err
	}
	recorder := func(event intelligence.LifecycleEvent) error {
		if event.ProjectId != registration.ProjectId {
			return fmt.Errorf(
				"M0.3 event ProjectId %q does not match registered ProjectId %q",
				event.ProjectId,
				registration.ProjectId,
			)
		}
		metadata, metadataError := repositoryIntelligenceMetadata(event)
		if metadataError != nil {
			return metadataError
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
	return intelligence.New(container.RepositoryInspection, recorder)
}

func repositoryIntelligenceMetadata(event intelligence.LifecycleEvent) (map[string]any, error) {
	switch event.EventType {
	case intelligence.EventSourceSnapshotCaptured:
		snapshot := event.Snapshot
		return map[string]any{
			"head_revision":       snapshot.HeadRevision(),
			"working_tree_state":  string(snapshot.WorkingTreeState()),
			"tracked_path_count":  len(snapshot.TrackedPaths()),
			"source_state_digest": string(snapshot.SourceStateDigest()),
		}, nil
	case intelligence.EventImpactAnalysisProduced:
		analysis := event.ImpactAnalysis
		return surfaceMetadata(
			analysis.SourceStateDigest(),
			analysis.CandidateSurface(),
		), nil
	case intelligence.EventChangeSurfaceEstablished:
		approvedScope := event.ApprovedScope
		metadata := surfaceMetadata(
			approvedScope.SourceStateDigest(),
			approvedScope.Surface(),
		)
		metadata["scope_status"] = "approved-for-m0.3-validation"
		return metadata, nil
	case intelligence.EventChangeSurfaceValidated,
		intelligence.EventChangeSurfaceViolation:
		approvedScope := event.ApprovedScope
		validation := event.Validation
		metadata := surfaceMetadata(
			approvedScope.SourceStateDigest(),
			approvedScope.Surface(),
		)
		metadata["allowed"] = validation.Allowed()
		metadata["actual_paths"] = validation.SuppliedPaths()
		metadata["expected_changes"] = repositoryPathStrings(validation.ExpectedChanges())
		metadata["possible_changes"] = repositoryPathStrings(validation.PossibleChanges())
		metadata["violations"] = violationMetadata(validation.Violations())
		return metadata, nil
	default:
		return nil, fmt.Errorf("unknown M0.3 lifecycle event type %q", event.EventType)
	}
}

func surfaceMetadata(digest source.SourceStateDigest, surface source.ChangeSurface) map[string]any {
	return map[string]any{
		"source_state_digest": string(digest),
		"expected_paths":      repositoryPathStrings(surface.ExpectedPaths()),
		"possible_paths":      repositoryPathStrings(surface.PossiblePaths()),
		"protected_paths":     repositoryPathStrings(surface.ProtectedPaths()),
	}
}

func repositoryPathStrings(paths []source.RepositoryPath) []string {
	values := make([]string, len(paths))
	for index, repositoryPath := range paths {
		values[index] = string(repositoryPath)
	}
	return values
}

func violationMetadata(violations []source.SurfaceViolation) []map[string]string {
	metadata := make([]map[string]string, len(violations))
	for index, violation := range violations {
		metadata[index] = map[string]string{
			"kind":   string(violation.Kind()),
			"path":   violation.Path(),
			"reason": violation.Reason(),
		}
	}
	return metadata
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
