package command

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/Eu-Pedro0ficial/praetor/internal/approval"
	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"github.com/Eu-Pedro0ficial/praetor/internal/presentation/preferences"
	"github.com/Eu-Pedro0ficial/praetor/internal/proposal"
	"github.com/Eu-Pedro0ficial/praetor/internal/source"
	"github.com/Eu-Pedro0ficial/praetor/internal/verification"
)

func handleStatus(session *Session, invocation Invocation, output io.Writer) (Result, error) {
	if len(invocation.Arguments) != 0 {
		return Result{}, errInvalidArguments
	}
	snapshot := session.StatusSnapshot()
	fmt.Fprintf(output, "Project ID: %s\n", snapshot.Project)
	fmt.Fprintf(output, "Repository root: %s\n", snapshot.Repository)
	fmt.Fprintf(output, "Git repository: %s\n", snapshot.GitRepository)
	fmt.Fprintf(output, "AI provider adapter: %s\n", snapshot.ProviderAdapter)
	fmt.Fprintf(output, "AI provider vendor: %s\n", snapshot.ProviderVendor)
	fmt.Fprintf(output, "AI model: %s\n", snapshot.ProviderModel)
	fmt.Fprintf(output, "Current change: %s\n", snapshot.Change)
	fmt.Fprintf(output, "Current proposal: %s\n", snapshot.Proposal)
	if snapshot.ProposalBase != "" {
		fmt.Fprintf(output, "Proposal base revision: %s\n", snapshot.ProposalBase)
	}
	if snapshot.VerificationAttempt != "" {
		fmt.Fprintf(output, "Last verification: %s (%s)\n", snapshot.VerificationAttempt, snapshot.Verification)
	} else {
		fmt.Fprintln(output, "Last verification: none")
	}
	if snapshot.HumanActor != "" {
		fmt.Fprintf(output, "Last human decision: %s (%s)\n", snapshot.HumanDecision, snapshot.HumanActor)
	} else {
		fmt.Fprintln(output, "Last human decision: none")
	}
	return Result{}, nil
}

func handleLayoutShow(session *Session, invocation Invocation, output io.Writer) (Result, error) {
	if len(invocation.Arguments) != 0 {
		return Result{}, errInvalidArguments
	}
	layout := session.LayoutPreferences()
	fmt.Fprintln(output, "Layout")
	fmt.Fprintf(output, "  Sidebar       %s\n", onOff(layout.Sidebar.Visible))
	fmt.Fprintf(output, "    Identity    %s\n", onOff(layout.Sidebar.Identity))
	fmt.Fprintf(output, "    Context     %s\n", onOff(layout.Sidebar.Context))
	fmt.Fprintf(output, "    Provider    %s\n", onOff(layout.Sidebar.Provider))
	fmt.Fprintf(output, "    Status      %s\n", onOff(layout.Sidebar.Status))
	fmt.Fprintln(output)
	fmt.Fprintln(output, "  Colors")
	fmt.Fprintf(output, "    Accent      %s\n", layout.Colors.Accent)
	fmt.Fprintf(output, "    Border      %s\n", layout.Colors.Border)
	fmt.Fprintf(output, "    Background  %s\n", layout.Colors.Background)
	fmt.Fprintf(output, "    Text        %s\n", layout.Colors.Text)
	return Result{}, nil
}

func handleSidebarVisible(session *Session, invocation Invocation, output io.Writer) (Result, error) {
	value, err := parseOnOff(invocation.Arguments)
	if err != nil {
		return Result{}, err
	}
	if err := session.SetSidebarVisible(value); err != nil {
		return Result{}, err
	}
	fmt.Fprintf(output, "Sidebar: %s\n", onOff(value))
	return Result{}, nil
}

func handleSidebarSection(section string) Handler {
	return func(session *Session, invocation Invocation, output io.Writer) (Result, error) {
		value, err := parseOnOff(invocation.Arguments)
		if err != nil {
			return Result{}, err
		}
		if err := session.SetSidebarSection(section, value); err != nil {
			return Result{}, err
		}
		fmt.Fprintf(output, "Sidebar %s: %s\n", section, onOff(value))
		return Result{}, nil
	}
}

func handlePresentationColor(role string) Handler {
	return func(session *Session, invocation Invocation, output io.Writer) (Result, error) {
		if len(invocation.Arguments) != 1 {
			return Result{}, errInvalidArguments
		}
		color, err := preferences.ParseColor(invocation.Arguments[0], role == "background")
		if err != nil {
			return Result{}, err
		}
		if err := session.SetPresentationColor(role, color); err != nil {
			return Result{}, err
		}
		fmt.Fprintf(output, "Layout color %s: %s\n", role, color)
		return Result{}, nil
	}
}

func handleLayoutReset(session *Session, invocation Invocation, output io.Writer) (Result, error) {
	if len(invocation.Arguments) != 0 {
		return Result{}, errInvalidArguments
	}
	if err := session.ResetLayoutPreferences(); err != nil {
		return Result{}, err
	}
	fmt.Fprintln(output, "Layout preferences reset to Praetor defaults")
	return Result{}, nil
}

func parseOnOff(arguments []string) (bool, error) {
	if len(arguments) != 1 {
		return false, errInvalidArguments
	}
	switch strings.ToLower(arguments[0]) {
	case "on":
		return true, nil
	case "off":
		return false, nil
	default:
		return false, fmt.Errorf("expected on or off")
	}
}

func onOff(value bool) string {
	if value {
		return "on"
	}
	return "off"
}

func handleProviderShow(session *Session, invocation Invocation, output io.Writer) (Result, error) {
	if len(invocation.Arguments) != 0 {
		return Result{}, errInvalidArguments
	}
	writeProviderSelection(output, session)
	return Result{}, nil
}

func handleProviderList(session *Session, invocation Invocation, output io.Writer) (Result, error) {
	if len(invocation.Arguments) != 0 {
		return Result{}, errInvalidArguments
	}
	selected := session.ProviderSelection().ProviderIdentifier()
	for _, descriptor := range session.ProviderDescriptors() {
		marker := ""
		if descriptor.Identifier() == selected {
			marker = " (selected)"
		}
		capabilities := make([]string, len(descriptor.Capabilities()))
		for index, capability := range descriptor.Capabilities() {
			capabilities[index] = string(capability)
		}
		fmt.Fprintf(
			output,
			"%s%s — vendor=%s; name=%s; capabilities=%s\n",
			descriptor.Identifier(),
			marker,
			descriptor.Vendor(),
			descriptor.DisplayName(),
			strings.Join(capabilities, ","),
		)
	}
	return Result{}, nil
}

func handleProviderSelect(session *Session, invocation Invocation, output io.Writer) (Result, error) {
	if len(invocation.Arguments) != 1 {
		return Result{}, errInvalidArguments
	}
	if err := session.SelectProvider(invocation.Arguments[0]); err != nil {
		return Result{}, err
	}
	writeProviderSelection(output, session)
	return Result{}, nil
}

func handleProviderModel(session *Session, invocation Invocation, output io.Writer) (Result, error) {
	if len(invocation.Arguments) != 1 {
		return Result{}, errInvalidArguments
	}
	if err := session.SelectProviderModel(invocation.Arguments[0]); err != nil {
		return Result{}, err
	}
	writeProviderSelection(output, session)
	return Result{}, nil
}

func handleChangeNew(session *Session, invocation Invocation, output io.Writer) (Result, error) {
	if err := requireNoCurrentProposal(session); err != nil {
		return Result{}, err
	}
	arguments := invocation.Arguments
	if len(arguments) < 2 {
		return Result{}, errInvalidArguments
	}
	changeId, err := change.NewChangeId(arguments[0])
	if err != nil {
		return Result{}, err
	}
	intent, err := change.NewChangeIntent(arguments[1])
	if err != nil {
		return Result{}, err
	}
	states := make([]change.ChangeState, 0, len(arguments)-2)
	for _, stateValue := range arguments[2:] {
		state, parseError := change.ParseState(stateValue)
		if parseError != nil {
			return Result{}, parseError
		}
		states = append(states, state)
	}

	currentChange, err := session.changeWorkflow.Create(changeId, intent, invocation.CommandPath)
	if err != nil {
		return Result{}, err
	}
	session.setCurrentChange(currentChange)
	for _, state := range states {
		currentChange, err = session.changeWorkflow.Transition(
			changeId,
			state,
			fmt.Sprintf("%s transition to %s", invocation.CommandPath, state),
		)
		if err != nil {
			return Result{}, err
		}
		session.setCurrentChange(currentChange)
	}

	fmt.Fprintf(output, "Change ID: %s\n", currentChange.ChangeId())
	fmt.Fprintf(output, "Project ID: %s\n", currentChange.ProjectId())
	fmt.Fprintf(output, "Intent: %s\n", currentChange.Intent())
	fmt.Fprintf(output, "State: %s\n", currentChange.State())
	return Result{}, nil
}

func handleAnalysisImpact(session *Session, invocation Invocation, output io.Writer) (Result, error) {
	if err := requireNoCurrentProposal(session); err != nil {
		return Result{}, err
	}
	arguments := invocation.Arguments
	if len(arguments) < 2 {
		return Result{}, errInvalidArguments
	}
	changeId, err := change.NewChangeId(arguments[0])
	if err != nil {
		return Result{}, err
	}
	intent, err := change.NewChangeIntent(arguments[1])
	if err != nil {
		return Result{}, err
	}
	scopeRequest, actualPaths, err := parseSurfaceArguments(arguments[2:])
	if err != nil {
		return Result{}, err
	}
	if _, err := source.NewChangeSurface(scopeRequest); err != nil {
		return Result{}, err
	}

	currentChange, err := session.changeWorkflow.Create(changeId, intent, invocation.CommandPath)
	if err != nil {
		return Result{}, err
	}
	session.setCurrentChange(currentChange)
	preparedSurface, err := session.repositoryIntelligence.EstablishSurface(
		currentChange,
		session.registration.RepositoryRoot,
		scopeRequest,
	)
	if err != nil {
		return Result{}, err
	}
	validation, validationError := session.repositoryIntelligence.ValidateActualSurface(
		preparedSurface.ApprovedScope(),
		actualPaths,
	)
	if validationError != nil {
		var surfaceError *source.SurfaceValidationError
		if !errors.As(validationError, &surfaceError) {
			return Result{}, validationError
		}
	}

	writeSurfaceReport(output, currentChange, preparedSurface.Snapshot(), preparedSurface.ApprovedScope(), validation)
	return Result{}, validationError
}

func handleChangeIsolate(session *Session, invocation Invocation, output io.Writer) (Result, error) {
	if err := requireNoCurrentProposal(session); err != nil {
		return Result{}, err
	}
	arguments := invocation.Arguments
	if len(arguments) < 3 {
		return Result{}, errInvalidArguments
	}
	changeId, err := change.NewChangeId(arguments[0])
	if err != nil {
		return Result{}, err
	}
	intent, err := change.NewChangeIntent(arguments[1])
	if err != nil {
		return Result{}, err
	}
	scopeRequest, _, err := parseCategorizedSurfaceArguments(arguments[2:], false)
	if err != nil {
		return Result{}, err
	}
	if _, err := source.NewChangeSurface(scopeRequest); err != nil {
		return Result{}, err
	}

	currentChange, err := session.changeWorkflow.Create(changeId, intent, invocation.CommandPath)
	if err != nil {
		return Result{}, err
	}
	session.setCurrentChange(currentChange)
	currentChange, err = session.changeWorkflow.Transition(
		changeId,
		change.StatePlanned,
		invocation.CommandPath+" proposal planning",
	)
	if err != nil {
		return Result{}, err
	}
	session.setCurrentChange(currentChange)

	preparedSurface, err := session.repositoryIntelligence.EstablishSurface(
		currentChange,
		session.registration.RepositoryRoot,
		scopeRequest,
	)
	if err != nil {
		return Result{}, errors.Join(
			err,
			session.rejectCurrentChange("proposal surface establishment failed"),
		)
	}
	currentProposal, err := session.proposalLifecycle.CreateWorkspace(
		currentChange,
		preparedSurface.Snapshot(),
		preparedSurface.ApprovedScope(),
	)
	if err != nil {
		return Result{}, errors.Join(
			err,
			session.rejectCurrentChange("proposal workspace creation failed"),
		)
	}
	currentChange, transitionError := session.changeWorkflow.Transition(
		changeId,
		change.StateIsolated,
		invocation.CommandPath+" proposal workspace created",
	)
	if transitionError != nil {
		cleanedProposal, cleanupError := session.proposalLifecycle.Discard(
			currentProposal,
			"Change isolation transition failed",
		)
		if cleanedProposal.Workspace().State() != proposal.WorkspaceCleaned {
			session.setCurrentProposal(cleanedProposal)
		}
		return Result{}, errors.Join(
			transitionError,
			session.rejectCurrentChange("proposal isolation transition failed"),
			cleanupError,
		)
	}
	session.setCurrentChange(currentChange)
	session.setCurrentProposal(currentProposal)

	workspace := currentProposal.Workspace()
	fmt.Fprintf(output, "Change ID: %s\n", workspace.ChangeId())
	fmt.Fprintf(output, "Project ID: %s\n", workspace.ProjectId())
	fmt.Fprintf(output, "Change state: %s\n", currentChange.State())
	fmt.Fprintf(output, "Workspace ID: %s\n", workspace.WorkspaceId())
	fmt.Fprintf(output, "Workspace root: %s\n", workspace.Root())
	fmt.Fprintf(output, "Workspace state: %s\n", workspace.State())
	fmt.Fprintf(output, "Base revision: %s\n", workspace.BaseRevision())
	fmt.Fprintf(output, "Source state digest: %s\n", workspace.SourceStateDigest())
	fmt.Fprintln(output, "Isolation boundary: Git source/workspace only; not process, network, container, VM, or hostile-code isolation")
	return Result{}, nil
}

func handleChangePatch(session *Session, invocation Invocation, output io.Writer) (Result, error) {
	if len(invocation.Arguments) != 0 {
		return Result{}, errInvalidArguments
	}
	currentProposal, ok := session.CurrentProposal()
	if !ok {
		return Result{}, fmt.Errorf("no current isolated proposal; use change isolate first")
	}

	classifiedProposal, validation, extractionError := session.proposalLifecycle.ExtractPatch(currentProposal)
	session.setCurrentProposal(classifiedProposal)
	if extractionError != nil && classifiedProposal.Workspace().State() != proposal.WorkspaceRejected {
		fmt.Fprintln(output, "Patch extraction: failed")
		return Result{}, extractionError
	}
	writePatchReport(output, classifiedProposal, validation)
	if extractionError == nil {
		return Result{}, nil
	}

	cleanupError := session.rejectAndDiscardProposal("patch rejected by M0.4 surface comparison")
	return Result{}, errors.Join(extractionError, cleanupError)
}

func handleChangeImplement(session *Session, invocation Invocation, output io.Writer) (Result, error) {
	if len(invocation.Arguments) != 0 {
		return Result{}, errInvalidArguments
	}
	currentProposal, ok := session.CurrentProposal()
	if !ok {
		return Result{}, fmt.Errorf("no current isolated proposal; use change isolate first")
	}
	currentChange, ok := session.CurrentChange()
	if !ok || currentChange.State() != change.StateIsolated {
		return Result{}, fmt.Errorf("current Change must be isolated before implementation")
	}
	if currentChange.ChangeId() != currentProposal.Workspace().ChangeId() {
		return Result{}, fmt.Errorf("current Change and proposal linkage is inconsistent")
	}

	executionResult, implementationError := session.providerExecution.Implement(
		invocation.Context,
		currentChange,
		currentProposal,
		session.ProviderSelection(),
	)
	session.setCurrentProposal(executionResult.Proposal())
	if implementationError != nil {
		cleanupError := session.rejectAndDiscardProposal("provider implementation failed")
		return Result{}, errors.Join(implementationError, cleanupError)
	}

	response, completed := executionResult.Response()
	if !completed {
		cleanupError := session.rejectAndDiscardProposal("provider implementation returned no response")
		return Result{}, errors.Join(fmt.Errorf("provider implementation returned no completed response"), cleanupError)
	}
	fmt.Fprintf(output, "Execution attempt: %s\n", executionResult.AttemptId())
	fmt.Fprintf(output, "Provider: %s\n", response.Selection().ProviderIdentifier())
	if modelIdentifier, selected := response.Selection().ModelIdentifier(); selected {
		fmt.Fprintf(output, "Model: %s\n", modelIdentifier)
	} else {
		fmt.Fprintln(output, "Model: provider default")
	}
	fmt.Fprintf(output, "Provider outcome: %s\n", response.Outcome())
	if response.ExternalExecutionId() != "" {
		fmt.Fprintf(output, "External execution ID: %s\n", response.ExternalExecutionId())
	}
	writePatchReport(output, executionResult.Proposal(), executionResult.Validation())
	return Result{}, nil
}

func handleChangeVerify(session *Session, invocation Invocation, output io.Writer) (Result, error) {
	if len(invocation.Arguments) != 0 {
		return Result{}, errInvalidArguments
	}
	currentProposal, ok := session.CurrentProposal()
	if !ok {
		return Result{}, fmt.Errorf("no retained proposal; use change implement or change patch first")
	}
	currentChange, ok := session.CurrentChange()
	if !ok || currentChange.State() != change.StateIsolated {
		return Result{}, fmt.Errorf("current Change must be isolated before verification")
	}
	if currentChange.ChangeId() != currentProposal.Workspace().ChangeId() ||
		currentProposal.Workspace().State() != proposal.WorkspaceRetained {
		return Result{}, fmt.Errorf("verification requires the current retained proposal for the isolated Change")
	}

	verificationResult, verificationError := session.verification.Verify(
		invocation.Context,
		currentChange,
		currentProposal,
		func(ctx context.Context, request verification.PlanningRequest) (verification.PlanningResult, error) {
			return session.providerExecution.PlanVerification(
				ctx,
				currentChange,
				currentProposal,
				session.ProviderSelection(),
				request,
			)
		},
	)
	session.setLastVerification(verificationResult)
	writeVerificationReport(output, verificationResult, currentChange.State())
	if verificationError != nil {
		return Result{}, verificationError
	}
	validatedChange, err := session.changeWorkflow.Transition(
		currentChange.ChangeId(),
		change.StateValidated,
		invocation.CommandPath+" deterministic verification passed",
	)
	if err != nil {
		return Result{}, err
	}
	session.setCurrentChange(validatedChange)
	fmt.Fprintf(output, "Change state: %s\n", validatedChange.State())
	return Result{}, nil
}

func writeVerificationReport(output io.Writer, result verification.Result, currentState change.ChangeState) {
	if result.AttemptId() == "" {
		fmt.Fprintln(output, "Verification attempt: unavailable")
		fmt.Fprintln(output, "Verification result: FAIL")
		fmt.Fprintf(output, "Change state: %s\n", currentState)
		return
	}
	fmt.Fprintf(output, "Verification attempt: %s\n", result.AttemptId())
	evidenceSet := result.EvidenceSet()
	verdict := "FAIL"
	if evidenceSet.Passed() {
		verdict = "PASS"
	}
	fmt.Fprintf(output, "Verification result: %s\n", verdict)
	if planning, used := result.PlanningResult(); used {
		fmt.Fprintf(output, "AI planning provenance: provider=%s attempt=%s candidates=%d\n",
			planning.Provider(), planning.ExecutionAttemptId(), len(planning.Candidates()))
	} else if result.PlanningFailure() != "" {
		fmt.Fprintln(output, "AI planning provenance: failed; deterministic candidates retained")
	} else {
		fmt.Fprintln(output, "AI planning provenance: not required")
	}
	for _, evidence := range evidenceSet.Evidence() {
		arguments := strings.Join(evidence.Arguments(), " ")
		command := evidence.Executable()
		if arguments != "" {
			command += " " + arguments
		}
		fmt.Fprintf(output, "Check: %s [%s] %s (%s)\n",
			evidence.Kind(), evidence.Outcome(), command, evidence.Origin())
	}
	if evidenceSet.Id() != "" {
		fmt.Fprintf(output, "Evidence set: %s (%d items)\n", evidenceSet.Id(), len(evidenceSet.Evidence()))
	}
	if !evidenceSet.Passed() {
		fmt.Fprintf(output, "Change state: %s\n", currentState)
	}
}

func handleChangeApprove(session *Session, invocation Invocation, output io.Writer) (Result, error) {
	return handleHumanDecision(session, invocation, output, approval.DecisionApprove)
}

func handleChangeReject(session *Session, invocation Invocation, output io.Writer) (Result, error) {
	return handleHumanDecision(session, invocation, output, approval.DecisionReject)
}

func handleChangeApply(session *Session, invocation Invocation, output io.Writer) (Result, error) {
	if len(invocation.Arguments) != 0 {
		return Result{}, errInvalidArguments
	}
	currentChange, hasChange := session.CurrentChange()
	currentProposal, hasProposal := session.CurrentProposal()
	verificationResult, hasVerification := session.LastVerification()
	decision, hasDecision := session.LastDecision()
	if !hasChange || !hasProposal || !hasVerification || !hasDecision {
		return Result{}, fmt.Errorf("canonical application requires the current approved Change, retained proposal, EvidenceSet, and APPROVE decision")
	}
	writeHumanDecisionSummary(output, currentChange, currentProposal, verificationResult)
	fmt.Fprintln(output, "Canonical application: explicit working-tree-only operation; HEAD and index must remain unchanged")
	applicationResult, terminal, err := session.canonicalIntegration.Apply(
		invocation.Context,
		currentChange,
		currentProposal,
		verificationResult,
		decision,
	)
	if applicationResult.CanonicalMutationOccurred() {
		session.markCanonicalMutation()
	}
	if err != nil {
		if applicationResult.CanonicalMutationOccurred() {
			fmt.Fprintln(output, "Canonical application: mutation occurred; lifecycle closure is incomplete")
			if applicationResult.CanonicalApplicationProven() {
				fmt.Fprintln(output, "Canonical proof: completed")
			}
		} else {
			fmt.Fprintln(output, "Canonical application: not performed")
		}
		fmt.Fprintf(output, "Change state: %s\n", currentChange.State())
		return Result{}, err
	}
	session.setCurrentChange(terminal)
	fmt.Fprintln(output, "Canonical application: completed and deterministically proven")
	fmt.Fprintf(output, "Canonical result digest: %s\n", applicationResult.ResultDigest())
	fmt.Fprintf(output, "Canonical HEAD: %s (unchanged)\n", applicationResult.CanonicalHead())
	fmt.Fprintf(output, "Git index: unchanged=%t\n", applicationResult.IndexUnchanged())
	fmt.Fprintf(output, "Changed paths: %s\n", formatSuppliedPaths(applicationResult.ChangedPaths()))
	fmt.Fprintln(output, "Git commit: not created")
	fmt.Fprintln(output, "Git push: not performed")
	fmt.Fprintf(output, "Change state: %s\n", terminal.State())
	cleanupError := session.cleanupTerminalProposal("approved Change reached audit-locked after canonical application")
	return Result{}, cleanupError
}

func handleChangeClose(session *Session, invocation Invocation, output io.Writer) (Result, error) {
	if len(invocation.Arguments) != 0 {
		return Result{}, errInvalidArguments
	}
	currentChange, hasChange := session.CurrentChange()
	currentProposal, hasProposal := session.CurrentProposal()
	verificationResult, hasVerification := session.LastVerification()
	decision, hasDecision := session.LastDecision()
	if !hasChange || !hasProposal || !hasVerification || !hasDecision {
		return Result{}, fmt.Errorf("rejection closure requires the current rejected Change, retained proposal, EvidenceSet, and REJECT decision")
	}
	terminal, err := session.canonicalIntegration.CloseRejected(
		invocation.Context,
		currentChange,
		currentProposal,
		verificationResult,
		decision,
	)
	if err != nil {
		return Result{}, err
	}
	session.setCurrentChange(terminal)
	fmt.Fprintln(output, "Rejected Change closure: canonical source proven unchanged")
	fmt.Fprintln(output, "Canonical application: not performed")
	fmt.Fprintf(output, "Change state: %s\n", terminal.State())
	cleanupError := session.cleanupTerminalProposal("rejected Change reached audit-locked with canonical source unchanged")
	return Result{}, cleanupError
}

func handleHumanDecision(
	session *Session,
	invocation Invocation,
	output io.Writer,
	kind approval.DecisionKind,
) (Result, error) {
	if len(invocation.Arguments) > 1 {
		return Result{}, errInvalidArguments
	}
	currentChange, hasChange := session.CurrentChange()
	if !hasChange {
		return Result{}, fmt.Errorf("no current Change for human decision")
	}
	currentProposal, hasProposal := session.CurrentProposal()
	if !hasProposal {
		return Result{}, fmt.Errorf("no retained proposal for human decision")
	}
	verificationResult, hasVerification := session.LastVerification()
	if !hasVerification {
		return Result{}, fmt.Errorf("no deterministic EvidenceSet for human decision")
	}
	rationale := ""
	if len(invocation.Arguments) == 1 {
		rationale = invocation.Arguments[0]
	}
	writeHumanDecisionSummary(output, currentChange, currentProposal, verificationResult)
	decision, transitioned, err := session.approval.Decide(
		invocation.Context,
		currentChange,
		currentProposal,
		verificationResult,
		kind,
		rationale,
	)
	if err != nil {
		if decision.Kind() == "" {
			fmt.Fprintln(output, "Human decision: NOT RECORDED")
		} else {
			fmt.Fprintf(output, "Human decision: %s RECORDED; state transition failed\n", decision.Kind())
			fmt.Fprintf(output, "Change state: %s\n", currentChange.State())
		}
		return Result{}, err
	}
	session.setCurrentChange(transitioned)
	session.setLastDecision(decision)
	fmt.Fprintf(output, "Human decision: %s\n", decision.Kind())
	fmt.Fprintf(output, "Actor provenance: %s\n", decision.Actor())
	if decision.Rationale() != "" {
		fmt.Fprintf(output, "Rationale: %s\n", decision.Rationale())
	}
	fmt.Fprintf(output, "Change state: %s\n", transitioned.State())
	fmt.Fprintln(output, "Canonical source: unchanged; decision authorizes or rejects later application")
	return Result{}, nil
}

func writeHumanDecisionSummary(
	output io.Writer,
	currentChange change.Change,
	currentProposal proposal.Proposal,
	verificationResult verification.Result,
) {
	artifact, hasArtifact := currentProposal.PatchArtifact()
	evidenceSet := verificationResult.EvidenceSet()
	fmt.Fprintln(output, "Decision summary:")
	fmt.Fprintf(output, "  Change: %s\n", boundedSingleLine(string(currentChange.ChangeId()), 160))
	fmt.Fprintf(output, "  Intent: %s\n", boundedSingleLine(string(currentChange.Intent()), 512))
	fmt.Fprintf(output, "  State: %s\n", currentChange.State())
	if hasArtifact {
		fmt.Fprintf(output, "  Patch: %s (%s)\n", artifact.PatchDigest(), artifact.DiffSummary())
		fmt.Fprintf(output, "  Changed files: %d\n", len(artifact.ChangedPaths()))
		for index, changedPath := range artifact.ChangedPaths() {
			if index == 10 {
				fmt.Fprintf(output, "    ... %d more\n", len(artifact.ChangedPaths())-index)
				break
			}
			fmt.Fprintf(output, "    %s\n", boundedSingleLine(changedPath, 256))
		}
		fmt.Fprintf(output, "  Base revision: %s\n", boundedSingleLine(artifact.BaseRevision(), 160))
	}
	verificationOutcome := "FAIL"
	if evidenceSet.Passed() {
		verificationOutcome = "PASS"
	}
	fmt.Fprintf(output, "  Verification: %s; attempt=%s; evidence=%s; checks=%d\n",
		verificationOutcome,
		evidenceSet.VerificationAttemptId(),
		evidenceSet.Id(),
		len(evidenceSet.Evidence()),
	)
}

func boundedSingleLine(value string, maximumBytes int) string {
	if maximumBytes <= 0 {
		return ""
	}
	value = strings.ToValidUTF8(value, "�")
	var builder strings.Builder
	truncated := false
	for _, character := range value {
		if unicode.IsControl(character) || unicode.In(character, unicode.Zl, unicode.Zp) {
			character = ' '
		}
		characterBytes := utf8.RuneLen(character)
		if builder.Len()+characterBytes > maximumBytes {
			truncated = true
			break
		}
		builder.WriteRune(character)
	}
	result := strings.TrimSpace(builder.String())
	if truncated {
		if len(result)+3 <= maximumBytes {
			result += "..."
		}
	}
	return result
}

func handleChangeDiscard(session *Session, invocation Invocation, output io.Writer) (Result, error) {
	if len(invocation.Arguments) != 0 {
		return Result{}, errInvalidArguments
	}
	currentProposal, ok := session.CurrentProposal()
	if !ok {
		return Result{}, fmt.Errorf("no current proposal workspace to discard")
	}
	workspaceId := currentProposal.Workspace().WorkspaceId()
	err := session.rejectAndDiscardProposal("developer discarded proposal workspace")
	if _, stillPresent := session.CurrentProposal(); !stillPresent {
		fmt.Fprintf(output, "Proposal workspace discarded: %s\n", workspaceId)
	}
	return Result{}, err
}

func requireNoCurrentProposal(session *Session) error {
	if currentProposal, ok := session.CurrentProposal(); ok {
		return fmt.Errorf(
			"proposal workspace %q is still %s; use change patch or change discard",
			currentProposal.Workspace().WorkspaceId(),
			currentProposal.Workspace().State(),
		)
	}
	return nil
}

func writeProviderSelection(output io.Writer, session *Session) {
	selection := session.ProviderSelection()
	descriptor, available := session.SelectedProviderDescriptor()
	fmt.Fprintf(output, "AI provider adapter: %s\n", selection.ProviderIdentifier())
	if available {
		fmt.Fprintf(output, "AI provider vendor: %s\n", descriptor.Vendor())
	}
	if modelIdentifier, selected := selection.ModelIdentifier(); selected {
		fmt.Fprintf(output, "AI model: %s\n", modelIdentifier)
	} else {
		fmt.Fprintln(output, "AI model: provider default")
	}
}

func parseSurfaceArguments(arguments []string) (source.ScopeRequest, []string, error) {
	return parseCategorizedSurfaceArguments(arguments, true)
}

func parseCategorizedSurfaceArguments(arguments []string, includeActual bool) (source.ScopeRequest, []string, error) {
	var request source.ScopeRequest
	var actual []string
	destinations := map[string]*[]string{
		"--expected":  &request.Expected,
		"--possible":  &request.Possible,
		"--protected": &request.Protected,
	}
	if includeActual {
		destinations["--actual"] = &actual
	}
	seen := make(map[string]bool, len(destinations))
	var current *[]string
	var currentFlag string
	for _, argument := range arguments {
		if destination, isFlag := destinations[argument]; isFlag {
			if seen[argument] {
				return source.ScopeRequest{}, nil, fmt.Errorf("surface option %s was provided more than once", argument)
			}
			if currentFlag != "" && len(*current) == 0 {
				return source.ScopeRequest{}, nil, fmt.Errorf("surface option %s requires at least one path", currentFlag)
			}
			seen[argument] = true
			current = destination
			currentFlag = argument
			continue
		}
		if strings.HasPrefix(argument, "--") {
			return source.ScopeRequest{}, nil, fmt.Errorf("unknown surface option %q", argument)
		}
		if current == nil {
			return source.ScopeRequest{}, nil, fmt.Errorf("surface path %q must follow a category option", argument)
		}
		*current = append(*current, argument)
	}
	if currentFlag != "" && len(*current) == 0 {
		return source.ScopeRequest{}, nil, fmt.Errorf("surface option %s requires at least one path", currentFlag)
	}
	if len(request.Expected) == 0 {
		return source.ScopeRequest{}, nil, fmt.Errorf("--expected requires at least one path")
	}
	if includeActual && len(actual) == 0 {
		return source.ScopeRequest{}, nil, fmt.Errorf("--actual requires at least one path")
	}
	return request, actual, nil
}

func writePatchReport(
	output io.Writer,
	currentProposal proposal.Proposal,
	validation source.SurfaceValidationResult,
) {
	workspace := currentProposal.Workspace()
	fmt.Fprintf(output, "Workspace ID: %s\n", workspace.WorkspaceId())
	fmt.Fprintf(output, "Workspace state: %s\n", workspace.State())
	fmt.Fprintf(output, "Base revision: %s\n", workspace.BaseRevision())
	fmt.Fprintf(output, "Source state digest: %s\n", workspace.SourceStateDigest())
	artifact, hasArtifact := currentProposal.PatchArtifact()
	if !hasArtifact {
		fmt.Fprintln(output, "Patch: empty")
		return
	}
	fmt.Fprintf(output, "Patch digest: %s\n", artifact.PatchDigest())
	fmt.Fprintf(output, "Diff summary: %s\n", artifact.DiffSummary())
	fmt.Fprintf(output, "Changed paths: %s\n", formatSuppliedPaths(artifact.ChangedPaths()))
	fmt.Fprintf(output, "Surface valid: %t\n", validation.Allowed())
	if len(validation.Violations()) == 0 {
		fmt.Fprintln(output, "Violations: none")
		fmt.Fprintln(output, "Disposition: surface-valid; retained for later deterministic validation and human approval")
		return
	}
	fmt.Fprintln(output, "Violations:")
	for _, violation := range validation.Violations() {
		fmt.Fprintf(
			output,
			"- %s: %s (%s)\n",
			violation.Kind(),
			formatPathForOutput(violation.Path()),
			violation.Reason(),
		)
	}
	fmt.Fprintln(output, "Disposition: rejected; no partial acceptance")
}

func writeSurfaceReport(
	output io.Writer,
	currentChange change.Change,
	snapshot source.SourceSnapshot,
	approvedScope source.ApprovedScope,
	validation source.SurfaceValidationResult,
) {
	surface := approvedScope.Surface()
	fmt.Fprintf(output, "Change ID: %s\n", currentChange.ChangeId())
	fmt.Fprintf(output, "Project ID: %s\n", currentChange.ProjectId())
	fmt.Fprintf(output, "Repository root: %s\n", snapshot.RepositoryRoot())
	fmt.Fprintf(output, "HEAD revision: %s\n", snapshot.HeadRevision())
	fmt.Fprintf(output, "Working tree: %s\n", snapshot.WorkingTreeState())
	fmt.Fprintf(output, "Tracked paths: %d\n", len(snapshot.TrackedPaths()))
	fmt.Fprintf(output, "Source state digest: %s\n", snapshot.SourceStateDigest())
	fmt.Fprintf(output, "Expected: %s\n", formatRepositoryPaths(surface.ExpectedPaths()))
	fmt.Fprintf(output, "Possible: %s\n", formatRepositoryPaths(surface.PossiblePaths()))
	fmt.Fprintf(output, "Protected: %s\n", formatRepositoryPaths(surface.ProtectedPaths()))
	fmt.Fprintf(output, "Actual: %s\n", formatSuppliedPaths(validation.SuppliedPaths()))
	fmt.Fprintf(output, "Allowed: %t\n", validation.Allowed())
	fmt.Fprintf(output, "Expected changes: %s\n", formatRepositoryPaths(validation.ExpectedChanges()))
	fmt.Fprintf(output, "Possible changes: %s\n", formatRepositoryPaths(validation.PossibleChanges()))
	if len(validation.Violations()) == 0 {
		fmt.Fprintln(output, "Violations: none")
		return
	}
	fmt.Fprintln(output, "Violations:")
	for _, violation := range validation.Violations() {
		fmt.Fprintf(
			output,
			"- %s: %s (%s)\n",
			violation.Kind(),
			formatPathForOutput(violation.Path()),
			violation.Reason(),
		)
	}
}

func formatSuppliedPaths(paths []string) string {
	values := make([]string, len(paths))
	for index, repositoryPath := range paths {
		values[index] = formatPathForOutput(repositoryPath)
	}
	return strings.Join(values, ", ")
}

func formatPathForOutput(repositoryPath string) string {
	if _, err := source.NormalizeRepositoryPath(repositoryPath); err != nil {
		return strconv.Quote(repositoryPath)
	}
	return repositoryPath
}

func formatRepositoryPaths(paths []source.RepositoryPath) string {
	if len(paths) == 0 {
		return "none"
	}
	values := make([]string, len(paths))
	for index, repositoryPath := range paths {
		values[index] = string(repositoryPath)
	}
	return strings.Join(values, ", ")
}
