// Package aiprovider owns Praetor's provider-independent AI execution
// contract. Concrete provider protocols and process details belong to adapters.
package aiprovider

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"github.com/Eu-Pedro0ficial/praetor/internal/project"
	"github.com/Eu-Pedro0ficial/praetor/internal/proposal"
	"github.com/Eu-Pedro0ficial/praetor/internal/source"
)

// ProviderIdentifier is a stable adapter identifier used for explicit runtime
// selection. It is not a model identifier or a routing policy.
type ProviderIdentifier string

// ModelIdentifier is provider-scoped execution metadata. An absent model
// means the selected provider's configured default.
type ModelIdentifier string

// ExecutionAttemptId is Praetor-owned opaque identity for one provider call.
// Provider request or thread identifiers remain external metadata.
type ExecutionAttemptId string

// ProviderCapability is descriptive M0.5 adapter contract metadata. It does
// not rank, route, or automatically select providers.
type ProviderCapability string

const (
	CapabilityWorkspaceMutation   ProviderCapability = "workspace-mutation"
	CapabilityWorkspaceReadOnly   ProviderCapability = "workspace-read-only"
	CapabilityNonInteractive      ProviderCapability = "non-interactive-execution"
	CapabilityStructuredEvents    ProviderCapability = "structured-events"
	CapabilityContextCancellation ProviderCapability = "context-cancellation"
)

// ProviderRole identifies one explicitly supported AI responsibility.
type ProviderRole string

const (
	RoleImplementation       ProviderRole = "implementation"
	RoleVerificationPlanning ProviderRole = "verification-planning"
)

// WorkspaceAccess expresses the provider-independent filesystem authority of
// one invocation. Concrete adapter sandbox names do not cross this boundary.
type WorkspaceAccess string

const (
	WorkspaceAccessReadOnly WorkspaceAccess = "read-only"
	WorkspaceAccessWrite    WorkspaceAccess = "workspace-write"
)

// ProviderRoleContract states what the selected provider invocation is doing
// and the minimum capabilities required for that invocation.
type ProviderRoleContract struct {
	role                 ProviderRole
	workspaceAccess      WorkspaceAccess
	requiredCapabilities []ProviderCapability
}

// ImplementationRoleContract returns the narrow M0.5 implementation role.
func ImplementationRoleContract() ProviderRoleContract {
	return ProviderRoleContract{
		role:            RoleImplementation,
		workspaceAccess: WorkspaceAccessWrite,
		requiredCapabilities: []ProviderCapability{
			CapabilityWorkspaceMutation,
			CapabilityContextCancellation,
		},
	}
}

// VerificationPlanningRoleContract returns the read-only M0.6 planning role.
func VerificationPlanningRoleContract() ProviderRoleContract {
	return ProviderRoleContract{
		role:            RoleVerificationPlanning,
		workspaceAccess: WorkspaceAccessReadOnly,
		requiredCapabilities: []ProviderCapability{
			CapabilityWorkspaceReadOnly,
			CapabilityContextCancellation,
		},
	}
}

// Role returns the role identity.
func (contract ProviderRoleContract) Role() ProviderRole { return contract.role }

// WorkspaceAccess returns the invocation's provider-independent write boundary.
func (contract ProviderRoleContract) WorkspaceAccess() WorkspaceAccess {
	return contract.workspaceAccess
}

// RequiredCapabilities returns a defensive copy of contract requirements.
func (contract ProviderRoleContract) RequiredCapabilities() []ProviderCapability {
	return append([]ProviderCapability(nil), contract.requiredCapabilities...)
}

func (contract ProviderRoleContract) validate() error {
	switch contract.role {
	case RoleImplementation:
		if contract.workspaceAccess != WorkspaceAccessWrite {
			return fmt.Errorf("implementation role requires workspace-write access")
		}
	case RoleVerificationPlanning:
		if contract.workspaceAccess != WorkspaceAccessReadOnly {
			return fmt.Errorf("verification-planning role requires read-only access")
		}
	default:
		return fmt.Errorf("unknown ProviderRole %q", contract.role)
	}
	if len(contract.requiredCapabilities) == 0 {
		return fmt.Errorf("provider role requires capability metadata")
	}
	seen := make(map[ProviderCapability]struct{}, len(contract.requiredCapabilities))
	for _, capability := range contract.requiredCapabilities {
		if !isKnownCapability(capability) {
			return fmt.Errorf("unknown ProviderCapability %q", capability)
		}
		if _, duplicate := seen[capability]; duplicate {
			return fmt.Errorf("duplicate ProviderCapability %q", capability)
		}
		seen[capability] = struct{}{}
	}
	return nil
}

// ProviderDescriptor exposes provider-independent identity and descriptive
// capability metadata for one compile-time registered adapter.
type ProviderDescriptor struct {
	identifier   ProviderIdentifier
	vendor       string
	displayName  string
	capabilities []ProviderCapability
}

// NewProviderDescriptor validates adapter-owned provider metadata.
func NewProviderDescriptor(
	identifier string,
	vendor string,
	displayName string,
	capabilities []ProviderCapability,
) (ProviderDescriptor, error) {
	providerIdentifier, err := parseProviderIdentifier(identifier)
	if err != nil {
		return ProviderDescriptor{}, err
	}
	vendorName := strings.TrimSpace(vendor)
	if vendorName == "" || vendorName != vendor || len(vendorName) > 128 {
		return ProviderDescriptor{}, fmt.Errorf("provider vendor is required")
	}
	name := strings.TrimSpace(displayName)
	if name == "" || name != displayName || len(name) > 128 {
		return ProviderDescriptor{}, fmt.Errorf("provider display name is required")
	}
	for _, value := range []string{vendorName, name} {
		for _, character := range value {
			if unicode.IsControl(character) {
				return ProviderDescriptor{}, fmt.Errorf("provider descriptive metadata contains control characters")
			}
		}
	}
	if len(capabilities) == 0 {
		return ProviderDescriptor{}, fmt.Errorf("provider capabilities are required")
	}
	seen := make(map[ProviderCapability]struct{}, len(capabilities))
	validated := make([]ProviderCapability, 0, len(capabilities))
	for _, capability := range capabilities {
		if !isKnownCapability(capability) {
			return ProviderDescriptor{}, fmt.Errorf("unknown ProviderCapability %q", capability)
		}
		if _, duplicate := seen[capability]; duplicate {
			return ProviderDescriptor{}, fmt.Errorf("duplicate ProviderCapability %q", capability)
		}
		seen[capability] = struct{}{}
		validated = append(validated, capability)
	}
	return ProviderDescriptor{
		identifier:   providerIdentifier,
		vendor:       vendorName,
		displayName:  name,
		capabilities: validated,
	}, nil
}

// Identifier returns the explicit adapter identifier.
func (descriptor ProviderDescriptor) Identifier() ProviderIdentifier {
	return descriptor.identifier
}

// Vendor returns descriptive vendor identity independent from adapter identity.
func (descriptor ProviderDescriptor) Vendor() string { return descriptor.vendor }

// DisplayName returns human-facing provider metadata.
func (descriptor ProviderDescriptor) DisplayName() string { return descriptor.displayName }

// Capabilities returns a defensive copy of descriptive metadata.
func (descriptor ProviderDescriptor) Capabilities() []ProviderCapability {
	return append([]ProviderCapability(nil), descriptor.capabilities...)
}

// Supports reports contractual compatibility without selecting or routing.
func (descriptor ProviderDescriptor) Supports(capability ProviderCapability) bool {
	for _, available := range descriptor.capabilities {
		if available == capability {
			return true
		}
	}
	return false
}

// Selection is explicit session/runtime provider and optional provider-scoped
// model configuration.
type Selection struct {
	provider ProviderIdentifier
	model    ModelIdentifier
}

// NewSelection validates provider-independent selection metadata.
func NewSelection(providerValue string, modelValue string) (Selection, error) {
	providerIdentifier, err := parseProviderIdentifier(providerValue)
	if err != nil {
		return Selection{}, err
	}
	modelIdentifier, err := parseModelIdentifier(modelValue)
	if err != nil {
		return Selection{}, err
	}
	return Selection{provider: providerIdentifier, model: modelIdentifier}, nil
}

// ProviderIdentifier returns the manually/configurationally selected adapter.
func (selection Selection) ProviderIdentifier() ProviderIdentifier { return selection.provider }

// ModelIdentifier returns the selected provider-scoped model when present.
func (selection Selection) ModelIdentifier() (ModelIdentifier, bool) {
	return selection.model, selection.model != ""
}

// ExecutionRequest contains normalized Praetor semantics for one isolated
// implementation attempt. It contains no credentials or provider wire types.
type ExecutionRequest struct {
	attemptId     ExecutionAttemptId
	projectId     project.ProjectId
	changeId      change.ChangeId
	intent        change.ChangeIntent
	workspace     ExecutionWorkspace
	approvedScope source.ApprovedScope
	baseRevision  string
	sourceDigest  source.SourceStateDigest
	roleContract  ProviderRoleContract
	selection     Selection
	planningInput VerificationPlanningInput
	hasPlanning   bool
}

// PlanningEvidence is bounded repository context supplied to the read-only
// verification planner. It is runtime input, not a persistent representation.
type PlanningEvidence struct {
	path    source.RepositoryPath
	kind    string
	content string
}

// NewPlanningEvidence validates one bounded provider-facing evidence item.
func NewPlanningEvidence(pathValue string, kindValue string, content string) (PlanningEvidence, error) {
	repositoryPath, err := source.NormalizeRepositoryPath(pathValue)
	if err != nil {
		return PlanningEvidence{}, err
	}
	kind := strings.TrimSpace(kindValue)
	if kind == "" || kind != kindValue || len(kind) > 64 {
		return PlanningEvidence{}, fmt.Errorf("planning evidence kind is invalid")
	}
	if len(content) > 32<<10 {
		return PlanningEvidence{}, fmt.Errorf("planning evidence content exceeds 32 KiB")
	}
	return PlanningEvidence{path: repositoryPath, kind: kind, content: content}, nil
}

func (evidence PlanningEvidence) Path() source.RepositoryPath { return evidence.path }
func (evidence PlanningEvidence) Kind() string                { return evidence.kind }
func (evidence PlanningEvidence) Content() string             { return evidence.content }

// VerificationPlanningInput carries only the bounded patch and repository
// context needed by the M0.6 read-only role.
type VerificationPlanningInput struct {
	patchDigest  string
	diffSummary  string
	changedPaths []string
	evidence     []PlanningEvidence
}

func newVerificationPlanningInput(
	artifact proposal.PatchArtifact,
	evidence []PlanningEvidence,
) (VerificationPlanningInput, error) {
	if len(evidence) > 64 {
		return VerificationPlanningInput{}, fmt.Errorf("verification planning evidence exceeds 64 items")
	}
	totalBytes := 0
	items := make([]PlanningEvidence, len(evidence))
	for index, item := range evidence {
		if item.path == "" || item.kind == "" {
			return VerificationPlanningInput{}, fmt.Errorf("verification planning evidence item %d is invalid", index)
		}
		totalBytes += len(item.content)
		if totalBytes > 256<<10 {
			return VerificationPlanningInput{}, fmt.Errorf("verification planning evidence exceeds 256 KiB")
		}
		items[index] = item
	}
	return VerificationPlanningInput{
		patchDigest:  artifact.PatchDigest(),
		diffSummary:  artifact.DiffSummary(),
		changedPaths: artifact.ChangedPaths(),
		evidence:     items,
	}, nil
}

func (input VerificationPlanningInput) PatchDigest() string { return input.patchDigest }
func (input VerificationPlanningInput) DiffSummary() string { return input.diffSummary }
func (input VerificationPlanningInput) ChangedPaths() []string {
	return append([]string(nil), input.changedPaths...)
}
func (input VerificationPlanningInput) Evidence() []PlanningEvidence {
	return append([]PlanningEvidence(nil), input.evidence...)
}

// ExecutionWorkspace is the safe provider-facing projection of an existing
// ProposalWorkspace. It intentionally omits canonical RepositoryRoot.
type ExecutionWorkspace struct {
	workspaceId  proposal.WorkspaceId
	root         string
	baseRevision string
	sourceDigest source.SourceStateDigest
}

func (workspace ExecutionWorkspace) WorkspaceId() proposal.WorkspaceId { return workspace.workspaceId }
func (workspace ExecutionWorkspace) Root() string                      { return workspace.root }
func (workspace ExecutionWorkspace) BaseRevision() string              { return workspace.baseRevision }
func (workspace ExecutionWorkspace) SourceStateDigest() source.SourceStateDigest {
	return workspace.sourceDigest
}

// NewExecutionRequest derives and validates all linkage from the existing
// isolated Change and Proposal rather than accepting arbitrary paths.
func NewExecutionRequest(
	attemptId ExecutionAttemptId,
	currentChange change.Change,
	currentProposal proposal.Proposal,
	roleContract ProviderRoleContract,
	selection Selection,
) (ExecutionRequest, error) {
	if roleContract.Role() != RoleImplementation {
		return ExecutionRequest{}, fmt.Errorf("implementation request requires the implementation provider role")
	}
	return newExecutionRequest(
		attemptId,
		currentChange,
		currentProposal,
		roleContract,
		selection,
		VerificationPlanningInput{},
		false,
	)
}

// NewVerificationPlanningRequest constructs one fresh, read-only provider
// request against a retained surface-valid proposal.
func NewVerificationPlanningRequest(
	attemptId ExecutionAttemptId,
	currentChange change.Change,
	currentProposal proposal.Proposal,
	selection Selection,
	evidence []PlanningEvidence,
) (ExecutionRequest, error) {
	artifact, hasArtifact := currentProposal.PatchArtifact()
	if !hasArtifact {
		return ExecutionRequest{}, fmt.Errorf("verification planning requires a retained PatchArtifact")
	}
	planningInput, err := newVerificationPlanningInput(artifact, evidence)
	if err != nil {
		return ExecutionRequest{}, err
	}
	return newExecutionRequest(
		attemptId,
		currentChange,
		currentProposal,
		VerificationPlanningRoleContract(),
		selection,
		planningInput,
		true,
	)
}

func newExecutionRequest(
	attemptId ExecutionAttemptId,
	currentChange change.Change,
	currentProposal proposal.Proposal,
	roleContract ProviderRoleContract,
	selection Selection,
	planningInput VerificationPlanningInput,
	hasPlanning bool,
) (ExecutionRequest, error) {
	if err := validateExecutionAttemptId(attemptId); err != nil {
		return ExecutionRequest{}, err
	}
	if currentChange.State() != change.StateIsolated {
		return ExecutionRequest{}, fmt.Errorf(
			"Change %q must be isolated before provider execution",
			currentChange.ChangeId(),
		)
	}
	if err := roleContract.validate(); err != nil {
		return ExecutionRequest{}, err
	}
	if selection.provider == "" {
		return ExecutionRequest{}, fmt.Errorf("selected provider is required")
	}

	workspace := currentProposal.Workspace()
	approvedScope := currentProposal.ApprovedScope()
	canonicalSource := currentProposal.CanonicalSource()
	requiredWorkspaceState := proposal.WorkspaceActive
	if roleContract.Role() == RoleVerificationPlanning {
		requiredWorkspaceState = proposal.WorkspaceRetained
	}
	if workspace.State() != requiredWorkspaceState {
		if roleContract.Role() == RoleImplementation {
			return ExecutionRequest{}, fmt.Errorf(
				"proposal workspace %q is not active for provider execution",
				workspace.WorkspaceId(),
			)
		}
		return ExecutionRequest{}, fmt.Errorf(
			"proposal workspace %q must be %q for role %q",
			workspace.WorkspaceId(),
			requiredWorkspaceState,
			roleContract.Role(),
		)
	}
	if currentChange.ProjectId() != workspace.ProjectId() ||
		currentChange.ProjectId() != approvedScope.ProjectId() ||
		currentChange.ProjectId() != canonicalSource.ProjectId() {
		return ExecutionRequest{}, fmt.Errorf("provider execution ProjectId linkage is inconsistent")
	}
	if currentChange.ChangeId() != workspace.ChangeId() ||
		currentChange.ChangeId() != approvedScope.ChangeId() {
		return ExecutionRequest{}, fmt.Errorf("provider execution ChangeId linkage is inconsistent")
	}
	if workspace.BaseRevision() != canonicalSource.HeadRevision() ||
		workspace.SourceStateDigest() != canonicalSource.SourceStateDigest() ||
		workspace.SourceStateDigest() != approvedScope.SourceStateDigest() {
		return ExecutionRequest{}, fmt.Errorf("provider execution source-state linkage is inconsistent")
	}
	if workspace.Root() == "" || workspace.Root() == workspace.CanonicalRoot() {
		return ExecutionRequest{}, fmt.Errorf("provider execution requires an isolated ProposalWorkspace")
	}

	return ExecutionRequest{
		attemptId: attemptId,
		projectId: currentChange.ProjectId(),
		changeId:  currentChange.ChangeId(),
		intent:    currentChange.Intent(),
		workspace: ExecutionWorkspace{
			workspaceId:  workspace.WorkspaceId(),
			root:         workspace.Root(),
			baseRevision: workspace.BaseRevision(),
			sourceDigest: workspace.SourceStateDigest(),
		},
		approvedScope: approvedScope,
		baseRevision:  workspace.BaseRevision(),
		sourceDigest:  workspace.SourceStateDigest(),
		roleContract:  roleContract,
		selection:     selection,
		planningInput: planningInput,
		hasPlanning:   hasPlanning,
	}, nil
}

func (request ExecutionRequest) AttemptId() ExecutionAttemptId { return request.attemptId }
func (request ExecutionRequest) ProjectId() project.ProjectId  { return request.projectId }
func (request ExecutionRequest) ChangeId() change.ChangeId     { return request.changeId }
func (request ExecutionRequest) Intent() change.ChangeIntent   { return request.intent }
func (request ExecutionRequest) Workspace() ExecutionWorkspace {
	return request.workspace
}
func (request ExecutionRequest) ApprovedScope() source.ApprovedScope {
	return request.approvedScope
}
func (request ExecutionRequest) BaseRevision() string { return request.baseRevision }
func (request ExecutionRequest) SourceStateDigest() source.SourceStateDigest {
	return request.sourceDigest
}
func (request ExecutionRequest) RoleContract() ProviderRoleContract {
	return request.roleContract
}
func (request ExecutionRequest) Selection() Selection { return request.selection }
func (request ExecutionRequest) VerificationPlanningInput() (VerificationPlanningInput, bool) {
	return request.planningInput, request.hasPlanning
}

// ProviderUsage normalizes token usage when the adapter supplies it.
type ProviderUsage struct {
	available             bool
	inputTokens           int64
	cachedInputTokens     int64
	outputTokens          int64
	reasoningOutputTokens int64
}

// NewProviderUsage validates provider-supplied usage counts.
func NewProviderUsage(inputTokens, cachedInputTokens, outputTokens, reasoningOutputTokens int64) (ProviderUsage, error) {
	for label, value := range map[string]int64{
		"input":            inputTokens,
		"cached input":     cachedInputTokens,
		"output":           outputTokens,
		"reasoning output": reasoningOutputTokens,
	} {
		if value < 0 {
			return ProviderUsage{}, fmt.Errorf("provider %s token count cannot be negative", label)
		}
	}
	return ProviderUsage{
		available:             true,
		inputTokens:           inputTokens,
		cachedInputTokens:     cachedInputTokens,
		outputTokens:          outputTokens,
		reasoningOutputTokens: reasoningOutputTokens,
	}, nil
}

func (usage ProviderUsage) Available() bool              { return usage.available }
func (usage ProviderUsage) InputTokens() int64           { return usage.inputTokens }
func (usage ProviderUsage) CachedInputTokens() int64     { return usage.cachedInputTokens }
func (usage ProviderUsage) OutputTokens() int64          { return usage.outputTokens }
func (usage ProviderUsage) ReasoningOutputTokens() int64 { return usage.reasoningOutputTokens }

// ProviderOutcome is the normalized result of the provider call itself. Patch
// acceptance remains owned by the M0.4 lifecycle.
type ProviderOutcome string

const ProviderOutcomeCompleted ProviderOutcome = "completed"

// ProviderResponse is untrusted provider evidence/provenance. The modified
// ProposalWorkspace and Git-extracted PatchArtifact remain source authority.
type ProviderResponse struct {
	attemptId           ExecutionAttemptId
	selection           Selection
	providerVersion     string
	externalExecutionId string
	outcome             ProviderOutcome
	summary             string
	summaryTruncated    bool
	usage               ProviderUsage
	startedAt           time.Time
	completedAt         time.Time
}

// NewProviderResponse validates one successfully completed provider call.
func NewProviderResponse(
	attemptId ExecutionAttemptId,
	selection Selection,
	providerVersion string,
	externalExecutionId string,
	summary string,
	summaryTruncated bool,
	usage ProviderUsage,
	startedAt time.Time,
	completedAt time.Time,
) (ProviderResponse, error) {
	if err := validateExecutionAttemptId(attemptId); err != nil {
		return ProviderResponse{}, err
	}
	if selection.provider == "" {
		return ProviderResponse{}, fmt.Errorf("provider response selection is required")
	}
	if err := validateProviderVersion(providerVersion); err != nil {
		return ProviderResponse{}, err
	}
	externalIdentifier, err := parseExternalExecutionIdentifier(externalExecutionId)
	if err != nil {
		return ProviderResponse{}, err
	}
	if startedAt.IsZero() || completedAt.IsZero() || completedAt.Before(startedAt) {
		return ProviderResponse{}, fmt.Errorf("provider response timestamps are invalid")
	}
	return ProviderResponse{
		attemptId:           attemptId,
		selection:           selection,
		providerVersion:     providerVersion,
		externalExecutionId: externalIdentifier,
		outcome:             ProviderOutcomeCompleted,
		summary:             summary,
		summaryTruncated:    summaryTruncated,
		usage:               usage,
		startedAt:           startedAt.UTC(),
		completedAt:         completedAt.UTC(),
	}, nil
}

func (response ProviderResponse) AttemptId() ExecutionAttemptId { return response.attemptId }
func (response ProviderResponse) Selection() Selection          { return response.selection }
func (response ProviderResponse) ProviderVersion() string       { return response.providerVersion }
func (response ProviderResponse) ExternalExecutionId() string   { return response.externalExecutionId }
func (response ProviderResponse) Outcome() ProviderOutcome      { return response.outcome }
func (response ProviderResponse) Summary() string               { return response.summary }
func (response ProviderResponse) SummaryTruncated() bool        { return response.summaryTruncated }
func (response ProviderResponse) Usage() ProviderUsage          { return response.usage }
func (response ProviderResponse) StartedAt() time.Time          { return response.startedAt }
func (response ProviderResponse) CompletedAt() time.Time        { return response.completedAt }

// Provider is the provider-independent AI Provider Port.
type Provider interface {
	Descriptor() ProviderDescriptor
	Execute(context.Context, ExecutionRequest) (ProviderResponse, error)
}

// GenerateExecutionAttemptId creates random Praetor-owned attempt identity.
func GenerateExecutionAttemptId() (ExecutionAttemptId, error) {
	var randomBytes [16]byte
	if _, err := rand.Read(randomBytes[:]); err != nil {
		return "", fmt.Errorf("generate ExecutionAttemptId: %w", err)
	}
	return ExecutionAttemptId("attempt-" + hex.EncodeToString(randomBytes[:])), nil
}

func validateExecutionAttemptId(attemptId ExecutionAttemptId) error {
	const prefix = "attempt-"
	value := string(attemptId)
	if !strings.HasPrefix(value, prefix) {
		return fmt.Errorf("ExecutionAttemptId must use the attempt prefix")
	}
	encoded := strings.TrimPrefix(value, prefix)
	if len(encoded) != 32 {
		return fmt.Errorf("ExecutionAttemptId must contain 128 bits of random identity")
	}
	if _, err := hex.DecodeString(encoded); err != nil {
		return fmt.Errorf("ExecutionAttemptId contains invalid random identity: %w", err)
	}
	return nil
}

func parseProviderIdentifier(value string) (ProviderIdentifier, error) {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "", fmt.Errorf("provider identifier is required")
	}
	if trimmed != value || len(trimmed) > 64 {
		return "", fmt.Errorf("invalid provider identifier %q", value)
	}
	for index, character := range trimmed {
		valid := character >= 'a' && character <= 'z' ||
			character >= '0' && character <= '9' ||
			(index > 0 && (character == '-' || character == '.'))
		if !valid {
			return "", fmt.Errorf("invalid provider identifier %q", value)
		}
	}
	return ProviderIdentifier(trimmed), nil
}

func parseModelIdentifier(value string) (ModelIdentifier, error) {
	if value == "" {
		return "", nil
	}
	if strings.TrimSpace(value) != value || len(value) > 256 {
		return "", fmt.Errorf("invalid provider-scoped model identifier")
	}
	for _, character := range value {
		if unicode.IsSpace(character) || unicode.IsControl(character) {
			return "", fmt.Errorf("invalid provider-scoped model identifier")
		}
	}
	return ModelIdentifier(value), nil
}

func parseExternalExecutionIdentifier(value string) (string, error) {
	if value == "" {
		return "", nil
	}
	trimmed := strings.TrimSpace(value)
	if trimmed == "" || trimmed != value || len(trimmed) > 512 {
		return "", fmt.Errorf("invalid provider external execution identity")
	}
	for _, character := range trimmed {
		if unicode.IsSpace(character) || unicode.IsControl(character) {
			return "", fmt.Errorf("invalid provider external execution identity")
		}
	}
	return trimmed, nil
}

func validateProviderVersion(value string) error {
	if value == "" {
		return nil
	}
	if strings.TrimSpace(value) != value || len(value) > 256 {
		return fmt.Errorf("invalid provider version metadata")
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return fmt.Errorf("invalid provider version metadata")
		}
	}
	return nil
}

func isKnownCapability(capability ProviderCapability) bool {
	switch capability {
	case CapabilityWorkspaceMutation,
		CapabilityWorkspaceReadOnly,
		CapabilityNonInteractive,
		CapabilityStructuredEvents,
		CapabilityContextCancellation:
		return true
	default:
		return false
	}
}
