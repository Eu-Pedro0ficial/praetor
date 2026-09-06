package audit

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
)

func TestAppendPersistsHistoryAndMetadata(t *testing.T) {
	dir := t.TempDir()

	event1, err := Append(dir, EventInitialization, "proj-1", "/tmp/repo-a", map[string]any{
		"phase": "startup",
		"mode":  "status",
	})
	if err != nil {
		t.Fatalf("append first event: %v", err)
	}
	if event1.EventType != EventInitialization {
		t.Fatalf("first event type mismatch: got %q", event1.EventType)
	}
	if !strings.HasPrefix(event1.EventID, "evt-") {
		t.Fatalf("first EventId has unexpected format: %q", event1.EventID)
	}
	if event1.SchemaVersion != schemaVersion {
		t.Fatalf("first schema version mismatch: got %d", event1.SchemaVersion)
	}
	if event1.ProjectID != "proj-1" {
		t.Fatalf("first project id mismatch: got %q", event1.ProjectID)
	}
	if event1.RepositoryRoot != "/tmp/repo-a" {
		t.Fatalf("first repository root mismatch: got %q", event1.RepositoryRoot)
	}
	if event1.ChangeID != "" {
		t.Fatalf("M0.1 event unexpectedly acquired ChangeId %q", event1.ChangeID)
	}

	event2, err := Append(dir, EventProjectAttach, "proj-1", "/tmp/repo-a", map[string]any{
		"registered": true,
	})
	if err != nil {
		t.Fatalf("append second event: %v", err)
	}
	if event2.EventType != EventProjectAttach {
		t.Fatalf("second event type mismatch: got %q", event2.EventType)
	}

	events, err := Read(dir)
	if err != nil {
		t.Fatalf("read events: %v", err)
	}
	if len(events) != 2 {
		t.Fatalf("expected 2 events, got %d", len(events))
	}
	if events[0].EventID == "" || events[1].EventID == "" {
		t.Fatal("event ids should be generated")
	}
	if events[0].Timestamp.IsZero() || events[1].Timestamp.IsZero() {
		t.Fatal("timestamps should be set")
	}
	if events[0].Metadata["phase"] != "startup" {
		t.Fatalf("first event metadata missing phase: %#v", events[0].Metadata)
	}
	if events[1].Metadata["registered"] != true {
		t.Fatalf("second event metadata missing registered flag: %#v", events[1].Metadata)
	}
}

func TestAppendChangePersistsTopLevelLifecycleLinkage(t *testing.T) {
	dir := t.TempDir()
	metadata := map[string]any{
		"previous_state":       "created",
		"resulting_state":      "planned",
		"transition_timestamp": "2026-08-31T12:00:01Z",
		"context":              "planning established",
	}

	event, err := AppendChange(
		dir,
		EventChangeTransition,
		"project-1",
		"change-1",
		"/tmp/repo",
		metadata,
	)
	if err != nil {
		t.Fatalf("AppendChange() error = %v", err)
	}
	if event.ChangeID != "change-1" || event.ProjectID != "project-1" {
		t.Fatalf("AppendChange() identity linkage = %#v", event)
	}

	events, err := Read(dir)
	if err != nil {
		t.Fatalf("Read() error = %v", err)
	}
	if len(events) != 1 {
		t.Fatalf("Read() returned %d events, want 1", len(events))
	}
	stored := events[0]
	if stored.EventType != EventChangeTransition || stored.ChangeID != "change-1" || stored.ProjectID != "project-1" {
		t.Fatalf("stored Change event = %#v", stored)
	}
	for key, want := range metadata {
		if stored.Metadata[key] != want {
			t.Fatalf("stored metadata[%q] = %#v, want %#v", key, stored.Metadata[key], want)
		}
	}
}

func TestAppendChangeRejectsMissingChangeId(t *testing.T) {
	if _, err := AppendChange(
		t.TempDir(),
		EventChangeCreated,
		"project-1",
		" ",
		"/tmp/repo",
		nil,
	); err == nil {
		t.Fatal("AppendChange() accepted an empty ChangeId")
	}
}

func TestAppendCannotWriteChangeEventWithoutChangeId(t *testing.T) {
	eventTypes := []string{
		EventChangeCreated,
		EventChangeTransition,
		EventSourceSnapshotCaptured,
		EventImpactAnalysisProduced,
		EventChangeSurfaceEstablished,
		EventChangeSurfaceValidated,
		EventChangeSurfaceViolation,
		EventProposalWorkspaceCreated,
		EventPatchExtracted,
		EventPatchSurfaceValidated,
		EventPatchRejected,
		EventProposalWorkspaceDiscarded,
		EventProviderExecutionStarted,
		EventProviderExecutionCompleted,
		EventProviderExecutionFailed,
		EventVerificationPlanningStarted,
		EventVerificationPlanningCompleted,
		EventVerificationPlanningFailed,
		EventVerificationStarted,
		EventVerificationStepCompleted,
		EventVerificationCompleted,
		EventVerificationFailed,
		EventHumanDecisionRecorded,
		EventCanonicalApplicationStarted,
		EventCanonicalApplicationCompleted,
		EventCanonicalApplicationFailed,
		EventChangeClosureRecorded,
	}
	for _, eventType := range eventTypes {
		t.Run(eventType, func(t *testing.T) {
			dir := t.TempDir()
			if _, err := Append(
				dir,
				eventType,
				"project-1",
				"/tmp/repo",
				nil,
			); err == nil {
				t.Fatalf("Append() wrote %s without ChangeId", eventType)
			}
			events, err := Read(dir)
			if err != nil {
				t.Fatalf("Read() error = %v", err)
			}
			if len(events) != 0 {
				t.Fatalf("invalid Change event reached the ledger: %#v", events)
			}
		})
	}
}

func TestReadRejectsMalformedExistingEvent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ledgerFile)
	if err := os.WriteFile(path, []byte("{not-valid-json}\n"), 0o600); err != nil {
		t.Fatalf("write malformed ledger: %v", err)
	}

	_, err := Read(dir)
	if err == nil {
		t.Fatal("expected malformed ledger read to fail")
	}
}

func TestReadRejectsIncompleteTrailingEvent(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, ledgerFile)
	event := Event{
		EventID:        "evt-1234567890abcdef",
		EventType:      EventInitialization,
		Timestamp:      time.Now().UTC(),
		ProjectID:      "proj-1",
		RepositoryRoot: "/tmp/repo",
		SchemaVersion:  schemaVersion,
	}
	payload, err := json.Marshal(event)
	if err != nil {
		t.Fatalf("marshal event: %v", err)
	}
	if err := os.WriteFile(path, payload, 0o600); err != nil {
		t.Fatalf("write incomplete ledger: %v", err)
	}

	if _, err := Read(dir); err == nil {
		t.Fatal("expected incomplete trailing event to fail")
	}
}

func TestReadRejectsIncompleteEventFields(t *testing.T) {
	tests := []struct {
		name  string
		event Event
	}{
		{name: "missing event id", event: Event{EventType: EventInitialization, Timestamp: time.Now().UTC(), ProjectID: "proj-1", RepositoryRoot: "/tmp/repo", SchemaVersion: schemaVersion}},
		{name: "missing event type", event: Event{EventID: "evt-1234567890abcdef", Timestamp: time.Now().UTC(), ProjectID: "proj-1", RepositoryRoot: "/tmp/repo", SchemaVersion: schemaVersion}},
		{name: "missing timestamp", event: Event{EventID: "evt-1234567890abcdef", EventType: EventInitialization, ProjectID: "proj-1", RepositoryRoot: "/tmp/repo", SchemaVersion: schemaVersion}},
		{name: "missing project id", event: Event{EventID: "evt-1234567890abcdef", EventType: EventInitialization, Timestamp: time.Now().UTC(), RepositoryRoot: "/tmp/repo", SchemaVersion: schemaVersion}},
		{name: "missing ChangeId on creation", event: Event{EventID: "evt-1234567890abcdef", EventType: EventChangeCreated, Timestamp: time.Now().UTC(), ProjectID: "proj-1", RepositoryRoot: "/tmp/repo", SchemaVersion: schemaVersion}},
		{name: "missing ChangeId on transition", event: Event{EventID: "evt-1234567890abcdef", EventType: EventChangeTransition, Timestamp: time.Now().UTC(), ProjectID: "proj-1", RepositoryRoot: "/tmp/repo", SchemaVersion: schemaVersion}},
		{name: "missing repository root", event: Event{EventID: "evt-1234567890abcdef", EventType: EventInitialization, Timestamp: time.Now().UTC(), ProjectID: "proj-1", SchemaVersion: schemaVersion}},
		{name: "missing schema version", event: Event{EventID: "evt-1234567890abcdef", EventType: EventInitialization, Timestamp: time.Now().UTC(), ProjectID: "proj-1", RepositoryRoot: "/tmp/repo"}},
		{name: "unsupported schema version", event: Event{EventID: "evt-1234567890abcdef", EventType: EventInitialization, Timestamp: time.Now().UTC(), ProjectID: "proj-1", RepositoryRoot: "/tmp/repo", SchemaVersion: schemaVersion + 1}},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			dir := t.TempDir()
			payload, err := json.Marshal(test.event)
			if err != nil {
				t.Fatalf("marshal event: %v", err)
			}
			payload = append(payload, '\n')
			if err := os.WriteFile(filepath.Join(dir, ledgerFile), payload, 0o600); err != nil {
				t.Fatalf("write ledger: %v", err)
			}

			if _, err := Read(dir); err == nil {
				t.Fatal("expected incomplete event to fail")
			}
		})
	}
}

func TestAppendRejectsMissingRequiredContext(t *testing.T) {
	tests := []struct {
		name           string
		eventType      string
		projectID      string
		repositoryRoot string
	}{
		{name: "event type", projectID: "proj-1", repositoryRoot: "/tmp/repo"},
		{name: "project id", eventType: EventInitialization, repositoryRoot: "/tmp/repo"},
		{name: "repository root", eventType: EventInitialization, projectID: "proj-1"},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := Append(t.TempDir(), test.eventType, test.projectID, test.repositoryRoot, nil); err == nil {
				t.Fatalf("expected missing %s to fail", test.name)
			}
		})
	}
}

func TestAcquireLockTimesOut(t *testing.T) {
	dir := t.TempDir()
	lockPath := filepath.Join(dir, lockFile)
	heldLock, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		t.Fatalf("open held lock: %v", err)
	}
	defer heldLock.Close()
	if err := syscall.Flock(int(heldLock.Fd()), syscall.LOCK_EX); err != nil {
		t.Fatalf("acquire held lock: %v", err)
	}
	defer syscall.Flock(int(heldLock.Fd()), syscall.LOCK_UN)

	lockHandle, err := acquireLock(dir, syscall.LOCK_EX, 20*time.Millisecond)
	if lockHandle != nil {
		releaseLock(lockHandle)
		t.Fatal("expected lock acquisition to time out")
	}
	if err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("expected timeout error, got %v", err)
	}
}

func TestAppendConcurrentWritersProduceStableHistory(t *testing.T) {
	dir := t.TempDir()
	const workers = 12

	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			_, err := Append(dir, EventConfiguration, "proj-1", "/tmp/repo", map[string]any{
				"index": i,
				"run":   time.Now().UnixNano(),
			})
			if err != nil {
				panic(err)
			}
		}(i)
	}
	wg.Wait()

	events, err := Read(dir)
	if err != nil {
		t.Fatalf("read concurrent events: %v", err)
	}
	if len(events) != workers {
		t.Fatalf("expected %d events, got %d", workers, len(events))
	}

	seen := map[string]bool{}
	for _, event := range events {
		if event.EventType != EventConfiguration {
			t.Fatalf("unexpected type %q", event.EventType)
		}
		if seen[event.EventID] {
			t.Fatalf("duplicate event id %q", event.EventID)
		}
		seen[event.EventID] = true
	}
}

func TestAppendPropagatesPersistenceFailure(t *testing.T) {
	root := t.TempDir()
	filePath := filepath.Join(root, "not-a-directory")
	if err := os.WriteFile(filePath, []byte("x"), 0o600); err != nil {
		t.Fatalf("write blocking file: %v", err)
	}
	if _, err := Append(filePath, EventInitialization, "proj-1", "/tmp/repo", nil); err == nil {
		t.Fatal("expected append to fail when the target path is not a directory")
	}
}
