package audit

import (
	"bytes"
	"crypto/rand"
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
	lockTimeout       = 5 * time.Second
	lockRetryInterval = 10 * time.Millisecond

	EventInitialization = "INITIALIZATION"
	EventProjectAttach  = "PROJECT_ATTACH"
	EventConfiguration  = "CONFIGURATION"
)

// Event is the minimal append-oriented initialization audit record for M0.1.
type Event struct {
	EventID        string         `json:"EventId"`
	EventType      string         `json:"EventType"`
	Timestamp      time.Time      `json:"Timestamp"`
	ProjectID      string         `json:"ProjectId"`
	RepositoryRoot string         `json:"RepositoryRoot"`
	SchemaVersion  int            `json:"SchemaVersion"`
	Metadata       map[string]any `json:"Metadata,omitempty"`
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

func acquireLock(dataDirectory string, lockOperation int, timeout time.Duration) (*os.File, error) {
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
	if strings.TrimSpace(dataDirectory) == "" {
		resolved, err := ResolveDataDir()
		if err != nil {
			return Event{}, err
		}
		dataDirectory = resolved
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

	lockFileHandle, err := acquireLock(dataDirectory, syscall.LOCK_EX, lockTimeout)
	if err != nil {
		return Event{}, err
	}
	defer releaseLock(lockFileHandle)

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
		RepositoryRoot: repositoryRoot,
		SchemaVersion:  schemaVersion,
		Metadata:       metadata,
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

	lockFileHandle, err := acquireLock(dataDirectory, syscall.LOCK_SH, lockTimeout)
	if err != nil {
		return nil, err
	}
	defer releaseLock(lockFileHandle)

	return readLedger(dataDirectory)
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
	if strings.TrimSpace(event.RepositoryRoot) == "" {
		return errors.New("empty RepositoryRoot")
	}
	if event.SchemaVersion != schemaVersion {
		return fmt.Errorf("unsupported SchemaVersion %d", event.SchemaVersion)
	}
	return nil
}
