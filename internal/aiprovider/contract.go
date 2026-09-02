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
	CapabilityNonInteractive      ProviderCapability = "non-interactive-execution"
	CapabilityStructuredEvents    ProviderCapability = "structured-events"
	CapabilityContextCancellation ProviderCapability = "context-cancellation"
)

// ProviderRole identifies the single role implemented in M0.5.
type ProviderRole string

const RoleImplementation ProviderRole = "implementation"

// ProviderRoleContract states what the selected provider invocation is doing
// and the minimum capabilities required for that invocation.
type ProviderRoleContract struct {
	role                 ProviderRole
	requiredCapabilities []ProviderCapability
}

// ImplementationRoleContract returns the narrow M0.5 implementation role.
func ImplementationRoleContract() ProviderRoleContract {
	return ProviderRoleContract{
		role: RoleImplementation,
		requiredCapabilities: []ProviderCapability{
			CapabilityWorkspaceMutation,
			CapabilityContextCancellation,
		},
	}
}

// Role returns the role identity.
func (contract ProviderRoleContract) Role() ProviderRole { return contract.role }

// RequiredCapabilities returns a defensive copy of contract requirements.
func (contract ProviderRoleContract) RequiredCapabilities() []ProviderCapability {
	return append([]ProviderCapability(nil), contract.requiredCapabilities...)
}

func (contract ProviderRoleContract) validate() error {
	if contract.role != RoleImplementation {
		return fmt.Errorf("M0.5 requires the implementation provider role")
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
	if workspace.State() != proposal.WorkspaceActive {
		return ExecutionRequest{}, fmt.Errorf(
			"proposal workspace %q is not active",
			workspace.WorkspaceId(),
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
		CapabilityNonInteractive,
		CapabilityStructuredEvents,
		CapabilityContextCancellation:
		return true
	default:
		return false
	}
}
