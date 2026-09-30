// Package authority defines the narrow application contracts for M1.1 durable
// artifacts, audit, operations, inspection, and all-or-none authority commits.
package authority

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/Eu-Pedro0ficial/praetor/internal/artifact"
	"github.com/Eu-Pedro0ficial/praetor/internal/audit"
	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"github.com/Eu-Pedro0ficial/praetor/internal/project"
	"github.com/Eu-Pedro0ficial/praetor/internal/workflow"
)

var (
	ErrStaleRevision     = errors.New("stale Change revision")
	ErrOperationConflict = errors.New("OperationId request digest conflict")
	ErrBusy              = errors.New("durable store is busy")
	ErrCorrupt           = errors.New("durable authority is corrupt")
	ErrIncompatible      = errors.New("durable store is incompatible")
)

type OperationId string
type OperationState string
type RecoveryCondition string

const (
	OperationReserved  OperationState = "reserved"
	OperationCompleted OperationState = "completed"
	OperationFailed    OperationState = "failed"

	RecoveryClean             RecoveryCondition = "clean"
	RecoveryIncomplete        RecoveryCondition = "operation-incomplete"
	RecoveryExternalUncertain RecoveryCondition = "external-effect-uncertain"
	RecoverySourceDiverged    RecoveryCondition = "source-diverged"
	RecoveryArtifactCorrupt   RecoveryCondition = "artifact-corrupt-or-missing"
	RecoveryStoreIncompatible RecoveryCondition = "store-incompatible"
)

type ArtifactBinding struct {
	Role       string
	ArtifactId artifact.ArtifactId
	Revision   uint64
}

type Operation struct {
	Id               OperationId
	ProjectId        project.ProjectId
	ChangeId         change.ChangeId
	Kind             string
	RequestDigest    string
	ExpectedRevision uint64
	State            OperationState
	Result           []byte
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type AuditRecord struct {
	Event      audit.Event
	RawJSON    []byte
	LegacySeq  uint64
	SourceHash string
}

type AuthorityCommit struct {
	ExpectedRevision uint64
	Candidate        change.Change
	Artifacts        []artifact.Artifact
	Relationships    []artifact.Relationship
	Supersessions    []artifact.Supersession
	Bindings         []ArtifactBinding
	Operation        *Operation
	AuditEvents      []audit.Event
}

type ChangeStore interface {
	GetChange(change.ChangeId) (change.Change, workflow.WorkflowSnapshot, error)
	ListChanges() ([]change.Change, error)
}

type ArtifactStore interface {
	ListArtifacts(change.ChangeId) ([]ArtifactMetadata, error)
	GetArtifact(change.ChangeId, artifact.ArtifactId, bool) (artifact.Artifact, error)
	ListRelationships(change.ChangeId) ([]artifact.Relationship, error)
	ListBindings(change.ChangeId) ([]ArtifactBinding, error)
	ProjectArtifactBytes() (int64, error)
}

type AuditLedger interface {
	AppendAudit(audit.Event) error
	AuditHistory(change.ChangeId) ([]audit.Event, error)
}

type DurableChangeCommitter interface {
	CommitAuthority(AuthorityCommit) error
	CommitArtifacts(change.ChangeId, uint64, []artifact.Artifact, []artifact.Relationship, []artifact.Supersession, []ArtifactBinding, []audit.Event) error
}

type OperationStore interface {
	ReserveOperation(Operation) (Operation, bool, error)
	GetOperation(OperationId) (Operation, error)
	ListOperations(change.ChangeId) ([]Operation, error)
	ListIncompleteOperations() ([]Operation, error)
	CompleteOperation(OperationId, string, []byte, []audit.Event) (Operation, error)
	FailOperation(OperationId, string, []byte, []audit.Event) (Operation, error)
}

type Store interface {
	workflow.DurableStore
	ChangeStore
	ArtifactStore
	AuditLedger
	DurableChangeCommitter
	OperationStore
	ValidateAttachment() error
	Validate() error
	Backup(string) error
	Close() error
}

type ArtifactMetadata struct {
	Id                    artifact.ArtifactId
	Kind                  artifact.Kind
	EnvelopeSchemaVersion uint32
	KindSchemaVersion     uint32
	MediaType             string
	ByteLength            int64
	CreatedAt             time.Time
	Producer              artifact.Producer
	ContentDigest         string
	RecordDigest          string
}

type Diagnosis struct {
	Condition RecoveryCondition
	Operation *Operation
	Detail    string
}

func GenerateOperationId() (OperationId, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate OperationId: %w", err)
	}
	return OperationId("op-" + hex.EncodeToString(value[:])), nil
}

func ValidateOperation(value Operation) error {
	if len(value.Id) != 35 || !strings.HasPrefix(string(value.Id), "op-") {
		return fmt.Errorf("invalid OperationId %q", value.Id)
	}
	if _, err := hex.DecodeString(strings.TrimPrefix(string(value.Id), "op-")); err != nil {
		return fmt.Errorf("invalid OperationId %q", value.Id)
	}
	if !value.ProjectId.IsValid() {
		return fmt.Errorf("valid operation ProjectId is required")
	}
	if strings.TrimSpace(value.Kind) == "" {
		return fmt.Errorf("operation kind is required")
	}
	if !validDigest(value.RequestDigest) {
		return fmt.Errorf("operation RequestDigest is invalid")
	}
	if value.ExpectedRevision == 0 && value.ChangeId != "" {
		return fmt.Errorf("Change operation ExpectedRevision must be positive")
	}
	if value.State != OperationReserved && value.State != OperationCompleted && value.State != OperationFailed {
		return fmt.Errorf("unsupported operation state %q", value.State)
	}
	if value.CreatedAt.IsZero() || value.UpdatedAt.IsZero() || value.UpdatedAt.Before(value.CreatedAt) {
		return fmt.Errorf("operation timestamps are invalid")
	}
	if len(value.Result) > 64<<10 {
		return fmt.Errorf("operation result exceeds bounded limit")
	}
	return nil
}

func validDigest(value string) bool {
	if len(value) != 71 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(value, "sha256:"))
	return err == nil
}
