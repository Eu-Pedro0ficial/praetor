// Package inspection provides presentation-neutral, read-oriented M1.1 use cases.
package inspection

import (
	"fmt"
	"sort"

	"github.com/Eu-Pedro0ficial/praetor/internal/artifact"
	"github.com/Eu-Pedro0ficial/praetor/internal/audit"
	"github.com/Eu-Pedro0ficial/praetor/internal/authority"
	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"github.com/Eu-Pedro0ficial/praetor/internal/workflow"
)

const MaximumContentRead = 1 << 20

type ChangeDetail struct {
	Change        change.Change
	Workflow      workflow.WorkflowSnapshot
	Artifacts     []authority.ArtifactMetadata
	Relationships []artifact.Relationship
	Bindings      []authority.ArtifactBinding
	Audit         []audit.Event
	Diagnoses     []authority.Diagnosis
	Operations    []authority.Operation
}

type Service struct{ store authority.Store }

func New(store authority.Store) (*Service, error) {
	if store == nil {
		return nil, fmt.Errorf("durable inspection store is required")
	}
	return &Service{store: store}, nil
}

func (s *Service) ListChanges() ([]change.Change, error) { return s.store.ListChanges() }

func (s *Service) InspectChange(id change.ChangeId) (ChangeDetail, error) {
	current, snapshot, err := s.store.GetChange(id)
	if err != nil {
		return ChangeDetail{}, err
	}
	artifacts, err := s.store.ListArtifacts(id)
	if err != nil {
		return ChangeDetail{}, err
	}
	bindings, err := s.store.ListBindings(id)
	if err != nil {
		return ChangeDetail{}, err
	}
	relationships, err := s.store.ListRelationships(id)
	if err != nil {
		return ChangeDetail{}, err
	}
	history, err := s.store.AuditHistory(id)
	if err != nil {
		return ChangeDetail{}, err
	}
	diagnoses, err := s.Diagnose(id)
	if err != nil {
		return ChangeDetail{}, err
	}
	operations, err := s.store.ListOperations(id)
	if err != nil {
		return ChangeDetail{}, err
	}
	return ChangeDetail{Change: current, Workflow: snapshot, Artifacts: artifacts, Relationships: relationships, Bindings: bindings, Audit: history, Diagnoses: diagnoses, Operations: operations}, nil
}

func (s *Service) Content(changeId change.ChangeId, id artifact.ArtifactId) ([]byte, error) {
	item, err := s.store.GetArtifact(changeId, id, true)
	if err != nil {
		return nil, err
	}
	if item.ByteLength() > MaximumContentRead {
		return nil, fmt.Errorf("artifact content exceeds %d-byte interactive inspection limit", MaximumContentRead)
	}
	return item.Payload(), nil
}

func (s *Service) Diagnose(id change.ChangeId) ([]authority.Diagnosis, error) {
	current, _, err := s.store.GetChange(id)
	if err != nil {
		return nil, err
	}
	operations, err := s.store.ListOperations(id)
	if err != nil {
		return nil, err
	}
	result := make([]authority.Diagnosis, 0)
	for index := range operations {
		operation := operations[index]
		copyOperation := operation
		switch {
		case operation.State == authority.OperationReserved:
			result = append(result, authority.Diagnosis{Condition: authority.RecoveryIncomplete, Operation: &copyOperation, Detail: "reserved operation has no durable outcome"})
		case operation.Kind == "canonical-git-apply" && operation.State == authority.OperationCompleted && current.State() != change.StateAuditLocked:
			result = append(result, authority.Diagnosis{Condition: authority.RecoveryExternalUncertain, Operation: &copyOperation, Detail: "completed canonical operation requires terminal Change authority reconciliation"})
		case operation.Kind == "canonical-git-apply" && operation.State == authority.OperationCompleted && current.State() == change.StateAuditLocked:
			bindings, bindingError := s.store.ListBindings(id)
			if bindingError != nil {
				return nil, bindingError
			}
			found := false
			for _, binding := range bindings {
				if binding.Role == "application-result" {
					found = true
					break
				}
			}
			if !found {
				result = append(result, authority.Diagnosis{Condition: authority.RecoveryArtifactCorrupt, Operation: &copyOperation, Detail: "terminal Change is missing application-result authority"})
			}
		}
	}
	if len(result) == 0 {
		result = append(result, authority.Diagnosis{Condition: authority.RecoveryClean, Detail: "no incomplete durable operation"})
	}
	sort.Slice(result, func(i, j int) bool { return result[i].Condition < result[j].Condition })
	return result, nil
}
