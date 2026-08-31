package project

import (
	"encoding/hex"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Eu-Pedro0ficial/praetor/internal/repository"
)

func initGitRepo(t *testing.T) string {
	t.Helper()
	repoDir := t.TempDir()
	cmd := exec.Command("git", "-C", repoDir, "init")
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("git init failed: %v: %s", err, string(out))
	}
	return repoDir
}

func parseUUIDv7Bytes(raw string) ([]byte, error) {
	if len(raw) != 36 {
		return nil, fmt.Errorf("UUID length mismatch: %d", len(raw))
	}
	parts := strings.Split(raw, "-")
	if len(parts) != 5 {
		return nil, fmt.Errorf("UUID parts mismatch: %d", len(parts))
	}
	if parts[2][:1] != "7" {
		return nil, fmt.Errorf("UUID version mismatch: %q", parts[2])
	}
	if !regexp.MustCompile(`^[0-9a-f]{8}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{4}-[0-9a-f]{12}$`).MatchString(raw) {
		return nil, fmt.Errorf("UUID format mismatch: %q", raw)
	}

	decoded, err := hex.DecodeString(strings.ReplaceAll(raw, "-", ""))
	if err != nil {
		return nil, fmt.Errorf("decode UUID: %w", err)
	}
	return decoded, nil
}

func TestGenerateProjectID_UUIDv7Shape(t *testing.T) {
	id, err := GenerateProjectID()
	if err != nil {
		t.Fatalf("GenerateProjectID() error = %v", err)
	}
	if !id.IsValid() {
		t.Fatalf("GenerateProjectID() returned invalid UUIDv7: %s", id)
	}

	bits, err := parseUUIDv7Bytes(string(id))
	if err != nil {
		t.Fatalf("parseUUIDv7Bytes() error = %v", err)
	}
	if bits[6]&0xf0 != 0x70 {
		t.Fatalf("UUIDv7 version bits incorrect: 0x%02x", bits[6])
	}
	if bits[8]&0xc0 != 0x80 {
		t.Fatalf("UUIDv7 variant bits incorrect: 0x%02x", bits[8])
	}

	timestamp := uint64(bits[0])<<40 | uint64(bits[1])<<32 | uint64(bits[2])<<24 | uint64(bits[3])<<16 | uint64(bits[4])<<8 | uint64(bits[5])
	now := uint64(time.Now().UnixMilli())
	if timestamp == 0 || timestamp > now+30_000 || timestamp < now-5*60*1000 {
		t.Fatalf("UUIDv7 timestamp is not plausible: %d vs now %d", timestamp, now)
	}
	if strings.ToLower(string(id)) != string(id) {
		t.Fatalf("UUIDv7 is not canonical lowercase: %s", id)
	}
}

func TestEnsureRegistrationCreatesAndPersistsProjectId(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "xdg"))
	repoDir := initGitRepo(t)

	reg1, err := EnsureRegistration(repoDir)
	if err != nil {
		t.Fatalf("EnsureRegistration() first call error = %v", err)
	}
	if !reg1.ProjectId.IsValid() {
		t.Fatalf("EnsureRegistration() created invalid ProjectId: %s", reg1.ProjectId)
	}
	if reg1.RepositoryRoot != repoDir {
		t.Fatalf("RepositoryRoot mismatch: got %q want %q", reg1.RepositoryRoot, repoDir)
	}
	if _, err := os.Stat(filepath.Join(repoDir, ".praetor")); !os.IsNotExist(err) {
		t.Fatalf("source repository was mutated unexpectedly: .praetor exists or stat returned %v", err)
	}

	reg2, err := EnsureRegistration(repoDir)
	if err != nil {
		t.Fatalf("EnsureRegistration() second call error = %v", err)
	}
	if reg1.ProjectId != reg2.ProjectId {
		t.Fatalf("ProjectId changed across repeated registrations: %s != %s", reg1.ProjectId, reg2.ProjectId)
	}

	dataDir, err := ResolveDataDir()
	if err != nil {
		t.Fatalf("ResolveDataDir() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(dataDir, registryFile)); err != nil {
		t.Fatalf("registry file not created: %v", err)
	}
}

func TestEnsureRegistrationDifferentReposReceiveDifferentIds(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "xdg"))
	repoA := initGitRepo(t)
	repoB := initGitRepo(t)

	regA, err := EnsureRegistration(repoA)
	if err != nil {
		t.Fatalf("EnsureRegistration(repoA) error = %v", err)
	}
	regB, err := EnsureRegistration(repoB)
	if err != nil {
		t.Fatalf("EnsureRegistration(repoB) error = %v", err)
	}
	if regA.ProjectId == regB.ProjectId {
		t.Fatalf("different repositories produced same ProjectId: %s", regA.ProjectId)
	}
}

func TestEnsureRegistrationNestedPathReusesSameProjectId(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "xdg"))
	repoDir := initGitRepo(t)
	nestedDir := filepath.Join(repoDir, "nested", "child")
	if err := os.MkdirAll(nestedDir, 0o755); err != nil {
		t.Fatalf("create nested dir: %v", err)
	}

	rootInfo, err := repository.Discover(repoDir)
	if err != nil {
		t.Fatalf("repository.Discover(repoDir) error = %v", err)
	}
	regRoot, err := EnsureRegistration(rootInfo.Root)
	if err != nil {
		t.Fatalf("EnsureRegistration(repo root) error = %v", err)
	}

	nestedInfo, err := repository.Discover(nestedDir)
	if err != nil {
		t.Fatalf("repository.Discover(nestedDir) error = %v", err)
	}
	regNested, err := EnsureRegistration(nestedInfo.Root)
	if err != nil {
		t.Fatalf("EnsureRegistration(nested repo root) error = %v", err)
	}
	if regRoot.ProjectId != regNested.ProjectId {
		t.Fatalf("nested repo path did not reuse same ProjectId: %s != %s", regRoot.ProjectId, regNested.ProjectId)
	}
}

func TestLoadRegistrationsRejectsMalformedJSON(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "xdg"))
	dataDir, err := ResolveDataDir()
	if err != nil {
		t.Fatalf("ResolveDataDir() error = %v", err)
	}
	if err := os.MkdirAll(dataDir, 0o755); err != nil {
		t.Fatalf("create data dir: %v", err)
	}
	path := filepath.Join(dataDir, registryFile)
	if err := os.WriteFile(path, []byte("{not-valid-json"), 0o600); err != nil {
		t.Fatalf("write malformed registry: %v", err)
	}
	if _, err := LoadRegistrations(dataDir); err == nil {
		t.Fatal("LoadRegistrations() accepted malformed registration data")
	}
}

func TestResolveDataDirUsesXDGOverride(t *testing.T) {
	expected := filepath.Join(t.TempDir(), "custom-xdg")
	t.Setenv("XDG_DATA_HOME", expected)
	resolved, err := ResolveDataDir()
	if err != nil {
		t.Fatalf("ResolveDataDir() error = %v", err)
	}
	want := filepath.Join(expected, "praetor")
	if resolved != want {
		t.Fatalf("ResolveDataDir() = %q, want %q", resolved, want)
	}
}

func TestEnsureRegistrationRejectsSourceTreeMutation(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "xdg"))
	repoDir := initGitRepo(t)
	if _, err := EnsureRegistration(repoDir); err != nil {
		t.Fatalf("EnsureRegistration() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(repoDir, ".praetor")); !os.IsNotExist(err) {
		t.Fatalf(".praetor directory was created in the repository: %v", err)
	}
}

func TestEnsureRegistrationConcurrentSameRepositorySettlesOnSingleProjectId(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "xdg"))
	repoDir := initGitRepo(t)
	dataDir, err := ResolveDataDir()
	if err != nil {
		t.Fatalf("ResolveDataDir() error = %v", err)
	}

	const workers = 12
	ids := make(chan ProjectId, workers)
	errs := make(chan error, workers)
	var wg sync.WaitGroup
	for i := 0; i < workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			reg, err := EnsureRegistration(repoDir)
			if err != nil {
				errs <- err
				return
			}
			ids <- reg.ProjectId
		}()
	}
	wg.Wait()
	close(ids)
	close(errs)

	seen := map[ProjectId]struct{}{}
	for id := range ids {
		seen[id] = struct{}{}
	}
	for err := range errs {
		if err != nil {
			t.Fatalf("concurrent EnsureRegistration() error = %v", err)
		}
	}
	if len(seen) != 1 {
		t.Fatalf("concurrent same-repo registration produced %d unique IDs: %v", len(seen), seen)
	}

	registrations, err := LoadRegistrations(dataDir)
	if err != nil {
		t.Fatalf("LoadRegistrations() error = %v", err)
	}
	count := 0
	for _, reg := range registrations {
		if reg.RepositoryRoot == repoDir {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("registry should contain exactly one registration for the repository, got %d", count)
	}
}

func TestEnsureRegistrationConcurrentDifferentRepositoriesAreNotLost(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", filepath.Join(t.TempDir(), "xdg"))
	repoA := initGitRepo(t)
	repoB := initGitRepo(t)
	dataDir, err := ResolveDataDir()
	if err != nil {
		t.Fatalf("ResolveDataDir() error = %v", err)
	}

	var wg sync.WaitGroup
	results := make(chan ProjectId, 2)
	for _, repo := range []string{repoA, repoB} {
		wg.Add(1)
		go func(repo string) {
			defer wg.Done()
			reg, err := EnsureRegistration(repo)
			if err != nil {
				t.Errorf("EnsureRegistration(%q) error = %v", repo, err)
				return
			}
			results <- reg.ProjectId
		}(repo)
	}
	wg.Wait()
	close(results)

	seen := map[ProjectId]struct{}{}
	for id := range results {
		seen[id] = struct{}{}
	}
	if len(seen) != 2 {
		t.Fatalf("concurrent different-repository registration lost an entry: got %d ids", len(seen))
	}

	registrations, err := LoadRegistrations(dataDir)
	if err != nil {
		t.Fatalf("LoadRegistrations() error = %v", err)
	}
	if len(registrations) != 2 {
		t.Fatalf("registry should contain exactly two registrations, got %d", len(registrations))
	}
}
