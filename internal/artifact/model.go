// Package artifact defines the immutable M1.1 governed artifact envelope.
package artifact

import (
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
	"time"

	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"github.com/Eu-Pedro0ficial/praetor/internal/project"
)

const (
	NormalSizeLimit       int64 = 1 << 20
	LargeArtifactBoundary int64 = 10 << 20
	HardSizeLimit         int64 = 50 << 20
	ChangeAggregateLimit  int64 = 100 << 20
)

type ArtifactId string
type Kind string
type RelationshipKind string

const (
	KindWorkflowSnapshot   Kind             = "workflow-snapshot"
	KindSourceSnapshot     Kind             = "source-snapshot"
	KindApprovedScope      Kind             = "approved-scope"
	KindPatch              Kind             = "patch"
	KindVerificationPlan   Kind             = "verification-plan"
	KindEvidenceSet        Kind             = "evidence-set"
	KindPolicyDecision     Kind             = "policy-decision"
	KindHumanDecision      Kind             = "human-decision"
	KindApplicationResult  Kind             = "application-result"
	KindOperationData      Kind             = "operation-data"
	KindImpactReport       Kind             = "impact-report"
	RelationshipParent     RelationshipKind = "parent"
	RelationshipDependency RelationshipKind = "dependency"
)

// Producer is bounded provenance for the component/operation that emitted an artifact.
type Producer struct {
	Component   string
	OperationId string
}

// Artifact is immutable after construction. Accessors return payload copies.
type Artifact struct {
	id                    ArtifactId
	projectId             project.ProjectId
	changeId              change.ChangeId
	kind                  Kind
	envelopeSchemaVersion uint32
	kindSchemaVersion     uint32
	mediaType             string
	createdAt             time.Time
	producer              Producer
	payload               []byte
	contentDigest         string
	recordDigest          string
}

// Relationship is an immutable directed edge between artifacts in one Change.
type Relationship struct {
	From ArtifactId
	To   ArtifactId
	Kind RelationshipKind
}

// Supersession records an append-only replacement relationship.
type Supersession struct {
	Previous ArtifactId
	Current  ArtifactId
}

func GenerateId() (ArtifactId, error) {
	var value [16]byte
	if _, err := rand.Read(value[:]); err != nil {
		return "", fmt.Errorf("generate ArtifactId: %w", err)
	}
	return ArtifactId("art-" + hex.EncodeToString(value[:])), nil
}

func New(id ArtifactId, projectId project.ProjectId, changeId change.ChangeId, kind Kind, envelopeVersion, kindVersion uint32, mediaType string, createdAt time.Time, producer Producer, payload []byte, allowLarge bool) (Artifact, error) {
	if err := validateId(id); err != nil {
		return Artifact{}, err
	}
	if !projectId.IsValid() {
		return Artifact{}, fmt.Errorf("valid ProjectId is required")
	}
	if _, err := change.NewChangeId(string(changeId)); err != nil {
		return Artifact{}, err
	}
	if !knownKind(kind) {
		return Artifact{}, fmt.Errorf("unsupported ArtifactKind %q", kind)
	}
	if envelopeVersion == 0 || kindVersion == 0 {
		return Artifact{}, fmt.Errorf("artifact schema versions must be positive")
	}
	mediaType = strings.TrimSpace(mediaType)
	if mediaType == "" {
		return Artifact{}, fmt.Errorf("artifact MediaType is required")
	}
	if createdAt.IsZero() {
		return Artifact{}, fmt.Errorf("artifact CreatedAt is required")
	}
	producer.Component = strings.TrimSpace(producer.Component)
	producer.OperationId = strings.TrimSpace(producer.OperationId)
	if producer.Component == "" {
		return Artifact{}, fmt.Errorf("artifact producer component is required")
	}
	if err := validatePayloadSize(int64(len(payload)), allowLarge); err != nil {
		return Artifact{}, err
	}
	copyPayload := append([]byte(nil), payload...)
	contentDigest := digest(copyPayload)
	createdAt = createdAt.UTC()
	recordDigest := recordIdentity(id, projectId, changeId, kind, envelopeVersion, kindVersion, mediaType, createdAt, producer, int64(len(copyPayload)), contentDigest)
	return Artifact{id: id, projectId: projectId, changeId: changeId, kind: kind, envelopeSchemaVersion: envelopeVersion, kindSchemaVersion: kindVersion, mediaType: mediaType, createdAt: createdAt, producer: producer, payload: copyPayload, contentDigest: contentDigest, recordDigest: recordDigest}, nil
}

func validatePayloadSize(size int64, allowLarge bool) error {
	if size < 0 || size > HardSizeLimit {
		return fmt.Errorf("artifact payload exceeds %d-byte hard limit", HardSizeLimit)
	}
	if size > NormalSizeLimit && !allowLarge {
		return fmt.Errorf("artifact payload requires explicit large-artifact handling")
	}
	return nil
}

// Rehydrate reconstructs an artifact and verifies both persisted digests.
func Rehydrate(id ArtifactId, projectId project.ProjectId, changeId change.ChangeId, kind Kind, envelopeVersion, kindVersion uint32, mediaType string, createdAt time.Time, producer Producer, payload []byte, contentDigest, recordDigest string) (Artifact, error) {
	value, err := New(id, projectId, changeId, kind, envelopeVersion, kindVersion, mediaType, createdAt, producer, payload, true)
	if err != nil {
		return Artifact{}, err
	}
	if value.contentDigest != contentDigest {
		return Artifact{}, fmt.Errorf("artifact %q content digest mismatch", id)
	}
	if value.recordDigest != recordDigest {
		return Artifact{}, fmt.Errorf("artifact %q record digest mismatch", id)
	}
	return value, nil
}

func (a Artifact) Id() ArtifactId                { return a.id }
func (a Artifact) ProjectId() project.ProjectId  { return a.projectId }
func (a Artifact) ChangeId() change.ChangeId     { return a.changeId }
func (a Artifact) Kind() Kind                    { return a.kind }
func (a Artifact) EnvelopeSchemaVersion() uint32 { return a.envelopeSchemaVersion }
func (a Artifact) KindSchemaVersion() uint32     { return a.kindSchemaVersion }
func (a Artifact) MediaType() string             { return a.mediaType }
func (a Artifact) ByteLength() int64             { return int64(len(a.payload)) }
func (a Artifact) CreatedAt() time.Time          { return a.createdAt }
func (a Artifact) Producer() Producer            { return a.producer }
func (a Artifact) Payload() []byte               { return append([]byte(nil), a.payload...) }
func (a Artifact) ContentDigest() string         { return a.contentDigest }
func (a Artifact) RecordDigest() string          { return a.recordDigest }
func (a Artifact) IsLarge() bool                 { return a.ByteLength() > LargeArtifactBoundary }

func ValidateRelationship(value Relationship) error {
	if err := validateId(value.From); err != nil {
		return fmt.Errorf("relationship source: %w", err)
	}
	if err := validateId(value.To); err != nil {
		return fmt.Errorf("relationship target: %w", err)
	}
	if value.From == value.To {
		return fmt.Errorf("artifact cannot relate to itself")
	}
	if value.Kind != RelationshipParent && value.Kind != RelationshipDependency {
		return fmt.Errorf("unsupported artifact relationship %q", value.Kind)
	}
	return nil
}

func validateId(id ArtifactId) error {
	value := string(id)
	if len(value) != 36 || !strings.HasPrefix(value, "art-") {
		return fmt.Errorf("invalid ArtifactId %q", id)
	}
	if _, err := hex.DecodeString(strings.TrimPrefix(value, "art-")); err != nil {
		return fmt.Errorf("invalid ArtifactId %q", id)
	}
	return nil
}

func knownKind(kind Kind) bool {
	switch kind {
	case KindWorkflowSnapshot, KindSourceSnapshot, KindApprovedScope, KindPatch, KindVerificationPlan, KindEvidenceSet, KindPolicyDecision, KindHumanDecision, KindApplicationResult, KindOperationData, KindImpactReport:
		return true
	default:
		return false
	}
}

func digest(value []byte) string {
	sum := sha256.Sum256(value)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func recordIdentity(id ArtifactId, projectId project.ProjectId, changeId change.ChangeId, kind Kind, envelopeVersion, kindVersion uint32, mediaType string, createdAt time.Time, producer Producer, length int64, contentDigest string) string {
	fields := []string{"praetor-artifact-record-v1", string(id), string(projectId), string(changeId), string(kind), fmt.Sprint(envelopeVersion), fmt.Sprint(kindVersion), mediaType, createdAt.Format(time.RFC3339Nano), producer.Component, producer.OperationId, fmt.Sprint(length), contentDigest}
	return digest([]byte(strings.Join(fields, "\x1f")))
}
