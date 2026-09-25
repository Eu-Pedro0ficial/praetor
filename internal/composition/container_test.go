package composition

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Eu-Pedro0ficial/praetor/internal/adapters/aiprovider/codexcli"
	sqliteadapter "github.com/Eu-Pedro0ficial/praetor/internal/adapters/persistence/sqlite"
	"github.com/Eu-Pedro0ficial/praetor/internal/aiprovider"
	"github.com/Eu-Pedro0ficial/praetor/internal/audit"
	"github.com/Eu-Pedro0ficial/praetor/internal/authority"
	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"github.com/Eu-Pedro0ficial/praetor/internal/project"
	"github.com/Eu-Pedro0ficial/praetor/internal/proposal"
	"github.com/Eu-Pedro0ficial/praetor/internal/repository"
	"github.com/Eu-Pedro0ficial/praetor/internal/source"
)

const compositionProjectId project.ProjectId = "01890c29-7a78-7abc-8def-0123456789ab"

func TestNewExplicitlyRegistersOnlyCodexCLIWithRuntimeSelectionMetadata(t *testing.T) {
	t.Setenv("PRAETOR_AI_PROVIDER", codexcli.Identifier)
	t.Setenv("PRAETOR_AI_MODEL", "provider-scoped-model")
	container := New()
	if container.ConfiguredProvider != codexcli.Identifier ||
		container.ConfiguredModel != "provider-scoped-model" {
		t.Fatalf(
			"runtime provider configuration = %q/%q",
			container.ConfiguredProvider,
			container.ConfiguredModel,
		)
	}
	registry, err := container.NewProviderRegistry()
	if err != nil {
		t.Fatalf("NewProviderRegistry() error = %v", err)
	}
	descriptors := registry.Descriptors()
	if len(descriptors) != 1 || descriptors[0].Identifier() != codexcli.Identifier ||
		descriptors[0].Vendor() != "OpenAI" {
		t.Fatalf("compile-time provider registrations = %#v", descriptors)
	}
	selection, err := registry.Select(container.ConfiguredProvider, container.ConfiguredModel)
	if err != nil {
		t.Fatalf("registry.Select() error = %v", err)
	}
	model, selected := selection.ModelIdentifier()
	if !selected || model != "provider-scoped-model" {
		t.Fatalf("provider-scoped model selection = %#v", selection)
	}
}

func TestNewProviderRegistryRequiresExplicitAdapters(t *testing.T) {
	container := New()
	container.AIProviders = nil
	if _, err := container.NewProviderRegistry(); err == nil {
		t.Fatal("NewProviderRegistry() accepted no compile-time adapters")
	}

	descriptor, err := aiprovider.NewProviderDescriptor(
		"duplicate",
		"Test Vendor",
		"Duplicate",
		[]aiprovider.ProviderCapability{aiprovider.CapabilityWorkspaceMutation},
	)
	if err != nil {
		t.Fatalf("NewProviderDescriptor() error = %v", err)
	}
	provider := &compositionProvider{descriptor: descriptor}
	container.AIProviders = []aiprovider.Provider{provider, provider}
	if _, err := container.NewProviderRegistry(); err == nil {
		t.Fatal("NewProviderRegistry() accepted duplicate adapter identities")
	}
}

type compositionProvider struct {
	descriptor aiprovider.ProviderDescriptor
}

type attachmentValidationStore struct {
	authority.Store
	called *bool
	err    error
}

func (store *attachmentValidationStore) ValidateAttachment() error {
	*store.called = true
	return store.err
}

func TestProductionAttachValidatesAuthorityBeforeRuntimeStartup(t *testing.T) {
	dataHome := filepath.Join(t.TempDir(), "xdg")
	t.Setenv("XDG_DATA_HOME", dataHome)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(t.TempDir(), "cache"))
	registration := project.Registration{ProjectId: compositionProjectId, RepositoryRoot: t.TempDir(), SchemaVersion: 1, CreatedAt: time.Now().UTC()}
	container := New()
	container.RepositoryDiscovery = func(string) (repository.Context, error) {
		return repository.Context{Root: registration.RepositoryRoot}, nil
	}
	container.ProjectRegistration = func(string) (project.Registration, error) { return registration, nil }
	baseFactory := container.DurableStoreFactory
	called := false
	injected := errors.New("injected attachment corruption")
	container.DurableStoreFactory = func(dataDirectory string, value project.Registration) (authority.Store, error) {
		store, err := baseFactory(dataDirectory, value)
		if err != nil {
			return nil, err
		}
		return &attachmentValidationStore{Store: store, called: &called, err: injected}, nil
	}
	if _, err := container.NewInteractiveSession(registration.RepositoryRoot); !errors.Is(err, injected) {
		t.Fatalf("attachment validation error = %v", err)
	}
	if !called {
		t.Fatal("production attachment skipped durable authority validation")
	}
	events, err := sqliteadapter.ReadAuditEvents(filepath.Join(dataHome, "praetor"))
	if err != nil {
		t.Fatal(err)
	}
	if len(events) != 0 {
		t.Fatalf("failed attachment emitted startup authority: %#v", events)
	}
}

func TestOrdinaryAttachDoesNotImplicitlyMigrateLegacyAudit(t *testing.T) {
	dataHome := filepath.Join(t.TempDir(), "xdg")
	t.Setenv("XDG_DATA_HOME", dataHome)
	t.Setenv("XDG_CACHE_HOME", filepath.Join(t.TempDir(), "cache"))
	dataDirectory := filepath.Join(dataHome, "praetor")
	if err := os.MkdirAll(dataDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dataDirectory, "audit.log"), nil, 0o600); err != nil {
		t.Fatal(err)
	}
	registration := project.Registration{ProjectId: compositionProjectId, RepositoryRoot: t.TempDir(), SchemaVersion: 1, CreatedAt: time.Now().UTC()}
	container := New()
	container.RepositoryDiscovery = func(string) (repository.Context, error) {
		return repository.Context{Root: registration.RepositoryRoot}, nil
	}
	container.ProjectRegistration = func(string) (project.Registration, error) { return registration, nil }
	session, err := container.NewInteractiveSession(registration.RepositoryRoot)
	if err != nil {
		t.Fatal(err)
	}
	if err := session.Close(); err != nil {
		t.Fatal(err)
	}
	manifest := filepath.Join(dataDirectory, "audit-migration-v1.json")
	if _, err := os.Stat(manifest); !os.IsNotExist(err) {
		t.Fatalf("ordinary attach implicitly published migration manifest: %v", err)
	}
	if err := container.MigrateLegacyAudit(); err != nil {
		t.Fatalf("explicit legacy migration failed: %v", err)
	}
	if _, err := os.Stat(manifest); err != nil {
		t.Fatalf("explicit migration did not publish manifest: %v", err)
	}
}

func (provider *compositionProvider) Descriptor() aiprovider.ProviderDescriptor {
	return provider.descriptor
}

func (*compositionProvider) Execute(
	context.Context,
	aiprovider.ExecutionRequest,
) (aiprovider.ProviderResponse, error) {
	return aiprovider.ProviderResponse{}, errors.New("not executed")
}

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
	events, err := sqliteadapter.ReadAuditEvents(dataDirectory)
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

func TestNewRepositoryIntelligencePersistsM03AuditLinkage(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "xdg"))
	registration := project.Registration{
		ProjectId:      compositionProjectId,
		RepositoryRoot: "/tmp/repository",
	}
	currentChange, err := change.New(
		"change-surface-composed",
		compositionProjectId,
		"bound source paths",
		time.Date(2026, time.August, 31, 12, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("change.New() error = %v", err)
	}
	snapshot, err := source.NewSourceSnapshot(
		compositionProjectId,
		registration.RepositoryRoot,
		"0123456789abcdef",
		source.WorkingTreeClean,
		[]string{"go.mod", "internal/service/service.go", "internal/service/service_test.go"},
		"sha256:0123456789abcdef0123456789abcdef0123456789abcdef0123456789abcdef",
	)
	if err != nil {
		t.Fatalf("source.NewSourceSnapshot() error = %v", err)
	}

	container := New()
	container.RepositoryInspection = func(project.ProjectId, string) (source.SourceSnapshot, error) {
		return snapshot, nil
	}
	repositoryIntelligence, err := container.NewRepositoryIntelligence(registration)
	if err != nil {
		t.Fatalf("NewRepositoryIntelligence() error = %v", err)
	}
	prepared, err := repositoryIntelligence.EstablishSurface(
		currentChange,
		registration.RepositoryRoot,
		source.ScopeRequest{
			Expected:  []string{"internal/service/service.go"},
			Possible:  []string{"internal/service/service_test.go"},
			Protected: []string{"go.mod"},
		},
	)
	if err != nil {
		t.Fatalf("EstablishSurface() error = %v", err)
	}
	validation, err := repositoryIntelligence.ValidateActualSurface(
		prepared.ApprovedScope(),
		[]string{"internal/service/service.go", "go.mod"},
	)
	var validationError *source.SurfaceValidationError
	if !errors.As(err, &validationError) || validation.Allowed() {
		t.Fatalf("ValidateActualSurface() result = %#v, error = %v", validation, err)
	}

	dataDirectory, err := audit.ResolveDataDir()
	if err != nil {
		t.Fatalf("audit.ResolveDataDir() error = %v", err)
	}
	events, err := sqliteadapter.ReadAuditEvents(dataDirectory)
	if err != nil {
		t.Fatalf("audit.Read() error = %v", err)
	}
	if len(events) != 4 {
		t.Fatalf("M0.3 audit contains %d events, want 4", len(events))
	}
	wantTypes := []string{
		audit.EventSourceSnapshotCaptured,
		audit.EventImpactAnalysisProduced,
		audit.EventChangeSurfaceEstablished,
		audit.EventChangeSurfaceViolation,
	}
	for index, event := range events {
		if event.EventType != wantTypes[index] || event.ChangeID != "change-surface-composed" || event.ProjectID != string(compositionProjectId) {
			t.Fatalf("M0.3 event %d linkage = %#v", index, event)
		}
		if event.RepositoryRoot != registration.RepositoryRoot || event.Metadata["source_state_digest"] != string(snapshot.SourceStateDigest()) {
			t.Fatalf("M0.3 event %d source context = %#v", index, event)
		}
	}
	if events[0].Metadata["head_revision"] != snapshot.HeadRevision() || events[0].Metadata["working_tree_state"] != "clean" {
		t.Fatalf("SourceSnapshot audit metadata = %#v", events[0].Metadata)
	}
	if events[2].Metadata["scope_status"] != "approved-for-m0.3-validation" {
		t.Fatalf("ApprovedScope audit metadata = %#v", events[2].Metadata)
	}
	if events[3].Metadata["allowed"] != false {
		t.Fatalf("violation audit metadata = %#v", events[3].Metadata)
	}
	violations, ok := events[3].Metadata["violations"].([]any)
	if !ok || len(violations) != 1 {
		t.Fatalf("violation audit details = %#v", events[3].Metadata["violations"])
	}
}

func TestNewRepositoryIntelligenceRequiresM03Dependencies(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "xdg"))
	registration := project.Registration{
		ProjectId:      compositionProjectId,
		RepositoryRoot: "/tmp/repository",
	}
	tests := []struct {
		name   string
		mutate func(*Container)
	}{
		{name: "repository inspection", mutate: func(container *Container) { container.RepositoryInspection = nil }},
		{name: "Change audit logger", mutate: func(container *Container) { container.ChangeAuditLogger = nil }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			container := New()
			test.mutate(&container)
			if _, err := container.NewRepositoryIntelligence(registration); err == nil {
				t.Fatalf("NewRepositoryIntelligence() accepted missing %s", test.name)
			}
		})
	}
}

type compositionProposalAdapter struct {
	workspaceRoot string
}

func (adapter *compositionProposalAdapter) Create(request proposal.WorkspaceRequest) (proposal.ProposalWorkspace, error) {
	return proposal.NewProposalWorkspace(
		"proposal-00112233445566778899aabbccddeeff",
		request.ProjectId,
		request.ChangeId,
		request.CanonicalRoot,
		adapter.workspaceRoot,
		request.BaseRevision,
		request.SourceStateDigest,
	)
}

func (*compositionProposalAdapter) Extract(proposal.ProposalWorkspace) (proposal.ExtractedPatch, error) {
	return proposal.ExtractedPatch{
		Content:      []byte("secret patch body"),
		ChangedPaths: []string{"service.go"},
	}, nil
}

func (*compositionProposalAdapter) Remove(proposal.ProposalWorkspace) error { return nil }

func TestNewProposalServicePersistsBoundedM04AuditMetadata(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "xdg"))
	canonicalRoot := t.TempDir()
	workspaceRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(canonicalRoot, "service.go"), []byte("package service\n"), 0o600); err != nil {
		t.Fatalf("write fixture source: %v", err)
	}
	registration := project.Registration{ProjectId: compositionProjectId, RepositoryRoot: canonicalRoot}
	currentChange, err := change.New(
		"change-proposal-composed",
		compositionProjectId,
		"compose proposal",
		time.Date(2026, time.August, 31, 12, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("change.New() error = %v", err)
	}
	if _, err := currentChange.Transition(change.StatePlanned, time.Date(2026, time.August, 31, 12, 0, 1, 0, time.UTC), "planned"); err != nil {
		t.Fatalf("Change.Transition() error = %v", err)
	}
	snapshot, err := source.NewSourceSnapshot(
		compositionProjectId,
		canonicalRoot,
		strings.Repeat("a", 40),
		source.WorkingTreeClean,
		[]string{"service.go"},
		source.SourceStateDigest("sha256:"+strings.Repeat("0", 64)),
	)
	if err != nil {
		t.Fatalf("source.NewSourceSnapshot() error = %v", err)
	}
	analysis, err := source.AnalyzeImpact(currentChange, snapshot, source.ScopeRequest{Expected: []string{"service.go"}})
	if err != nil {
		t.Fatalf("source.AnalyzeImpact() error = %v", err)
	}
	approvedScope, err := source.EstablishApprovedScope(analysis)
	if err != nil {
		t.Fatalf("source.EstablishApprovedScope() error = %v", err)
	}

	adapter := &compositionProposalAdapter{workspaceRoot: workspaceRoot}
	container := New()
	container.ProposalWorkspaces = adapter
	container.PatchExtraction = adapter
	container.RepositoryInspection = func(project.ProjectId, string) (source.SourceSnapshot, error) {
		return snapshot, nil
	}
	container.ProposalClock = func() time.Time {
		return time.Date(2026, time.August, 31, 12, 0, 2, 0, time.UTC)
	}
	proposalService, err := container.NewProposalService(registration)
	if err != nil {
		t.Fatalf("NewProposalService() error = %v", err)
	}
	currentProposal, err := proposalService.CreateWorkspace(currentChange, snapshot, approvedScope)
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}
	currentProposal, validation, err := proposalService.ExtractPatch(currentProposal)
	if err != nil || !validation.Allowed() {
		t.Fatalf("ExtractPatch() validation/error = %#v/%v", validation, err)
	}
	if _, err := proposalService.Discard(currentProposal, "composition test complete"); err != nil {
		t.Fatalf("Discard() error = %v", err)
	}

	dataDirectory, err := audit.ResolveDataDir()
	if err != nil {
		t.Fatalf("audit.ResolveDataDir() error = %v", err)
	}
	events, err := sqliteadapter.ReadAuditEvents(dataDirectory)
	if err != nil {
		t.Fatalf("audit.Read() error = %v", err)
	}
	wantTypes := []string{
		audit.EventProposalWorkspaceCreated,
		audit.EventPatchExtracted,
		audit.EventPatchSurfaceValidated,
		audit.EventProposalWorkspaceDiscarded,
	}
	if len(events) != len(wantTypes) {
		t.Fatalf("M0.4 audit events = %#v", events)
	}
	for index, event := range events {
		if event.EventType != wantTypes[index] || event.ChangeID != string(currentChange.ChangeId()) || event.ProjectID != string(compositionProjectId) {
			t.Fatalf("M0.4 event %d linkage = %#v", index, event)
		}
		metadataText := fmt.Sprintf("%v", event.Metadata)
		if strings.Contains(metadataText, "secret patch body") || strings.Contains(metadataText, workspaceRoot) {
			t.Fatalf("M0.4 event stores patch body or workspace root: %#v", event.Metadata)
		}
		if event.Metadata["workspace_id"] == "" || event.Metadata["base_revision"] != snapshot.HeadRevision() ||
			event.Metadata["source_state_digest"] != string(snapshot.SourceStateDigest()) {
			t.Fatalf("M0.4 event %d provenance = %#v", index, event.Metadata)
		}
	}
	if events[2].Metadata["allowed"] != true || events[2].Metadata["disposition"] != "surface-valid" {
		t.Fatalf("surface-valid metadata = %#v", events[2].Metadata)
	}
}

func TestNewProposalServiceRequiresM04Dependencies(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "xdg"))
	registration := project.Registration{ProjectId: compositionProjectId, RepositoryRoot: "/tmp/repository"}
	tests := []struct {
		name   string
		mutate func(*Container)
	}{
		{name: "proposal workspace", mutate: func(container *Container) { container.ProposalWorkspaces = nil }},
		{name: "patch extraction", mutate: func(container *Container) { container.PatchExtraction = nil }},
		{name: "repository inspection", mutate: func(container *Container) { container.RepositoryInspection = nil }},
		{name: "Change audit logger", mutate: func(container *Container) { container.ChangeAuditLogger = nil }},
		{name: "proposal clock", mutate: func(container *Container) { container.ProposalClock = nil }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			container := New()
			test.mutate(&container)
			if _, err := container.NewProposalService(registration); err == nil {
				t.Fatalf("NewProposalService() accepted missing %s", test.name)
			}
		})
	}
}
