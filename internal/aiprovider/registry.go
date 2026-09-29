package aiprovider

import (
	"context"
	"fmt"
	"sort"
)

// Registry contains only providers explicitly supplied by the composition
// root. It performs exact manual/configuration lookup, not routing.
type Registry struct {
	providers map[ProviderIdentifier]Provider
}

// NewRegistry validates explicit compile-time provider registration.
func NewRegistry(providers ...Provider) (*Registry, error) {
	if len(providers) == 0 {
		return nil, fmt.Errorf("at least one AI provider adapter must be registered")
	}
	registry := &Registry{providers: make(map[ProviderIdentifier]Provider, len(providers))}
	for _, provider := range providers {
		if provider == nil {
			return nil, fmt.Errorf("nil AI provider adapter cannot be registered")
		}
		descriptor := provider.Descriptor()
		if descriptor.Identifier() == "" || descriptor.Vendor() == "" || descriptor.DisplayName() == "" {
			return nil, fmt.Errorf("AI provider adapter has invalid descriptor")
		}
		if _, duplicate := registry.providers[descriptor.Identifier()]; duplicate {
			return nil, fmt.Errorf("AI provider %q is registered more than once", descriptor.Identifier())
		}
		registry.providers[descriptor.Identifier()] = provider
	}
	return registry, nil
}

// Select validates an explicit provider/model choice against registered
// adapters. It never chooses a provider on the caller's behalf.
func (registry *Registry) Select(providerValue string, modelValue string) (Selection, error) {
	if registry == nil {
		return Selection{}, fmt.Errorf("AI provider registry is required")
	}
	selection, err := NewSelection(providerValue, modelValue)
	if err != nil {
		return Selection{}, err
	}
	if _, exists := registry.providers[selection.ProviderIdentifier()]; !exists {
		return Selection{}, fmt.Errorf("AI provider %q is not registered; use provider list to inspect available adapters", selection.ProviderIdentifier())
	}
	return selection, nil
}

// Resolve returns the exactly selected adapter without fallback or ranking.
func (registry *Registry) Resolve(selection Selection) (Provider, error) {
	if registry == nil {
		return nil, fmt.Errorf("AI provider registry is required")
	}
	provider, exists := registry.providers[selection.ProviderIdentifier()]
	if !exists {
		return nil, fmt.Errorf("AI provider %q is not registered", selection.ProviderIdentifier())
	}
	return provider, nil
}

// InspectReadiness resolves the exact selected adapter, proves role
// compatibility, and requests local-only readiness evidence when supported.
// It never routes, invokes provider work, or creates execution history.
func (registry *Registry) InspectReadiness(
	ctx context.Context,
	selection Selection,
	contract ProviderRoleContract,
	inspection ReadinessInspection,
) (ProviderDescriptor, LocalReadiness, error) {
	provider, err := registry.Resolve(selection)
	if err != nil {
		return ProviderDescriptor{}, LocalReadiness{}, err
	}
	descriptor := provider.Descriptor()
	if err := ValidateRole(descriptor, contract); err != nil {
		readiness, readinessError := NewLocalReadiness(
			ReadinessUnsupported,
			"",
			"",
			"the selected adapter lacks a capability required by the requested role",
			"select a compatible registered provider",
			"not inspected",
		)
		if readinessError != nil {
			return ProviderDescriptor{}, LocalReadiness{}, readinessError
		}
		return descriptor, readiness, nil
	}
	inspector, supported := provider.(ReadinessInspector)
	if !supported {
		return descriptor, IndeterminateLocalReadiness(), nil
	}
	readiness := inspector.InspectReadiness(ctx, inspection)
	if err := readiness.validate(); err != nil {
		readiness, _ = NewLocalReadiness(
			ReadinessMisconfigured,
			"",
			"",
			"the selected adapter returned invalid local readiness evidence",
			"review the adapter configuration and run provider diagnose again",
			"not retained",
		)
	}
	return descriptor, readiness, nil
}

// Descriptors returns deterministic provider-independent discovery metadata.
func (registry *Registry) Descriptors() []ProviderDescriptor {
	if registry == nil {
		return nil
	}
	descriptors := make([]ProviderDescriptor, 0, len(registry.providers))
	for _, provider := range registry.providers {
		descriptors = append(descriptors, provider.Descriptor())
	}
	sort.Slice(descriptors, func(left int, right int) bool {
		return descriptors[left].Identifier() < descriptors[right].Identifier()
	})
	return descriptors
}

// ValidateRole proves the manually selected adapter satisfies the requested
// role contract. It does not search for an alternative provider.
func ValidateRole(descriptor ProviderDescriptor, contract ProviderRoleContract) error {
	if err := contract.validate(); err != nil {
		return err
	}
	for _, required := range contract.RequiredCapabilities() {
		if !descriptor.Supports(required) {
			return fmt.Errorf(
				"selected AI provider %q lacks required capability %q",
				descriptor.Identifier(),
				required,
			)
		}
	}
	return nil
}
