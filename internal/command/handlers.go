package command

import (
	"context"
	"errors"
	"fmt"
	"io"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Eu-Pedro0ficial/praetor/internal/approval"
	"github.com/Eu-Pedro0ficial/praetor/internal/artifact"
	"github.com/Eu-Pedro0ficial/praetor/internal/authority"
	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"github.com/Eu-Pedro0ficial/praetor/internal/impact"
	"github.com/Eu-Pedro0ficial/praetor/internal/integration"
	"github.com/Eu-Pedro0ficial/praetor/internal/policy"
	"github.com/Eu-Pedro0ficial/praetor/internal/presentation/preferences"
	"github.com/Eu-Pedro0ficial/praetor/internal/proposal"
	"github.com/Eu-Pedro0ficial/praetor/internal/source"
	"github.com/Eu-Pedro0ficial/praetor/internal/verification"
	"github.com/Eu-Pedro0ficial/praetor/internal/workflow"
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
	fmt.Fprintf(output, "Recovery: %s\n", snapshot.Recovery)
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

func requireDurableInspection(session *Session) error {
	if session == nil || session.durableInspection == nil {
		return fmt.Errorf("durable Change inspection is not configured")
	}
	return nil
}

func inspectionChangeId(session *Session, arguments []string) (change.ChangeId, error) {
	if len(arguments) > 1 {
		return "", errInvalidArguments
	}
	if len(arguments) == 1 {
		return change.NewChangeId(arguments[0])
	}
	current, ok := session.CurrentChange()
	if !ok {
		return "", fmt.Errorf("select or name a durable Change")
	}
	return current.ChangeId(), nil
}

func handleChangeList(session *Session, invocation Invocation, output io.Writer) (Result, error) {
	if len(invocation.Arguments) != 0 {
		return Result{}, errInvalidArguments
	}
	if err := requireDurableInspection(session); err != nil {
		return Result{}, err
	}
	changes, err := session.durableInspection.ListChanges()
	if err != nil {
		return Result{}, err
	}
	if len(changes) == 0 {
		fmt.Fprintln(output, "No durable Changes.")
		return Result{}, nil
	}
	for _, current := range changes {
		fmt.Fprintf(output, "%s state=%s revision=%d updated=%s\n", current.ChangeId(), current.State(), current.Revision(), current.UpdatedAt().Format(time.RFC3339Nano))
	}
	return Result{}, nil
}

func handleChangeShow(session *Session, invocation Invocation, output io.Writer) (Result, error) {
	if err := requireDurableInspection(session); err != nil {
		return Result{}, err
	}
	id, err := inspectionChangeId(session, invocation.Arguments)
	if err != nil {
		return Result{}, err
	}
	detail, err := session.durableInspection.InspectChange(id)
	if err != nil {
		return Result{}, err
	}
	current := detail.Change
	fmt.Fprintf(output, "Change ID: %s\nProject ID: %s\nIntent: %s\nState: %s\nRevision: %d\nCreated: %s\nUpdated: %s\nWorkflow: %s@%s schema=%d digest=%s\nArtifacts: %d\nAudit events: %d\n", current.ChangeId(), current.ProjectId(), boundedSingleLine(string(current.Intent()), 4096), current.State(), current.Revision(), current.CreatedAt().Format(time.RFC3339Nano), current.UpdatedAt().Format(time.RFC3339Nano), detail.Workflow.WorkflowId(), detail.Workflow.WorkflowVersion(), detail.Workflow.SchemaVersion(), detail.Workflow.Digest(), len(detail.Artifacts), len(detail.Audit))
	return Result{}, nil
}

func handleChangeSelect(session *Session, invocation Invocation, output io.Writer) (Result, error) {
	if len(invocation.Arguments) != 1 {
		return Result{}, errInvalidArguments
	}
	if err := requireDurableInspection(session); err != nil {
		return Result{}, err
	}
	id, err := change.NewChangeId(invocation.Arguments[0])
	if err != nil {
		return Result{}, err
	}
	detail, err := session.durableInspection.InspectChange(id)
	if err != nil {
		return Result{}, err
	}
	if err := session.hydrateDurableChange(detail); err != nil {
		return Result{}, err
	}
	fmt.Fprintf(output, "Selected Change %s state=%s revision=%d\n", id, detail.Change.State(), detail.Change.Revision())
	return Result{}, nil
}

func handleChangeArtifacts(session *Session, invocation Invocation, output io.Writer) (Result, error) {
	if err := requireDurableInspection(session); err != nil {
		return Result{}, err
	}
	id, err := inspectionChangeId(session, invocation.Arguments)
	if err != nil {
		return Result{}, err
	}
	detail, err := session.durableInspection.InspectChange(id)
	if err != nil {
		return Result{}, err
	}
	for _, item := range detail.Artifacts {
		fmt.Fprintf(output, "%s kind=%s bytes=%d media=%s content=%s record=%s created=%s\n", item.Id, item.Kind, item.ByteLength, item.MediaType, item.ContentDigest, item.RecordDigest, item.CreatedAt.Format(time.RFC3339Nano))
	}
	for _, binding := range detail.Bindings {
		fmt.Fprintf(output, "binding role=%s artifact=%s revision=%d\n", binding.Role, binding.ArtifactId, binding.Revision)
	}
	for _, relationship := range detail.Relationships {
		fmt.Fprintf(output, "relationship %s -%s-> %s\n", relationship.From, relationship.Kind, relationship.To)
	}
	for _, operation := range detail.Operations {
		fmt.Fprintf(output, "operation %s kind=%s state=%s expected-revision=%d request=%s\n", operation.Id, boundedSingleLine(operation.Kind, 128), operation.State, operation.ExpectedRevision, operation.RequestDigest)
	}
	if len(detail.Artifacts) == 0 {
		fmt.Fprintln(output, "No durable artifacts.")
	}
	return Result{}, nil
}

func handleChangeHistory(session *Session, invocation Invocation, output io.Writer) (Result, error) {
	if err := requireDurableInspection(session); err != nil {
		return Result{}, err
	}
	id, err := inspectionChangeId(session, invocation.Arguments)
	if err != nil {
		return Result{}, err
	}
	detail, err := session.durableInspection.InspectChange(id)
	if err != nil {
		return Result{}, err
	}
	for _, event := range detail.Audit {
		fmt.Fprintf(output, "%s %s %s\n", event.Timestamp.Format(time.RFC3339Nano), boundedSingleLine(event.EventType, 128), boundedSingleLine(event.EventID, 128))
	}
	return Result{}, nil
}

func handleChangeDiagnose(session *Session, invocation Invocation, output io.Writer) (Result, error) {
	if err := requireDurableInspection(session); err != nil {
		return Result{}, err
	}
	if len(invocation.Arguments) == 0 {
		if _, selected := session.CurrentChange(); !selected {
			fmt.Fprintln(output, "No Change selected; specify a ChangeId to diagnose durable recovery authority.")
			return Result{}, nil
		}
	}
	id, err := inspectionChangeId(session, invocation.Arguments)
	if err != nil {
		return Result{}, err
	}
	diagnoses, err := session.durableInspection.Diagnose(id)
	if err != nil {
		return Result{}, err
	}
	for _, diagnosis := range diagnoses {
		fmt.Fprintf(output, "%s: %s", diagnosis.Condition, boundedSingleLine(diagnosis.Detail, 1024))
		if diagnosis.Operation != nil {
			fmt.Fprintf(output, " operation=%s kind=%s", diagnosis.Operation.Id, boundedSingleLine(diagnosis.Operation.Kind, 128))
		}
		fmt.Fprintln(output)
	}
	return Result{}, nil
}

func handleChangeRecover(session *Session, invocation Invocation, output io.Writer) (Result, error) {
	if len(invocation.Arguments) != 1 {
		return Result{}, errInvalidArguments
	}
	if session == nil || session.canonicalRecovery == nil {
		return Result{}, fmt.Errorf("canonical recovery is not configured")
	}
	condition, proof, err := session.canonicalRecovery.RecoverPersisted(authority.OperationId(invocation.Arguments[0]), func(operation authority.Operation, proof integration.CanonicalProof) error {
		_, finalizeError := session.finalizeRecoveredCanonical(operation, proof)
		return finalizeError
	})
	if err != nil {
		return Result{}, err
	}
	fmt.Fprintf(output, "Recovery condition: %s\n", condition)
	if condition == integration.ConditionPRE {
		fmt.Fprintln(output, "Operation remains incomplete and was not replayed.")
	} else if condition == integration.ConditionPOST {
		operation, operationError := session.durableAuthority.GetOperation(authority.OperationId(invocation.Arguments[0]))
		if operationError != nil {
			return Result{}, operationError
		}
		terminal, _, loadError := session.durableAuthority.GetChange(operation.ChangeId)
		if loadError != nil {
			return Result{}, loadError
		}
		session.setCurrentChange(terminal)
		fmt.Fprintf(output, "Operation finalized from exact POST without reapplication; patch=%s head=%s\n", proof.PatchDigest(), proof.HeadRevision())
		fmt.Fprintf(output, "Change state: %s revision=%d\n", terminal.State(), terminal.Revision())
	}
	return Result{}, nil
}

func handleChangeContent(session *Session, invocation Invocation, output io.Writer) (Result, error) {
	if len(invocation.Arguments) < 1 || len(invocation.Arguments) > 2 {
		return Result{}, errInvalidArguments
	}
	if err := requireDurableInspection(session); err != nil {
		return Result{}, err
	}
	changeArgs := invocation.Arguments[1:]
	id, err := inspectionChangeId(session, changeArgs)
	if err != nil {
		return Result{}, err
	}
	payload, err := session.durableInspection.Content(id, artifact.ArtifactId(invocation.Arguments[0]))
	if err != nil {
		return Result{}, err
	}
	fmt.Fprintf(output, "Artifact %s content (%d bytes): %q\n", invocation.Arguments[0], len(payload), string(payload))
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

func handleAnalysisModel(session *Session, invocation Invocation, output io.Writer) (Result, error) {
	if len(invocation.Arguments) != 0 {
		return Result{}, errInvalidArguments
	}
	if session == nil || session.repositoryModeling == nil {
		return Result{}, fmt.Errorf("repository modeling is not configured")
	}
	built, err := session.repositoryModeling.Build()
	if err != nil {
		return Result{}, err
	}
	freshness, err := session.repositoryModeling.Freshness(built.Model)
	if err != nil {
		return Result{}, err
	}
	fingerprint := built.Model.Fingerprint()
	fmt.Fprintf(output, "RepositoryModel ID: %s\n", built.Model.ModelId())
	fmt.Fprintf(output, "Model digest: %s\n", built.Model.ModelDigest())
	fmt.Fprintf(output, "Build key: %s\n", built.Model.BuildKey().Digest)
	fmt.Fprintf(output, "Source fingerprint: %s\n", fingerprint.Digest)
	fmt.Fprintf(output, "Source condition: %s freshness=%s\n", fingerprint.WorkingTreeState, freshness)
	fmt.Fprintf(output, "Graph: nodes=%d edges=%d gaps=%d truncated=%t\n", len(built.Model.Nodes()), len(built.Model.Edges()), len(built.Model.Gaps()), built.Model.Truncated())
	fmt.Fprintf(output, "Build: cache_hit=%t reused_files=%d analyzed_files=%d duration=%s\n", built.Statistics.CacheHit, built.Statistics.ReusedFiles, built.Statistics.AnalyzedFiles, built.Statistics.Duration)
	return Result{}, nil
}

func handleAnalysisReport(session *Session, invocation Invocation, output io.Writer) (Result, error) {
	if session == nil || session.impactAnalysis == nil {
		return Result{}, fmt.Errorf("durable M1.2 impact analysis is not configured")
	}
	if len(invocation.Arguments) < 3 {
		return Result{}, errInvalidArguments
	}
	changeId, err := change.NewChangeId(invocation.Arguments[0])
	if err != nil {
		return Result{}, err
	}
	request, _, err := parseCategorizedSurfaceArguments(invocation.Arguments[1:], false, false)
	if err != nil {
		return Result{}, err
	}
	result, err := session.impactAnalysis.AnalyzeAndPersist(changeId, impact.Request{Expected: request.Expected, Possible: request.Possible, Protected: request.Protected})
	if err != nil {
		return Result{}, err
	}
	writeImpactReport(output, result.Report, result.Artifact.Id(), "CURRENT")
	fmt.Fprintf(output, "Model build: cache_hit=%t reused_files=%d analyzed_files=%d\n", result.BuildStats.CacheHit, result.BuildStats.ReusedFiles, result.BuildStats.AnalyzedFiles)
	return Result{}, nil
}

func handleAnalysisInspect(session *Session, invocation Invocation, output io.Writer) (Result, error) {
	if session == nil || session.impactAnalysis == nil {
		return Result{}, fmt.Errorf("durable M1.2 impact analysis is not configured")
	}
	if len(invocation.Arguments) < 1 || len(invocation.Arguments) > 2 {
		return Result{}, errInvalidArguments
	}
	changeId, err := change.NewChangeId(invocation.Arguments[0])
	if err != nil {
		return Result{}, err
	}
	var artifactId artifact.ArtifactId
	if len(invocation.Arguments) == 2 {
		artifactId = artifact.ArtifactId(invocation.Arguments[1])
	}
	report, resolvedArtifactId, freshness, err := session.impactAnalysis.Inspect(changeId, artifactId)
	if err != nil {
		return Result{}, err
	}
	writeImpactReport(output, report, resolvedArtifactId, string(freshness))
	return Result{}, nil
}

func writeImpactReport(output io.Writer, report impact.Report, artifactId artifact.ArtifactId, freshness string) {
	if artifactId != "" {
		fmt.Fprintf(output, "ImpactReport artifact: %s\n", artifactId)
	}
	fmt.Fprintf(output, "Change ID: %s\n", report.ChangeId)
	fmt.Fprintf(output, "RepositoryModel: %s digest=%s\n", report.RepositoryModelId, report.RepositoryModelDigest)
	fmt.Fprintf(output, "Build key: %s freshness=%s\n", report.BuildKey.Digest, freshness)
	fmt.Fprintf(output, "Report digest: %s\n", report.ReportDigest)
	fmt.Fprintf(output, "Risk: %s advisory=%t\n", report.Risk.Overall, report.Risk.Advisory)
	for _, dimension := range report.Risk.Dimensions {
		fmt.Fprintf(output, "- risk %s disposition=%s evidence=%s\n", dimension.Name, dimension.Disposition, dimension.Explanation)
	}
	fmt.Fprintf(output, "Impact: items=%d gaps=%d truncated=%t\n", len(report.Items), len(report.KnowledgeGaps), report.Truncated)
	for _, item := range report.Items {
		fmt.Fprintf(output, "- %s %s kind=%s path=%s basis=%s", item.Classification, item.Element.Name, item.Element.Kind, item.Element.Path, item.Basis)
		if item.Confidence != "" {
			fmt.Fprintf(output, " confidence=%s", item.Confidence)
		}
		fmt.Fprintf(output, " explanation_steps=%d gaps=%s\n", len(item.Explanation), strings.Join(item.GapIds, ","))
		for _, step := range item.Explanation {
			fmt.Fprintf(output, "  why %s -> %s relation=%s assertion=%s->%s basis=%s", step.From, step.To, step.Relation, step.AssertionFrom, step.AssertionTo, step.Basis)
			if step.Confidence != "" {
				fmt.Fprintf(output, " confidence=%s", step.Confidence)
			}
			fmt.Fprintf(output, " analyzer=%s@%s\n", step.Provenance.AnalyzerId, step.Provenance.AnalyzerVersion)
		}
	}
	for _, gap := range report.KnowledgeGaps {
		fmt.Fprintf(output, "- gap %s scope=%s reason=%s\n", gap.Category, gap.Scope, gap.Reason)
	}
}

func handleChangeIsolate(session *Session, invocation Invocation, output io.Writer) (Result, error) {
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
	scopeRequest, _, err := parseCategorizedSurfaceArguments(arguments[2:], false, true)
	if err != nil {
		return Result{}, err
	}
	if _, err := source.NewChangeSurface(scopeRequest); err != nil {
		return Result{}, err
	}

	var currentChange change.Change
	if session.durableAuthority != nil {
		currentChange, _, err = session.changeWorkflow.GetDurable(changeId)
		if err != nil {
			if !errors.Is(err, workflow.ErrChangeNotFound) {
				return Result{}, err
			}
			currentChange, err = session.changeWorkflow.Create(changeId, intent, invocation.CommandPath)
			if err != nil {
				return Result{}, err
			}
		} else {
			if currentChange.ProjectId() != session.registration.ProjectId {
				return Result{}, fmt.Errorf("existing Change %q belongs to another Project", changeId)
			}
			if currentChange.Intent() != intent {
				return Result{}, fmt.Errorf("existing Change %q intent does not match isolate request", changeId)
			}
			if currentChange.State() != change.StateCreated {
				return Result{}, fmt.Errorf("existing Change %q must be in CREATED state to isolate; current state is %s", changeId, currentChange.State())
			}
		}
	} else {
		currentChange, err = session.changeWorkflow.Create(changeId, intent, invocation.CommandPath)
		if err != nil {
			return Result{}, err
		}
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
	foundation, artifactError := proposalFoundationSpecs(currentProposal)
	if artifactError != nil {
		return Result{}, artifactError
	}
	var transitionError error
	if session.durableAuthority != nil {
		currentChange, transitionError = session.commitTransitionWithArtifacts(currentChange, change.StateIsolated, invocation.CommandPath+" proposal workspace created", foundation)
	} else {
		currentChange, transitionError = session.changeWorkflow.Transition(changeId, change.StateIsolated, invocation.CommandPath+" proposal workspace created")
	}
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
	if session.durableAuthority == nil {
		if err := session.persistGovernedArtifacts(currentChange, foundation...); err != nil {
			return Result{}, err
		}
	}

	workspace := currentProposal.Workspace()
	fmt.Fprintf(output, "Change ID: %s\n", workspace.ChangeId())
	fmt.Fprintf(output, "Project ID: %s\n", workspace.ProjectId())
	fmt.Fprintf(output, "Change state: %s\n", currentChange.State())
	fmt.Fprintf(output, "Workspace ID: %s\n", workspace.WorkspaceId())
	fmt.Fprintf(output, "Workspace root: %s\n", workspace.Root())
	fmt.Fprintf(output, "Workspace state: %s\n", workspace.State())
	fmt.Fprintf(output, "Base revision: %s\n", workspace.BaseRevision())
	fmt.Fprintf(output, "Source state digest: %s\n", workspace.SourceStateDigest())
	fmt.Fprintf(output, "Authorization mode: %s\n", currentProposal.ApprovedScope().Surface().AuthorizationMode())
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
		currentChange, _ := session.CurrentChange()
		spec, artifactError := patchSpec(classifiedProposal)
		if artifactError != nil {
			return Result{}, artifactError
		}
		if err := session.persistGovernedArtifacts(currentChange, spec); err != nil {
			return Result{}, err
		}
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

	implementationContext := session.implementationContext(currentChange)
	executionResult, implementationError := session.providerExecution.ImplementWithContext(
		invocation.Context,
		currentChange,
		currentProposal,
		session.ProviderSelection(),
		implementationContext,
	)
	session.setCurrentProposal(executionResult.Proposal())
	if implementationError != nil {
		cleanupReason := implementationCleanupReason(implementationError)
		var emptyPatch proposal.EmptyPatchError
		if errors.As(implementationError, &emptyPatch) {
			writeNoPatchDiagnostics(output, executionResult, cleanupReason)
		}
		cleanupError := session.rejectAndDiscardProposal(cleanupReason)
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
	spec, artifactError := patchSpec(executionResult.Proposal())
	if artifactError != nil {
		return Result{}, artifactError
	}
	if err := session.persistGovernedArtifacts(currentChange, spec); err != nil {
		return Result{}, err
	}
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
	var policyDecision policy.BundleDecision
	var err error
	if session.durableAuthority != nil {
		policyDecision, err = session.policy.EvaluateCandidate(session.registration.RepositoryRoot, currentChange.ProjectId(), currentChange.ChangeId(), verificationResult.EvidenceSet())
	} else {
		policyDecision, err = session.policy.Evaluate(session.registration.RepositoryRoot, currentChange.ProjectId(), currentChange.ChangeId(), verificationResult.EvidenceSet())
	}
	if err != nil {
		return Result{}, fmt.Errorf("evaluate Project Policy: %w", err)
	}
	specs, artifactError := verificationSpecs(verificationResult, policyDecision)
	if artifactError != nil {
		return Result{}, artifactError
	}
	var validatedChange change.Change
	if session.durableAuthority != nil {
		policyEvent, eventError := session.policyDecisionAuditEvent(policyDecision)
		if eventError != nil {
			return Result{}, eventError
		}
		validatedChange, err = session.commitTransitionWithArtifacts(currentChange, change.StateValidated, invocation.CommandPath+" deterministic verification passed", specs, policyEvent)
	} else {
		validatedChange, err = session.changeWorkflow.Transition(currentChange.ChangeId(), change.StateValidated, invocation.CommandPath+" deterministic verification passed")
		if err == nil {
			err = session.persistGovernedArtifacts(validatedChange, specs...)
		}
	}
	if err != nil {
		return Result{}, err
	}
	session.setCurrentChange(validatedChange)
	session.setLastPolicyDecision(policyDecision)
	if session.durableAuthority != nil {
		session.policy.RetainDecision(policyDecision)
	}
	fmt.Fprintf(output, "Change state: %s\n", validatedChange.State())
	writePolicyDecision(output, policyDecision)
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
	policyDecision, hasPolicyDecision := session.LastPolicyDecision()
	if !hasChange || !hasProposal || !hasVerification || !hasDecision || !hasPolicyDecision {
		return Result{}, fmt.Errorf("canonical application requires the current approved Change, retained proposal, EvidenceSet, and APPROVE decision")
	}
	writeHumanDecisionSummary(output, currentChange, currentProposal, verificationResult)
	fmt.Fprintln(output, "Canonical application: explicit working-tree-only operation; HEAD and index must remain unchanged")
	applicationResult, terminal, err := session.canonicalIntegration.Apply(
		invocation.Context,
		currentChange,
		currentProposal,
		verificationResult,
		policyDecision,
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
	if session.durableAuthority == nil {
		resultArtifact, artifactError := applicationResultSpec(applicationResult)
		if artifactError != nil {
			return Result{}, artifactError
		}
		if err := session.persistGovernedArtifacts(terminal, resultArtifact); err != nil {
			return Result{}, err
		}
	}
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
	policyDecision, hasPolicyDecision := session.LastPolicyDecision()
	if !hasChange || !hasProposal || !hasVerification || !hasDecision || !hasPolicyDecision {
		return Result{}, fmt.Errorf("rejection closure requires the current rejected Change, retained proposal, EvidenceSet, and REJECT decision")
	}
	terminal, err := session.canonicalIntegration.CloseRejected(
		invocation.Context,
		currentChange,
		currentProposal,
		verificationResult,
		policyDecision,
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
	policyDecision, hasPolicyDecision := session.LastPolicyDecision()
	if !hasPolicyDecision {
		return Result{}, fmt.Errorf("no retained PolicyDecision for human decision")
	}
	rationale := ""
	if len(invocation.Arguments) == 1 {
		rationale = invocation.Arguments[0]
	}
	writeHumanDecisionSummary(output, currentChange, currentProposal, verificationResult)
	var decision approval.HumanDecision
	var transitioned change.Change
	var err error
	if session.durableAuthority != nil {
		decision, err = session.approval.PrepareDecision(invocation.Context, currentChange, currentProposal, verificationResult, policyDecision, kind, rationale)
		if err == nil {
			decisionArtifact, artifactError := humanDecisionSpec(decision)
			if artifactError != nil {
				return Result{}, artifactError
			}
			decisionEvent, eventError := session.humanDecisionAuditEvent(decision)
			if eventError != nil {
				return Result{}, eventError
			}
			transitioned, err = session.commitTransitionWithArtifacts(currentChange, decision.RequestedState(), transitionContextForDecision(decision), []durableArtifactSpec{decisionArtifact}, decisionEvent)
		}
	} else {
		decision, transitioned, err = session.approval.Decide(invocation.Context, currentChange, currentProposal, verificationResult, policyDecision, kind, rationale)
	}
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

func transitionContextForDecision(decision approval.HumanDecision) string {
	if decision.Kind() == approval.DecisionApprove {
		return "explicit local human approval recorded"
	}
	return "explicit local human rejection recorded"
}

func handlePolicyShow(session *Session, invocation Invocation, output io.Writer) (Result, error) {
	if len(invocation.Arguments) != 0 {
		return Result{}, errInvalidArguments
	}
	bundle, err := session.policy.Load(session.registration.RepositoryRoot)
	if err != nil {
		return Result{}, err
	}
	fmt.Fprintf(output, "Project Policy Manifest: engineering/policies/praetor.yaml\nBundle: %s version=%s digest=%s\nPolicies: %d\n", bundle.Id(), bundle.Version(), bundle.Digest(), len(bundle.Policies()))
	if decision, ok := session.LastPolicyDecision(); ok {
		fmt.Fprintln(output, "Latest retained policy evaluation:")
		writePolicyDecision(output, decision)
	}
	if currentChange, ok := session.CurrentChange(); ok {
		if candidate, found := session.policy.Candidate(currentChange.ChangeId()); found {
			writePolicyCandidate(output, candidate)
		}
	}
	return Result{}, nil
}

func handlePolicyList(session *Session, invocation Invocation, output io.Writer) (Result, error) {
	if len(invocation.Arguments) != 0 {
		return Result{}, errInvalidArguments
	}
	bundle, err := session.policy.Load(session.registration.RepositoryRoot)
	if err != nil {
		return Result{}, err
	}
	for _, rule := range bundle.Policies() {
		fmt.Fprintf(output, "%s@%s family=%s severity=%s outcome=%s evidence=%s non-overridable=%t exception-candidate=%t\n", rule.Id(), rule.Version(), rule.Family(), rule.Severity(), rule.Outcome(), rule.RequiredEvidenceKind(), rule.NonOverridable(), rule.ExceptionCandidateAllowed())
	}
	return Result{}, nil
}

func handlePolicyEvaluate(session *Session, invocation Invocation, output io.Writer) (Result, error) {
	if len(invocation.Arguments) != 0 {
		return Result{}, errInvalidArguments
	}
	currentChange, ok := session.CurrentChange()
	if !ok || currentChange.State() != change.StateValidated {
		return Result{}, fmt.Errorf("current Change must be validated before policy evaluation")
	}
	verificationResult, ok := session.LastVerification()
	if !ok {
		return Result{}, fmt.Errorf("policy evaluation requires deterministic EvidenceSet")
	}
	decision, err := session.policy.Evaluate(session.registration.RepositoryRoot, currentChange.ProjectId(), currentChange.ChangeId(), verificationResult.EvidenceSet())
	if err != nil {
		return Result{}, err
	}
	session.setLastPolicyDecision(decision)
	writePolicyDecision(output, decision)
	return Result{}, nil
}

func handlePolicyException(session *Session, invocation Invocation, output io.Writer) (Result, error) {
	if len(invocation.Arguments) != 4 {
		return Result{}, errInvalidArguments
	}
	decision, ok := session.LastPolicyDecision()
	if !ok {
		return Result{}, fmt.Errorf("exception candidate requires a retained PolicyDecision")
	}
	candidate, err := session.policy.CreateExceptionCandidate(decision, policy.PolicyId(invocation.Arguments[0]), invocation.Arguments[3], invocation.Arguments[1], invocation.Arguments[2], nil)
	if err != nil {
		return Result{}, err
	}
	writePolicyCandidate(output, candidate)
	return Result{}, nil
}

func writePolicyCandidate(output io.Writer, candidate policy.PolicyExceptionCandidate) {
	fmt.Fprintf(output, "Exception candidate: %s policy=%s evaluation=%s scope=%s authority=%s\nOriginal outcome unchanged; no exception was granted or consumed.\n", candidate.Id(), candidate.PolicyId(), candidate.EvaluationId(), candidate.Scope(), candidate.RequestedAuthority())
}

func writePolicyDecision(output io.Writer, decision policy.BundleDecision) {
	aggregate := decision.Aggregate()
	fmt.Fprintf(output, "Policy evaluation: %s bundle=%s@%s digest=%s\n", decision.Id(), decision.Bundle().Id(), decision.Bundle().Version(), decision.Bundle().Digest())
	for _, item := range decision.Decisions() {
		fmt.Fprintf(output, "Policy: %s severity=%s outcome=%s reason=%s\n", item.Policy().Id(), item.Policy().Severity(), item.Outcome(), item.Reason())
	}
	fmt.Fprintf(output, "Policy requirements: denied=%t review=%t approval=%t\n", aggregate.Denied, aggregate.RequiresReview, aggregate.RequiresApproval)
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
	return parseCategorizedSurfaceArguments(arguments, true, false)
}

func parseCategorizedSurfaceArguments(arguments []string, includeActual, defaultRepositoryWide bool) (source.ScopeRequest, []string, error) {
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
	expectedRoot := slices.Contains(request.Expected, ".")
	possibleRoot := slices.Contains(request.Possible, ".")
	if slices.Contains(request.Protected, ".") {
		return source.ScopeRequest{}, nil, fmt.Errorf("--protected . would prohibit the complete repository")
	}
	if expectedRoot || possibleRoot {
		if expectedRoot && possibleRoot {
			return source.ScopeRequest{}, nil, fmt.Errorf("--expected . and --possible . are redundant; use one repository-root form")
		}
		if len(request.Expected)+len(request.Possible) != 1 {
			return source.ScopeRequest{}, nil, fmt.Errorf("repository-root authorization cannot be combined with explicit expected or possible paths")
		}
		request.AuthorizationMode = source.AuthorizationRepositoryWide
		request.Expected = nil
		request.Possible = nil
	} else if len(request.Expected) == 0 && len(request.Possible) == 0 {
		if !defaultRepositoryWide {
			return source.ScopeRequest{}, nil, fmt.Errorf("--expected or --possible requires at least one path")
		}
		request.AuthorizationMode = source.AuthorizationRepositoryWide
	} else {
		request.AuthorizationMode = source.AuthorizationExplicitPaths
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
	fmt.Fprintf(output, "Authorization mode: %s\n", surface.AuthorizationMode())
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
