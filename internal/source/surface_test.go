package source_test

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"github.com/Eu-Pedro0ficial/praetor/internal/project"
	"github.com/Eu-Pedro0ficial/praetor/internal/source"
)

func TestNormalizeRepositoryPathRejectsUnsafeAndAmbiguousInputs(t *testing.T) {
	tests := []struct {
		name       string
		value      string
		want       source.RepositoryPath
		shouldFail bool
	}{
		{name: "plain", value: "internal/service/service.go", want: "internal/service/service.go"},
		{name: "canonicalizes dot and repeated separator", value: "./internal//service/service.go", want: "internal/service/service.go"},
		{name: "canonicalizes trailing separator", value: "internal/service/", want: "internal/service"},
		{name: "empty", shouldFail: true},
		{name: "whitespace", value: " ", shouldFail: true},
		{name: "surrounding whitespace", value: " go.mod", shouldFail: true},
		{name: "absolute", value: "/etc/passwd", shouldFail: true},
		{name: "Windows drive", value: "C:/Windows/system.ini", shouldFail: true},
		{name: "backslash", value: `internal\service\service.go`, shouldFail: true},
		{name: "parent traversal", value: "../go.mod", shouldFail: true},
		{name: "embedded traversal", value: "internal/../go.mod", shouldFail: true},
		{name: "control character", value: "internal\nservice.go", shouldFail: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			normalized, err := source.NormalizeRepositoryPath(test.value)
			if test.shouldFail {
				if err == nil {
					t.Fatalf("NormalizeRepositoryPath(%q) = %q, want error", test.value, normalized)
				}
				return
			}
			if err != nil || normalized != test.want {
				t.Fatalf("NormalizeRepositoryPath(%q) = %q, %v; want %q", test.value, normalized, err, test.want)
			}
		})
	}
}

func TestNewChangeSurfaceAppliesDeterministicCategoryPrecedence(t *testing.T) {
	surface, err := source.NewChangeSurface(source.ScopeRequest{
		Expected:  []string{"internal/service/service.go", "go.mod", "internal/service/service.go"},
		Possible:  []string{"README.md", "internal/service/service_test.go", "internal/service/service.go"},
		Protected: []string{"go.mod", "README.md", "go.mod"},
	})
	if err != nil {
		t.Fatalf("NewChangeSurface() error = %v", err)
	}
	assertRepositoryPaths(t, surface.ExpectedPaths(), []source.RepositoryPath{"internal/service/service.go"})
	assertRepositoryPaths(t, surface.PossiblePaths(), []source.RepositoryPath{"internal/service/service_test.go"})
	assertRepositoryPaths(t, surface.ProtectedPaths(), []source.RepositoryPath{"README.md", "go.mod"})

	returned := surface.ProtectedPaths()
	returned[0] = "mutated"
	assertRepositoryPaths(t, surface.ProtectedPaths(), []source.RepositoryPath{"README.md", "go.mod"})
}

func TestNewChangeSurfaceRequiresAuthorizedPath(t *testing.T) {
	if _, err := source.NewChangeSurface(source.ScopeRequest{
		Expected:  []string{"go.mod"},
		Protected: []string{"go.mod"},
	}); err == nil {
		t.Fatal("NewChangeSurface() accepted a surface with no authorized path")
	}
}

func TestRepositoryWideSurfaceAuthorizesDiscoveryAndProtectsSubtrees(t *testing.T) {
	currentChange := newSourceTestChange(t, sourceProjectId)
	snapshot := newSourceTestSnapshot(t, sourceProjectId)
	analysis, err := source.AnalyzeImpact(currentChange, snapshot, source.ScopeRequest{
		AuthorizationMode: source.AuthorizationRepositoryWide,
		Protected:         []string{"internal/service"},
	})
	if err != nil {
		t.Fatalf("AnalyzeImpact() repository-wide error = %v", err)
	}
	approvedScope, err := source.EstablishApprovedScope(analysis)
	if err != nil {
		t.Fatalf("EstablishApprovedScope() error = %v", err)
	}
	surface := approvedScope.Surface()
	if surface.AuthorizationMode() != source.AuthorizationRepositoryWide ||
		len(surface.ExpectedPaths()) != 0 || len(surface.PossiblePaths()) != 0 {
		t.Fatalf("repository-wide surface = mode %q expected %#v possible %#v", surface.AuthorizationMode(), surface.ExpectedPaths(), surface.PossiblePaths())
	}

	allowed, err := approvedScope.ValidateActualSurface([]string{"README.md"})
	if err != nil || !allowed.Allowed() {
		t.Fatalf("repository-wide discovered path = %#v, error = %v", allowed, err)
	}
	assertRepositoryPaths(t, allowed.PossibleChanges(), []source.RepositoryPath{"README.md"})

	rejected, err := approvedScope.ValidateActualSurface([]string{"internal/service/service.go"})
	var validationError *source.SurfaceValidationError
	if !errors.As(err, &validationError) || rejected.Allowed() {
		t.Fatalf("protected subtree validation = %#v, error = %v", rejected, err)
	}
	if violations := rejected.Violations(); len(violations) != 1 ||
		violations[0].Kind() != source.ViolationProtected ||
		violations[0].Path() != "internal/service/service.go" {
		t.Fatalf("protected subtree violations = %#v", violations)
	}

	outside, err := approvedScope.ValidateActualSurface([]string{"/tmp/outside-repository.go", "../outside-repository.go"})
	if err == nil || outside.Allowed() {
		t.Fatalf("repository boundary validation = %#v, error = %v", outside, err)
	}
	if violations := outside.Violations(); len(violations) != 2 ||
		violations[0].Kind() != source.ViolationInvalid ||
		violations[1].Kind() != source.ViolationInvalid {
		t.Fatalf("repository boundary violations = %#v", violations)
	}
}

func TestChangeSurfaceAuthorizationModesRemainUnambiguous(t *testing.T) {
	if _, err := source.NewChangeSurface(source.ScopeRequest{}); err == nil {
		t.Fatal("zero-value historical explicit scope became repository-wide")
	}
	if _, err := source.NewChangeSurface(source.ScopeRequest{
		AuthorizationMode: source.AuthorizationRepositoryWide,
		Expected:          []string{"README.md"},
	}); err == nil || !strings.Contains(err.Error(), "cannot be combined") {
		t.Fatalf("mixed repository-wide and explicit scope error = %v", err)
	}
	if _, err := source.NewChangeSurface(source.ScopeRequest{
		AuthorizationMode: "unknown",
	}); err == nil || !strings.Contains(err.Error(), "unsupported") {
		t.Fatalf("unknown authorization mode error = %v", err)
	}
}

func TestImpactAnalysisLinksChangeSnapshotAndTrackedCandidateSurface(t *testing.T) {
	currentChange := newSourceTestChange(t, sourceProjectId)
	snapshot := newSourceTestSnapshot(t, sourceProjectId)
	analysis, err := source.AnalyzeImpact(currentChange, snapshot, standardScopeRequest())
	if err != nil {
		t.Fatalf("AnalyzeImpact() error = %v", err)
	}
	if analysis.ChangeId() != currentChange.ChangeId() || analysis.ProjectId() != sourceProjectId {
		t.Fatalf("ImpactAnalysis identity = Change %q, Project %q", analysis.ChangeId(), analysis.ProjectId())
	}
	if analysis.SourceStateDigest() != sourceDigest {
		t.Fatalf("ImpactAnalysis digest = %q", analysis.SourceStateDigest())
	}
	approvedScope, err := source.EstablishApprovedScope(analysis)
	if err != nil {
		t.Fatalf("EstablishApprovedScope() error = %v", err)
	}
	if approvedScope.ChangeId() != analysis.ChangeId() || approvedScope.ProjectId() != analysis.ProjectId() {
		t.Fatalf("ApprovedScope identity = Change %q, Project %q", approvedScope.ChangeId(), approvedScope.ProjectId())
	}
	if approvedScope.SourceStateDigest() != sourceDigest {
		t.Fatalf("ApprovedScope digest = %q", approvedScope.SourceStateDigest())
	}
}

func TestImpactAnalysisRejectsUntrackedAndMismatchedInputs(t *testing.T) {
	currentChange := newSourceTestChange(t, sourceProjectId)
	snapshot := newSourceTestSnapshot(t, sourceProjectId)
	if _, err := source.AnalyzeImpact(currentChange, snapshot, source.ScopeRequest{
		Expected: []string{"missing.go"},
	}); err == nil || !strings.Contains(err.Error(), "not present") {
		t.Fatalf("AnalyzeImpact() untracked path error = %v", err)
	}

	otherProjectId := project.ProjectId("01890c29-7a78-7abc-9def-0123456789ab")
	mismatchedSnapshot := newSourceTestSnapshot(t, otherProjectId)
	if _, err := source.AnalyzeImpact(currentChange, mismatchedSnapshot, standardScopeRequest()); err == nil || !strings.Contains(err.Error(), "does not match") {
		t.Fatalf("AnalyzeImpact() ProjectId mismatch error = %v", err)
	}
}

func TestApprovedScopeValidatesCompleteActualSurfaceFailClosed(t *testing.T) {
	approvedScope := newApprovedScope(t)
	tests := []struct {
		name             string
		actual           []string
		allowed          bool
		expectedChanges  []source.RepositoryPath
		possibleChanges  []source.RepositoryPath
		violationKinds   []source.ViolationKind
		violationPaths   []string
		normalizedActual []source.RepositoryPath
	}{
		{
			name:             "expected path",
			actual:           []string{"internal/service/service.go"},
			allowed:          true,
			expectedChanges:  []source.RepositoryPath{"internal/service/service.go"},
			normalizedActual: []source.RepositoryPath{"internal/service/service.go"},
		},
		{
			name:             "possible path",
			actual:           []string{"internal/service/service_test.go"},
			allowed:          true,
			possibleChanges:  []source.RepositoryPath{"internal/service/service_test.go"},
			normalizedActual: []source.RepositoryPath{"internal/service/service_test.go"},
		},
		{
			name:             "expected and possible",
			actual:           []string{"internal/service/service_test.go", "internal/service/service.go"},
			allowed:          true,
			expectedChanges:  []source.RepositoryPath{"internal/service/service.go"},
			possibleChanges:  []source.RepositoryPath{"internal/service/service_test.go"},
			normalizedActual: []source.RepositoryPath{"internal/service/service.go", "internal/service/service_test.go"},
		},
		{
			name:             "protected path",
			actual:           []string{"go.mod"},
			violationKinds:   []source.ViolationKind{source.ViolationProtected},
			violationPaths:   []string{"go.mod"},
			normalizedActual: []source.RepositoryPath{"go.mod"},
		},
		{
			name:             "unexpected path",
			actual:           []string{"README.md"},
			violationKinds:   []source.ViolationKind{source.ViolationUnexpected},
			violationPaths:   []string{"README.md"},
			normalizedActual: []source.RepositoryPath{"README.md"},
		},
		{
			name:             "mixed valid and protected fails entire surface",
			actual:           []string{"internal/service/service.go", "go.mod"},
			expectedChanges:  []source.RepositoryPath{"internal/service/service.go"},
			violationKinds:   []source.ViolationKind{source.ViolationProtected},
			violationPaths:   []string{"go.mod"},
			normalizedActual: []source.RepositoryPath{"go.mod", "internal/service/service.go"},
		},
		{
			name:           "traversal rejected",
			actual:         []string{"internal/../go.mod"},
			violationKinds: []source.ViolationKind{source.ViolationInvalid},
			violationPaths: []string{"internal/../go.mod"},
		},
		{
			name:           "absolute rejected",
			actual:         []string{"/tmp/repository/internal/service/service.go"},
			violationKinds: []source.ViolationKind{source.ViolationInvalid},
			violationPaths: []string{"/tmp/repository/internal/service/service.go"},
		},
		{
			name:             "normalization cannot bypass protected path",
			actual:           []string{"./go.mod"},
			violationKinds:   []source.ViolationKind{source.ViolationProtected},
			violationPaths:   []string{"go.mod"},
			normalizedActual: []source.RepositoryPath{"go.mod"},
		},
		{
			name:             "normalized duplicates classify once",
			actual:           []string{"internal/service/service.go", "./internal//service/service.go"},
			allowed:          true,
			expectedChanges:  []source.RepositoryPath{"internal/service/service.go"},
			normalizedActual: []source.RepositoryPath{"internal/service/service.go"},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			result, err := approvedScope.ValidateActualSurface(test.actual)
			if result.Allowed() != test.allowed {
				t.Fatalf("Allowed() = %t, want %t", result.Allowed(), test.allowed)
			}
			if test.allowed {
				if err != nil {
					t.Fatalf("ValidateActualSurface() error = %v", err)
				}
			} else {
				var validationError *source.SurfaceValidationError
				if !errors.As(err, &validationError) {
					t.Fatalf("ValidateActualSurface() error = %v, want SurfaceValidationError", err)
				}
				if len(validationError.Violations()) != len(test.violationKinds) {
					t.Fatalf("validation error violations = %#v", validationError.Violations())
				}
			}
			assertRepositoryPaths(t, result.ActualPaths(), test.normalizedActual)
			assertRepositoryPaths(t, result.ExpectedChanges(), test.expectedChanges)
			assertRepositoryPaths(t, result.PossibleChanges(), test.possibleChanges)
			violations := result.Violations()
			if len(violations) != len(test.violationKinds) {
				t.Fatalf("Violations() = %#v, want %d", violations, len(test.violationKinds))
			}
			for index := range violations {
				if violations[index].Kind() != test.violationKinds[index] || violations[index].Path() != test.violationPaths[index] {
					t.Fatalf("violation %d = %s %q", index, violations[index].Kind(), violations[index].Path())
				}
			}
		})
	}
}

func newSourceTestChange(t *testing.T, projectId project.ProjectId) change.Change {
	t.Helper()
	currentChange, err := change.New(
		"change-source-test",
		projectId,
		"prove bounded source validation",
		time.Date(2026, time.August, 31, 12, 0, 0, 0, time.UTC),
	)
	if err != nil {
		t.Fatalf("change.New() error = %v", err)
	}
	return currentChange
}

func newSourceTestSnapshot(t *testing.T, projectId project.ProjectId) source.SourceSnapshot {
	t.Helper()
	snapshot, err := source.NewSourceSnapshot(
		projectId,
		"/tmp/repository",
		"0123456789abcdef",
		source.WorkingTreeClean,
		[]string{
			"README.md",
			"go.mod",
			"internal/service/service.go",
			"internal/service/service_test.go",
		},
		sourceDigest,
	)
	if err != nil {
		t.Fatalf("NewSourceSnapshot() error = %v", err)
	}
	return snapshot
}

func standardScopeRequest() source.ScopeRequest {
	return source.ScopeRequest{
		Expected:  []string{"internal/service/service.go"},
		Possible:  []string{"internal/service/service_test.go"},
		Protected: []string{"go.mod"},
	}
}

func newApprovedScope(t *testing.T) source.ApprovedScope {
	t.Helper()
	analysis, err := source.AnalyzeImpact(
		newSourceTestChange(t, sourceProjectId),
		newSourceTestSnapshot(t, sourceProjectId),
		standardScopeRequest(),
	)
	if err != nil {
		t.Fatalf("AnalyzeImpact() error = %v", err)
	}
	approvedScope, err := source.EstablishApprovedScope(analysis)
	if err != nil {
		t.Fatalf("EstablishApprovedScope() error = %v", err)
	}
	return approvedScope
}
