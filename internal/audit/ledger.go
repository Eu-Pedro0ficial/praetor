package audit

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const (
	schemaVersion     = 1
	ledgerFile        = "audit.log"
	lockFile          = "audit.lock"
	retirementFile    = "audit-migration-v1.json"
	lockTimeout       = 5 * time.Second
	lockRetryInterval = 10 * time.Millisecond

	EventInitialization                   = "INITIALIZATION"
	EventProjectAttach                    = "PROJECT_ATTACH"
	EventConfiguration                    = "CONFIGURATION"
	EventChangeCreated                    = "CHANGE_CREATED"
	EventChangeTransition                 = "CHANGE_TRANSITION"
	EventSourceSnapshotCaptured           = "SOURCE_SNAPSHOT_CAPTURED"
	EventImpactAnalysisProduced           = "IMPACT_ANALYSIS_PRODUCED"
	EventChangeSurfaceEstablished         = "CHANGE_SURFACE_ESTABLISHED"
	EventChangeSurfaceValidated           = "CHANGE_SURFACE_VALIDATED"
	EventChangeSurfaceViolation           = "CHANGE_SURFACE_VIOLATION"
	EventProposalWorkspaceCreated         = "PROPOSAL_WORKSPACE_CREATED"
	EventPatchExtracted                   = "PATCH_EXTRACTED"
	EventPatchSurfaceValidated            = "PATCH_SURFACE_VALIDATED"
	EventPatchRejected                    = "PATCH_REJECTED"
	EventProposalWorkspaceDiscarded       = "PROPOSAL_WORKSPACE_DISCARDED"
	EventProviderExecutionStarted         = "PROVIDER_EXECUTION_STARTED"
	EventProviderExecutionCompleted       = "PROVIDER_EXECUTION_COMPLETED"
	EventProviderExecutionFailed          = "PROVIDER_EXECUTION_FAILED"
	EventVerificationPlanningStarted      = "VERIFICATION_PLANNING_STARTED"
	EventVerificationPlanningCompleted    = "VERIFICATION_PLANNING_COMPLETED"
	EventVerificationPlanningFailed       = "VERIFICATION_PLANNING_FAILED"
	EventVerificationStarted              = "VERIFICATION_STARTED"
	EventVerificationStepCompleted        = "VERIFICATION_STEP_COMPLETED"
	EventVerificationCompleted            = "VERIFICATION_COMPLETED"
	EventVerificationFailed               = "VERIFICATION_FAILED"
	EventHumanDecisionRecorded            = "HUMAN_DECISION_RECORDED"
	EventCanonicalApplicationStarted      = "CANONICAL_APPLICATION_STARTED"
	EventCanonicalApplicationCompleted    = "CANONICAL_APPLICATION_COMPLETED"
	EventCanonicalApplicationFailed       = "CANONICAL_APPLICATION_FAILED"
	EventChangeClosureRecorded            = "CHANGE_CLOSURE_RECORDED"
	EventPolicyDecisionRecorded           = "POLICY_DECISION_RECORDED"
	EventPolicyExceptionCandidateRecorded = "POLICY_EXCEPTION_CANDIDATE_RECORDED"
	EventArtifactCommitted                = "ARTIFACT_COMMITTED"
	EventOperationRecovered               = "OPERATION_RECOVERED"
)

// Event is one append-oriented local runtime audit record.
type Event struct {
	EventID        string         `json:"EventId"`
	EventType      string         `json:"EventType"`
	Timestamp      time.Time      `json:"Timestamp"`
	ProjectID      string         `json:"ProjectId"`
	ChangeID       string         `json:"ChangeId,omitempty"`
	RepositoryRoot string         `json:"RepositoryRoot"`
	SchemaVersion  int            `json:"SchemaVersion"`
	Metadata       map[string]any `json:"Metadata,omitempty"`
}

// LegacyRecord preserves the exact historical JSONL bytes and order.
type LegacyRecord struct {
	Event    Event
	RawJSON  []byte
	Sequence uint64
}

type retirementManifest struct {
	SchemaVersion int       `json:"SchemaVersion"`
	SourceDigest  string    `json:"SourceDigest"`
	EventCount    int       `json:"EventCount"`
	CompletedAt   time.Time `json:"CompletedAt"`
}

// ResolveDataDir resolves the local durable data directory for Praetor metadata.
func ResolveDataDir() (string, error) {
	if envPath := strings.TrimSpace(os.Getenv("XDG_DATA_HOME")); envPath != "" {
		return filepath.Join(envPath, "praetor"), nil
	}

	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home dir: %w", err)
	}
	return filepath.Join(homeDir, ".local", "share", "praetor"), nil
}

// LedgerPath returns the append-only audit ledger path.
func LedgerPath() (string, error) {
	dataDir, err := ResolveDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dataDir, ledgerFile), nil
}

func newEventID() (string, error) {
	var randomBytes [16]byte
	if _, err := rand.Read(randomBytes[:]); err != nil {
		return "", fmt.Errorf("generate audit EventId: %w", err)
	}
	return "evt-" + hex.EncodeToString(randomBytes[:]), nil
}

// NewEvent creates and validates a bounded audit event for a non-legacy
// durable ledger adapter. It does not write the historical JSONL ledger.
func NewEvent(eventType, projectID, changeID, repositoryRoot string, metadata map[string]any, occurredAt time.Time) (Event, error) {
	if occurredAt.IsZero() {
		return Event{}, errors.New("audit timestamp is required")
	}
	eventID, err := newEventID()
	if err != nil {
		return Event{}, err
	}
	event := Event{EventID: eventID, EventType: eventType, Timestamp: occurredAt.UTC(), ProjectID: projectID, ChangeID: changeID, RepositoryRoot: repositoryRoot, SchemaVersion: schemaVersion, Metadata: metadata}
	if err := validateEvent(event); err != nil {
		return Event{}, fmt.Errorf("validate audit event: %w", err)
	}
	return event, nil
}

// ValidateEvent validates an event before a durable adapter accepts it.
func ValidateEvent(event Event) error { return validateEvent(event) }

func acquireLock(dataDirectory string, lockOperation int, timeout time.Duration) (*os.File, error) {
	return acquireLockWithContention(dataDirectory, lockOperation, timeout, nil)
}

func acquireLockWithContention(dataDirectory string, lockOperation int, timeout time.Duration, onContention func()) (*os.File, error) {
	if err := os.MkdirAll(dataDirectory, 0o755); err != nil {
		return nil, fmt.Errorf("create data dir %q: %w", dataDirectory, err)
	}

	lockPath := filepath.Join(dataDirectory, lockFile)
	lockFileHandle, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open audit lock %q: %w", lockPath, err)
	}

	deadline := time.Now().Add(timeout)
	for {
		lockError := syscall.Flock(int(lockFileHandle.Fd()), lockOperation|syscall.LOCK_NB)
		if lockError == nil {
			return lockFileHandle, nil
		}
		if !errors.Is(lockError, syscall.EAGAIN) && !errors.Is(lockError, syscall.EWOULDBLOCK) {
			_ = lockFileHandle.Close()
			return nil, fmt.Errorf("acquire audit lock %q: %w", lockPath, lockError)
		}
		if onContention != nil {
			onContention()
			onContention = nil
		}
		if time.Now().After(deadline) {
			_ = lockFileHandle.Close()
			return nil, fmt.Errorf("acquire audit lock %q: timed out", lockPath)
		}
		time.Sleep(lockRetryInterval)
	}
}

func releaseLock(lockFile *os.File) {
	if lockFile == nil {
		return
	}
	_ = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)
	_ = lockFile.Close()
}

// Append records a single append-only audit event to the local ledger.
func Append(dataDirectory string, eventType string, projectID string, repositoryRoot string, metadata map[string]any) (Event, error) {
	return appendEvent(dataDirectory, eventType, projectID, "", repositoryRoot, metadata)
}

// AppendChange records one append-only Change lifecycle event.
func AppendChange(dataDirectory string, eventType string, projectID string, changeID string, repositoryRoot string, metadata map[string]any) (Event, error) {
	if strings.TrimSpace(changeID) == "" {
		return Event{}, errors.New("audit ChangeId is required")
	}
	return appendEvent(dataDirectory, eventType, projectID, changeID, repositoryRoot, metadata)
}

func appendEvent(dataDirectory string, eventType string, projectID string, changeID string, repositoryRoot string, metadata map[string]any) (Event, error) {
	return appendEventWithLockHooks(dataDirectory, eventType, projectID, changeID, repositoryRoot, metadata, nil, nil)
}

// appendEventWithLockHooks retains private synchronization seams for the
// migration race regression; production callers pass no hooks.
func appendEventWithLockHooks(dataDirectory string, eventType string, projectID string, changeID string, repositoryRoot string, metadata map[string]any, beforeLock, onContention func()) (Event, error) {
	if strings.TrimSpace(dataDirectory) == "" {
		resolved, err := ResolveDataDir()
		if err != nil {
			return Event{}, err
		}
		dataDirectory = resolved
	}
	if _, err := os.Stat(filepath.Join(dataDirectory, retirementFile)); err == nil {
		return Event{}, errors.New("legacy audit writer is retired; use the Project AuditLedger")
	} else if !os.IsNotExist(err) {
		return Event{}, fmt.Errorf("inspect legacy audit retirement: %w", err)
	}
	if strings.TrimSpace(eventType) == "" {
		return Event{}, errors.New("audit event type is required")
	}
	if strings.TrimSpace(projectID) == "" {
		return Event{}, errors.New("audit ProjectId is required")
	}
	if strings.TrimSpace(repositoryRoot) == "" {
		return Event{}, errors.New("audit RepositoryRoot is required")
	}
	if beforeLock != nil {
		beforeLock()
	}

	lockFileHandle, err := acquireLockWithContention(dataDirectory, syscall.LOCK_EX, lockTimeout, onContention)
	if err != nil {
		return Event{}, err
	}
	defer releaseLock(lockFileHandle)
	// Migration publishes retirement while holding this same lock. A writer
	// that checked before waiting must recheck after it acquires exclusion.
	if _, err := os.Stat(filepath.Join(dataDirectory, retirementFile)); err == nil {
		return Event{}, errors.New("legacy audit writer is retired; use the Project AuditLedger")
	} else if !os.IsNotExist(err) {
		return Event{}, fmt.Errorf("inspect legacy audit retirement: %w", err)
	}

	if _, err := readLedger(dataDirectory); err != nil {
		return Event{}, err
	}

	eventID, err := newEventID()
	if err != nil {
		return Event{}, err
	}

	event := Event{
		EventID:        eventID,
		EventType:      eventType,
		Timestamp:      time.Now().UTC(),
		ProjectID:      projectID,
		ChangeID:       changeID,
		RepositoryRoot: repositoryRoot,
		SchemaVersion:  schemaVersion,
		Metadata:       metadata,
	}
	if err := validateEvent(event); err != nil {
		return Event{}, fmt.Errorf("validate audit event: %w", err)
	}

	payload, err := json.Marshal(event)
	if err != nil {
		return Event{}, fmt.Errorf("marshal audit event: %w", err)
	}

	path := filepath.Join(dataDirectory, ledgerFile)
	file, err := os.OpenFile(path, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0o600)
	if err != nil {
		return Event{}, fmt.Errorf("open audit ledger %q: %w", path, err)
	}

	if _, err = file.Write(append(payload, '\n')); err != nil {
		_ = file.Close()
		return Event{}, fmt.Errorf("write audit event %q: %w", event.EventType, err)
	}
	if err := file.Sync(); err != nil {
		_ = file.Close()
		return Event{}, fmt.Errorf("sync audit ledger %q: %w", path, err)
	}
	if err := file.Close(); err != nil {
		return Event{}, fmt.Errorf("close audit ledger %q: %w", path, err)
	}
	return event, nil
}

// MigrateLegacy executes one exclusive, validated migration callback and then
// atomically retires the JSONL writer. The source ledger is never modified.
func MigrateLegacy(dataDirectory string, migrate func([]LegacyRecord, string) error) error {
	if migrate == nil {
		return errors.New("legacy audit migration callback is required")
	}
	lock, err := acquireLock(dataDirectory, syscall.LOCK_EX, lockTimeout)
	if err != nil {
		return err
	}
	defer releaseLock(lock)
	manifestPath := filepath.Join(dataDirectory, retirementFile)
	if _, err := os.Stat(manifestPath); err == nil {
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	records, sourceDigest, err := readLegacyRecords(dataDirectory)
	if err != nil {
		return err
	}
	if err := migrate(records, sourceDigest); err != nil {
		return err
	}
	legacyPath := filepath.Join(dataDirectory, ledgerFile)
	if _, err := os.Stat(legacyPath); os.IsNotExist(err) {
		file, createError := os.OpenFile(legacyPath, os.O_CREATE|os.O_WRONLY, 0o600)
		if createError != nil {
			return fmt.Errorf("create empty legacy audit evidence: %w", createError)
		}
		if closeError := file.Close(); closeError != nil {
			return closeError
		}
	}
	manifest := retirementManifest{SchemaVersion: 1, SourceDigest: sourceDigest, EventCount: len(records), CompletedAt: time.Now().UTC()}
	payload, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	payload = append(payload, '\n')
	temporary := manifestPath + ".tmp"
	if err := os.WriteFile(temporary, payload, 0o600); err != nil {
		return fmt.Errorf("write audit migration manifest: %w", err)
	}
	if err := os.Rename(temporary, manifestPath); err != nil {
		return fmt.Errorf("publish audit migration manifest: %w", err)
	}
	return nil
}

func readLegacyRecords(dataDirectory string) ([]LegacyRecord, string, error) {
	path := filepath.Join(dataDirectory, ledgerFile)
	info, err := os.Lstat(path)
	if err != nil {
		if os.IsNotExist(err) {
			empty := sha256.Sum256(nil)
			return nil, "sha256:" + hex.EncodeToString(empty[:]), nil
		}
		return nil, "", err
	}
	if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
		return nil, "", fmt.Errorf("legacy audit source is not a regular file")
	}
	if info.Mode().Perm()&0o077 != 0 {
		return nil, "", fmt.Errorf("legacy audit source permissions %04o are not user-restricted", info.Mode().Perm())
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, "", err
	}
	sum := sha256.Sum256(data)
	sourceDigest := "sha256:" + hex.EncodeToString(sum[:])
	if len(data) == 0 {
		return nil, sourceDigest, nil
	}
	if data[len(data)-1] != '\n' {
		return nil, "", fmt.Errorf("audit ledger %q has an incomplete trailing event", path)
	}
	lines := bytes.Split(data[:len(data)-1], []byte{'\n'})
	records := make([]LegacyRecord, 0, len(lines))
	for index, line := range lines {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var event Event
		if err := json.Unmarshal(line, &event); err != nil {
			return nil, "", fmt.Errorf("decode audit ledger line %d: %w", index+1, err)
		}
		if err := validateEvent(event); err != nil {
			return nil, "", fmt.Errorf("audit ledger line %d: %w", index+1, err)
		}
		records = append(records, LegacyRecord{Event: event, RawJSON: append([]byte(nil), line...), Sequence: uint64(index + 1)})
	}
	return records, sourceDigest, nil
}

// Read reads the append-only audit ledger and returns all well-formed events.
func Read(dataDirectory string) ([]Event, error) {
	if strings.TrimSpace(dataDirectory) == "" {
		resolved, err := ResolveDataDir()
		if err != nil {
			return nil, err
		}
		dataDirectory = resolved
	}

	if _, err := os.Stat(dataDirectory); err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("inspect audit data dir %q: %w", dataDirectory, err)
	}
	if _, err := os.Stat(filepath.Join(dataDirectory, retirementFile)); err == nil {
		return nil, fmt.Errorf("legacy audit is retired; read durable audit through the persistence adapter")
	} else if !os.IsNotExist(err) {
		return nil, err
	}

	lockFileHandle, err := acquireLock(dataDirectory, syscall.LOCK_SH, lockTimeout)
	if err != nil {
		return nil, err
	}
	defer releaseLock(lockFileHandle)

	return readLedger(dataDirectory)
}

// ValidateRetiredLegacy proves that retained migration evidence is unchanged.
func ValidateRetiredLegacy(dataDirectory string) error {
	payload, err := os.ReadFile(filepath.Join(dataDirectory, retirementFile))
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var manifest retirementManifest
	if err := json.Unmarshal(payload, &manifest); err != nil {
		return fmt.Errorf("decode audit retirement manifest: %w", err)
	}
	if manifest.SchemaVersion != 1 {
		return fmt.Errorf("unsupported audit retirement schema %d", manifest.SchemaVersion)
	}
	if _, err := readLedger(dataDirectory); err != nil {
		return err
	}
	data, err := os.ReadFile(filepath.Join(dataDirectory, ledgerFile))
	if err != nil {
		return fmt.Errorf("read retained legacy audit evidence: %w", err)
	}
	sum := sha256.Sum256(data)
	if actual := "sha256:" + hex.EncodeToString(sum[:]); actual != manifest.SourceDigest {
		return fmt.Errorf("retained legacy audit source digest mismatch")
	}
	return nil
}

func readLedger(dataDirectory string) ([]Event, error) {
	path := filepath.Join(dataDirectory, ledgerFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read audit ledger %q: %w", path, err)
	}

	if len(data) == 0 {
		return nil, nil
	}
	if data[len(data)-1] != '\n' {
		return nil, fmt.Errorf("audit ledger %q has an incomplete trailing event", path)
	}

	lines := bytes.Split(data[:len(data)-1], []byte{'\n'})
	events := make([]Event, 0, len(lines))
	for i, line := range lines {
		if len(bytes.TrimSpace(line)) == 0 {
			continue
		}
		var event Event
		if err := json.Unmarshal(line, &event); err != nil {
			return nil, fmt.Errorf("decode audit ledger line %d: %w", i+1, err)
		}
		if err := validateEvent(event); err != nil {
			return nil, fmt.Errorf("audit ledger line %d: %w", i+1, err)
		}
		events = append(events, event)
	}
	return events, nil
}

func validateEvent(event Event) error {
	if strings.TrimSpace(event.EventID) == "" {
		return errors.New("empty EventId")
	}
	if strings.TrimSpace(event.EventType) == "" {
		return errors.New("empty EventType")
	}
	if event.Timestamp.IsZero() {
		return errors.New("zero Timestamp")
	}
	if strings.TrimSpace(event.ProjectID) == "" {
		return errors.New("empty ProjectId")
	}
	if eventRequiresChangeId(event.EventType) && strings.TrimSpace(event.ChangeID) == "" {
		return errors.New("empty ChangeId")
	}
	if strings.TrimSpace(event.RepositoryRoot) == "" {
		return errors.New("empty RepositoryRoot")
	}
	if event.SchemaVersion != schemaVersion {
		return fmt.Errorf("unsupported SchemaVersion %d", event.SchemaVersion)
	}
	return nil
}

func eventRequiresChangeId(eventType string) bool {
	switch eventType {
	case EventChangeCreated,
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
		EventPolicyDecisionRecorded,
		EventPolicyExceptionCandidateRecorded:
		// M1.1 artifact envelopes are Change-scoped authority.
		fallthrough
	case EventArtifactCommitted, EventOperationRecovered:
		return true
	default:
		return false
	}
}
