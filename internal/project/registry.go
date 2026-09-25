package project

import (
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"syscall"
	"time"
)

const (
	schemaVersion      = 1
	registryFile       = "project-registry.json"
	registryLockName   = "project-registry.lock"
	registryLockPeriod = 5 * time.Second
)

// ProjectId is the opaque logical identifier for a Praetor project.
type ProjectId string

// Registration stores the small local metadata association required by C02.
type Registration struct {
	ProjectId      ProjectId `json:"ProjectId"`
	RepositoryRoot string    `json:"RepositoryRoot"`
	SchemaVersion  int       `json:"SchemaVersion"`
	CreatedAt      time.Time `json:"CreatedAt"`
}

// IsValid validates the UUIDv7 shape required for C02.
func (p ProjectId) IsValid() bool {
	if len(p) != 36 {
		return false
	}

	matcher := regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-7[0-9a-f]{3}-[89ab][0-9a-f]{3}-[0-9a-f]{12}$`)
	return matcher.MatchString(string(p))
}

// GenerateProjectID creates a local UUIDv7 ProjectId using standard-library primitives only.
func GenerateProjectID() (ProjectId, error) {
	var raw [16]byte
	if _, err := rand.Read(raw[:]); err != nil {
		return "", fmt.Errorf("generate uuidv7 random bytes: %w", err)
	}

	nowMillis := uint64(time.Now().UnixMilli())
	raw[0] = byte((nowMillis >> 40) & 0xff)
	raw[1] = byte((nowMillis >> 32) & 0xff)
	raw[2] = byte((nowMillis >> 24) & 0xff)
	raw[3] = byte((nowMillis >> 16) & 0xff)
	raw[4] = byte((nowMillis >> 8) & 0xff)
	raw[5] = byte(nowMillis & 0xff)

	// UUIDv7: version 7 in the 3rd group high nibble.
	raw[6] = (raw[6] & 0x0f) | 0x70
	// RFC 4122 variant in the 4th group high bits.
	raw[8] = (raw[8] & 0x3f) | 0x80

	projectID := ProjectId(fmt.Sprintf("%x-%x-%x-%x-%x",
		raw[0:4],
		raw[4:6],
		raw[6:8],
		raw[8:10],
		raw[10:16],
	))
	if !projectID.IsValid() {
		return "", fmt.Errorf("generated ProjectId is invalid: %s", projectID)
	}
	return projectID, nil
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

// ResolveStateDir resolves transient operational state separately from DATA.
func ResolveStateDir() (string, error) {
	if envPath := strings.TrimSpace(os.Getenv("XDG_STATE_HOME")); envPath != "" {
		return filepath.Join(envPath, "praetor"), nil
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home dir: %w", err)
	}
	return filepath.Join(homeDir, ".local", "state", "praetor"), nil
}

// ResolveCacheDir resolves replaceable user-local cache storage separately
// from durable DATA and operational STATE.
func ResolveCacheDir() (string, error) {
	if envPath := strings.TrimSpace(os.Getenv("XDG_CACHE_HOME")); envPath != "" {
		return filepath.Join(envPath, "praetor"), nil
	}
	homeDir, err := os.UserHomeDir()
	if err != nil {
		return "", fmt.Errorf("resolve user home dir: %w", err)
	}
	return filepath.Join(homeDir, ".cache", "praetor"), nil
}

// RegistryPath returns the JSON file path used for the local Project Registry V1.
func RegistryPath() (string, error) {
	dataDir, err := ResolveDataDir()
	if err != nil {
		return "", err
	}
	return filepath.Join(dataDir, registryFile), nil
}

// LoadRegistrations reads the Project Registry file if it exists.
func LoadRegistrations(dataDir string) ([]Registration, error) {
	path := filepath.Join(dataDir, registryFile)
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("read registry %q: %w", path, err)
	}
	if len(strings.TrimSpace(string(data))) == 0 {
		return nil, nil
	}

	var registrations []Registration
	if err := json.Unmarshal(data, &registrations); err != nil {
		return nil, fmt.Errorf("decode registry %q: %w", path, err)
	}
	for i, reg := range registrations {
		if reg.ProjectId == "" || !reg.ProjectId.IsValid() {
			return nil, fmt.Errorf("registry entry %d has invalid ProjectId", i)
		}
		if reg.RepositoryRoot == "" {
			return nil, fmt.Errorf("registry entry %d has empty RepositoryRoot", i)
		}
		if reg.SchemaVersion != schemaVersion {
			return nil, fmt.Errorf("registry entry %d has unsupported schema version %d", i, reg.SchemaVersion)
		}
		if reg.CreatedAt.IsZero() {
			return nil, fmt.Errorf("registry entry %d has zero CreatedAt", i)
		}
	}
	return registrations, nil
}

// SaveRegistrations persists Project Registry V1 with atomic replace semantics.
func SaveRegistrations(dataDir string, registrations []Registration) error {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return fmt.Errorf("create data dir %q: %w", dataDir, err)
	}

	path := filepath.Join(dataDir, registryFile)
	payload, err := json.MarshalIndent(registrations, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal registry for %q: %w", path, err)
	}
	payload = append(payload, '\n')

	tmpPath := path + ".tmp"
	if err := os.WriteFile(tmpPath, payload, 0o600); err != nil {
		return fmt.Errorf("write registry temp file %q: %w", tmpPath, err)
	}
	if err := os.Rename(tmpPath, path); err != nil {
		return fmt.Errorf("atomic replace registry %q: %w", path, err)
	}
	return nil
}

func acquireRegistryLock(dataDir string, timeout time.Duration) (*os.File, error) {
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		return nil, fmt.Errorf("create data dir %q: %w", dataDir, err)
	}

	lockPath := filepath.Join(dataDir, registryLockName)
	lockFile, err := os.OpenFile(lockPath, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, fmt.Errorf("open registry lock %q: %w", lockPath, err)
	}

	deadline := time.Now().Add(timeout)
	for {
		flockErr := syscall.Flock(int(lockFile.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if flockErr == nil {
			return lockFile, nil
		}
		if !errors.Is(flockErr, syscall.EAGAIN) && !errors.Is(flockErr, syscall.EWOULDBLOCK) {
			_ = lockFile.Close()
			return nil, fmt.Errorf("acquire registry lock %q: %w", lockPath, flockErr)
		}
		if time.Now().After(deadline) {
			_ = lockFile.Close()
			return nil, fmt.Errorf("acquire registry lock %q: timed out", lockPath)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

// EnsureRegistration ensures a local registration exists for the given repository root.
func EnsureRegistration(repoRoot string) (Registration, error) {
	resolvedRoot, err := filepath.Abs(repoRoot)
	if err != nil {
		return Registration{}, fmt.Errorf("resolve repository root %q: %w", repoRoot, err)
	}

	dataDir, err := ResolveDataDir()
	if err != nil {
		return Registration{}, err
	}

	lockFile, err := acquireRegistryLock(dataDir, registryLockPeriod)
	if err != nil {
		return Registration{}, err
	}
	defer func() {
		_ = syscall.Flock(int(lockFile.Fd()), syscall.LOCK_UN)
		_ = lockFile.Close()
	}()

	registrations, err := LoadRegistrations(dataDir)
	if err != nil {
		return Registration{}, err
	}
	for _, reg := range registrations {
		if reg.RepositoryRoot == resolvedRoot {
			return reg, nil
		}
	}

	projectID, err := GenerateProjectID()
	if err != nil {
		return Registration{}, err
	}

	newReg := Registration{
		ProjectId:      projectID,
		RepositoryRoot: resolvedRoot,
		SchemaVersion:  schemaVersion,
		CreatedAt:      time.Now().UTC(),
	}
	registrations = append(registrations, newReg)
	if err := SaveRegistrations(dataDir, registrations); err != nil {
		return Registration{}, err
	}
	return newReg, nil
}
