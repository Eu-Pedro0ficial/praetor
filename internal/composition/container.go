package composition

import (
	"fmt"
	"os"
	"time"

	"github.com/Eu-Pedro0ficial/praetor/internal/adapters/aiprovider/codexcli"
	"github.com/Eu-Pedro0ficial/praetor/internal/adapters/gitproposal"
	"github.com/Eu-Pedro0ficial/praetor/internal/aiprovider"
	"github.com/Eu-Pedro0ficial/praetor/internal/audit"
	"github.com/Eu-Pedro0ficial/praetor/internal/command"
	"github.com/Eu-Pedro0ficial/praetor/internal/execution"
	"github.com/Eu-Pedro0ficial/praetor/internal/intelligence"
	"github.com/Eu-Pedro0ficial/praetor/internal/project"
	"github.com/Eu-Pedro0ficial/praetor/internal/proposal"
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

// Container assembles only the dependencies required by the current M0.5 runtime scope.
type Container struct {
	RepositoryDiscovery  RepositoryDiscoveryFunc
	RepositoryInspection intelligence.RepositoryInspector
	ProjectRegistration  ProjectRegistrationFunc
	AuditLogger          AuditLoggerFunc
	ChangeAuditLogger    ChangeAuditLoggerFunc
	ChangeStore          *workflow.MemoryStore
	WorkflowClock        workflow.Clock
	ProposalWorkspaces   proposal.WorkspacePort
	PatchExtraction      proposal.PatchPort
	ProposalClock        proposal.Clock
	AIProviders          []aiprovider.Provider
	ConfiguredProvider   string
	ConfiguredModel      string
	ExecutionAttemptIds  execution.AttemptIdGenerator
	ExecutionClock       execution.Clock
}

// New creates the explicit composition root for the current runtime boundary.
func New() Container {
	gitProposalAdapter := gitproposal.NewDefault()
	configuredProvider := os.Getenv("PRAETOR_AI_PROVIDER")
	if configuredProvider == "" {
		configuredProvider = codexcli.Identifier
	}
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
		ProposalWorkspaces: gitProposalAdapter,
		PatchExtraction:    gitProposalAdapter,
		ProposalClock: func() time.Time {
			return time.Now().UTC()
		},
		AIProviders:         []aiprovider.Provider{codexcli.NewDefault()},
		ConfiguredProvider:  configuredProvider,
		ConfiguredModel:     os.Getenv("PRAETOR_AI_MODEL"),
		ExecutionAttemptIds: aiprovider.GenerateExecutionAttemptId,
		ExecutionClock: func() time.Time {
			return time.Now().UTC()
		},
	}
}

// NewInteractiveSession composes one retained shell session around the active
// Project and the current M0.1-M0.5 application capabilities.
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
	proposalLifecycle, err := container.NewProposalService(registration)
	if err != nil {
		return nil, err
	}
	providerRegistry, err := container.NewProviderRegistry()
	if err != nil {
		return nil, err
	}
	providerSelection, err := providerRegistry.Select(
		container.ConfiguredProvider,
		container.ConfiguredModel,
	)
	if err != nil {
		return nil, err
	}
	providerExecution, err := container.NewProviderExecutionService(
		registration,
		proposalLifecycle,
		providerRegistry,
	)
	if err != nil {
		return nil, err
	}
	session, err := command.NewSession(
		registration,
		changeWorkflow,
		repositoryIntelligence,
		proposalLifecycle,
		providerExecution,
		providerRegistry,
		providerSelection,
	)
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
		"command":     "interactive shell",
		"runtime":     "local",
		"directory":   "data",
		"ai_provider": string(providerSelection.ProviderIdentifier()),
		"ai_model":    selectedModelMetadata(providerSelection),
	}); err != nil {
		return nil, err
	}
	return session, nil
}

// NewProviderRegistry performs explicit compile-time adapter registration.
func (container Container) NewProviderRegistry() (*aiprovider.Registry, error) {
	if len(container.AIProviders) == 0 {
		return nil, fmt.Errorf("AI provider adapters are not configured")
	}
	return aiprovider.NewRegistry(container.AIProviders...)
}

// NewProviderExecutionService composes the provider-independent M0.5
// execution service around the existing ProposalWorkspace lifecycle.
func (container Container) NewProviderExecutionService(
	registration project.Registration,
	proposalLifecycle *proposal.Service,
	providerRegistry *aiprovider.Registry,
) (*execution.Service, error) {
	if container.ChangeAuditLogger == nil {
		return nil, fmt.Errorf("Change audit logger dependency is not configured")
	}
	if container.ExecutionAttemptIds == nil {
		return nil, fmt.Errorf("execution attempt identity dependency is not configured")
	}
	if container.ExecutionClock == nil {
		return nil, fmt.Errorf("provider execution clock dependency is not configured")
	}
	dataDirectory, err := audit.ResolveDataDir()
	if err != nil {
		return nil, err
	}
	recorder := func(event execution.LifecycleEvent) error {
		request := event.Request
		if request.ProjectId() != registration.ProjectId {
			return fmt.Errorf(
				"M0.5 event ProjectId %q does not match registered ProjectId %q",
				request.ProjectId(),
				registration.ProjectId,
			)
		}
		metadata, metadataError := providerExecutionMetadata(event)
		if metadataError != nil {
			return metadataError
		}
		_, recordError := container.ChangeAuditLogger(
			dataDirectory,
			event.EventType,
			string(request.ProjectId()),
			string(request.ChangeId()),
			registration.RepositoryRoot,
			metadata,
		)
		return recordError
	}
	return execution.New(
		providerRegistry,
		proposalLifecycle,
		recorder,
		container.ExecutionAttemptIds,
		container.ExecutionClock,
	)
}

// NewProposalService assembles M0.4 Git worktree isolation, deterministic
// patch extraction, canonical-source guarding, and audit provenance.
func (container Container) NewProposalService(registration project.Registration) (*proposal.Service, error) {
	if container.ProposalWorkspaces == nil {
		return nil, fmt.Errorf("proposal workspace dependency is not configured")
	}
	if container.PatchExtraction == nil {
		return nil, fmt.Errorf("patch extraction dependency is not configured")
	}
	if container.RepositoryInspection == nil {
		return nil, fmt.Errorf("repository inspection dependency is not configured")
	}
	if container.ChangeAuditLogger == nil {
		return nil, fmt.Errorf("Change audit logger dependency is not configured")
	}
	if container.ProposalClock == nil {
		return nil, fmt.Errorf("proposal clock dependency is not configured")
	}

	dataDirectory, err := audit.ResolveDataDir()
	if err != nil {
		return nil, err
	}
	recorder := func(event proposal.LifecycleEvent) error {
		if event.Workspace.ProjectId() != registration.ProjectId {
			return fmt.Errorf(
				"M0.4 event ProjectId %q does not match registered ProjectId %q",
				event.Workspace.ProjectId(),
				registration.ProjectId,
			)
		}
		metadata, metadataError := proposalLifecycleMetadata(event)
		if metadataError != nil {
			return metadataError
		}
		_, recordError := container.ChangeAuditLogger(
			dataDirectory,
			event.EventType,
			string(event.Workspace.ProjectId()),
			string(event.Workspace.ChangeId()),
			registration.RepositoryRoot,
			metadata,
		)
		return recordError
	}
	return proposal.New(
		container.ProposalWorkspaces,
		container.PatchExtraction,
		proposal.RepositoryInspector(container.RepositoryInspection),
		recorder,
		container.ProposalClock,
	)
}

func proposalLifecycleMetadata(event proposal.LifecycleEvent) (map[string]any, error) {
	workspace := event.Workspace
	metadata := map[string]any{
		"workspace_id":        string(workspace.WorkspaceId()),
		"workspace_state":     string(workspace.State()),
		"base_revision":       workspace.BaseRevision(),
		"source_state_digest": string(workspace.SourceStateDigest()),
		"disposition":         event.Disposition,
	}
	if event.Reason != "" {
		metadata["reason"] = event.Reason
	}
	if event.ExecutionAttemptId != "" {
		metadata["execution_attempt_id"] = event.ExecutionAttemptId
	}
	if event.HasArtifact {
		artifact := event.Artifact
		if artifact.WorkspaceId() != workspace.WorkspaceId() ||
			artifact.ProjectId() != workspace.ProjectId() ||
			artifact.ChangeId() != workspace.ChangeId() {
			return nil, fmt.Errorf("M0.4 patch artifact linkage is inconsistent")
		}
		metadata["patch_digest"] = artifact.PatchDigest()
		metadata["diff_summary"] = artifact.DiffSummary()
		metadata["changed_paths"] = artifact.ChangedPaths()
	}
	switch event.EventType {
	case proposal.EventProposalWorkspaceCreated,
		proposal.EventPatchExtracted,
		proposal.EventProposalWorkspaceDiscarded:
		return metadata, nil
	case proposal.EventPatchSurfaceValidated,
		proposal.EventPatchRejected:
		metadata["allowed"] = event.Validation.Allowed()
		metadata["expected_changes"] = repositoryPathStrings(event.Validation.ExpectedChanges())
		metadata["possible_changes"] = repositoryPathStrings(event.Validation.PossibleChanges())
		metadata["violations"] = violationMetadata(event.Validation.Violations())
		return metadata, nil
	default:
		return nil, fmt.Errorf("unknown M0.4 lifecycle event type %q", event.EventType)
	}
}

func providerExecutionMetadata(event execution.LifecycleEvent) (map[string]any, error) {
	request := event.Request
	descriptor := event.Descriptor
	selection := request.Selection()
	workspace := request.Workspace()
	if descriptor.Identifier() == "" || descriptor.Identifier() != selection.ProviderIdentifier() {
		return nil, fmt.Errorf("M0.5 provider descriptor and selection linkage is inconsistent")
	}
	if event.OccurredAt.IsZero() {
		return nil, fmt.Errorf("M0.5 provider execution event timestamp is required")
	}
	metadata := map[string]any{
		"execution_attempt_id":  string(request.AttemptId()),
		"workspace_id":          string(workspace.WorkspaceId()),
		"provider":              string(descriptor.Identifier()),
		"provider_vendor":       descriptor.Vendor(),
		"provider_display_name": descriptor.DisplayName(),
		"provider_capabilities": providerCapabilityStrings(descriptor.Capabilities()),
		"role":                  string(request.RoleContract().Role()),
		"base_revision":         request.BaseRevision(),
		"source_state_digest":   string(request.SourceStateDigest()),
		"event_timestamp":       event.OccurredAt.UTC().Format(time.RFC3339Nano),
	}
	if modelIdentifier, selected := selection.ModelIdentifier(); selected {
		metadata["model"] = string(modelIdentifier)
	}

	switch event.EventType {
	case execution.EventProviderExecutionStarted:
		metadata["disposition"] = "started"
	case execution.EventProviderExecutionCompleted:
		if !event.HasResponse ||
			event.Response.AttemptId() != request.AttemptId() ||
			event.Response.Selection().ProviderIdentifier() != selection.ProviderIdentifier() {
			return nil, fmt.Errorf("M0.5 completed response linkage is inconsistent")
		}
		response := event.Response
		metadata["disposition"] = string(response.Outcome())
		if response.ProviderVersion() != "" {
			metadata["provider_version"] = response.ProviderVersion()
		}
		if response.ExternalExecutionId() != "" {
			metadata["external_execution_id"] = response.ExternalExecutionId()
		}
		metadata["provider_started_at"] = response.StartedAt().Format(time.RFC3339Nano)
		metadata["provider_completed_at"] = response.CompletedAt().Format(time.RFC3339Nano)
		metadata["duration_milliseconds"] = response.CompletedAt().Sub(response.StartedAt()).Milliseconds()
		metadata["summary_present"] = response.Summary() != ""
		metadata["summary_truncated"] = response.SummaryTruncated()
		if response.Usage().Available() {
			metadata["usage"] = map[string]int64{
				"input_tokens":            response.Usage().InputTokens(),
				"cached_input_tokens":     response.Usage().CachedInputTokens(),
				"output_tokens":           response.Usage().OutputTokens(),
				"reasoning_output_tokens": response.Usage().ReasoningOutputTokens(),
			}
		}
	case execution.EventProviderExecutionFailed:
		if event.FailureKind == "" {
			return nil, fmt.Errorf("M0.5 failed execution requires a failure classification")
		}
		metadata["disposition"] = "failed"
		metadata["failure_kind"] = string(event.FailureKind)
		metadata["workspace_may_be_changed"] = event.WorkspaceMayBeChanged
		metadata["changed_paths"] = append([]string(nil), event.ChangedPaths...)
		if event.ExternalExecutionId != "" {
			metadata["external_execution_id"] = event.ExternalExecutionId
		}
	default:
		return nil, fmt.Errorf("unknown M0.5 lifecycle event type %q", event.EventType)
	}
	return metadata, nil
}

func providerCapabilityStrings(capabilities []aiprovider.ProviderCapability) []string {
	values := make([]string, len(capabilities))
	for index, capability := range capabilities {
		values[index] = string(capability)
	}
	return values
}

func selectedModelMetadata(selection aiprovider.Selection) string {
	if modelIdentifier, selected := selection.ModelIdentifier(); selected {
		return string(modelIdentifier)
	}
	return "provider-default"
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
