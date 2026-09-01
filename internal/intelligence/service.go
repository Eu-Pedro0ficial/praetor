// Package intelligence coordinates M0.3 repository inspection, deterministic
// impact analysis, ApprovedScope establishment, and surface validation.
package intelligence

import (
	"fmt"

	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"github.com/Eu-Pedro0ficial/praetor/internal/project"
	"github.com/Eu-Pedro0ficial/praetor/internal/source"
)

const (
	EventSourceSnapshotCaptured   = "SOURCE_SNAPSHOT_CAPTURED"
	EventImpactAnalysisProduced   = "IMPACT_ANALYSIS_PRODUCED"
	EventChangeSurfaceEstablished = "CHANGE_SURFACE_ESTABLISHED"
	EventChangeSurfaceValidated   = "CHANGE_SURFACE_VALIDATED"
	EventChangeSurfaceViolation   = "CHANGE_SURFACE_VIOLATION"
)

// RepositoryInspector is the M0.3 application boundary implemented by the
// local Git adapter.
type RepositoryInspector func(project.ProjectId, string) (source.SourceSnapshot, error)

// LifecycleRecorder persists one typed M0.3 provenance event.
type LifecycleRecorder func(LifecycleEvent) error

// LifecycleEvent carries typed M0.3 provenance to the composition boundary.
type LifecycleEvent struct {
	EventType      string
	ChangeId       change.ChangeId
	ProjectId      project.ProjectId
	Snapshot       source.SourceSnapshot
	ImpactAnalysis source.ImpactAnalysis
	ApprovedScope  source.ApprovedScope
	Validation     source.SurfaceValidationResult
}

// PreparedSurface groups the immutable facts established before validation.
type PreparedSurface struct {
	snapshot       source.SourceSnapshot
	impactAnalysis source.ImpactAnalysis
	approvedScope  source.ApprovedScope
}

// Snapshot returns the source state used for impact analysis.
func (prepared PreparedSurface) Snapshot() source.SourceSnapshot {
	return prepared.snapshot
}

// ImpactAnalysis returns the candidate surface and its linkage.
func (prepared PreparedSurface) ImpactAnalysis() source.ImpactAnalysis {
	return prepared.impactAnalysis
}

// ApprovedScope returns the M0.3 file-validation boundary.
func (prepared PreparedSurface) ApprovedScope() source.ApprovedScope {
	return prepared.approvedScope
}

// Service performs stateless M0.3 orchestration through explicit dependencies.
type Service struct {
	inspector RepositoryInspector
	recorder  LifecycleRecorder
}

// New constructs the M0.3 application service.
func New(inspector RepositoryInspector, recorder LifecycleRecorder) (*Service, error) {
	if inspector == nil {
		return nil, fmt.Errorf("repository inspector dependency is not configured")
	}
	if recorder == nil {
		return nil, fmt.Errorf("M0.3 lifecycle recorder dependency is not configured")
	}
	return &Service{inspector: inspector, recorder: recorder}, nil
}

// EstablishSurface captures source state, produces an explicit candidate, and
// establishes it as the current M0.3 ApprovedScope.
func (service *Service) EstablishSurface(
	currentChange change.Change,
	repositoryRoot string,
	request source.ScopeRequest,
) (PreparedSurface, error) {
	snapshot, err := service.inspector(currentChange.ProjectId(), repositoryRoot)
	if err != nil {
		return PreparedSurface{}, err
	}
	if snapshot.RepositoryRoot() != repositoryRoot {
		return PreparedSurface{}, fmt.Errorf(
			"SourceSnapshot RepositoryRoot %q does not match registered RepositoryRoot %q",
			snapshot.RepositoryRoot(),
			repositoryRoot,
		)
	}
	if err := service.recorder(LifecycleEvent{
		EventType: EventSourceSnapshotCaptured,
		ChangeId:  currentChange.ChangeId(),
		ProjectId: currentChange.ProjectId(),
		Snapshot:  snapshot,
	}); err != nil {
		return PreparedSurface{}, fmt.Errorf("record SourceSnapshot capture: %w", err)
	}

	impactAnalysis, err := source.AnalyzeImpact(currentChange, snapshot, request)
	if err != nil {
		return PreparedSurface{}, err
	}
	if err := service.recorder(LifecycleEvent{
		EventType:      EventImpactAnalysisProduced,
		ChangeId:       currentChange.ChangeId(),
		ProjectId:      currentChange.ProjectId(),
		ImpactAnalysis: impactAnalysis,
	}); err != nil {
		return PreparedSurface{}, fmt.Errorf("record ImpactAnalysis: %w", err)
	}

	approvedScope, err := source.EstablishApprovedScope(impactAnalysis)
	if err != nil {
		return PreparedSurface{}, err
	}
	if err := service.recorder(LifecycleEvent{
		EventType:     EventChangeSurfaceEstablished,
		ChangeId:      currentChange.ChangeId(),
		ProjectId:     currentChange.ProjectId(),
		ApprovedScope: approvedScope,
	}); err != nil {
		return PreparedSurface{}, fmt.Errorf("record Change Surface establishment: %w", err)
	}

	return PreparedSurface{
		snapshot:       snapshot,
		impactAnalysis: impactAnalysis,
		approvedScope:  approvedScope,
	}, nil
}

// ValidateActualSurface records either the successful comparison or its full
// violation set before returning the deterministic domain result.
func (service *Service) ValidateActualSurface(
	approvedScope source.ApprovedScope,
	actualPathValues []string,
) (source.SurfaceValidationResult, error) {
	result, validationError := approvedScope.ValidateActualSurface(actualPathValues)
	eventType := EventChangeSurfaceValidated
	if validationError != nil {
		eventType = EventChangeSurfaceViolation
	}
	if err := service.recorder(LifecycleEvent{
		EventType:     eventType,
		ChangeId:      approvedScope.ChangeId(),
		ProjectId:     approvedScope.ProjectId(),
		ApprovedScope: approvedScope,
		Validation:    result,
	}); err != nil {
		return result, fmt.Errorf("record Change Surface validation: %w", err)
	}
	return result, validationError
}
