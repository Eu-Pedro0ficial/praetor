package aiprovider_test

import (
	"strings"
	"testing"

	"github.com/Eu-Pedro0ficial/praetor/internal/aiprovider"
)

func TestRequestContextAccountingMeasuresExactBoundedMetadata(t *testing.T) {
	intent, err := aiprovider.MeasureRequestContextComponent(
		aiprovider.RequestContextIntent,
		"TASK INTENT\nAção segura.\n\n",
		1,
		0,
		false,
	)
	if err != nil {
		t.Fatalf("MeasureRequestContextComponent() error = %v", err)
	}
	scope, err := aiprovider.MeasureRequestContextComponent(
		aiprovider.RequestContextApprovedScope,
		"AUTHORIZATION\n- service.go\n",
		1,
		2,
		true,
	)
	if err != nil {
		t.Fatalf("MeasureRequestContextComponent() error = %v", err)
	}
	components := []aiprovider.RequestContextComponent{intent, scope}
	accounting, err := aiprovider.NewRequestContextAccounting(components)
	if err != nil {
		t.Fatalf("NewRequestContextAccounting() error = %v", err)
	}
	wantBytes := len("TASK INTENT\nAção segura.\n\n") + len("AUTHORIZATION\n- service.go\n")
	wantCharacters := len([]rune("TASK INTENT\nAção segura.\n\n")) + len([]rune("AUTHORIZATION\n- service.go\n"))
	if accounting.TotalBytes() != wantBytes || accounting.TotalCharacters() != wantCharacters ||
		accounting.TotalItems() != 2 || !accounting.Truncated() {
		t.Fatalf("accounting totals = bytes:%d chars:%d items:%d truncated:%t",
			accounting.TotalBytes(), accounting.TotalCharacters(), accounting.TotalItems(), accounting.Truncated())
	}
	if intent.ByteCount() == intent.CharacterCount() {
		t.Fatalf("UTF-8 byte/character counts unexpectedly equal: %#v", intent)
	}

	returned := accounting.Components()
	returned[0] = scope
	if accounting.Components()[0].Kind() != aiprovider.RequestContextIntent {
		t.Fatal("Components() exposed mutable accounting state")
	}
}

func TestRequestContextAccountingRejectsUnboundedOrAmbiguousMetadata(t *testing.T) {
	component, err := aiprovider.MeasureRequestContextComponent(
		aiprovider.RequestContextOther,
		"other",
		0,
		0,
		false,
	)
	if err != nil {
		t.Fatalf("MeasureRequestContextComponent() error = %v", err)
	}
	if _, err := aiprovider.NewRequestContextAccounting([]aiprovider.RequestContextComponent{component, component}); err == nil ||
		!strings.Contains(err.Error(), "duplicated") {
		t.Fatalf("duplicate component error = %v", err)
	}
	tooMany := make([]aiprovider.RequestContextComponent, aiprovider.MaximumRequestContextComponents+1)
	for index := range tooMany {
		tooMany[index] = component
	}
	if _, err := aiprovider.NewRequestContextAccounting(tooMany); err == nil || !strings.Contains(err.Error(), "exceeds") {
		t.Fatalf("component bound error = %v", err)
	}
	if _, err := aiprovider.MeasureRequestContextComponent("raw_prompt", "secret", 1, 0, false); err == nil {
		t.Fatal("unregistered component category was accepted")
	}
}

func TestImplementationContextMakesBudgetOmissionsExplicit(t *testing.T) {
	context, err := aiprovider.NewImplementationContextWithDiagnostics(
		"current ImpactReport",
		[]string{"impact=EXPECTED path=service.go"},
		3,
		true,
	)
	if err != nil {
		t.Fatalf("NewImplementationContextWithDiagnostics() error = %v", err)
	}
	if !context.Truncated() || context.AvailableEntries() != 3 || context.OmittedEntries() != 2 {
		t.Fatalf("context diagnostics = available:%d omitted:%d truncated:%t",
			context.AvailableEntries(), context.OmittedEntries(), context.Truncated())
	}
	if _, err := aiprovider.NewImplementationContextWithDiagnostics(
		"current ImpactReport",
		[]string{"impact=EXPECTED path=service.go"},
		3,
		false,
	); err == nil {
		t.Fatal("inconsistent truncation metadata was accepted")
	}
}
