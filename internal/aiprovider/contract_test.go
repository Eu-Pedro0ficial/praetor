package aiprovider_test

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Eu-Pedro0ficial/praetor/internal/aiprovider"
	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"github.com/Eu-Pedro0ficial/praetor/internal/project"
	"github.com/Eu-Pedro0ficial/praetor/internal/proposal"
	"github.com/Eu-Pedro0ficial/praetor/internal/source"
)

const providerTestProjectId project.ProjectId = "01890c29-7a78-7abc-8def-0123456789ab"

func TestExecutionRequestPreservesProviderIndependentIsolationLinkage(t *testing.T) {
	currentChange, currentProposal := prepareExecutionRequest(t)
	selection, err := aiprovider.NewSelection("test-provider", "model/test-1")
	if err != nil {
		t.Fatalf("NewSelection() error = %v", err)
	}
	attemptId, err := aiprovider.GenerateExecutionAttemptId()
	if err != nil {
		t.Fatalf("GenerateExecutionAttemptId() error = %v", err)
	}
	request, err := aiprovider.NewExecutionRequest(
		attemptId,
		currentChange,
		currentProposal,
		aiprovider.ImplementationRoleContract(),
		selection,
	)
	if err != nil {
		t.Fatalf("NewExecutionRequest() error = %v", err)
	}

	workspace := currentProposal.Workspace()
	if request.ProjectId() != currentChange.ProjectId() || request.ChangeId() != currentChange.ChangeId() {
		t.Fatalf("request Change/Project linkage = %#v", request)
	}
	if request.Workspace().WorkspaceId() != workspace.WorkspaceId() || request.Workspace().Root() != workspace.Root() {
		t.Fatalf("request workspace linkage = %#v", request.Workspace())
	}
	if request.BaseRevision() != workspace.BaseRevision() || request.SourceStateDigest() != workspace.SourceStateDigest() {
		t.Fatalf("request source linkage = %#v", request)
	}
	if request.ApprovedScope().ChangeId() != currentChange.ChangeId() ||
		request.RoleContract().Role() != aiprovider.RoleImplementation {
		t.Fatalf("request scope/role linkage = %#v", request)
	}
	if strings.Contains(request.Workspace().Root(), currentProposal.Workspace().CanonicalRoot()) {
		t.Fatalf("isolated root %q unexpectedly nested in canonical %q", request.Workspace().Root(), workspace.CanonicalRoot())
	}
}

func TestExecutionRequestRejectsNonIsolatedChangeAndInactiveWorkspace(t *testing.T) {
	currentChange, currentProposal := prepareExecutionRequest(t)
	selection, _ := aiprovider.NewSelection("test-provider", "")
	attemptId, _ := aiprovider.GenerateExecutionAttemptId()

	createdChange, err := change.New("created-change", providerTestProjectId, "intent", time.Now().UTC())
	if err != nil {
		t.Fatalf("change.New() error = %v", err)
	}
	if _, err := aiprovider.NewExecutionRequest(
		attemptId,
		createdChange,
		currentProposal,
		aiprovider.ImplementationRoleContract(),
		selection,
	); err == nil || !strings.Contains(err.Error(), "must be isolated") {
		t.Fatalf("non-isolated request error = %v", err)
	}

	proposalService := requestTestProposalService(t, currentProposal.CanonicalSource())
	retained, _, err := proposalService.ExtractPatch(currentProposal)
	if err == nil || retained.Workspace().State() != proposal.WorkspaceRejected {
		t.Fatalf("empty workspace classification = %q/%v", retained.Workspace().State(), err)
	}
	if _, err := aiprovider.NewExecutionRequest(
		attemptId,
		currentChange,
		retained,
		aiprovider.ImplementationRoleContract(),
		selection,
	); err == nil || !strings.Contains(err.Error(), "not active") {
		t.Fatalf("inactive request error = %v", err)
	}
}

func TestSelectionKeepsModelProviderScopedAndOptional(t *testing.T) {
	selection, err := aiprovider.NewSelection("codex-cli", "gpt-test/model:1")
	if err != nil {
		t.Fatalf("NewSelection() error = %v", err)
	}
	model, selected := selection.ModelIdentifier()
	if selection.ProviderIdentifier() != "codex-cli" || !selected || model != "gpt-test/model:1" {
		t.Fatalf("selection = %#v", selection)
	}
	providerDefault, err := aiprovider.NewSelection("codex-cli", "")
	if err != nil {
		t.Fatalf("provider-default selection error = %v", err)
	}
	if _, selected := providerDefault.ModelIdentifier(); selected {
		t.Fatal("empty model became a hard-coded selection")
	}
	for _, test := range []struct{ provider, model string }{
		{provider: ""},
		{provider: "Codex CLI"},
		{provider: "codex-cli", model: "model with spaces"},
		{provider: "codex-cli", model: " model"},
	} {
		if _, err := aiprovider.NewSelection(test.provider, test.model); err == nil {
			t.Fatalf("NewSelection(%q, %q) succeeded", test.provider, test.model)
		}
	}
}

func TestProviderResponseNormalizesIdentityUsageAndTime(t *testing.T) {
	attemptId, _ := aiprovider.GenerateExecutionAttemptId()
	selection, _ := aiprovider.NewSelection("codex-cli", "model-1")
	usage, err := aiprovider.NewProviderUsage(10, 4, 5, 2)
	if err != nil {
		t.Fatalf("NewProviderUsage() error = %v", err)
	}
	started := time.Date(2026, time.September, 1, 12, 0, 0, 0, time.FixedZone("test", -3*60*60))
	response, err := aiprovider.NewProviderResponse(
		attemptId,
		selection,
		"provider-cli 1.0",
		"thread-1",
		"implemented",
		false,
		usage,
		started,
		started.Add(2*time.Second),
	)
	if err != nil {
		t.Fatalf("NewProviderResponse() error = %v", err)
	}
	if response.Outcome() != aiprovider.ProviderOutcomeCompleted || response.ExternalExecutionId() != "thread-1" {
		t.Fatalf("response outcome/identity = %#v", response)
	}
	if response.ProviderVersion() != "provider-cli 1.0" {
		t.Fatalf("response provider version = %q", response.ProviderVersion())
	}
	withoutExternalIdentity, err := aiprovider.NewProviderResponse(
		attemptId,
		selection,
		"provider-cli 1.0",
		"",
		"implemented",
		false,
		usage,
		started,
		started.Add(time.Second),
	)
	if err != nil || withoutExternalIdentity.ExternalExecutionId() != "" {
		t.Fatalf("optional external execution identity = %#v/%v", withoutExternalIdentity, err)
	}
	if !response.Usage().Available() || response.Usage().InputTokens() != 10 || response.Usage().OutputTokens() != 5 {
		t.Fatalf("response usage = %#v", response.Usage())
	}
	if response.StartedAt().Location() != time.UTC || response.CompletedAt().Sub(response.StartedAt()) != 2*time.Second {
		t.Fatalf("response time = %s/%s", response.StartedAt(), response.CompletedAt())
	}
	if _, err := aiprovider.NewProviderUsage(-1, 0, 0, 0); err == nil {
		t.Fatal("negative usage was accepted")
	}
	if _, err := aiprovider.NewProviderResponse(
		attemptId,
		selection,
		"provider-cli 1.0\nunsafe",
		"thread-version",
		"",
		false,
		aiprovider.ProviderUsage{},
		started,
		started.Add(time.Second),
	); err == nil {
		t.Fatal("unsafe provider version metadata was accepted")
	}
	if _, err := aiprovider.NewProviderResponse(
		attemptId,
		selection,
		"provider-cli 1.0",
		strings.Repeat("x", 513),
		"",
		false,
		aiprovider.ProviderUsage{},
		started,
		started.Add(time.Second),
	); err == nil {
		t.Fatal("unbounded external execution identity was accepted")
	}
	normalized := aiprovider.NewExecutionError(
		aiprovider.FailureProcess,
		"codex-cli",
		"unsafe external identity\nsecret",
		errors.New("process failed"),
	)
	var executionError *aiprovider.ExecutionError
	if !errors.As(normalized, &executionError) || executionError.ExternalExecutionId() != "" {
		t.Fatalf("unsafe failure identity was retained: %#v", executionError)
	}
}

func TestExecutionAttemptIdentityIsOpaqueAndUnique(t *testing.T) {
	first, err := aiprovider.GenerateExecutionAttemptId()
	if err != nil {
		t.Fatalf("first identity error = %v", err)
	}
	second, err := aiprovider.GenerateExecutionAttemptId()
	if err != nil {
		t.Fatalf("second identity error = %v", err)
	}
	if first == second || !strings.HasPrefix(string(first), "attempt-") || len(first) != len("attempt-")+32 {
		t.Fatalf("attempt identities = %q/%q", first, second)
	}
}

func TestImplementationRoleRequiresOnlyItsDeclaredCapabilities(t *testing.T) {
	contract := aiprovider.ImplementationRoleContract()
	if contract.Role() != aiprovider.RoleImplementation || string(contract.Role()) != "implementation" ||
		len(contract.RequiredCapabilities()) != 2 {
		t.Fatalf("implementation role contract = %#v", contract)
	}
	incomplete, err := aiprovider.NewProviderDescriptor(
		"incomplete-provider",
		"Test Vendor",
		"Incomplete Provider",
		[]aiprovider.ProviderCapability{aiprovider.CapabilityWorkspaceMutation},
	)
	if err != nil {
		t.Fatalf("NewProviderDescriptor() error = %v", err)
	}
	if err := aiprovider.ValidateRole(incomplete, contract); err == nil ||
		!strings.Contains(err.Error(), string(aiprovider.CapabilityContextCancellation)) {
		t.Fatalf("ValidateRole() missing-capability error = %v", err)
	}
	complete, err := aiprovider.NewProviderDescriptor(
		"complete-provider",
		"Test Vendor",
		"Complete Provider",
		contract.RequiredCapabilities(),
	)
	if err != nil {
		t.Fatalf("NewProviderDescriptor() error = %v", err)
	}
	if err := aiprovider.ValidateRole(complete, contract); err != nil {
		t.Fatalf("ValidateRole() error = %v", err)
	}
}

func TestVerificationPlanningRoleRequiresReadOnlyCapability(t *testing.T) {
	contract := aiprovider.VerificationPlanningRoleContract()
	if contract.Role() != aiprovider.RoleVerificationPlanning ||
		contract.WorkspaceAccess() != aiprovider.WorkspaceAccessReadOnly {
		t.Fatalf("verification-planning role contract = %#v", contract)
	}
	incomplete, err := aiprovider.NewProviderDescriptor(
		"write-only-provider",
		"Test Vendor",
		"Write-only Provider",
		[]aiprovider.ProviderCapability{
			aiprovider.CapabilityWorkspaceMutation,
			aiprovider.CapabilityContextCancellation,
		},
	)
	if err != nil {
		t.Fatalf("NewProviderDescriptor() error = %v", err)
	}
	if err := aiprovider.ValidateRole(incomplete, contract); err == nil ||
		!strings.Contains(err.Error(), string(aiprovider.CapabilityWorkspaceReadOnly)) {
		t.Fatalf("ValidateRole() read-only error = %v", err)
	}
	complete, err := aiprovider.NewProviderDescriptor(
		"planner-provider",
		"Test Vendor",
		"Planner Provider",
		contract.RequiredCapabilities(),
	)
	if err != nil {
		t.Fatalf("NewProviderDescriptor() error = %v", err)
	}
	if err := aiprovider.ValidateRole(complete, contract); err != nil {
		t.Fatalf("ValidateRole() error = %v", err)
	}
}

type requestWorkspacePort struct{ workspaceRoot string }

func (port requestWorkspacePort) Create(request proposal.WorkspaceRequest) (proposal.ProposalWorkspace, error) {
	return proposal.NewProposalWorkspace(
		"proposal-0123456789abcdef0123456789abcdef",
		request.ProjectId,
		request.ChangeId,
		request.CanonicalRoot,
		port.workspaceRoot,
		request.BaseRevision,
		request.SourceStateDigest,
	)
}

func (requestWorkspacePort) Remove(proposal.ProposalWorkspace) error { return nil }

type requestPatchPort struct{}

func (requestPatchPort) Extract(proposal.ProposalWorkspace) (proposal.ExtractedPatch, error) {
	return proposal.ExtractedPatch{}, nil
}

func prepareExecutionRequest(t *testing.T) (change.Change, proposal.Proposal) {
	t.Helper()
	canonicalRoot := t.TempDir()
	workspaceRoot := t.TempDir()
	if err := os.WriteFile(filepath.Join(canonicalRoot, "service.go"), []byte("package service\n"), 0o600); err != nil {
		t.Fatalf("write canonical fixture: %v", err)
	}
	createdAt := time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC)
	currentChange, err := change.New("change-provider", providerTestProjectId, "implement greeting", createdAt)
	if err != nil {
		t.Fatalf("change.New() error = %v", err)
	}
	if _, err := currentChange.Transition(change.StatePlanned, createdAt.Add(time.Second), "planned"); err != nil {
		t.Fatalf("planned transition error = %v", err)
	}
	snapshot, err := source.NewSourceSnapshot(
		providerTestProjectId,
		canonicalRoot,
		"0123456789abcdef0123456789abcdef01234567",
		source.WorkingTreeClean,
		[]string{"service.go"},
		source.SourceStateDigest("sha256:"+strings.Repeat("a", 64)),
	)
	if err != nil {
		t.Fatalf("NewSourceSnapshot() error = %v", err)
	}
	analysis, err := source.AnalyzeImpact(currentChange, snapshot, source.ScopeRequest{Expected: []string{"service.go"}})
	if err != nil {
		t.Fatalf("AnalyzeImpact() error = %v", err)
	}
	approvedScope, err := source.EstablishApprovedScope(analysis)
	if err != nil {
		t.Fatalf("EstablishApprovedScope() error = %v", err)
	}
	proposalService := requestTestProposalServiceWithWorkspace(t, snapshot, workspaceRoot)
	currentProposal, err := proposalService.CreateWorkspace(currentChange, snapshot, approvedScope)
	if err != nil {
		t.Fatalf("CreateWorkspace() error = %v", err)
	}
	if _, err := currentChange.Transition(change.StateIsolated, createdAt.Add(2*time.Second), "isolated"); err != nil {
		t.Fatalf("isolated transition error = %v", err)
	}
	return currentChange, currentProposal
}

func requestTestProposalService(t *testing.T, snapshot source.SourceSnapshot) *proposal.Service {
	t.Helper()
	return requestTestProposalServiceWithWorkspace(t, snapshot, t.TempDir())
}

func requestTestProposalServiceWithWorkspace(
	t *testing.T,
	snapshot source.SourceSnapshot,
	workspaceRoot string,
) *proposal.Service {
	t.Helper()
	service, err := proposal.New(
		requestWorkspacePort{workspaceRoot: workspaceRoot},
		requestPatchPort{},
		func(project.ProjectId, string) (source.SourceSnapshot, error) { return snapshot, nil },
		func(proposal.LifecycleEvent) error { return nil },
		func() time.Time { return time.Now().UTC() },
	)
	if err != nil {
		t.Fatalf("proposal.New() error = %v", err)
	}
	return service
}

type registryProvider struct{ descriptor aiprovider.ProviderDescriptor }

func newRegistryProvider(t *testing.T, identifier string) *registryProvider {
	t.Helper()
	descriptor, err := aiprovider.NewProviderDescriptor(
		identifier,
		"Test Vendor",
		identifier,
		[]aiprovider.ProviderCapability{
			aiprovider.CapabilityWorkspaceMutation,
			aiprovider.CapabilityContextCancellation,
		},
	)
	if err != nil {
		t.Fatalf("NewProviderDescriptor() error = %v", err)
	}
	return &registryProvider{descriptor: descriptor}
}

func (provider *registryProvider) Descriptor() aiprovider.ProviderDescriptor {
	return provider.descriptor
}
func (*registryProvider) Execute(context.Context, aiprovider.ExecutionRequest) (aiprovider.ProviderResponse, error) {
	return aiprovider.ProviderResponse{}, errors.New("not used")
}

func TestRegistryUsesExactSelectionWithoutRoutingOrFallback(t *testing.T) {
	first := newRegistryProvider(t, "first-provider")
	second := newRegistryProvider(t, "second-provider")
	registry, err := aiprovider.NewRegistry(first, second)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	selection, err := registry.Select("second-provider", "model-b")
	if err != nil {
		t.Fatalf("Select() error = %v", err)
	}
	resolved, err := registry.Resolve(selection)
	if err != nil {
		t.Fatalf("Resolve() error = %v", err)
	}
	if resolved != second {
		t.Fatalf("Resolve() selected %#v, want exact second adapter", resolved)
	}
	if _, err := registry.Select("missing-provider", ""); err == nil {
		t.Fatal("unknown provider unexpectedly fell back")
	}
	if descriptors := registry.Descriptors(); len(descriptors) != 2 || descriptors[0].Identifier() != "first-provider" {
		t.Fatalf("Descriptors() = %#v", descriptors)
	}
	if _, err := aiprovider.NewRegistry(first, first); err == nil {
		t.Fatal("duplicate provider registration succeeded")
	}
}
