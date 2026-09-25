package command_test

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestM12CommandsBuildPersistAndInspectExplainableImpact(t *testing.T) {
	repositoryRoot, _, session, registry := prepareCommittedCommandTest(t)
	if _, err := registry.Dispatch(session, `change new change-m12 "repository intelligence"`, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if _, err := registry.Dispatch(session, "analysis model", &output); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"RepositoryModel ID:", "Source fingerprint:", "freshness=CURRENT", "Graph:"} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("model output %q lacks %q", output.String(), expected)
		}
	}
	output.Reset()
	if _, err := registry.Dispatch(session, "analysis report change-m12 --expected internal/service/service.go --protected internal/service/service.go", &output); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"ImpactReport artifact:", "Risk:", "risk breadth", "freshness=CURRENT", "PROTECTED", "protected exposure", "why "} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("report output %q lacks %q", output.String(), expected)
		}
	}
	output.Reset()
	if _, err := registry.Dispatch(session, "change artifacts change-m12", &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "impact-report") || !strings.Contains(output.String(), "binding role=impact-report") {
		t.Fatalf("durable artifact output=%q", output.String())
	}
	if err := os.WriteFile(filepath.Join(repositoryRoot, "internal", "service", "service.go"), []byte("package service\nfunc Changed() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if _, err := registry.Dispatch(session, "analysis inspect change-m12", &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "ImpactReport artifact:") || !strings.Contains(output.String(), "freshness=STALE") || !strings.Contains(output.String(), "Report digest:") {
		t.Fatalf("stale inspection output=%q", output.String())
	}
}
