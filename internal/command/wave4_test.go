package command_test

import (
	"bytes"
	"context"
	"io"
	"strings"
	"testing"

	"github.com/Eu-Pedro0ficial/praetor/internal/aiprovider"
	"github.com/Eu-Pedro0ficial/praetor/internal/audit"
)

func TestWave4ProviderDiagnoseShowsEffectiveReadySelectionWithoutExecution(t *testing.T) {
	executions := 0
	provider := newCommandFakeProvider(t, func(context.Context, aiprovider.ExecutionRequest) (aiprovider.ProviderResponse, error) {
		executions++
		return aiprovider.ProviderResponse{}, nil
	})
	_, dataDirectory, session, registry := prepareM05CommandTest(t, provider)
	if _, err := registry.Dispatch(session, "provider model explicit-model", io.Discard); err != nil {
		t.Fatalf("provider model error = %v", err)
	}

	var output bytes.Buffer
	if _, err := registry.Dispatch(session, "provider diagnose", &output); err != nil {
		t.Fatalf("provider diagnose error = %v", err)
	}
	for _, expected := range []string{
		"Provider: codex-cli (OpenAI Codex CLI)",
		"Model: explicit-model (explicit; remote availability not verified)",
		"Selection source: provider=composition configuration; model=session command",
		"Adapter: registered",
		"Local availability: locally-ready (/test/bin/provider)",
		"Capability compatibility: implementation=compatible; verification-planning=compatible",
		"Authentication: not-verified; remote authentication and connectivity were not probed",
		"Readiness: locally-ready",
	} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("provider diagnose output lacks %q:\n%s", expected, output.String())
		}
	}
	if executions != 0 || provider.readinessChecks != 1 {
		t.Fatalf("diagnose executions/readiness checks = %d/%d", executions, provider.readinessChecks)
	}
	if _, exists := session.CurrentChange(); exists {
		t.Fatal("provider diagnose created or selected a Change")
	}
	if _, exists := session.CurrentProposal(); exists {
		t.Fatal("provider diagnose created a ProposalWorkspace")
	}
	for _, event := range readCommandAudit(t, dataDirectory) {
		if event.EventType == audit.EventProviderExecutionStarted ||
			event.EventType == audit.EventProviderExecutionCompleted ||
			event.EventType == audit.EventProviderExecutionFailed {
			t.Fatalf("provider diagnose created execution audit: %#v", event)
		}
	}
}

func TestWave4ProviderDiagnoseReportsUnavailableAndAuthenticationUnverified(t *testing.T) {
	executions := 0
	provider := newCommandFakeProvider(t, func(context.Context, aiprovider.ExecutionRequest) (aiprovider.ProviderResponse, error) {
		executions++
		return aiprovider.ProviderResponse{}, nil
	})
	readiness, err := aiprovider.NewLocalReadiness(
		aiprovider.ReadinessUnavailable,
		"",
		"",
		"the configured provider executable is not discoverable locally",
		"install the provider executable or configure PATH",
		"local configuration marker present",
	)
	if err != nil {
		t.Fatalf("NewLocalReadiness() error = %v", err)
	}
	provider.readiness = readiness
	_, dataDirectory, session, registry := prepareM05CommandTest(t, provider)

	var output bytes.Buffer
	if _, err := registry.Dispatch(session, "provider diagnose", &output); err != nil {
		t.Fatalf("provider diagnose error = %v", err)
	}
	for _, expected := range []string{
		"Model: provider default",
		"Local availability: unavailable",
		"Local configuration evidence: local configuration marker present",
		"Authentication: not-verified",
		"Action: install the provider executable or configure PATH",
	} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("unavailable diagnose output lacks %q:\n%s", expected, output.String())
		}
	}
	if executions != 0 {
		t.Fatal("unavailable diagnostic invoked provider execution")
	}
	for _, event := range readCommandAudit(t, dataDirectory) {
		if strings.HasPrefix(event.EventType, "PROVIDER_EXECUTION_") {
			t.Fatalf("unavailable diagnostic created execution audit: %#v", event)
		}
	}
}

func TestWave4RootStatusStaysConciseAndShowsEffectiveSelection(t *testing.T) {
	provider := newCommandFakeProvider(t, func(context.Context, aiprovider.ExecutionRequest) (aiprovider.ProviderResponse, error) {
		t.Fatal("status invoked provider execution")
		return aiprovider.ProviderResponse{}, nil
	})
	_, _, session, registry := prepareM05CommandTest(t, provider)
	if _, err := registry.Dispatch(session, "provider model status-model", io.Discard); err != nil {
		t.Fatalf("provider model error = %v", err)
	}
	var output bytes.Buffer
	if _, err := registry.Dispatch(session, "status", &output); err != nil {
		t.Fatalf("status error = %v", err)
	}
	for _, expected := range []string{
		"AI provider adapter: codex-cli",
		"AI model: status-model",
		"AI selection source: provider=composition configuration; model=session command",
		"AI provider readiness: locally-ready",
	} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("status output lacks %q:\n%s", expected, output.String())
		}
	}
	if strings.Contains(output.String(), "Local configuration evidence:") || strings.Contains(output.String(), "Local version:") {
		t.Fatalf("root status contains detailed provider diagnostics:\n%s", output.String())
	}
}
