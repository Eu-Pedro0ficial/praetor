package command

import (
	"errors"
	"fmt"
	"io"
	"strconv"
	"strings"

	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"github.com/Eu-Pedro0ficial/praetor/internal/proposal"
	"github.com/Eu-Pedro0ficial/praetor/internal/source"
)

func handleStatus(session *Session, invocation Invocation, output io.Writer) (Result, error) {
	if len(invocation.Arguments) != 0 {
		return Result{}, errInvalidArguments
	}
	registration := session.Registration()
	fmt.Fprintf(output, "Project ID: %s\n", registration.ProjectId)
	fmt.Fprintf(output, "Repository root: %s\n", registration.RepositoryRoot)
	fmt.Fprintln(output, "Git repository: true")
	if currentChange, ok := session.CurrentChange(); ok {
		fmt.Fprintf(output, "Current change: %s (%s)\n", currentChange.ChangeId(), currentChange.State())
	} else {
		fmt.Fprintln(output, "Current change: none")
	}
	if currentProposal, ok := session.CurrentProposal(); ok {
		workspace := currentProposal.Workspace()
		fmt.Fprintf(output, "Current proposal: %s (%s)\n", workspace.WorkspaceId(), workspace.State())
		fmt.Fprintf(output, "Proposal base revision: %s\n", workspace.BaseRevision())
	} else {
		fmt.Fprintln(output, "Current proposal: none")
	}
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
