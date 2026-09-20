package artifact

import (
	"encoding/json"
	"fmt"
	"strings"
)

// SourceSnapshotPayload is the bounded canonical-source authority needed to
// classify an interrupted Git operation after the proposal process is gone.
type SourceSnapshotPayload struct {
	RepositoryRoot    string   `json:"repository_root"`
	WorkspaceId       string   `json:"workspace_id"`
	WorkspaceRoot     string   `json:"workspace_root"`
	HeadRevision      string   `json:"head_revision"`
	TrackedPaths      []string `json:"tracked_paths"`
	SourceStateDigest string   `json:"source_state_digest"`
}

// PatchPayload retains the exact approved patch and its deterministic
// application metadata. JSON encodes Content as base64 without altering it.
type PatchPayload struct {
	WorkspaceId       string   `json:"workspace_id"`
	BaseRevision      string   `json:"base_revision"`
	SourceStateDigest string   `json:"source_state_digest"`
	ChangedPaths      []string `json:"changed_paths"`
	PatchDigest       string   `json:"patch_digest"`
	Content           []byte   `json:"content"`
	CreatedAt         string   `json:"created_at"`
}

func EncodeSourceSnapshotPayload(value SourceSnapshotPayload) ([]byte, error) {
	if strings.TrimSpace(value.RepositoryRoot) == "" || strings.TrimSpace(value.WorkspaceId) == "" || strings.TrimSpace(value.WorkspaceRoot) == "" || strings.TrimSpace(value.HeadRevision) == "" || strings.TrimSpace(value.SourceStateDigest) == "" {
		return nil, fmt.Errorf("source snapshot recovery payload is incomplete")
	}
	return json.Marshal(value)
}

func DecodeSourceSnapshotPayload(payload []byte) (SourceSnapshotPayload, error) {
	var value SourceSnapshotPayload
	if err := json.Unmarshal(payload, &value); err != nil {
		return value, fmt.Errorf("decode source snapshot payload: %w", err)
	}
	if strings.TrimSpace(value.RepositoryRoot) == "" || strings.TrimSpace(value.WorkspaceId) == "" || strings.TrimSpace(value.WorkspaceRoot) == "" || strings.TrimSpace(value.HeadRevision) == "" || strings.TrimSpace(value.SourceStateDigest) == "" {
		return value, fmt.Errorf("source snapshot recovery payload is incomplete")
	}
	return value, nil
}

func EncodePatchPayload(value PatchPayload) ([]byte, error) {
	if strings.TrimSpace(value.WorkspaceId) == "" || strings.TrimSpace(value.BaseRevision) == "" || strings.TrimSpace(value.SourceStateDigest) == "" || strings.TrimSpace(value.PatchDigest) == "" || strings.TrimSpace(value.CreatedAt) == "" || len(value.Content) == 0 || len(value.ChangedPaths) == 0 {
		return nil, fmt.Errorf("patch recovery payload is incomplete")
	}
	return json.Marshal(value)
}

func DecodePatchPayload(payload []byte) (PatchPayload, error) {
	var value PatchPayload
	if err := json.Unmarshal(payload, &value); err != nil {
		return value, fmt.Errorf("decode patch payload: %w", err)
	}
	if strings.TrimSpace(value.WorkspaceId) == "" || strings.TrimSpace(value.BaseRevision) == "" || strings.TrimSpace(value.SourceStateDigest) == "" || strings.TrimSpace(value.PatchDigest) == "" || strings.TrimSpace(value.CreatedAt) == "" || len(value.Content) == 0 || len(value.ChangedPaths) == 0 {
		return value, fmt.Errorf("patch recovery payload is incomplete")
	}
	return value, nil
}
