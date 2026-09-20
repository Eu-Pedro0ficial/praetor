package workflow

import (
	"bytes"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"go.yaml.in/yaml/v3"
)

const SupportedWorkflowSchemaVersion uint32 = 1

var ErrUnsupportedWorkflowSchema = errors.New("unsupported workflow schema")

//go:embed core_v0.yaml
var coreV0YAML []byte

type WorkflowSnapshot struct {
	id            string
	version       string
	schemaVersion uint32
	digest        string
	exactYAML     []byte
	definition    Definition
}

type Definition struct {
	SchemaVersion   uint32                 `yaml:"schema_version"`
	WorkflowId      string                 `yaml:"workflow_id"`
	WorkflowVersion string                 `yaml:"workflow_version"`
	States          []string               `yaml:"states"`
	Transitions     []TransitionDefinition `yaml:"transitions"`
}

type TransitionDefinition struct {
	From string `yaml:"from"`
	To   string `yaml:"to"`
}

func CoreV0Snapshot() (WorkflowSnapshot, error) { return ParseSnapshot(coreV0YAML) }

// InspectSnapshot reconstructs immutable workflow authority without requiring
// the current runtime to support its schema. Integrity and persisted metadata
// remain inspectable even when execution compatibility is unavailable.
func InspectSnapshot(id, version string, schemaVersion uint32, digest string, exact []byte) (WorkflowSnapshot, error) {
	if len(exact) == 0 || strings.TrimSpace(id) == "" || strings.TrimSpace(version) == "" || schemaVersion == 0 {
		return WorkflowSnapshot{}, fmt.Errorf("workflow snapshot metadata is incomplete")
	}
	sum := sha256.Sum256(exact)
	actualDigest := "sha256:" + hex.EncodeToString(sum[:])
	if digest != actualDigest {
		return WorkflowSnapshot{}, fmt.Errorf("workflow snapshot digest mismatch")
	}
	decoder := yaml.NewDecoder(bytes.NewReader(exact))
	var header struct {
		SchemaVersion   uint32 `yaml:"schema_version"`
		WorkflowId      string `yaml:"workflow_id"`
		WorkflowVersion string `yaml:"workflow_version"`
	}
	if err := decoder.Decode(&header); err != nil {
		return WorkflowSnapshot{}, fmt.Errorf("decode workflow snapshot metadata: %w", err)
	}
	if header.SchemaVersion != schemaVersion || header.WorkflowId != id || header.WorkflowVersion != version {
		return WorkflowSnapshot{}, fmt.Errorf("workflow snapshot persisted metadata mismatch")
	}
	snapshot := WorkflowSnapshot{id: id, version: version, schemaVersion: schemaVersion, digest: digest, exactYAML: append([]byte(nil), exact...)}
	if schemaVersion == SupportedWorkflowSchemaVersion {
		parsed, err := ParseSnapshot(exact)
		if err != nil {
			return WorkflowSnapshot{}, err
		}
		snapshot.definition = parsed.definition
	}
	return snapshot, nil
}

// ParseSnapshot validates exact YAML bytes and retains those bytes as authority.
func ParseSnapshot(exact []byte) (WorkflowSnapshot, error) {
	if len(exact) == 0 {
		return WorkflowSnapshot{}, fmt.Errorf("workflow YAML is required")
	}
	decoder := yaml.NewDecoder(bytes.NewReader(exact))
	decoder.KnownFields(true)
	var definition Definition
	if err := decoder.Decode(&definition); err != nil {
		return WorkflowSnapshot{}, fmt.Errorf("decode workflow YAML: %w", err)
	}
	var extra any
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return WorkflowSnapshot{}, fmt.Errorf("workflow YAML must contain one document")
		}
		return WorkflowSnapshot{}, fmt.Errorf("decode workflow YAML: %w", err)
	}
	if definition.SchemaVersion != SupportedWorkflowSchemaVersion {
		return WorkflowSnapshot{}, fmt.Errorf("unsupported workflow schema version %d", definition.SchemaVersion)
	}
	if strings.TrimSpace(definition.WorkflowId) == "" || strings.TrimSpace(definition.WorkflowVersion) == "" {
		return WorkflowSnapshot{}, fmt.Errorf("workflow identity and version are required")
	}
	wantStates := []change.ChangeState{change.StateCreated, change.StatePlanned, change.StateIsolated, change.StateValidated, change.StateApproved, change.StateRejected, change.StateAuditLocked}
	if len(definition.States) != len(wantStates) {
		return WorkflowSnapshot{}, fmt.Errorf("Core V0 workflow state set is invalid")
	}
	seenStates := map[change.ChangeState]bool{}
	for _, raw := range definition.States {
		state, err := change.ParseState(raw)
		if err != nil || seenStates[state] {
			return WorkflowSnapshot{}, fmt.Errorf("Core V0 workflow state %q is invalid or duplicated", raw)
		}
		seenStates[state] = true
	}
	for _, state := range wantStates {
		if !seenStates[state] {
			return WorkflowSnapshot{}, fmt.Errorf("Core V0 workflow omits state %q", state)
		}
	}
	wantEdges := map[string]bool{"created\x00planned": true, "created\x00rejected": true, "planned\x00isolated": true, "planned\x00rejected": true, "isolated\x00validated": true, "isolated\x00rejected": true, "validated\x00approved": true, "validated\x00rejected": true, "approved\x00audit-locked": true, "rejected\x00audit-locked": true}
	if len(definition.Transitions) != len(wantEdges) {
		return WorkflowSnapshot{}, fmt.Errorf("Core V0 workflow transition set is invalid")
	}
	seenEdges := map[string]bool{}
	for _, edge := range definition.Transitions {
		from, errFrom := change.ParseState(edge.From)
		to, errTo := change.ParseState(edge.To)
		key := string(from) + "\x00" + string(to)
		if errFrom != nil || errTo != nil || !wantEdges[key] || seenEdges[key] {
			return WorkflowSnapshot{}, fmt.Errorf("Core V0 workflow transition %q -> %q is invalid or duplicated", edge.From, edge.To)
		}
		seenEdges[key] = true
	}
	sum := sha256.Sum256(exact)
	return WorkflowSnapshot{id: definition.WorkflowId, version: definition.WorkflowVersion, schemaVersion: definition.SchemaVersion, digest: "sha256:" + hex.EncodeToString(sum[:]), exactYAML: append([]byte(nil), exact...), definition: definition}, nil
}

func (s WorkflowSnapshot) WorkflowId() string      { return s.id }
func (s WorkflowSnapshot) WorkflowVersion() string { return s.version }
func (s WorkflowSnapshot) SchemaVersion() uint32   { return s.schemaVersion }
func (s WorkflowSnapshot) Digest() string          { return s.digest }
func (s WorkflowSnapshot) ExactYAML() []byte       { return append([]byte(nil), s.exactYAML...) }
func (s WorkflowSnapshot) Definition() Definition  { return s.definition }
func (s WorkflowSnapshot) Executable() bool        { return s.schemaVersion == SupportedWorkflowSchemaVersion }
