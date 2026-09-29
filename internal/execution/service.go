// Package execution coordinates one M0.5 provider implementation attempt with
// the existing guarded ProposalWorkspace and M0.4 patch lifecycle.
package execution

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/Eu-Pedro0ficial/praetor/internal/aiprovider"
	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"github.com/Eu-Pedro0ficial/praetor/internal/proposal"
	"github.com/Eu-Pedro0ficial/praetor/internal/source"
)

const (
	EventProviderExecutionStarted   = "PROVIDER_EXECUTION_STARTED"
	EventProviderExecutionCompleted = "PROVIDER_EXECUTION_COMPLETED"
	EventProviderExecutionFailed    = "PROVIDER_EXECUTION_FAILED"
)

// AttemptIdGenerator creates Praetor-owned execution identity.
type AttemptIdGenerator func() (aiprovider.ExecutionAttemptId, error)

// Clock supplies audit lifecycle timestamps.
type Clock func() time.Time

// LifecycleRecorder appends bounded provider execution provenance.
type LifecycleRecorder func(LifecycleEvent) error

// LifecycleEvent carries provider-independent execution metadata to the
// composition/audit boundary. It contains no prompt, source body, or secret.
type LifecycleEvent struct {
	EventType             string
	Request               aiprovider.ExecutionRequest
	Descriptor            aiprovider.ProviderDescriptor
	Response              aiprovider.ProviderResponse
	HasResponse           bool
	FailureKind           aiprovider.FailureKind
	FailureStage          string
	ExternalExecutionId   string
	ProviderDiagnostic    string
	ChangedPaths          []string
	WorkspaceMayBeChanged bool
	RequestContext        aiprovider.RequestContextAccounting
	OccurredAt            time.Time
}

// Result retains the provider response and M0.4 classification produced by
// one implementation command.
type Result struct {
	attemptId         aiprovider.ExecutionAttemptId
	response          aiprovider.ProviderResponse
	proposal          proposal.Proposal
	validation        source.SurfaceValidationResult
	providerCompleted bool
	requestContext    aiprovider.RequestContextAccounting
}

func (result Result) AttemptId() aiprovider.ExecutionAttemptId { return result.attemptId }
func (result Result) Response() (aiprovider.ProviderResponse, bool) {
	return result.response, result.providerCompleted
}
func (result Result) Proposal() proposal.Proposal                { return result.proposal }
func (result Result) Validation() source.SurfaceValidationResult { return result.validation }
func (result Result) RequestContext() aiprovider.RequestContextAccounting {
	return result.requestContext
}

// Service composes exact manual provider selection with guarded workspace
// execution. It contains no routing, fallback, or provider ranking.
type Service struct {
	registry          *aiprovider.Registry
	proposalLifecycle *proposal.Service
	recorder          LifecycleRecorder
	attemptIds        AttemptIdGenerator
	clock             Clock
}

// New constructs the M0.5 application service.
func New(
	registry *aiprovider.Registry,
	proposalLifecycle *proposal.Service,
	recorder LifecycleRecorder,
	attemptIds AttemptIdGenerator,
	clock Clock,
) (*Service, error) {
	if registry == nil {
		return nil, fmt.Errorf("AI provider registry dependency is not configured")
	}
	if proposalLifecycle == nil {
		return nil, fmt.Errorf("proposal lifecycle dependency is not configured")
	}
	if recorder == nil {
		return nil, fmt.Errorf("provider execution recorder dependency is not configured")
	}
	if attemptIds == nil {
		return nil, fmt.Errorf("execution attempt identity dependency is not configured")
	}
	if clock == nil {
		return nil, fmt.Errorf("provider execution clock dependency is not configured")
	}
	return &Service{
		registry:          registry,
		proposalLifecycle: proposalLifecycle,
		recorder:          recorder,
		attemptIds:        attemptIds,
		clock:             clock,
	}, nil
}

// Implement runs the explicitly selected provider in the existing active
// ProposalWorkspace, then delegates patch extraction and surface comparison to
// M0.4. A surface-valid result leaves the Change isolated.
func (service *Service) Implement(
	ctx context.Context,
	currentChange change.Change,
	currentProposal proposal.Proposal,
	selection aiprovider.Selection,
) (Result, error) {
	return service.ImplementWithContext(ctx, currentChange, currentProposal, selection, aiprovider.ImplementationContext{})
}

func (service *Service) ImplementWithContext(
	ctx context.Context,
	currentChange change.Change,
	currentProposal proposal.Proposal,
	selection aiprovider.Selection,
	implementationContext aiprovider.ImplementationContext,
) (Result, error) {
	result := Result{proposal: currentProposal}
	if ctx == nil {
		return result, fmt.Errorf("provider execution context is required")
	}
	selectedProvider, err := service.registry.Resolve(selection)
	if err != nil {
		return result, err
	}
	descriptor := selectedProvider.Descriptor()
	roleContract := aiprovider.ImplementationRoleContract()
	if err := aiprovider.ValidateRole(descriptor, roleContract); err != nil {
		return result, err
	}
	inspection, err := aiprovider.NewReadinessInspection(
		aiprovider.ReadinessLocalProbe,
		currentProposal.Workspace().Root(),
	)
	if err != nil {
		return result, err
	}
	_, readiness, err := service.registry.InspectReadiness(ctx, selection, roleContract, inspection)
	if err != nil {
		return result, err
	}
	if readiness.BlocksExecution() {
		return result, aiprovider.NewReadinessError(selection.ProviderIdentifier(), readiness)
	}
	attemptId, err := service.attemptIds()
	if err != nil {
		return result, err
	}
	request, err := aiprovider.NewExecutionRequestWithContext(
		attemptId,
		currentChange,
		currentProposal,
		roleContract,
		selection,
		implementationContext,
	)
	if err != nil {
		return result, err
	}
	result.attemptId = attemptId
	requestContext, err := selectedProvider.AccountRequest(request)
	if err != nil {
		return result, fmt.Errorf("account provider request context: %w", err)
	}
	result.requestContext = requestContext
	if err := service.recorder(LifecycleEvent{
		EventType:      EventProviderExecutionStarted,
		Request:        request,
		Descriptor:     descriptor,
		RequestContext: requestContext,
		OccurredAt:     service.clock().UTC(),
	}); err != nil {
		return result, fmt.Errorf("record provider execution start: %w", err)
	}

	var response aiprovider.ProviderResponse
	providerInvoked := false
	mutationError := service.proposalLifecycle.Mutate(
		currentProposal,
		func(workspace proposal.ProposalWorkspace) error {
			if workspace.WorkspaceId() != request.Workspace().WorkspaceId() ||
				workspace.Root() != request.Workspace().Root() {
				return fmt.Errorf("provider execution workspace linkage changed")
			}
			providerInvoked = true
			var providerError error
			response, providerError = selectedProvider.Execute(ctx, request)
			if providerError != nil {
				return providerError
			}
			return validateProviderResponse(request, descriptor, response)
		},
	)
	if mutationError != nil {
		changedPaths, inspectionError := service.inspectFailureChanges(currentProposal, providerInvoked)
		externalExecutionId := ""
		providerDiagnostic := ""
		var providerError *aiprovider.ExecutionError
		if errors.As(mutationError, &providerError) {
			externalExecutionId = providerError.ExternalExecutionId()
			providerDiagnostic = providerError.Diagnostic()
		}
		var dispositionError error
		if providerInvoked {
			result.proposal, dispositionError = service.proposalLifecycle.FailWorkspace(
				currentProposal,
				"provider execution failed before a valid proposal was established",
				string(attemptId),
			)
		}
		failureStage := "pre-invocation-guard"
		if providerInvoked {
			failureStage = "provider-or-post-guard"
		}
		recordError := service.recorder(LifecycleEvent{
			EventType:             EventProviderExecutionFailed,
			FailureStage:          failureStage,
			Request:               request,
			Descriptor:            descriptor,
			FailureKind:           aiprovider.FailureKindOf(mutationError),
			ExternalExecutionId:   externalExecutionId,
			ProviderDiagnostic:    providerDiagnostic,
			ChangedPaths:          changedPaths,
			WorkspaceMayBeChanged: providerInvoked,
			RequestContext:        requestContext,
			OccurredAt:            service.clock().UTC(),
		})
		if recordError != nil {
			recordError = fmt.Errorf("record provider execution failure: %w", recordError)
		}
		return result, errors.Join(mutationError, inspectionError, dispositionError, recordError)
	}

	result.response = response
	result.providerCompleted = true
	if err := service.recorder(LifecycleEvent{
		EventType:      EventProviderExecutionCompleted,
		Request:        request,
		Descriptor:     descriptor,
		Response:       response,
		HasResponse:    true,
		RequestContext: requestContext,
		OccurredAt:     service.clock().UTC(),
	}); err != nil {
		return result, fmt.Errorf("record provider execution completion: %w", err)
	}

	classifiedProposal, validation, extractionError := service.proposalLifecycle.ExtractPatchForExecution(
		currentProposal,
		string(attemptId),
	)
	result.proposal = classifiedProposal
	result.validation = validation
	if extractionError != nil && classifiedProposal.Workspace().State() != proposal.WorkspaceRejected {
		failedProposal, dispositionError := service.proposalLifecycle.FailWorkspace(
			classifiedProposal,
			"provider completed but patch extraction did not establish a valid proposal",
			string(attemptId),
		)
		result.proposal = failedProposal
		extractionError = errors.Join(extractionError, dispositionError)
	}
	return result, extractionError
}

func validateProviderResponse(
	request aiprovider.ExecutionRequest,
	descriptor aiprovider.ProviderDescriptor,
	response aiprovider.ProviderResponse,
) error {
	responseModel, responseHasModel := response.Selection().ModelIdentifier()
	requestModel, requestHasModel := request.Selection().ModelIdentifier()
	if response.AttemptId() != request.AttemptId() ||
		response.Selection().ProviderIdentifier() != request.Selection().ProviderIdentifier() ||
		responseHasModel != requestHasModel ||
		responseModel != requestModel ||
		response.Outcome() != aiprovider.ProviderOutcomeCompleted {
		return aiprovider.NewExecutionError(
			aiprovider.FailureMalformedOutput,
			descriptor.Identifier(),
			response.ExternalExecutionId(),
			fmt.Errorf("provider response linkage is invalid"),
		)
	}
	return nil
}

func (service *Service) inspectFailureChanges(
	currentProposal proposal.Proposal,
	providerInvoked bool,
) ([]string, error) {
	if !providerInvoked {
		return nil, nil
	}
	changedPaths, err := service.proposalLifecycle.InspectChangedPaths(currentProposal)
	if err != nil {
		return nil, fmt.Errorf("inspect failed provider workspace changes: %w", err)
	}
	return changedPaths, nil
}
