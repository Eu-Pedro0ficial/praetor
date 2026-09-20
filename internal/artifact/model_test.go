package artifact_test

import (
	"bytes"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Eu-Pedro0ficial/praetor/internal/artifact"
	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"github.com/Eu-Pedro0ficial/praetor/internal/project"
)

func TestArtifactIdentityImmutabilityAndDigestValidation(t *testing.T) {
	projectId, _ := project.GenerateProjectID()
	id, _ := artifact.GenerateId()
	payload := []byte("authority")
	item, err := artifact.New(id, projectId, change.ChangeId("change"), artifact.KindEvidenceSet, 1, 1, "application/json", time.Now(), artifact.Producer{Component: "test", OperationId: "op"}, payload, false)
	if err != nil {
		t.Fatal(err)
	}
	payload[0] = 'X'
	copyPayload := item.Payload()
	copyPayload[0] = 'Y'
	if string(item.Payload()) != "authority" {
		t.Fatal("artifact payload is mutable")
	}
	if item.Id() == artifact.ArtifactId(item.ContentDigest()) {
		t.Fatal("ArtifactId collapsed into ContentDigest")
	}
	if _, err := artifact.Rehydrate(id, projectId, "change", item.Kind(), 1, 1, item.MediaType(), item.CreatedAt(), item.Producer(), []byte("changed"), item.ContentDigest(), item.RecordDigest()); err == nil {
		t.Fatal("corrupt payload rehydrated")
	}
}
func TestArtifactSafetyLimits(t *testing.T) {
	projectId, _ := project.GenerateProjectID()
	id, _ := artifact.GenerateId()
	producer := artifact.Producer{Component: "test"}
	large := bytes.Repeat([]byte{'x'}, int(artifact.NormalSizeLimit)+1)
	if _, err := artifact.New(id, projectId, "change", artifact.KindPatch, 1, 1, "application/octet-stream", time.Now(), producer, large, false); err == nil {
		t.Fatal("large artifact lacked explicit path")
	}
	if _, err := artifact.New(id, projectId, "change", artifact.KindPatch, 1, 1, "application/octet-stream", time.Now(), producer, large, true); err != nil {
		t.Fatalf("explicit large-artifact path rejected: %v", err)
	}
}

func TestRecoveryPayloadsPreserveExactPatchBytes(t *testing.T) {
	source := artifact.SourceSnapshotPayload{RepositoryRoot: "/tmp/repository", WorkspaceId: "proposal-0123456789abcdef0123456789abcdef", WorkspaceRoot: "/tmp/proposal", HeadRevision: "abc123", TrackedPaths: []string{"a.txt", "line\nb.txt"}, SourceStateDigest: "sha256:" + strings.Repeat("1", 64)}
	encodedSource, err := artifact.EncodeSourceSnapshotPayload(source)
	if err != nil {
		t.Fatal(err)
	}
	decodedSource, err := artifact.DecodeSourceSnapshotPayload(encodedSource)
	if err != nil || !reflect.DeepEqual(decodedSource, source) {
		t.Fatalf("source round trip=%#v error=%v", decodedSource, err)
	}
	patch := artifact.PatchPayload{WorkspaceId: source.WorkspaceId, BaseRevision: "abc123", SourceStateDigest: source.SourceStateDigest, ChangedPaths: []string{"binary.dat"}, PatchDigest: "sha256:" + strings.Repeat("2", 64), Content: []byte{0, 1, 2, 255}, CreatedAt: time.Now().UTC().Format(time.RFC3339Nano)}
	encodedPatch, err := artifact.EncodePatchPayload(patch)
	if err != nil {
		t.Fatal(err)
	}
	decodedPatch, err := artifact.DecodePatchPayload(encodedPatch)
	if err != nil || !reflect.DeepEqual(decodedPatch, patch) {
		t.Fatalf("patch round trip=%#v error=%v", decodedPatch, err)
	}
}
