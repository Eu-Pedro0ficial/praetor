package execution

import (
	"context"
	"errors"
	"fmt"

	"github.com/Eu-Pedro0ficial/praetor/internal/aiprovider"
	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"github.com/Eu-Pedro0ficial/praetor/internal/proposal"
	"github.com/Eu-Pedro0ficial/praetor/internal/verification"
)

const (
	EventVerificationPlanningStarted   = "VERIFICATION_PLANNING_STARTED"
	EventVerificationPlanningCompleted = "VERIFICATION_PLANNING_COMPLETED"
	EventVerificationPlanningFailed    = "VERIFICATION_PLANNING_FAILED"
)

// PlanVerification invokes the explicitly selected provider in its bounded,
// read-only verification-planning role. The returned candidates remain
// untrusted until verification.BuildPlan validates and normalizes them.
func (service *Service) PlanVerification(
	ctx context.Context,
	currentChange change.Change,
	currentProposal proposal.Proposal,
	selection aiprovider.Selection,
	planningRequest verification.PlanningRequest,
) (verification.PlanningResult, error) {
	var result verification.PlanningResult
	if ctx == nil {
		return result, fmt.Errorf("verification planning context is required")
	}
	if err := validatePlanningLinkage(currentChange, currentProposal, planningRequest); err != nil {
		return result, err
	}
	selectedProvider, err := service.registry.Resolve(selection)
	if err != nil {
		return result, err
	}
	descriptor := selectedProvider.Descriptor()
	roleContract := aiprovider.VerificationPlanningRoleContract()
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
	planningEvidence := make([]aiprovider.PlanningEvidence, 0, len(planningRequest.Evidence()))
	for _, evidence := range planningRequest.Evidence() {
		item, evidenceError := aiprovider.NewPlanningEvidence(
			string(evidence.Path()),
			evidence.Kind(),
			evidence.Content(),
		)
		if evidenceError != nil {
			return result, fmt.Errorf("prepare verification planning evidence: %w", evidenceError)
		}
		planningEvidence = append(planningEvidence, item)
	}
	request, err := aiprovider.NewVerificationPlanningRequest(
		attemptId,
		currentChange,
		currentProposal,
		selection,
		planningEvidence,
	)
	if err != nil {
		return result, err
	}
	requestContext, err := selectedProvider.AccountRequest(request)
	if err != nil {
		return result, fmt.Errorf("account verification planning request context: %w", err)
	}
	if err := service.recorder(LifecycleEvent{
		EventType:      EventVerificationPlanningStarted,
		Request:        request,
		Descriptor:     descriptor,
		RequestContext: requestContext,
		OccurredAt:     service.clock().UTC(),
	}); err != nil {
		return result, fmt.Errorf("record verification planning start: %w", err)
	}
	fail := func(primary error, response aiprovider.ProviderResponse, providerInvoked bool) (verification.PlanningResult, error) {
		externalExecutionId := response.ExternalExecutionId()
		var providerError *aiprovider.ExecutionError
		if errors.As(primary, &providerError) {
			externalExecutionId = providerError.ExternalExecutionId()
		}
		recordError := service.recorder(LifecycleEvent{
			EventType:             EventVerificationPlanningFailed,
			Request:               request,
			Descriptor:            descriptor,
			FailureKind:           aiprovider.FailureKindOf(primary),
			ExternalExecutionId:   externalExecutionId,
			WorkspaceMayBeChanged: providerInvoked,
			RequestContext:        requestContext,
			OccurredAt:            service.clock().UTC(),
		})
		if recordError != nil {
			recordError = fmt.Errorf("record verification planning failure: %w", recordError)
		}
		return verification.PlanningResult{}, errors.Join(primary, recordError)
	}

	if err := service.proposalLifecycle.VerifyIntegrity(currentProposal); err != nil {
		return fail(fmt.Errorf("pre-planning patch integrity failed: %w", err), aiprovider.ProviderResponse{}, false)
	}
	response, providerError := selectedProvider.Execute(ctx, request)
	if integrityError := service.proposalLifecycle.VerifyIntegrity(currentProposal); integrityError != nil {
		return fail(
			fmt.Errorf("read-only verification planner changed protected source state: %w", integrityError),
			response,
			true,
		)
	}
	if providerError != nil {
		return fail(providerError, response, true)
	}
	if err := validateProviderResponse(request, descriptor, response); err != nil {
		return fail(err, response, true)
	}
	if response.SummaryTruncated() {
		return fail(aiprovider.NewExecutionError(
			aiprovider.FailureMalformedOutput,
			descriptor.Identifier(),
			response.ExternalExecutionId(),
			fmt.Errorf("verification planner response was truncated"),
		), response, true)
	}
	candidates, err := verification.ParsePlanningResponse(response.Summary(), planningRequest)
	if err != nil {
		return fail(aiprovider.NewExecutionError(
			aiprovider.FailureMalformedOutput,
			descriptor.Identifier(),
			response.ExternalExecutionId(),
			err,
		), response, true)
	}
	model := ""
	if modelIdentifier, selected := selection.ModelIdentifier(); selected {
		model = string(modelIdentifier)
	}
	result, err = verification.NewPlanningResult(
		string(attemptId),
		string(descriptor.Identifier()),
		model,
		candidates,
	)
	if err != nil {
		return fail(err, response, true)
	}
	if err := service.recorder(LifecycleEvent{
		EventType:      EventVerificationPlanningCompleted,
		Request:        request,
		Descriptor:     descriptor,
		Response:       response,
		HasResponse:    true,
		RequestContext: requestContext,
		OccurredAt:     service.clock().UTC(),
	}); err != nil {
		return result, fmt.Errorf("record verification planning completion: %w", err)
	}
	return result, nil
}

func validatePlanningLinkage(
	currentChange change.Change,
	currentProposal proposal.Proposal,
	request verification.PlanningRequest,
) error {
	workspace := currentProposal.Workspace()
	artifact, hasArtifact := currentProposal.PatchArtifact()
	if !hasArtifact ||
		request.ProjectId() != currentChange.ProjectId() ||
		request.ChangeId() != currentChange.ChangeId() ||
		request.WorkspaceId() != workspace.WorkspaceId() ||
		request.BaseRevision() != workspace.BaseRevision() ||
		request.SourceStateDigest() != workspace.SourceStateDigest() ||
		request.PatchDigest() != artifact.PatchDigest() {
		return fmt.Errorf("verification planning request linkage is inconsistent")
	}
	return nil
}
