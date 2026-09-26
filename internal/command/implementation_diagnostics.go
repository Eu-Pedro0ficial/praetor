package command

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode/utf8"

	"github.com/Eu-Pedro0ficial/praetor/internal/aiprovider"
	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"github.com/Eu-Pedro0ficial/praetor/internal/execution"
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
	if err != nil || freshness != repositorymodel.FreshnessCurrent || report.Intent != string(current.Intent()) {
		return aiprovider.ImplementationContext{}
	}
	sourceDescription := fmt.Sprintf(
		"current ImpactReport %s digest=%s model=%s risk=%s",
		artifactId,
		report.ReportDigest,
		report.RepositoryModelId,
		report.Risk.Overall,
	)
	entries := make([]string, 0, aiprovider.MaximumImplementationContextEntries)
	totalBytes := len(sourceDescription)
	appendEntry := func(entry string) bool {
		if len(entries) == aiprovider.MaximumImplementationContextEntries ||
			totalBytes+len(entry) > aiprovider.MaximumImplementationContextBytes {
			return false
		}
		entries = append(entries, entry)
		totalBytes += len(entry)
		return true
	}
	for _, item := range report.Items {
		location := item.Element.Path
		if location == "" {
			location = item.Element.Name
		}
		if !appendEntry(boundedContextLine(fmt.Sprintf(
			"impact=%s kind=%s location=%s basis=%s confidence=%s",
			item.Classification,
			item.Element.Kind,
			location,
			item.Basis,
			item.Confidence,
		))) {
			break
		}
	}
	for _, gap := range report.KnowledgeGaps {
		if len(entries) == aiprovider.MaximumImplementationContextEntries {
			break
		}
		if !appendEntry(boundedContextLine(fmt.Sprintf(
			"knowledge-gap category=%s scope=%s consequence=%s",
			gap.Category,
			gap.Scope,
			gap.Consequence,
		))) {
			break
		}
	}
	context, err := aiprovider.NewImplementationContext(sourceDescription, entries)
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

func writeNoPatchDiagnostics(output io.Writer, result execution.Result, rejectionReason string) {
	response, completed := result.Response()
	if !completed {
		return
	}
	fmt.Fprintln(output, "Provider execution completed; implementation did not succeed.")
	fmt.Fprintf(output, "Execution attempt: %s\n", result.AttemptId())
	fmt.Fprintf(output, "Provider: %s\n", response.Selection().ProviderIdentifier())
	if model, selected := response.Selection().ModelIdentifier(); selected {
		fmt.Fprintf(output, "Model: %s\n", model)
	} else {
		fmt.Fprintln(output, "Model: provider default")
	}
	fmt.Fprintf(output, "Provider outcome: %s (protocol completion only)\n", response.Outcome())
	if response.Usage().Available() {
		fmt.Fprintf(
			output,
			"Token usage: input=%d cached_input=%d output=%d reasoning_output=%d\n",
			response.Usage().InputTokens(),
			response.Usage().CachedInputTokens(),
			response.Usage().OutputTokens(),
			response.Usage().ReasoningOutputTokens(),
		)
	} else {
		fmt.Fprintln(output, "Token usage: unavailable")
	}
	if response.Summary() != "" {
		fmt.Fprintf(output, "Provider summary (bounded, untrusted, truncated=%t): %s\n", response.SummaryTruncated(), response.Summary())
	} else {
		fmt.Fprintln(output, "Provider summary: unavailable")
	}
	fmt.Fprintln(output, "Git-visible changes: 0")
	fmt.Fprintln(output, "Implementation result: rejected; no patch was produced.")
	fmt.Fprintf(output, "Rejection reason: %s\n", rejectionReason)
}
