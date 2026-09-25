package impact

import (
	"errors"
	"fmt"
	"time"

	"github.com/Eu-Pedro0ficial/praetor/internal/artifact"
	"github.com/Eu-Pedro0ficial/praetor/internal/audit"
	"github.com/Eu-Pedro0ficial/praetor/internal/authority"
	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"github.com/Eu-Pedro0ficial/praetor/internal/project"
	"github.com/Eu-Pedro0ficial/praetor/internal/repositorymodel"
)

var (
	ErrModelNotCurrent    = errors.New("RepositoryModel is not CURRENT")
	ErrReportAlreadyBound = errors.New("an ImpactReport is already bound at the current Change revision")
)

const (
	BindingRole                = "impact-report"
	EventImpactReportCommitted = "IMPACT_REPORT_COMMITTED"
)

type ModelPort interface {
	Build() (repositorymodel.BuildResult, error)
	Freshness(repositorymodel.Model) (repositorymodel.Freshness, error)
	FreshnessForBuildKey(repositorymodel.BuildKey) (repositorymodel.Freshness, error)
}

type Result struct {
	Report     Report
	Artifact   artifact.Artifact
	Model      repositorymodel.Model
	BuildStats repositorymodel.BuildStatistics
}

type Service struct {
	projectId      project.ProjectId
	repositoryRoot string
	models         ModelPort
	store          authority.Store
	configuration  Configuration
	clock          func() time.Time
}

func NewService(projectId project.ProjectId, repositoryRoot string, models ModelPort, store authority.Store, configuration Configuration, clock func() time.Time) (*Service, error) {
	if !projectId.IsValid() || repositoryRoot == "" || models == nil || store == nil || clock == nil {
		return nil, fmt.Errorf("impact service dependencies are incomplete")
	}
	if configuration.MaximumDepth <= 0 || configuration.MaximumItems <= 0 || configuration.MaximumPathSteps <= 0 {
		return nil, fmt.Errorf("impact service limits must be positive")
	}
	return &Service{projectId: projectId, repositoryRoot: repositoryRoot, models: models, store: store, configuration: configuration, clock: clock}, nil
}

func (service *Service) AnalyzeAndPersist(changeId change.ChangeId, request Request) (Result, error) {
	current, _, err := service.store.GetChange(changeId)
	if err != nil {
		return Result{}, err
	}
	if current.ProjectId() != service.projectId {
		return Result{}, fmt.Errorf("cross-Project Change rejected")
	}
	built, err := service.models.Build()
	if err != nil {
		return Result{}, err
	}
	freshness, err := service.models.Freshness(built.Model)
	if err != nil {
		return Result{}, err
	}
	if freshness != repositorymodel.FreshnessCurrent {
		return Result{}, fmt.Errorf("%w: %s", ErrModelNotCurrent, freshness)
	}
	report, err := Analyze(current, built.Model, request, service.configuration, service.clock())
	if err != nil {
		return Result{}, err
	}
	payload, err := report.Encode()
	if err != nil {
		return Result{}, err
	}
	id, err := artifact.GenerateId()
	if err != nil {
		return Result{}, err
	}
	item, err := artifact.New(id, service.projectId, current.ChangeId(), artifact.KindImpactReport, 1, ReportSchemaVersion, "application/vnd.praetor.impact-report+json", report.CreatedAt, artifact.Producer{Component: "impact-engine"}, payload, false)
	if err != nil {
		return Result{}, err
	}
	bindings, err := service.store.ListBindings(current.ChangeId())
	if err != nil {
		return Result{}, err
	}
	var relationships []artifact.Relationship
	var supersessions []artifact.Supersession
	for _, binding := range bindings {
		if binding.Role != BindingRole {
			continue
		}
		if binding.Revision >= current.Revision() {
			return Result{}, ErrReportAlreadyBound
		}
		relationships = append(relationships, artifact.Relationship{From: id, To: binding.ArtifactId, Kind: artifact.RelationshipParent})
		supersessions = append(supersessions, artifact.Supersession{Previous: binding.ArtifactId, Current: id})
	}
	event, err := audit.NewEvent(EventImpactReportCommitted, string(service.projectId), string(current.ChangeId()), service.repositoryRoot, map[string]any{"artifact_id": string(id), "report_digest": report.ReportDigest, "repository_model_id": report.RepositoryModelId, "repository_model_digest": report.RepositoryModelDigest, "model_build_key": report.BuildKey.Digest, "source_fingerprint": report.SourceFingerprintDigest, "risk": string(report.Risk.Overall), "impact_count": len(report.Items), "knowledge_gap_count": len(report.KnowledgeGaps)}, report.CreatedAt)
	if err != nil {
		return Result{}, err
	}
	binding := authority.ArtifactBinding{Role: BindingRole, ArtifactId: id, Revision: current.Revision()}
	if err := service.store.CommitArtifacts(current.ChangeId(), current.Revision(), []artifact.Artifact{item}, relationships, supersessions, []authority.ArtifactBinding{binding}, []audit.Event{event}); err != nil {
		return Result{}, err
	}
	return Result{Report: report, Artifact: item, Model: built.Model, BuildStats: built.Statistics}, nil
}

func (service *Service) Inspect(changeId change.ChangeId, artifactId artifact.ArtifactId) (Report, artifact.ArtifactId, repositorymodel.Freshness, error) {
	if artifactId == "" {
		bindings, err := service.store.ListBindings(changeId)
		if err != nil {
			return Report{}, "", repositorymodel.FreshnessUnknown, err
		}
		for _, binding := range bindings {
			if binding.Role == BindingRole {
				artifactId = binding.ArtifactId
				break
			}
		}
		if artifactId == "" {
			return Report{}, "", repositorymodel.FreshnessUnknown, fmt.Errorf("Change %q has no bound ImpactReport", changeId)
		}
	}
	item, err := service.store.GetArtifact(changeId, artifactId, true)
	if err != nil {
		return Report{}, "", repositorymodel.FreshnessUnknown, err
	}
	if item.Kind() != artifact.KindImpactReport {
		return Report{}, "", repositorymodel.FreshnessUnknown, fmt.Errorf("artifact %q is not an ImpactReport", artifactId)
	}
	report, err := Decode(item.Payload())
	if err != nil {
		return Report{}, "", repositorymodel.FreshnessUnknown, err
	}
	if report.ProjectId != service.projectId || report.ChangeId != changeId {
		return Report{}, "", repositorymodel.FreshnessUnknown, fmt.Errorf("ImpactReport ownership mismatch")
	}
	freshness, err := service.models.FreshnessForBuildKey(report.BuildKey)
	if err != nil {
		return report, artifactId, repositorymodel.FreshnessUnknown, err
	}
	return report, artifactId, freshness, nil
}
