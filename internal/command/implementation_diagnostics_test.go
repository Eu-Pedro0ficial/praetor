package command

import (
	"strconv"
	"testing"
	"time"

	"github.com/Eu-Pedro0ficial/praetor/internal/artifact"
	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"github.com/Eu-Pedro0ficial/praetor/internal/impact"
	"github.com/Eu-Pedro0ficial/praetor/internal/project"
	"github.com/Eu-Pedro0ficial/praetor/internal/repositorymodel"
)

func TestImplementationContextExcludesStaleOrIntentMismatchedImpactReports(t *testing.T) {
	current, err := change.New(
		"change-context-filter",
		project.ProjectId("01890c29-7a78-7abc-8def-0123456789ab"),
		"current intent",
		time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("change.New() error = %v", err)
	}
	report := impact.Report{
		Intent:            "current intent",
		ReportDigest:      "sha256:report",
		RepositoryModelId: "model-current",
		Risk:              impact.RiskProfile{Overall: impact.RiskLow},
		Items: []impact.Item{{
			Element:        impact.Element{Kind: repositorymodel.NodeFile, Path: "service.go"},
			Classification: impact.Expected,
			Basis:          repositorymodel.EvidenceObserved,
			Confidence:     repositorymodel.ConfidenceHigh,
		}},
	}
	artifactId := artifact.ArtifactId("art-0123456789abcdef0123456789abcdef")

	if context := implementationContextFromImpactReport(
		current,
		report,
		artifactId,
		repositorymodel.FreshnessStale,
	); context.Available() {
		t.Fatalf("stale ImpactReport entered provider context: %#v", context)
	}
	report.Intent = "superseded intent"
	if context := implementationContextFromImpactReport(
		current,
		report,
		artifactId,
		repositorymodel.FreshnessCurrent,
	); context.Available() {
		t.Fatalf("intent-mismatched ImpactReport entered provider context: %#v", context)
	}
}

func TestImplementationContextBudgetReportsEveryOmittedImpactEntry(t *testing.T) {
	current, err := change.New(
		"change-context-budget",
		project.ProjectId("01890c29-7a78-7abc-8def-0123456789ab"),
		"bounded impact context",
		time.Date(2026, time.September, 1, 12, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("change.New() error = %v", err)
	}
	report := impact.Report{
		Intent:            string(current.Intent()),
		ReportDigest:      "sha256:report",
		RepositoryModelId: "model-current",
		Risk:              impact.RiskProfile{Overall: impact.RiskModerate},
	}
	for index := 0; index < 70; index++ {
		report.Items = append(report.Items, impact.Item{
			Element: impact.Element{
				Kind: repositorymodel.NodeFile,
				Path: "internal/service/file-" + strconv.Itoa(index) + ".go",
			},
			Classification: impact.Possible,
			Basis:          repositorymodel.EvidenceDerived,
			Confidence:     repositorymodel.ConfidenceMedium,
		})
	}

	context := implementationContextFromImpactReport(
		current,
		report,
		artifact.ArtifactId("art-0123456789abcdef0123456789abcdef"),
		repositorymodel.FreshnessCurrent,
	)
	if !context.Truncated() ||
		len(context.Entries()) != 64 ||
		context.AvailableEntries() != 70 ||
		context.OmittedEntries() != 6 {
		t.Fatalf("bounded context = entries:%d available:%d omitted:%d truncated:%t",
			len(context.Entries()), context.AvailableEntries(), context.OmittedEntries(), context.Truncated())
	}
}
