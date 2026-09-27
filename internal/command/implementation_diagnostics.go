package command

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/Eu-Pedro0ficial/praetor/internal/aiprovider"
	"github.com/Eu-Pedro0ficial/praetor/internal/artifact"
	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"github.com/Eu-Pedro0ficial/praetor/internal/execution"
	"github.com/Eu-Pedro0ficial/praetor/internal/impact"
	"github.com/Eu-Pedro0ficial/praetor/internal/proposal"
	"github.com/Eu-Pedro0ficial/praetor/internal/repositorymodel"
	"github.com/Eu-Pedro0ficial/praetor/internal/source"
)

const maximumImplementationContextLineBytes = 1000

func (session *Session) implementationContext(current change.Change) aiprovider.ImplementationContext {
	if session == nil || session.impactAnalysis == nil {
		return aiprovider.ImplementationContext{}
	}
	report, artifactId, freshness, err := session.impactAnalysis.Inspect(current.ChangeId(), "")
	if err != nil {
		return aiprovider.ImplementationContext{}
	}
	return implementationContextFromImpactReport(current, report, artifactId, freshness)
}

func implementationContextFromImpactReport(
	current change.Change,
	report impact.Report,
	artifactId artifact.ArtifactId,
	freshness repositorymodel.Freshness,
) aiprovider.ImplementationContext {
	if freshness != repositorymodel.FreshnessCurrent || report.Intent != string(current.Intent()) {
		return aiprovider.ImplementationContext{}
	}
	sourceDescription := fmt.Sprintf(
		"current ImpactReport %s digest=%s model=%s risk=%s",
		artifactId,
		report.ReportDigest,
		report.RepositoryModelId,
		report.Risk.Overall,
	)
	candidates := make([]string, 0, len(report.Items)+len(report.KnowledgeGaps))
	for _, item := range report.Items {
		location := item.Element.Path
		if location == "" {
			location = item.Element.Name
		}
		candidates = append(candidates, boundedContextLine(fmt.Sprintf(
			"impact=%s kind=%s location=%s basis=%s confidence=%s",
			item.Classification,
			item.Element.Kind,
			location,
			item.Basis,
			item.Confidence,
		)))
	}
	for _, gap := range report.KnowledgeGaps {
		candidates = append(candidates, boundedContextLine(fmt.Sprintf(
			"knowledge-gap category=%s scope=%s consequence=%s",
			gap.Category,
			gap.Scope,
			gap.Consequence,
		)))
	}

	entries := make([]string, 0, min(len(candidates), aiprovider.MaximumImplementationContextEntries))
	totalBytes := len(sourceDescription)
	for _, entry := range candidates {
		if len(entries) == aiprovider.MaximumImplementationContextEntries ||
			totalBytes+len(entry) > aiprovider.MaximumImplementationContextBytes {
			continue
		}
		entries = append(entries, entry)
		totalBytes += len(entry)
	}
	context, err := aiprovider.NewImplementationContextWithDiagnostics(
		sourceDescription,
		entries,
		len(candidates),
		len(entries) < len(candidates),
	)
	if err != nil {
		return aiprovider.ImplementationContext{}
	}
	return context
}

func boundedContextLine(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	if len(value) <= maximumImplementationContextLineBytes {
		return value
	}
	value = value[:maximumImplementationContextLineBytes]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value
}

func implementationCleanupReason(implementationError error) string {
	var emptyPatch proposal.EmptyPatchError
	if errors.As(implementationError, &emptyPatch) {
		return boundedDiagnosticReason(emptyPatch.Error())
	}
	var surfaceError *source.SurfaceValidationError
	if errors.As(implementationError, &surfaceError) {
		return boundedDiagnosticReason("provider produced a patch outside ApprovedScope: " + surfaceError.Error())
	}
	var providerError *aiprovider.ExecutionError
	if errors.As(implementationError, &providerError) {
		return boundedDiagnosticReason(fmt.Sprintf("provider execution failed: kind=%s", providerError.Kind()))
	}
	return boundedDiagnosticReason("provider implementation failed: " + implementationError.Error())
}

func boundedDiagnosticReason(value string) string {
	value = strings.Join(strings.Fields(value), " ")
	if len(value) > 1024 {
		value = value[:1024]
		for !utf8.ValidString(value) {
			value = value[:len(value)-1]
		}
	}
	return value
}

func writeRequestContextDiagnostics(output io.Writer, accounting aiprovider.RequestContextAccounting) {
	fmt.Fprintf(
		output,
		"Request context: total_bytes=%d total_characters=%d total_items=%d truncated=%t\n",
		accounting.TotalBytes(),
		accounting.TotalCharacters(),
		accounting.TotalItems(),
		accounting.Truncated(),
	)
	for _, component := range accounting.Components() {
		fmt.Fprintf(
			output,
			"Request context component: %s bytes=%d characters=%d items=%d omitted=%d truncated=%t\n",
			component.Kind(),
			component.ByteCount(),
			component.CharacterCount(),
			component.ItemCount(),
			component.OmittedItems(),
			component.Truncated(),
		)
	}
}

func writeProviderUsage(output io.Writer, response aiprovider.ProviderResponse) {
	if response.Usage().Available() {
		fmt.Fprintf(
			output,
			"Token usage: input=%d cached_input=%d output=%d reasoning_output=%d\n",
			response.Usage().InputTokens(),
			response.Usage().CachedInputTokens(),
			response.Usage().OutputTokens(),
			response.Usage().ReasoningOutputTokens(),
		)
		return
	}
	fmt.Fprintln(output, "Token usage: unavailable")
}

func writeNoPatchDiagnostics(output io.Writer, result execution.Result, rejectionReason string) {
	response, completed := result.Response()
	if !completed {
		return
	}
	fmt.Fprintln(output, "Provider execution completed; implementation did not succeed.")
	fmt.Fprintf(output, "Execution attempt: %s\n", result.AttemptId())
	writeRequestContextDiagnostics(output, result.RequestContext())
	fmt.Fprintf(output, "Provider: %s\n", response.Selection().ProviderIdentifier())
	if model, selected := response.Selection().ModelIdentifier(); selected {
		fmt.Fprintf(output, "Model: %s\n", model)
	} else {
		fmt.Fprintln(output, "Model: provider default")
	}
	fmt.Fprintf(output, "Provider outcome: %s (protocol completion only)\n", response.Outcome())
	writeProviderUsage(output, response)
	if response.Summary() != "" {
		fmt.Fprintf(output, "Provider summary (bounded, untrusted, truncated=%t): %s\n", response.SummaryTruncated(), response.Summary())
	} else {
		fmt.Fprintln(output, "Provider summary: unavailable")
	}
	fmt.Fprintln(output, "Git-visible changes: 0")
	fmt.Fprintln(output, "Implementation result: rejected; no patch was produced.")
	fmt.Fprintf(output, "Rejection reason: %s\n", rejectionReason)
}
