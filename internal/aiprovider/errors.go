package aiprovider

import (
	"errors"
	"fmt"
)

// FailureKind is the provider-independent failure classification recorded by
// M0.5. It does not expose provider response envelopes or raw process output.
type FailureKind string

const (
	FailureUnavailable     FailureKind = "provider-unavailable"
	FailureAuthentication  FailureKind = "authentication"
	FailureUnsupported     FailureKind = "unsupported-provider-interface"
	FailureTimeout         FailureKind = "timeout"
	FailureRateLimited     FailureKind = "rate-limited"
	FailureMalformedOutput FailureKind = "malformed-output"
	FailureProcess         FailureKind = "process-failure"
	FailureTransport       FailureKind = "transport-failure"
	FailureRefused         FailureKind = "provider-refusal"
	FailureCancelled       FailureKind = "cancelled"
	FailureExecution       FailureKind = "execution-failure"
)

// ExecutionError normalizes a failed provider attempt while retaining an
// internal cause for errors.Is/errors.As. Its message deliberately excludes
// raw provider output, which may contain source or credentials.
type ExecutionError struct {
	kind                FailureKind
	provider            ProviderIdentifier
	externalExecutionId string
	cause               error
}

// NewExecutionError constructs a safe normalized provider failure.
func NewExecutionError(
	kind FailureKind,
	provider ProviderIdentifier,
	externalExecutionId string,
	cause error,
) error {
	if !isKnownFailureKind(kind) {
		kind = FailureExecution
	}
	if provider == "" {
		provider = "unknown"
	}
	externalIdentifier, err := parseExternalExecutionIdentifier(externalExecutionId)
	if err != nil {
		externalIdentifier = ""
	}
	return &ExecutionError{
		kind:                kind,
		provider:            provider,
		externalExecutionId: externalIdentifier,
		cause:               cause,
	}
}

func (executionError *ExecutionError) Error() string {
	if executionError == nil {
		return "provider execution failed"
	}
	return fmt.Sprintf("provider %q execution failed: %s", executionError.provider, executionError.kind)
}

func (executionError *ExecutionError) Unwrap() error {
	if executionError == nil {
		return nil
	}
	return executionError.cause
}

func (executionError *ExecutionError) Kind() FailureKind {
	if executionError == nil {
		return FailureExecution
	}
	return executionError.kind
}

func (executionError *ExecutionError) ProviderIdentifier() ProviderIdentifier {
	if executionError == nil {
		return ""
	}
	return executionError.provider
}

func (executionError *ExecutionError) ExternalExecutionId() string {
	if executionError == nil {
		return ""
	}
	return executionError.externalExecutionId
}

// FailureKindOf returns the normalized kind or execution-failure for an
// application/guard error outside a provider adapter.
func FailureKindOf(err error) FailureKind {
	var executionError *ExecutionError
	if errors.As(err, &executionError) {
		return executionError.Kind()
	}
	return FailureExecution
}

func isKnownFailureKind(kind FailureKind) bool {
	switch kind {
	case FailureUnavailable,
		FailureAuthentication,
		FailureUnsupported,
		FailureTimeout,
		FailureRateLimited,
		FailureMalformedOutput,
		FailureProcess,
		FailureTransport,
		FailureRefused,
		FailureCancelled,
		FailureExecution:
		return true
	default:
		return false
	}
}
