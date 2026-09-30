package composition

import (
	"fmt"
	"os"
	"reflect"
	"time"

	"github.com/Eu-Pedro0ficial/praetor/internal/adapters/aiprovider/codexcli"
	"github.com/Eu-Pedro0ficial/praetor/internal/adapters/gitproposal"
	modelcache "github.com/Eu-Pedro0ficial/praetor/internal/adapters/persistence/modelcache"
	sqliteadapter "github.com/Eu-Pedro0ficial/praetor/internal/adapters/persistence/sqlite"
	projectpolicy "github.com/Eu-Pedro0ficial/praetor/internal/adapters/policy/project"
	"github.com/Eu-Pedro0ficial/praetor/internal/adapters/repositoryanalysis"
	"github.com/Eu-Pedro0ficial/praetor/internal/adapters/verification/localexec"
	"github.com/Eu-Pedro0ficial/praetor/internal/aiprovider"
	"github.com/Eu-Pedro0ficial/praetor/internal/approval"
	"github.com/Eu-Pedro0ficial/praetor/internal/audit"
	"github.com/Eu-Pedro0ficial/praetor/internal/authority"
	"github.com/Eu-Pedro0ficial/praetor/internal/command"
	"github.com/Eu-Pedro0ficial/praetor/internal/execution"
	"github.com/Eu-Pedro0ficial/praetor/internal/impact"
	"github.com/Eu-Pedro0ficial/praetor/internal/inspection"
	"github.com/Eu-Pedro0ficial/praetor/internal/integration"
	"github.com/Eu-Pedro0ficial/praetor/internal/intelligence"
	"github.com/Eu-Pedro0ficial/praetor/internal/policy"
	"github.com/Eu-Pedro0ficial/praetor/internal/presentation/preferences"
	"github.com/Eu-Pedro0ficial/praetor/internal/project"
	"github.com/Eu-Pedro0ficial/praetor/internal/proposal"
	"github.com/Eu-Pedro0ficial/praetor/internal/repository"
	"github.com/Eu-Pedro0ficial/praetor/internal/repositorymodel"
	"github.com/Eu-Pedro0ficial/praetor/internal/source"
	"github.com/Eu-Pedro0ficial/praetor/internal/verification"
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

// PresentationPreferencesFunc loads user-local rendering preferences.
type PresentationPreferencesFunc func() (*preferences.Service, error)

// DurableStoreFactoryFunc opens one per-Project M1.1 authority store.
type DurableStoreFactoryFunc func(string, project.Registration) (authority.Store, error)

// ModelCacheFactoryFunc opens one replaceable per-Project XDG CACHE store.
type ModelCacheFactoryFunc func(string, project.Registration) (repositorymodel.Cache, error)

func legacyAuditLogger(dataDirectory, eventType, projectID, repositoryRoot string, metadata map[string]any) (audit.Event, error) {
	return audit.Append(dataDirectory, eventType, projectID, repositoryRoot, metadata)
}

func legacyChangeAuditLogger(dataDirectory, eventType, projectID, changeID, repositoryRoot string, metadata map[string]any) (audit.Event, error) {
	return audit.AppendChange(dataDirectory, eventType, projectID, changeID, repositoryRoot, metadata)
}

func sameFunction(left, right any) bool {
	if left == nil || right == nil {
		return false
	}
	return reflect.ValueOf(left).Pointer() == reflect.ValueOf(right).Pointer()
}

// Container assembles only the dependencies required by the current M1.2 runtime scope.
type Container struct {
	RepositoryDiscovery          RepositoryDiscoveryFunc
	RepositoryInspection         intelligence.RepositoryInspector
	RepositoryModelInspection    repositorymodel.SourceInspector
	RepositoryAnalyzers          []repositorymodel.Analyzer
	ModelCacheFactory            ModelCacheFactoryFunc
	RepositoryModelConfiguration repositorymodel.Configuration
	ImpactConfiguration          impact.Configuration
	ProjectRegistration          ProjectRegistrationFunc
	AuditLogger                  AuditLoggerFunc
	ChangeAuditLogger            ChangeAuditLoggerFunc
	ChangeStore                  *workflow.MemoryStore
	DurableStoreFactory          DurableStoreFactoryFunc
	DurableAuthority             authority.Store
	WorkflowClock                workflow.Clock
	ProposalWorkspaces           proposal.WorkspacePort
	PatchExtraction              proposal.PatchPort
	ProposalClock                proposal.Clock
	AIProviders                  []aiprovider.Provider
	ConfiguredProvider           string
	ConfiguredModel              string
	ConfiguredProviderSource     aiprovider.SelectionSource
	ConfiguredModelSource        aiprovider.SelectionSource
	ExecutionAttemptIds          execution.AttemptIdGenerator
	ExecutionClock               execution.Clock
	VerificationRunner           verification.StepRunner
	VerificationAttemptIds       verification.AttemptIdGenerator
	VerificationClock            verification.Clock
	PolicySource                 policy.SourcePort
	PolicyClock                  policy.Clock
	ApprovalClock                approval.Clock
	CanonicalSource              integration.CanonicalSourcePort
	IntegrationClock             integration.Clock
	PresentationPreferences      PresentationPreferencesFunc
}

// New creates the explicit composition root for the current runtime boundary.
func New() Container {
	gitProposalAdapter := gitproposal.NewDefault()
	configuredProvider, providerConfigured := os.LookupEnv("PRAETOR_AI_PROVIDER")
	providerSource := aiprovider.SelectionSourceEnvironment
	if !providerConfigured || configuredProvider == "" {
		configuredProvider = codexcli.Identifier
		providerSource = aiprovider.SelectionSourceBuiltIn
	}
	configuredModel, modelConfigured := os.LookupEnv("PRAETOR_AI_MODEL")
	modelSource := aiprovider.SelectionSourceEnvironment
	if !modelConfigured || configuredModel == "" {
		modelSource = aiprovider.SelectionSourceProviderDefault
	}
	return Container{
		RepositoryDiscovery:       repository.Discover,
		RepositoryInspection:      repository.Inspect,
		RepositoryModelInspection: repository.InspectModelSource,
		RepositoryAnalyzers:       []repositorymodel.Analyzer{repositoryanalysis.New()},
		ModelCacheFactory: func(cacheDirectory string, registration project.Registration) (repositorymodel.Cache, error) {
			return modelcache.Open(cacheDirectory, registration)
		},
		RepositoryModelConfiguration: repositorymodel.DefaultConfiguration(),
		ImpactConfiguration:          impact.DefaultConfiguration(),
		ProjectRegistration:          project.EnsureRegistration,
		AuditLogger:                  legacyAuditLogger,
		ChangeAuditLogger:            legacyChangeAuditLogger,
		ChangeStore:                  workflow.NewMemoryStore(),
		DurableStoreFactory: func(dataDirectory string, registration project.Registration) (authority.Store, error) {
			return sqliteadapter.Open(dataDirectory, registration)
		},
		WorkflowClock: func() time.Time {
			return time.Now().UTC()
		},
		ProposalWorkspaces: gitProposalAdapter,
		PatchExtraction:    gitProposalAdapter,
		ProposalClock: func() time.Time {
			return time.Now().UTC()
		},
		AIProviders:              []aiprovider.Provider{codexcli.NewDefault()},
		ConfiguredProvider:       configuredProvider,
		ConfiguredModel:          configuredModel,
		ConfiguredProviderSource: providerSource,
		ConfiguredModelSource:    modelSource,
		ExecutionAttemptIds:      aiprovider.GenerateExecutionAttemptId,
		ExecutionClock: func() time.Time {
			return time.Now().UTC()
		},
		VerificationRunner:     localexec.NewDefault(),
		VerificationAttemptIds: verification.GenerateVerificationAttemptId,
		VerificationClock: func() time.Time {
			return time.Now().UTC()
		},
		PolicySource: projectpolicy.New(),
		PolicyClock:  func() time.Time { return time.Now().UTC() },
		ApprovalClock: func() time.Time {
			return time.Now().UTC()
		},
		CanonicalSource: gitProposalAdapter,
		IntegrationClock: func() time.Time {
			return time.Now().UTC()
		},
		PresentationPreferences: preferences.OpenDefault,
	}
}

// MigrateLegacyAudit explicitly performs the ADR-038 cutover. Ordinary
// Project attachment never invokes this operation implicitly.
func (container Container) MigrateLegacyAudit() error {
	dataDirectory, err := project.ResolveDataDir()
	if err != nil {
		return err
	}
	registrations, err := project.LoadRegistrations(dataDirectory)
	if err != nil {
		return err
	}
	if err := sqliteadapter.MigrateLegacyAudit(dataDirectory, registrations); err != nil {
		return fmt.Errorf("migrate legacy audit authority: %w", err)
	}
	return nil
}

// NewInteractiveSession composes one retained shell session around the active Project.
func (container Container) NewInteractiveSession(path string) (*command.Session, error) {
	registration, err := container.EnsureProjectRegistration(path)
	if err != nil {
		return nil, err
	}
	runtime := container
	var openedAuthority authority.Store
	var openedModeling *repositorymodel.Service
	authorityHandedOff := false
	defer func() {
		if !authorityHandedOff && openedAuthority != nil {
			_ = openedAuthority.Close()
		}
		if !authorityHandedOff && openedModeling != nil {
			_ = openedModeling.Close()
		}
	}()
	var durableInspection *inspection.Service
	var canonicalRecovery integration.RecoveryPort
	useDurableAuthority := container.DurableStoreFactory != nil &&
		sameFunction(container.AuditLogger, AuditLoggerFunc(legacyAuditLogger)) &&
		sameFunction(container.ChangeAuditLogger, ChangeAuditLoggerFunc(legacyChangeAuditLogger))
	if useDurableAuthority {
		dataDirectory, dataError := project.ResolveDataDir()
		if dataError != nil {
			return nil, dataError
		}
		store, openError := container.DurableStoreFactory(dataDirectory, registration)
		if openError != nil {
			return nil, openError
		}
		openedAuthority = store
		if validationError := store.ValidateAttachment(); validationError != nil {
			return nil, fmt.Errorf("validate Project authority on attach: %w", validationError)
		}
		if sameFunction(container.ChangeAuditLogger, ChangeAuditLoggerFunc(legacyChangeAuditLogger)) {
			runtime.DurableAuthority = store
		}
		if sameFunction(container.AuditLogger, AuditLoggerFunc(legacyAuditLogger)) {
			runtime.AuditLogger = func(_ string, eventType, projectID, repositoryRoot string, metadata map[string]any) (audit.Event, error) {
				event, eventError := audit.NewEvent(eventType, projectID, "", repositoryRoot, metadata, time.Now().UTC())
				if eventError != nil {
					return audit.Event{}, eventError
				}
				if appendError := store.AppendAudit(event); appendError != nil {
					return audit.Event{}, appendError
				}
				return event, nil
			}
		}
		if sameFunction(container.ChangeAuditLogger, ChangeAuditLoggerFunc(legacyChangeAuditLogger)) {
			runtime.ChangeAuditLogger = func(_ string, eventType, projectID, changeID, repositoryRoot string, metadata map[string]any) (audit.Event, error) {
				event, eventError := audit.NewEvent(eventType, projectID, changeID, repositoryRoot, metadata, time.Now().UTC())
				if eventError != nil {
					return audit.Event{}, eventError
				}
				if appendError := store.AppendAudit(event); appendError != nil {
					return audit.Event{}, appendError
				}
				return event, nil
			}
		}
		durableInspection, err = inspection.New(store)
		if err != nil {
			store.Close()
			return nil, err
		}
		probe, supported := runtime.CanonicalSource.(integration.RecoveryProbe)
		if !supported {
			return nil, fmt.Errorf("canonical source adapter does not support M1.1 recovery classification")
		}
		stateDirectory, stateError := project.ResolveStateDir()
		if stateError != nil {
			return nil, stateError
		}
		coordinated, coordinateError := integration.NewCoordinatedCanonical(runtime.CanonicalSource, probe, store, registration.ProjectId, stateDirectory)
		if coordinateError != nil {
			return nil, coordinateError
		}
		runtime.CanonicalSource = coordinated
		canonicalRecovery = coordinated
	}
	changeWorkflow, err := runtime.NewChangeWorkflow(registration)
	if err != nil {
		if runtime.DurableAuthority != nil {
			_ = runtime.DurableAuthority.Close()
		}
		return nil, err
	}
	repositoryIntelligence, err := runtime.NewRepositoryIntelligence(registration)
	if err != nil {
		return nil, err
	}
	if runtime.RepositoryModelInspection == nil || runtime.ModelCacheFactory == nil || len(runtime.RepositoryAnalyzers) == 0 {
		return nil, fmt.Errorf("M1.2 repository model dependencies are not configured")
	}
	cacheDirectory, err := project.ResolveCacheDir()
	if err != nil {
		return nil, err
	}
	modelCache, err := runtime.ModelCacheFactory(cacheDirectory, registration)
	if err != nil {
		return nil, err
	}
	openedModeling, err = repositorymodel.NewService(registration.ProjectId, registration.RepositoryRoot, runtime.RepositoryModelInspection, runtime.RepositoryAnalyzers, modelCache, runtime.RepositoryModelConfiguration, func() time.Time { return time.Now().UTC() })
	if err != nil {
		_ = modelCache.Close()
		return nil, err
	}
	var impactService *impact.Service
	if openedAuthority != nil {
		impactService, err = impact.NewService(registration.ProjectId, registration.RepositoryRoot, openedModeling, openedAuthority, runtime.ImpactConfiguration, func() time.Time { return time.Now().UTC() })
		if err != nil {
			return nil, err
		}
	}
	proposalLifecycle, err := runtime.NewProposalService(registration)
	if err != nil {
		return nil, err
	}
	providerRegistry, err := runtime.NewProviderRegistry()
	if err != nil {
		return nil, err
	}
	providerSelection, err := providerRegistry.Select(
		runtime.ConfiguredProvider,
		runtime.ConfiguredModel,
	)
	if err != nil {
		return nil, err
	}
	providerSource := runtime.ConfiguredProviderSource
	if providerSource == "" {
		providerSource = aiprovider.SelectionSourceComposition
	}
	modelSource := runtime.ConfiguredModelSource
	if modelSource == "" {
		if runtime.ConfiguredModel == "" {
			modelSource = aiprovider.SelectionSourceProviderDefault
		} else {
			modelSource = aiprovider.SelectionSourceComposition
		}
	}
	providerProvenance, err := aiprovider.NewSelectionProvenance(providerSource, modelSource)
	if err != nil {
		return nil, err
	}
	providerExecution, err := runtime.NewProviderExecutionService(
		registration,
		proposalLifecycle,
		providerRegistry,
	)
	if err != nil {
		return nil, err
	}
	verificationService, err := runtime.NewVerificationService(registration, proposalLifecycle)
	if err != nil {
		return nil, err
	}
	policyService, err := runtime.NewPolicyService(registration)
	if err != nil {
		return nil, err
	}
	approvalService, err := runtime.NewApprovalService(registration, changeWorkflow, proposalLifecycle)
	if err != nil {
		return nil, err
	}
	integrationService, err := runtime.NewIntegrationService(registration, changeWorkflow, proposalLifecycle)
	if err != nil {
		return nil, err
	}
	if runtime.PresentationPreferences == nil {
		return nil, fmt.Errorf("presentation preference loader is not configured")
	}
	presentationPreferences, err := runtime.PresentationPreferences()
	if err != nil {
		return nil, err
	}
	session, err := command.NewSession(
		registration,
		changeWorkflow,
		repositoryIntelligence,
		openedModeling,
		impactService,
		proposalLifecycle,
		providerExecution,
		verificationService,
		policyService,
		approvalService,
		integrationService,
		presentationPreferences,
		providerRegistry,
		providerSelection,
		providerProvenance,
		durableInspection,
		openedAuthority,
		canonicalRecovery,
	)
	if err != nil {
		return nil, err
	}

	if _, err := runtime.RecordInitialization(registration, map[string]any{
		"command": "interactive shell",
		"phase":   "startup",
	}); err != nil {
		return nil, err
	}
	if _, err := runtime.RecordProjectAttach(registration, map[string]any{
		"command":  "interactive shell",
		"attached": true,
	}); err != nil {
		return nil, err
	}
	if _, err := runtime.RecordConfiguration(registration, map[string]any{
		"command":     "interactive shell",
		"runtime":     "local",
		"directory":   "data",
		"ai_provider": string(providerSelection.ProviderIdentifier()),
		"ai_model":    selectedModelMetadata(providerSelection),
	}); err != nil {
		return nil, err
	}
	authorityHandedOff = true
	return session, nil
}

// NewPolicyService composes project-governance YAML loading, generic evaluation and bounded audit.
func (container Container) NewPolicyService(registration project.Registration) (*policy.Service, error) {
	if container.PolicySource == nil || container.PolicyClock == nil || container.ChangeAuditLogger == nil {
		return nil, fmt.Errorf("policy dependencies are not configured")
	}
	dataDirectory, err := audit.ResolveDataDir()
	if err != nil {
		return nil, err
	}
	recorder := func(event policy.LifecycleEvent) error {
		decision := event.Decision
		if decision.ProjectId() != registration.ProjectId {
			return fmt.Errorf("policy event ProjectId does not match registered ProjectId")
		}
		individual := make([]map[string]any, 0, len(decision.Decisions()))
		for _, item := range decision.Decisions() {
			individual = append(individual, map[string]any{
				"policy_id":      string(item.Policy().Id()),
				"policy_version": string(item.Policy().Version()),
				"family":         string(item.Policy().Family()),
				"severity":       string(item.Policy().Severity()),
				"outcome":        string(item.Outcome()),
				"evidence_count": len(item.EvidenceIds()),
				"reason":         item.Reason(),
			})
		}
		metadata := map[string]any{
			"policy_evaluation_id":    decision.Id(),
			"policy_bundle_id":        string(decision.Bundle().Id()),
			"policy_bundle_version":   string(decision.Bundle().Version()),
			"policy_bundle_digest":    decision.Bundle().Digest(),
			"workspace_id":            string(decision.WorkspaceId()),
			"verification_attempt_id": string(decision.VerificationAttemptId()),
			"evidence_set_id":         decision.EvidenceSetId(),
			"patch_digest":            decision.PatchDigest(),
			"source_state_digest":     string(decision.SourceStateDigest()),
			"policy_count":            len(decision.Decisions()),
			"denied":                  decision.Aggregate().Denied,
			"requires_review":         decision.Aggregate().RequiresReview,
			"requires_approval":       decision.Aggregate().RequiresApproval,
			"policy_decisions":        individual,
		}
		if event.EventType == policy.EventPolicyExceptionCandidateRecorded {
			candidate := event.Candidate
			metadata["exception_candidate_id"] = candidate.Id()
			metadata["policy_id"] = string(candidate.PolicyId())
			metadata["requested_scope"] = candidate.Scope()
			metadata["requested_authority"] = candidate.RequestedAuthority()
			metadata["reason"] = candidate.Reason()
			if expiry, ok := candidate.ExpiresAt(); ok {
				metadata["expires_at"] = expiry.Format(time.RFC3339Nano)
			}
		} else if event.EventType != policy.EventPolicyDecisionRecorded {
			return fmt.Errorf("unknown M1.0 policy lifecycle event type %q", event.EventType)
		}
		_, recordError := container.ChangeAuditLogger(dataDirectory, event.EventType, string(decision.ProjectId()), string(decision.ChangeId()), registration.RepositoryRoot, metadata)
		return recordError
	}
	return policy.New(container.PolicySource, policy.NewEngine(), recorder, container.PolicyClock)
}

// NewIntegrationService composes M0.8 canonical application and rejection
// closure through the existing local Git and append-oriented audit adapters.
func (container Container) NewIntegrationService(
	registration project.Registration,
	changeWorkflow *workflow.Service,
	proposalLifecycle *proposal.Service,
) (*integration.Service, error) {
	if changeWorkflow == nil {
		return nil, fmt.Errorf("Change workflow dependency is not configured")
	}
	if proposalLifecycle == nil {
		return nil, fmt.Errorf("proposal lifecycle dependency is not configured")
	}
	if container.CanonicalSource == nil {
		return nil, fmt.Errorf("canonical source application dependency is not configured")
	}
	if container.RepositoryInspection == nil {
		return nil, fmt.Errorf("repository inspection dependency is not configured")
	}
	if container.ChangeAuditLogger == nil {
		return nil, fmt.Errorf("Change audit logger dependency is not configured")
	}
	if container.IntegrationClock == nil {
		return nil, fmt.Errorf("canonical integration clock dependency is not configured")
	}
	dataDirectory, err := audit.ResolveDataDir()
	if err != nil {
		return nil, err
	}
	recorder := func(event integration.LifecycleEvent) error {
		if event.Change.ProjectId() != registration.ProjectId {
			return fmt.Errorf(
				"M0.8 event ProjectId %q does not match registered ProjectId %q",
				event.Change.ProjectId(),
				registration.ProjectId,
			)
		}
		metadata, metadataError := canonicalIntegrationMetadata(event)
		if metadataError != nil {
			return metadataError
		}
		_, recordError := container.ChangeAuditLogger(
			dataDirectory,
			event.EventType,
			string(event.Change.ProjectId()),
			string(event.Change.ChangeId()),
			registration.RepositoryRoot,
			metadata,
		)
		return recordError
	}
	if container.DurableAuthority != nil {
		return integration.NewDurable(changeWorkflow, proposalLifecycle.VerifyIntegrity, container.CanonicalSource, integration.RepositoryInspector(container.RepositoryInspection), recorder, container.IntegrationClock, container.DurableAuthority, registration.RepositoryRoot)
	}
	return integration.New(changeWorkflow, proposalLifecycle.VerifyIntegrity, container.CanonicalSource, integration.RepositoryInspector(container.RepositoryInspection), recorder, container.IntegrationClock)
}

func canonicalIntegrationMetadata(event integration.LifecycleEvent) (map[string]any, error) {
	workspace := event.Proposal.Workspace()
	artifact, hasArtifact := event.Proposal.PatchArtifact()
	evidence := event.Verification.EvidenceSet()
	decision := event.Decision
	if event.OccurredAt.IsZero() || !hasArtifact ||
		workspace.ProjectId() != event.Change.ProjectId() ||
		workspace.ChangeId() != event.Change.ChangeId() ||
		artifact.WorkspaceId() != workspace.WorkspaceId() ||
		evidence.WorkspaceId() != workspace.WorkspaceId() ||
		decision.WorkspaceId() != workspace.WorkspaceId() {
		return nil, fmt.Errorf("M0.8 lifecycle event linkage is inconsistent")
	}
	metadata := map[string]any{
		"workspace_id":                string(workspace.WorkspaceId()),
		"base_revision":               artifact.BaseRevision(),
		"source_state_digest":         string(artifact.SourceStateDigest()),
		"patch_digest":                artifact.PatchDigest(),
		"changed_paths":               artifact.ChangedPaths(),
		"changed_path_count":          len(artifact.ChangedPaths()),
		"verification_attempt_id":     string(evidence.VerificationAttemptId()),
		"evidence_set_id":             evidence.Id(),
		"evidence_count":              len(evidence.Evidence()),
		"human_decision":              string(decision.Kind()),
		"policy_evaluation_id":        decision.PolicyEvaluationId(),
		"policy_bundle_digest":        decision.PolicyBundleDigest(),
		"canonical_mutation_occurred": event.CanonicalMutationOccurred,
		"event_timestamp":             event.OccurredAt.UTC().Format(time.RFC3339Nano),
	}
	switch event.EventType {
	case integration.EventCanonicalApplicationStarted:
		metadata["disposition"] = "started"
	case integration.EventCanonicalApplicationCompleted:
		result := event.Result
		if !result.CanonicalMutationOccurred() || !result.CanonicalApplicationProven() ||
			result.PatchDigest() != artifact.PatchDigest() ||
			result.EvidenceSetId() != evidence.Id() {
			return nil, fmt.Errorf("M0.8 canonical completion proof is inconsistent")
		}
		metadata["disposition"] = "completed"
		metadata["resulting_source_state_digest"] = string(result.ResultingSourceStateDigest())
		metadata["canonical_head"] = result.CanonicalHead()
		metadata["index_unchanged"] = result.IndexUnchanged()
		metadata["canonical_result_digest"] = result.ResultDigest()
	case integration.EventCanonicalApplicationFailed:
		if event.FailureStage == "" || event.Failure == "" {
			return nil, fmt.Errorf("M0.8 canonical application failure metadata is incomplete")
		}
		metadata["disposition"] = "failed"
		metadata["failure_stage"] = event.FailureStage
		metadata["failure"] = event.Failure
		metadata["canonical_application_proven"] = event.Result.CanonicalApplicationProven()
	case integration.EventChangeClosureRecorded:
		if decision.Kind() != approval.DecisionReject || event.CanonicalMutationOccurred {
			return nil, fmt.Errorf("M0.8 rejection closure metadata is inconsistent")
		}
		metadata["disposition"] = "rejected-closure"
		metadata["canonical_source_unchanged"] = true
	default:
		return nil, fmt.Errorf("unknown M0.8 lifecycle event type %q", event.EventType)
	}
	return metadata, nil
}

// NewApprovalService composes the M0.7 explicit local-human decision gate
// around existing workflow, evidence, proposal-integrity, and audit behavior.
func (container Container) NewApprovalService(
	registration project.Registration,
	changeWorkflow *workflow.Service,
	proposalLifecycle *proposal.Service,
) (*approval.Service, error) {
	if changeWorkflow == nil {
		return nil, fmt.Errorf("Change workflow dependency is not configured")
	}
	if proposalLifecycle == nil {
		return nil, fmt.Errorf("proposal lifecycle dependency is not configured")
	}
	if container.ChangeAuditLogger == nil {
		return nil, fmt.Errorf("Change audit logger dependency is not configured")
	}
	if container.ApprovalClock == nil {
		return nil, fmt.Errorf("human decision clock dependency is not configured")
	}
	dataDirectory, err := audit.ResolveDataDir()
	if err != nil {
		return nil, err
	}
	recorder := func(event approval.LifecycleEvent) error {
		decision := event.Decision
		if decision.ProjectId() != registration.ProjectId {
			return fmt.Errorf(
				"M0.7 decision ProjectId %q does not match registered ProjectId %q",
				decision.ProjectId(),
				registration.ProjectId,
			)
		}
		metadata, metadataError := humanDecisionMetadata(event)
		if metadataError != nil {
			return metadataError
		}
		_, recordError := container.ChangeAuditLogger(
			dataDirectory,
			event.EventType,
			string(decision.ProjectId()),
			string(decision.ChangeId()),
			registration.RepositoryRoot,
			metadata,
		)
		return recordError
	}
	return approval.New(
		changeWorkflow,
		proposalLifecycle.VerifyIntegrity,
		recorder,
		container.ApprovalClock,
	)
}

func humanDecisionMetadata(event approval.LifecycleEvent) (map[string]any, error) {
	decision := event.Decision
	if event.EventType != approval.EventHumanDecisionRecorded {
		return nil, fmt.Errorf("unknown M0.7 lifecycle event type %q", event.EventType)
	}
	if event.OccurredAt.IsZero() || !event.OccurredAt.Equal(decision.OccurredAt()) {
		return nil, fmt.Errorf("M0.7 decision event timestamp linkage is inconsistent")
	}
	if decision.ProjectId() == "" || decision.ChangeId() == "" ||
		decision.WorkspaceId() == "" || decision.PatchDigest() == "" ||
		decision.SourceStateDigest() == "" || decision.VerificationAttemptId() == "" ||
		decision.EvidenceSetId() == "" || decision.EvidenceCount() <= 0 ||
		decision.ChangedPathCount() <= 0 || decision.PolicyEvaluationId() == "" ||
		decision.PolicyBundleDigest() == "" {
		return nil, fmt.Errorf("M0.7 decision event linkage is incomplete")
	}
	metadata := map[string]any{
		"decision":                 string(decision.Kind()),
		"decision_timestamp":       decision.OccurredAt().Format(time.RFC3339Nano),
		"actor_type":               "human",
		"actor_provenance":         string(decision.Actor()),
		"identity_assurance":       "local-process-interaction-only",
		"requested_state":          string(decision.RequestedState()),
		"workspace_id":             string(decision.WorkspaceId()),
		"base_revision":            decision.BaseRevision(),
		"source_state_digest":      string(decision.SourceStateDigest()),
		"patch_digest":             decision.PatchDigest(),
		"verification_attempt_id":  string(decision.VerificationAttemptId()),
		"evidence_set_id":          decision.EvidenceSetId(),
		"evidence_count":           decision.EvidenceCount(),
		"changed_path_count":       decision.ChangedPathCount(),
		"policy_evaluation_id":     decision.PolicyEvaluationId(),
		"policy_bundle_digest":     decision.PolicyBundleDigest(),
		"policy_denied":            decision.PolicyDenied(),
		"policy_requires_review":   decision.PolicyRequiresReview(),
		"policy_requires_approval": decision.PolicyRequiresApproval(),
	}
	if decision.Rationale() != "" {
		metadata["rationale"] = string(decision.Rationale())
	}
	return metadata, nil
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
				"provider event ProjectId %q does not match registered ProjectId %q",
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

// NewVerificationService composes deterministic verification execution and
// append-oriented evidence around one retained proposal lifecycle.
func (container Container) NewVerificationService(
	registration project.Registration,
	proposalLifecycle *proposal.Service,
) (*verification.Service, error) {
	if proposalLifecycle == nil {
		return nil, fmt.Errorf("proposal lifecycle dependency is not configured")
	}
	if container.VerificationRunner == nil {
		return nil, fmt.Errorf("verification process runner dependency is not configured")
	}
	if container.VerificationAttemptIds == nil {
		return nil, fmt.Errorf("verification attempt identity dependency is not configured")
	}
	if container.VerificationClock == nil {
		return nil, fmt.Errorf("verification clock dependency is not configured")
	}
	if container.ChangeAuditLogger == nil {
		return nil, fmt.Errorf("Change audit logger dependency is not configured")
	}
	dataDirectory, err := audit.ResolveDataDir()
	if err != nil {
		return nil, err
	}
	recorder := func(event verification.LifecycleEvent) error {
		workspace := event.Proposal.Workspace()
		if workspace.ProjectId() != registration.ProjectId {
			return fmt.Errorf(
				"M0.6 event ProjectId %q does not match registered ProjectId %q",
				workspace.ProjectId(),
				registration.ProjectId,
			)
		}
		metadata, metadataError := verificationLifecycleMetadata(event)
		if metadataError != nil {
			return metadataError
		}
		_, recordError := container.ChangeAuditLogger(
			dataDirectory,
			event.EventType,
			string(workspace.ProjectId()),
			string(workspace.ChangeId()),
			registration.RepositoryRoot,
			metadata,
		)
		return recordError
	}
	engine, err := verification.NewEngine(
		container.VerificationRunner,
		proposalLifecycle.VerifyIntegrity,
		container.VerificationClock,
	)
	if err != nil {
		return nil, err
	}
	return verification.New(
		engine,
		recorder,
		container.VerificationAttemptIds,
		container.VerificationClock,
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
	if container.DurableAuthority != nil {
		stateDirectory, stateError := project.ResolveStateDir()
		if stateError != nil {
			return nil, stateError
		}
		return proposal.NewDurable(
			container.ProposalWorkspaces,
			container.PatchExtraction,
			proposal.RepositoryInspector(container.RepositoryInspection),
			recorder,
			container.ProposalClock,
			container.DurableAuthority,
			stateDirectory,
			registration.RepositoryRoot,
		)
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
		proposal.EventProposalWorkspaceFailed,
		proposal.EventProposalWorkspaceCleanupFailed,
		proposal.EventProposalWorkspaceDiscarded:
		return metadata, nil
	case proposal.EventPatchSurfaceValidated,
		proposal.EventPatchRejected:
		if !event.HasApprovedScope {
			return nil, fmt.Errorf("patch classification event requires ApprovedScope")
		}
		surface := event.ApprovedScope.Surface()
		metadata["allowed"] = event.Validation.Allowed()
		metadata["approved_authorization_mode"] = string(surface.AuthorizationMode())
		metadata["approved_expected_paths"] = repositoryPathStrings(surface.ExpectedPaths())
		metadata["approved_possible_paths"] = repositoryPathStrings(surface.PossiblePaths())
		metadata["approved_protected_paths"] = repositoryPathStrings(surface.ProtectedPaths())
		metadata["actual_expected_changes"] = repositoryPathStrings(event.Validation.ExpectedChanges())
		metadata["actual_possible_changes"] = repositoryPathStrings(event.Validation.PossibleChanges())
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
		return nil, fmt.Errorf("provider descriptor and selection linkage is inconsistent")
	}
	if event.OccurredAt.IsZero() {
		return nil, fmt.Errorf("provider execution event timestamp is required")
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
	metadata["request_context"] = providerRequestContextMetadata(event.RequestContext)

	switch event.EventType {
	case execution.EventProviderExecutionStarted,
		execution.EventVerificationPlanningStarted:
		metadata["disposition"] = "started"
	case execution.EventProviderExecutionCompleted,
		execution.EventVerificationPlanningCompleted:
		if !event.HasResponse ||
			event.Response.AttemptId() != request.AttemptId() ||
			event.Response.Selection().ProviderIdentifier() != selection.ProviderIdentifier() {
			return nil, fmt.Errorf("completed provider response linkage is inconsistent")
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
		if response.Summary() != "" {
			metadata["provider_summary"] = response.Summary()
		}
		if response.Usage().Available() {
			metadata["usage"] = map[string]int64{
				"input_tokens":            response.Usage().InputTokens(),
				"cached_input_tokens":     response.Usage().CachedInputTokens(),
				"output_tokens":           response.Usage().OutputTokens(),
				"reasoning_output_tokens": response.Usage().ReasoningOutputTokens(),
			}
		}
	case execution.EventProviderExecutionFailed,
		execution.EventVerificationPlanningFailed:
		if event.FailureKind == "" {
			return nil, fmt.Errorf("failed provider execution requires a failure classification")
		}
		metadata["disposition"] = "failed"
		metadata["failure_kind"] = string(event.FailureKind)
		metadata["failure_stage"] = event.FailureStage
		metadata["workspace_may_be_changed"] = event.WorkspaceMayBeChanged
		metadata["changed_paths"] = append([]string(nil), event.ChangedPaths...)
		if event.ExternalExecutionId != "" {
			metadata["external_execution_id"] = event.ExternalExecutionId
		}
		if event.ProviderDiagnostic != "" {
			metadata["provider_diagnostic"] = event.ProviderDiagnostic
		}
	default:
		return nil, fmt.Errorf("unknown provider lifecycle event type %q", event.EventType)
	}
	return metadata, nil
}

func providerRequestContextMetadata(
	accounting aiprovider.RequestContextAccounting,
) map[string]any {
	components := make([]map[string]any, 0, len(accounting.Components()))
	for _, component := range accounting.Components() {
		components = append(components, map[string]any{
			"component":     string(component.Kind()),
			"bytes":         component.ByteCount(),
			"characters":    component.CharacterCount(),
			"items":         component.ItemCount(),
			"omitted_items": component.OmittedItems(),
			"truncated":     component.Truncated(),
		})
	}
	return map[string]any{
		"total_bytes":      accounting.TotalBytes(),
		"total_characters": accounting.TotalCharacters(),
		"total_items":      accounting.TotalItems(),
		"truncated":        accounting.Truncated(),
		"components":       components,
	}
}

func verificationLifecycleMetadata(event verification.LifecycleEvent) (map[string]any, error) {
	workspace := event.Proposal.Workspace()
	artifact, hasArtifact := event.Proposal.PatchArtifact()
	if event.AttemptId == "" || event.OccurredAt.IsZero() || !hasArtifact {
		return nil, fmt.Errorf("M0.6 verification event identity, time, and PatchArtifact are required")
	}
	if artifact.WorkspaceId() != workspace.WorkspaceId() ||
		artifact.ProjectId() != workspace.ProjectId() ||
		artifact.ChangeId() != workspace.ChangeId() {
		return nil, fmt.Errorf("M0.6 verification event linkage is inconsistent")
	}
	metadata := map[string]any{
		"verification_attempt_id": string(event.AttemptId),
		"workspace_id":            string(workspace.WorkspaceId()),
		"base_revision":           workspace.BaseRevision(),
		"source_state_digest":     string(workspace.SourceStateDigest()),
		"patch_digest":            artifact.PatchDigest(),
		"event_timestamp":         event.OccurredAt.UTC().Format(time.RFC3339Nano),
		"disposition":             event.Disposition,
	}
	if event.PlanStepCount > 0 {
		metadata["plan_step_count"] = event.PlanStepCount
	}
	if event.HasEvidence {
		evidence := event.Evidence
		metadata["step_id"] = evidence.StepId()
		metadata["step_kind"] = string(evidence.Kind())
		metadata["candidate_origin"] = string(evidence.Origin())
		metadata["executable"] = evidence.Executable()
		metadata["arguments"] = evidence.Arguments()
		metadata["working_directory"] = evidence.WorkingDirectory()
		metadata["supporting_evidence"] = repositoryPathStrings(evidence.SupportingEvidence())
		metadata["started_at"] = evidence.StartedAt().Format(time.RFC3339Nano)
		metadata["completed_at"] = evidence.CompletedAt().Format(time.RFC3339Nano)
		metadata["duration_milliseconds"] = evidence.Duration().Milliseconds()
		metadata["outcome"] = string(evidence.Outcome())
		metadata["stdout_present"] = evidence.StandardOutput() != ""
		metadata["stderr_present"] = evidence.StandardError() != ""
		metadata["output_truncated"] = evidence.OutputTruncated()
		if exitCode, available := evidence.ExitCode(); available {
			metadata["exit_code"] = exitCode
		}
	}
	if event.HasEvidenceSet {
		evidenceSet := event.EvidenceSet
		if evidenceSet.VerificationAttemptId() != event.AttemptId ||
			evidenceSet.ProjectId() != workspace.ProjectId() ||
			evidenceSet.ChangeId() != workspace.ChangeId() ||
			evidenceSet.WorkspaceId() != workspace.WorkspaceId() ||
			evidenceSet.PatchDigest() != artifact.PatchDigest() {
			return nil, fmt.Errorf("M0.6 EvidenceSet linkage is inconsistent")
		}
		metadata["evidence_set_id"] = evidenceSet.Id()
		metadata["evidence_count"] = len(evidenceSet.Evidence())
		metadata["passed"] = evidenceSet.Passed()
	}
	if event.Failure != "" {
		metadata["failure"] = event.Failure
	}
	switch event.EventType {
	case verification.EventVerificationStarted:
		if event.Disposition != "started" {
			return nil, fmt.Errorf("M0.6 start disposition is invalid")
		}
	case verification.EventVerificationStepCompleted:
		if !event.HasEvidence {
			return nil, fmt.Errorf("M0.6 step completion requires evidence")
		}
	case verification.EventVerificationCompleted:
		if !event.HasEvidenceSet || !event.EvidenceSet.Passed() {
			return nil, fmt.Errorf("M0.6 completion requires passing EvidenceSet")
		}
	case verification.EventVerificationFailed:
		if event.Failure == "" {
			return nil, fmt.Errorf("M0.6 failure requires bounded failure metadata")
		}
	default:
		return nil, fmt.Errorf("unknown M0.6 lifecycle event type %q", event.EventType)
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
	metadata := map[string]any{
		"source_state_digest": string(digest),
		"authorization_mode":  string(surface.AuthorizationMode()),
		"expected_paths":      repositoryPathStrings(surface.ExpectedPaths()),
		"possible_paths":      repositoryPathStrings(surface.PossiblePaths()),
		"protected_paths":     repositoryPathStrings(surface.ProtectedPaths()),
	}
	if surface.AuthorizationMode() == source.AuthorizationRepositoryWide {
		metadata["repository_scope"] = "."
	}
	return metadata
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
	if container.DurableAuthority != nil {
		if container.WorkflowClock == nil {
			return nil, fmt.Errorf("workflow clock dependency is not configured")
		}
		return workflow.NewDurable(container.DurableAuthority, registration.ProjectId, container.WorkflowClock)
	}
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
