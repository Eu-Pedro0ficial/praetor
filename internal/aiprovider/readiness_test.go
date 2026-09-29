package aiprovider_test

import (
	"context"
	"strings"
	"testing"

	"github.com/Eu-Pedro0ficial/praetor/internal/aiprovider"
)

type readinessProvider struct {
	descriptor aiprovider.ProviderDescriptor
	readiness  aiprovider.LocalReadiness
	inspected  bool
}

func (provider *readinessProvider) Descriptor() aiprovider.ProviderDescriptor {
	return provider.descriptor
}
func (*readinessProvider) AccountRequest(aiprovider.ExecutionRequest) (aiprovider.RequestContextAccounting, error) {
	return aiprovider.NewRequestContextAccounting(nil)
}
func (*readinessProvider) Execute(context.Context, aiprovider.ExecutionRequest) (aiprovider.ProviderResponse, error) {
	return aiprovider.ProviderResponse{}, nil
}
func (provider *readinessProvider) InspectReadiness(context.Context, aiprovider.ReadinessInspection) aiprovider.LocalReadiness {
	provider.inspected = true
	return provider.readiness
}

func TestReadinessRejectsUnsafeUnboundedDiagnostics(t *testing.T) {
	secret := "SECRET-VALUE"
	if _, err := aiprovider.NewLocalReadiness(
		aiprovider.ReadinessMisconfigured,
		"",
		"",
		"invalid configuration password="+secret+"\nsecond line",
		"repair configuration",
		"present",
	); err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("unsafe readiness error = %v", err)
	}
	if _, err := aiprovider.NewLocalReadiness(
		aiprovider.ReadinessMisconfigured,
		"",
		"",
		"invalid password="+secret,
		"repair configuration",
		"present",
	); err == nil || strings.Contains(err.Error(), secret) {
		t.Fatalf("secret-bearing readiness error = %v", err)
	}
	if _, err := aiprovider.NewLocalReadiness(
		aiprovider.ReadinessMisconfigured,
		"",
		"",
		strings.Repeat("x", 513),
		"repair configuration",
		"present",
	); err == nil {
		t.Fatal("unbounded readiness diagnostic was accepted")
	}
}

func TestRegistryRejectsUnsupportedCapabilityBeforeAdapterInspection(t *testing.T) {
	descriptor, err := aiprovider.NewProviderDescriptor(
		"read-only-provider",
		"Test Vendor",
		"Read Only Provider",
		[]aiprovider.ProviderCapability{
			aiprovider.CapabilityWorkspaceReadOnly,
			aiprovider.CapabilityContextCancellation,
		},
	)
	if err != nil {
		t.Fatalf("NewProviderDescriptor() error = %v", err)
	}
	provider := &readinessProvider{descriptor: descriptor, readiness: aiprovider.IndeterminateLocalReadiness()}
	registry, err := aiprovider.NewRegistry(provider)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	selection, _ := aiprovider.NewSelection("read-only-provider", "")
	inspection, _ := aiprovider.NewReadinessInspection(aiprovider.ReadinessLocalProbe, t.TempDir())
	_, readiness, err := registry.InspectReadiness(context.Background(), selection, aiprovider.ImplementationRoleContract(), inspection)
	if err != nil {
		t.Fatalf("InspectReadiness() error = %v", err)
	}
	if readiness.Disposition() != aiprovider.ReadinessUnsupported || provider.inspected {
		t.Fatalf("unsupported readiness/inspection = %#v/%t", readiness, provider.inspected)
	}
}

func TestRegistryNormalizesInvalidAdapterReadinessWithoutLeakingData(t *testing.T) {
	descriptor, err := aiprovider.NewProviderDescriptor(
		"invalid-readiness-provider",
		"Test Vendor",
		"Invalid Readiness Provider",
		[]aiprovider.ProviderCapability{
			aiprovider.CapabilityWorkspaceMutation,
			aiprovider.CapabilityContextCancellation,
		},
	)
	if err != nil {
		t.Fatalf("NewProviderDescriptor() error = %v", err)
	}
	provider := &readinessProvider{descriptor: descriptor}
	registry, err := aiprovider.NewRegistry(provider)
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	selection, _ := aiprovider.NewSelection("invalid-readiness-provider", "")
	inspection, _ := aiprovider.NewReadinessInspection(aiprovider.ReadinessLocalProbe, t.TempDir())
	_, readiness, err := registry.InspectReadiness(context.Background(), selection, aiprovider.ImplementationRoleContract(), inspection)
	if err != nil {
		t.Fatalf("InspectReadiness() error = %v", err)
	}
	if readiness.Disposition() != aiprovider.ReadinessMisconfigured ||
		!strings.Contains(readiness.Detail(), "invalid local readiness evidence") {
		t.Fatalf("normalized invalid readiness = %#v", readiness)
	}
}

func TestSelectionProvenanceDistinguishesDefaultsEnvironmentAndSession(t *testing.T) {
	provenance, err := aiprovider.NewSelectionProvenance(
		aiprovider.SelectionSourceEnvironment,
		aiprovider.SelectionSourceProviderDefault,
	)
	if err != nil {
		t.Fatalf("NewSelectionProvenance() error = %v", err)
	}
	if provenance.ProviderSource() != aiprovider.SelectionSourceEnvironment ||
		provenance.ModelSource() != aiprovider.SelectionSourceProviderDefault {
		t.Fatalf("selection provenance = %#v", provenance)
	}
	if _, err := aiprovider.NewSelectionProvenance(
		aiprovider.SelectionSourceProviderDefault,
		aiprovider.SelectionSourceSessionCommand,
	); err == nil {
		t.Fatal("provider default was accepted as provider source")
	}
}
