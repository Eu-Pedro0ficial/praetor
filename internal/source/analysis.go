package source

import (
	"fmt"

	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"github.com/Eu-Pedro0ficial/praetor/internal/project"
)

// ImpactAnalysis is deterministic candidate-surface output linked to the
// Change and source state used to produce it.
type ImpactAnalysis struct {
	changeId          change.ChangeId
	projectId         project.ProjectId
	sourceStateDigest SourceStateDigest
	candidateSurface  ChangeSurface
}

// AnalyzeImpact produces a candidate surface from explicit scope input and
// verifies every candidate path against the tracked snapshot inventory.
func AnalyzeImpact(
	currentChange change.Change,
	snapshot SourceSnapshot,
	request ScopeRequest,
) (ImpactAnalysis, error) {
	if currentChange.ChangeId() == "" {
		return ImpactAnalysis{}, fmt.Errorf("valid Change is required")
	}
	if currentChange.ProjectId() != snapshot.ProjectId() {
		return ImpactAnalysis{}, fmt.Errorf(
			"Change ProjectId %q does not match SourceSnapshot ProjectId %q",
			currentChange.ProjectId(),
			snapshot.ProjectId(),
		)
	}
	candidateSurface, err := NewChangeSurface(request)
	if err != nil {
		return ImpactAnalysis{}, err
	}
	for _, repositoryPath := range allSurfacePaths(candidateSurface) {
		if !snapshot.ContainsTrackedPath(repositoryPath) {
			return ImpactAnalysis{}, fmt.Errorf(
				"Change Surface path %q is not present in the tracked source inventory",
				repositoryPath,
			)
		}
	}

	return ImpactAnalysis{
		changeId:          currentChange.ChangeId(),
		projectId:         currentChange.ProjectId(),
		sourceStateDigest: snapshot.SourceStateDigest(),
		candidateSurface:  candidateSurface,
	}, nil
}

// ChangeId returns the associated Change identity.
func (analysis ImpactAnalysis) ChangeId() change.ChangeId {
	return analysis.changeId
}

// ProjectId returns the associated logical Project identity.
func (analysis ImpactAnalysis) ProjectId() project.ProjectId {
	return analysis.projectId
}

// SourceStateDigest returns the source state used by the analysis.
func (analysis ImpactAnalysis) SourceStateDigest() SourceStateDigest {
	return analysis.sourceStateDigest
}

// CandidateSurface returns the deterministic impact-analysis result.
func (analysis ImpactAnalysis) CandidateSurface() ChangeSurface {
	return analysis.candidateSurface
}

// ApprovedScope is the M0.3 authorization boundary used for deterministic
// file-path validation. It is not an M0.7 human approval decision.
type ApprovedScope struct {
	changeId          change.ChangeId
	projectId         project.ProjectId
	sourceStateDigest SourceStateDigest
	surface           ChangeSurface
}

// EstablishApprovedScope makes the explicit candidate surface the current
// M0.3 validation boundary.
func EstablishApprovedScope(analysis ImpactAnalysis) (ApprovedScope, error) {
	if analysis.changeId == "" || !analysis.projectId.IsValid() {
		return ApprovedScope{}, fmt.Errorf("valid ImpactAnalysis is required")
	}
	if err := validateSourceStateDigest(analysis.sourceStateDigest); err != nil {
		return ApprovedScope{}, err
	}
	return ApprovedScope{
		changeId:          analysis.changeId,
		projectId:         analysis.projectId,
		sourceStateDigest: analysis.sourceStateDigest,
		surface:           analysis.candidateSurface,
	}, nil
}

// RehydrateApprovedScope reconstructs the exact durable M0.3 boundary.
func RehydrateApprovedScope(changeId change.ChangeId, projectId project.ProjectId, sourceStateDigest SourceStateDigest, request ScopeRequest) (ApprovedScope, error) {
	if _, err := change.NewChangeId(string(changeId)); err != nil {
		return ApprovedScope{}, err
	}
	if !projectId.IsValid() {
		return ApprovedScope{}, fmt.Errorf("valid ProjectId is required")
	}
	if err := validateSourceStateDigest(sourceStateDigest); err != nil {
		return ApprovedScope{}, err
	}
	surface, err := NewChangeSurface(request)
	if err != nil {
		return ApprovedScope{}, err
	}
	return ApprovedScope{changeId: changeId, projectId: projectId, sourceStateDigest: sourceStateDigest, surface: surface}, nil
}

// ChangeId returns the Change governed by this scope.
func (scope ApprovedScope) ChangeId() change.ChangeId {
	return scope.changeId
}

// ProjectId returns the scope's logical Project association.
func (scope ApprovedScope) ProjectId() project.ProjectId {
	return scope.projectId
}

// SourceStateDigest returns the captured source-state context.
func (scope ApprovedScope) SourceStateDigest() SourceStateDigest {
	return scope.sourceStateDigest
}

// Surface returns the authorized file-level Change Surface.
func (scope ApprovedScope) Surface() ChangeSurface {
	return scope.surface
}

// ValidateActualSurface compares the complete actual path set with the
// approved boundary and fails the whole result when any path violates it.
func (scope ApprovedScope) ValidateActualSurface(actualPathValues []string) (SurfaceValidationResult, error) {
	if scope.changeId == "" || !scope.projectId.IsValid() {
		return SurfaceValidationResult{}, fmt.Errorf("valid ApprovedScope is required")
	}
	return validateActualSurface(scope.surface, actualPathValues)
}

func allSurfacePaths(surface ChangeSurface) []RepositoryPath {
	paths := make([]RepositoryPath, 0, len(surface.expected)+len(surface.possible)+len(surface.protected))
	paths = append(paths, surface.expected...)
	paths = append(paths, surface.possible...)
	paths = append(paths, surface.protected...)
	return paths
}
