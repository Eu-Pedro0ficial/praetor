package aiprovider

import (
	"context"
	"fmt"
	"strings"
	"unicode"
)

// ReadinessInspectionDepth separates cheap executable discovery from the
// bounded local interface probes used before provider execution.
type ReadinessInspectionDepth string

const (
	ReadinessDiscovery  ReadinessInspectionDepth = "discovery"
	ReadinessLocalProbe ReadinessInspectionDepth = "local-probe"
)

// ReadinessDisposition describes only locally provable adapter readiness.
// It deliberately makes no claim about remote authentication or connectivity.
type ReadinessDisposition string

const (
	ReadinessLocallyAvailable ReadinessDisposition = "locally-available"
	ReadinessLocallyReady     ReadinessDisposition = "locally-ready"
	ReadinessUnavailable      ReadinessDisposition = "unavailable"
	ReadinessMisconfigured    ReadinessDisposition = "misconfigured"
	ReadinessUnsupported      ReadinessDisposition = "unsupported"
	ReadinessIndeterminate    ReadinessDisposition = "indeterminate"
)

// AuthenticationDisposition is intentionally conservative. Core V0 does not
// inspect provider credentials or perform a remote authentication probe.
type AuthenticationDisposition string

const AuthenticationNotVerified AuthenticationDisposition = "not-verified"

// ReadinessInspection is a provider-independent request for local-only
// inspection. Implementations must not submit prompts or contact a provider.
type ReadinessInspection struct {
	depth            ReadinessInspectionDepth
	workingDirectory string
}

func NewReadinessInspection(depth ReadinessInspectionDepth, workingDirectory string) (ReadinessInspection, error) {
	if depth != ReadinessDiscovery && depth != ReadinessLocalProbe {
		return ReadinessInspection{}, fmt.Errorf("unknown provider readiness inspection depth %q", depth)
	}
	workingDirectory = strings.TrimSpace(workingDirectory)
	if workingDirectory == "" || strings.ContainsRune(workingDirectory, '\x00') {
		return ReadinessInspection{}, fmt.Errorf("provider readiness working directory is required")
	}
	return ReadinessInspection{depth: depth, workingDirectory: workingDirectory}, nil
}

func (inspection ReadinessInspection) Depth() ReadinessInspectionDepth { return inspection.depth }
func (inspection ReadinessInspection) WorkingDirectory() string        { return inspection.workingDirectory }

// LocalReadiness is bounded adapter-owned evidence. Detail and action are
// intentionally short, single-line, and must never contain credential data.
type LocalReadiness struct {
	disposition           ReadinessDisposition
	executable            string
	version               string
	detail                string
	action                string
	authentication        AuthenticationDisposition
	configurationEvidence string
}

func NewLocalReadiness(
	disposition ReadinessDisposition,
	executable string,
	version string,
	detail string,
	action string,
	configurationEvidence string,
) (LocalReadiness, error) {
	if !knownReadinessDisposition(disposition) {
		return LocalReadiness{}, fmt.Errorf("unknown provider readiness disposition %q", disposition)
	}
	values := []struct {
		name    string
		value   string
		maximum int
	}{
		{"executable", executable, 4096},
		{"version", version, 256},
		{"detail", detail, 512},
		{"action", action, 512},
		{"configuration evidence", configurationEvidence, 256},
	}
	for _, value := range values {
		if len(value.value) > value.maximum ||
			containsUnsafeDiagnosticCharacter(value.value) ||
			containsSensitiveDiagnostic(value.value) {
			return LocalReadiness{}, fmt.Errorf("provider readiness %s is invalid", value.name)
		}
	}
	if strings.TrimSpace(detail) == "" || strings.TrimSpace(action) == "" {
		return LocalReadiness{}, fmt.Errorf("provider readiness detail and action are required")
	}
	return LocalReadiness{
		disposition:           disposition,
		executable:            executable,
		version:               version,
		detail:                detail,
		action:                action,
		authentication:        AuthenticationNotVerified,
		configurationEvidence: configurationEvidence,
	}, nil
}

func IndeterminateLocalReadiness() LocalReadiness {
	readiness, _ := NewLocalReadiness(
		ReadinessIndeterminate,
		"",
		"",
		"the selected adapter does not expose local readiness inspection",
		"review the adapter documentation before provider execution",
		"not inspected",
	)
	return readiness
}

func (readiness LocalReadiness) Disposition() ReadinessDisposition { return readiness.disposition }
func (readiness LocalReadiness) Executable() string                { return readiness.executable }
func (readiness LocalReadiness) Version() string                   { return readiness.version }
func (readiness LocalReadiness) Detail() string                    { return readiness.detail }
func (readiness LocalReadiness) Action() string                    { return readiness.action }
func (readiness LocalReadiness) Authentication() AuthenticationDisposition {
	return readiness.authentication
}
func (readiness LocalReadiness) ConfigurationEvidence() string {
	return readiness.configurationEvidence
}

func (readiness LocalReadiness) validate() error {
	validated, err := NewLocalReadiness(
		readiness.disposition,
		readiness.executable,
		readiness.version,
		readiness.detail,
		readiness.action,
		readiness.configurationEvidence,
	)
	if err != nil || readiness.authentication != AuthenticationNotVerified ||
		validated.disposition != readiness.disposition {
		return fmt.Errorf("provider returned invalid local readiness evidence")
	}
	return nil
}

// BlocksExecution reports a locally proven setup failure. Indeterminate
// adapters retain backwards-compatible execution behavior and fail through
// their normal provider boundary if necessary.
func (readiness LocalReadiness) BlocksExecution() bool {
	switch readiness.disposition {
	case ReadinessUnavailable, ReadinessMisconfigured, ReadinessUnsupported:
		return true
	default:
		return false
	}
}

// ReadinessError classifies a locally proven pre-invocation setup failure.
// Its message contains only validated readiness metadata.
type ReadinessError struct {
	provider  ProviderIdentifier
	readiness LocalReadiness
}

func NewReadinessError(provider ProviderIdentifier, readiness LocalReadiness) error {
	return &ReadinessError{provider: provider, readiness: readiness}
}

func (readinessError *ReadinessError) Error() string {
	if readinessError == nil {
		return "AI provider readiness check failed"
	}
	return fmt.Sprintf(
		"AI provider %q readiness check failed: %s; %s",
		readinessError.provider,
		readinessError.readiness.Disposition(),
		readinessError.readiness.Action(),
	)
}

func (readinessError *ReadinessError) ProviderIdentifier() ProviderIdentifier {
	if readinessError == nil {
		return ""
	}
	return readinessError.provider
}

func (readinessError *ReadinessError) Readiness() LocalReadiness {
	if readinessError == nil {
		return LocalReadiness{}
	}
	return readinessError.readiness
}

// ReadinessInspector is an optional provider port for local-only checks.
// Implementations must not submit prompts, contact remote services, mutate
// credentials, or execute provider work.
type ReadinessInspector interface {
	InspectReadiness(context.Context, ReadinessInspection) LocalReadiness
}

func knownReadinessDisposition(disposition ReadinessDisposition) bool {
	switch disposition {
	case ReadinessLocallyAvailable, ReadinessLocallyReady, ReadinessUnavailable,
		ReadinessMisconfigured, ReadinessUnsupported, ReadinessIndeterminate:
		return true
	default:
		return false
	}
}

func containsUnsafeDiagnosticCharacter(value string) bool {
	for _, character := range value {
		if character == '\n' || character == '\r' || character == '\x00' || unicode.IsControl(character) {
			return true
		}
	}
	return false
}

// SelectionSource identifies the effective process-local origin of provider
// and model selection without introducing persistent provider configuration.
type SelectionSource string

const (
	SelectionSourceBuiltIn         SelectionSource = "built-in default"
	SelectionSourceEnvironment     SelectionSource = "environment"
	SelectionSourceComposition     SelectionSource = "composition configuration"
	SelectionSourceSessionCommand  SelectionSource = "session command"
	SelectionSourceProviderDefault SelectionSource = "provider default"
)

// SelectionProvenance retains the effective source of each selection value.
type SelectionProvenance struct {
	provider SelectionSource
	model    SelectionSource
}

func NewSelectionProvenance(provider, model SelectionSource) (SelectionProvenance, error) {
	if !knownSelectionSource(provider) || provider == SelectionSourceProviderDefault {
		return SelectionProvenance{}, fmt.Errorf("provider selection source is invalid")
	}
	if !knownSelectionSource(model) {
		return SelectionProvenance{}, fmt.Errorf("model selection source is invalid")
	}
	return SelectionProvenance{provider: provider, model: model}, nil
}

func (provenance SelectionProvenance) ProviderSource() SelectionSource { return provenance.provider }
func (provenance SelectionProvenance) ModelSource() SelectionSource    { return provenance.model }

func knownSelectionSource(source SelectionSource) bool {
	switch source {
	case SelectionSourceBuiltIn, SelectionSourceEnvironment,
		SelectionSourceComposition, SelectionSourceSessionCommand,
		SelectionSourceProviderDefault:
		return true
	default:
		return false
	}
}

func containsSensitiveDiagnostic(value string) bool {
	lower := strings.ToLower(value)
	for _, marker := range []string{
		"authorization:",
		"bearer ",
		"api_key=",
		"api-key=",
		"access_token=",
		"access-token=",
		"password=",
		"password:",
		"secret=",
		"secret:",
		"token=",
		"token:",
	} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}
