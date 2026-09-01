package source

import (
	"fmt"
	"sort"
	"strings"
)

// ScopeRequest is explicit developer input to deterministic M0.3 impact
// analysis. It contains no inferred semantic repository knowledge.
type ScopeRequest struct {
	Expected  []string
	Possible  []string
	Protected []string
}

// ChangeSurface distinguishes the exact file paths expected, optionally
// allowed, and protected for one Change.
type ChangeSurface struct {
	expected  []RepositoryPath
	possible  []RepositoryPath
	protected []RepositoryPath
}

// NewChangeSurface normalizes and canonicalizes an explicit surface request.
// Protected paths take precedence over expected paths, which take precedence
// over possible paths.
func NewChangeSurface(request ScopeRequest) (ChangeSurface, error) {
	expected, err := normalizeRepositoryPaths(request.Expected)
	if err != nil {
		return ChangeSurface{}, fmt.Errorf("expected surface: %w", err)
	}
	possible, err := normalizeRepositoryPaths(request.Possible)
	if err != nil {
		return ChangeSurface{}, fmt.Errorf("possible surface: %w", err)
	}
	protected, err := normalizeRepositoryPaths(request.Protected)
	if err != nil {
		return ChangeSurface{}, fmt.Errorf("protected surface: %w", err)
	}

	protectedSet := repositoryPathSet(protected)
	expected = filterRepositoryPaths(expected, func(repositoryPath RepositoryPath) bool {
		_, isProtected := protectedSet[repositoryPath]
		return !isProtected
	})
	expectedSet := repositoryPathSet(expected)
	possible = filterRepositoryPaths(possible, func(repositoryPath RepositoryPath) bool {
		_, isProtected := protectedSet[repositoryPath]
		_, isExpected := expectedSet[repositoryPath]
		return !isProtected && !isExpected
	})
	if len(expected) == 0 && len(possible) == 0 {
		return ChangeSurface{}, fmt.Errorf("Change Surface must authorize at least one expected or possible path")
	}

	return ChangeSurface{
		expected:  expected,
		possible:  possible,
		protected: protected,
	}, nil
}

// ExpectedPaths returns the paths strongly expected to change.
func (surface ChangeSurface) ExpectedPaths() []RepositoryPath {
	return copyRepositoryPaths(surface.expected)
}

// PossiblePaths returns paths authorized only if implementation needs them.
func (surface ChangeSurface) PossiblePaths() []RepositoryPath {
	return copyRepositoryPaths(surface.possible)
}

// ProtectedPaths returns paths which always reject the actual surface.
func (surface ChangeSurface) ProtectedPaths() []RepositoryPath {
	return copyRepositoryPaths(surface.protected)
}

func repositoryPathSet(paths []RepositoryPath) map[RepositoryPath]struct{} {
	set := make(map[RepositoryPath]struct{}, len(paths))
	for _, repositoryPath := range paths {
		set[repositoryPath] = struct{}{}
	}
	return set
}

func filterRepositoryPaths(paths []RepositoryPath, keep func(RepositoryPath) bool) []RepositoryPath {
	filtered := make([]RepositoryPath, 0, len(paths))
	for _, repositoryPath := range paths {
		if keep(repositoryPath) {
			filtered = append(filtered, repositoryPath)
		}
	}
	return filtered
}

// ViolationKind distinguishes protected, unexpected, and malformed actual
// source-path failures.
type ViolationKind string

const (
	ViolationProtected  ViolationKind = "protected"
	ViolationUnexpected ViolationKind = "unexpected"
	ViolationInvalid    ViolationKind = "invalid-path"
)

// SurfaceViolation is one inspectable reason an actual surface was rejected.
type SurfaceViolation struct {
	kind           ViolationKind
	repositoryPath RepositoryPath
	suppliedPath   string
	reason         string
}

// Kind returns the violation classification.
func (violation SurfaceViolation) Kind() ViolationKind {
	return violation.kind
}

// Path returns the canonical path when available, otherwise the rejected raw
// path input.
func (violation SurfaceViolation) Path() string {
	if violation.repositoryPath != "" {
		return string(violation.repositoryPath)
	}
	return violation.suppliedPath
}

// Reason returns an actionable rejection explanation.
func (violation SurfaceViolation) Reason() string {
	return violation.reason
}

// SurfaceValidationResult classifies one complete actual source surface.
type SurfaceValidationResult struct {
	allowed         bool
	suppliedPaths   []string
	actualPaths     []RepositoryPath
	expectedChanges []RepositoryPath
	possibleChanges []RepositoryPath
	violations      []SurfaceViolation
}

// Allowed reports whether every actual path is inside the approved boundary.
func (result SurfaceValidationResult) Allowed() bool {
	return result.allowed
}

// SuppliedPaths returns the original actual-surface inputs.
func (result SurfaceValidationResult) SuppliedPaths() []string {
	return append([]string(nil), result.suppliedPaths...)
}

// ActualPaths returns all successfully normalized actual paths.
func (result SurfaceValidationResult) ActualPaths() []RepositoryPath {
	return copyRepositoryPaths(result.actualPaths)
}

// ExpectedChanges returns actual paths classified as expected.
func (result SurfaceValidationResult) ExpectedChanges() []RepositoryPath {
	return copyRepositoryPaths(result.expectedChanges)
}

// PossibleChanges returns actual paths classified as possible.
func (result SurfaceValidationResult) PossibleChanges() []RepositoryPath {
	return copyRepositoryPaths(result.possibleChanges)
}

// Violations returns a defensive copy of all failures.
func (result SurfaceValidationResult) Violations() []SurfaceViolation {
	return append([]SurfaceViolation(nil), result.violations...)
}

// SurfaceValidationError is the deterministic rejection returned when any
// actual path violates the approved surface.
type SurfaceValidationError struct {
	violations []SurfaceViolation
}

func (validationError *SurfaceValidationError) Error() string {
	if validationError == nil || len(validationError.violations) == 0 {
		return "Change Surface validation rejected"
	}
	details := make([]string, 0, len(validationError.violations))
	for _, violation := range validationError.violations {
		details = append(details, fmt.Sprintf("%s path %q", violation.Kind(), violation.Path()))
	}
	return "Change Surface validation rejected: " + strings.Join(details, "; ")
}

// Violations returns a defensive copy of the rejection details.
func (validationError *SurfaceValidationError) Violations() []SurfaceViolation {
	if validationError == nil {
		return nil
	}
	return append([]SurfaceViolation(nil), validationError.violations...)
}

func validateActualSurface(surface ChangeSurface, values []string) (SurfaceValidationResult, error) {
	result := SurfaceValidationResult{
		suppliedPaths: append([]string(nil), values...),
	}
	actualSet := make(map[RepositoryPath]struct{}, len(values))
	invalidReasons := make(map[string]string)
	for _, value := range values {
		repositoryPath, err := NormalizeRepositoryPath(value)
		if err != nil {
			invalidReasons[value] = err.Error()
			continue
		}
		actualSet[repositoryPath] = struct{}{}
	}
	for repositoryPath := range actualSet {
		result.actualPaths = append(result.actualPaths, repositoryPath)
	}
	sort.Slice(result.actualPaths, func(left int, right int) bool {
		return result.actualPaths[left] < result.actualPaths[right]
	})

	for _, repositoryPath := range result.actualPaths {
		switch {
		case containsRepositoryPath(surface.protected, repositoryPath):
			result.violations = append(result.violations, SurfaceViolation{
				kind:           ViolationProtected,
				repositoryPath: repositoryPath,
				reason:         "path is protected by the approved Change Surface",
			})
		case containsRepositoryPath(surface.expected, repositoryPath):
			result.expectedChanges = append(result.expectedChanges, repositoryPath)
		case containsRepositoryPath(surface.possible, repositoryPath):
			result.possibleChanges = append(result.possibleChanges, repositoryPath)
		default:
			result.violations = append(result.violations, SurfaceViolation{
				kind:           ViolationUnexpected,
				repositoryPath: repositoryPath,
				reason:         "path is outside the approved Change Surface",
			})
		}
	}
	invalidPaths := make([]string, 0, len(invalidReasons))
	for suppliedPath := range invalidReasons {
		invalidPaths = append(invalidPaths, suppliedPath)
	}
	sort.Strings(invalidPaths)
	for _, suppliedPath := range invalidPaths {
		result.violations = append(result.violations, SurfaceViolation{
			kind:         ViolationInvalid,
			suppliedPath: suppliedPath,
			reason:       invalidReasons[suppliedPath],
		})
	}

	result.allowed = len(result.violations) == 0
	if !result.allowed {
		return result, &SurfaceValidationError{
			violations: append([]SurfaceViolation(nil), result.violations...),
		}
	}
	return result, nil
}
