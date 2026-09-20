package workflow_test

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"testing"

	"github.com/Eu-Pedro0ficial/praetor/internal/workflow"
)

func TestCoreV0WorkflowSnapshotRetainsExactBytes(t *testing.T) {
	snapshot, err := workflow.CoreV0Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	reloaded, err := workflow.ParseSnapshot(snapshot.ExactYAML())
	if err != nil {
		t.Fatal(err)
	}
	if snapshot.Digest() != reloaded.Digest() || !bytes.Equal(snapshot.ExactYAML(), reloaded.ExactYAML()) {
		t.Fatal("exact workflow bytes changed")
	}
	changed := append(snapshot.ExactYAML(), []byte("\n")...)
	other, err := workflow.ParseSnapshot(changed)
	if err != nil {
		t.Fatal(err)
	}
	if other.Digest() == snapshot.Digest() {
		t.Fatal("exact-byte workflow change retained digest")
	}
}
func TestUnsupportedWorkflowSchemaFailsClosed(t *testing.T) {
	raw := []byte("schema_version: 2\nworkflow_id: core-v0\nworkflow_version: 1.0.0\nstates: []\ntransitions: []\n")
	if _, err := workflow.ParseSnapshot(raw); err == nil {
		t.Fatal("unsupported workflow schema accepted")
	}
	digest := workflowDigest(raw)
	inspected, err := workflow.InspectSnapshot("core-v0", "1.0.0", 2, digest, raw)
	if err != nil {
		t.Fatalf("historical snapshot inspection failed: %v", err)
	}
	if inspected.Executable() || inspected.SchemaVersion() != 2 || inspected.Digest() != digest || !bytes.Equal(inspected.ExactYAML(), raw) {
		t.Fatalf("historical snapshot changed during inspection: %#v", inspected)
	}
}

func workflowDigest(raw []byte) string {
	// Obtain the canonical digest through the inspection boundary itself: an
	// intentionally bad digest reports integrity failure, including no bytes.
	// SHA-256 formatting is fixed by the public workflow contract.
	sum := sha256.Sum256(raw)
	return "sha256:" + hex.EncodeToString(sum[:])
}
