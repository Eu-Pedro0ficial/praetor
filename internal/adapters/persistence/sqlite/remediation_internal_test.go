package sqlite

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Eu-Pedro0ficial/praetor/internal/artifact"
	"github.com/Eu-Pedro0ficial/praetor/internal/audit"
	"github.com/Eu-Pedro0ficial/praetor/internal/project"
)

func TestArtifactBudgetBoundariesWithoutLargeAllocations(t *testing.T) {
	tests := []struct {
		name         string
		changeBytes  int64
		added        int64
		projectBytes int64
		quota        int64
		wantError    bool
	}{
		{name: "exact Change aggregate", changeBytes: artifact.ChangeAggregateLimit - 1, added: 1},
		{name: "Change aggregate exceeded", changeBytes: artifact.ChangeAggregateLimit, added: 1, wantError: true},
		{name: "exact Project quota", changeBytes: 1, added: 1, projectBytes: 8, quota: 9},
		{name: "Project quota exceeded", changeBytes: 1, added: 1, projectBytes: 9, quota: 9, wantError: true},
		{name: "overflow fails closed", changeBytes: artifact.ChangeAggregateLimit, added: int64(^uint64(0) >> 1), wantError: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			err := validateArtifactBudget(test.changeBytes, test.added, test.projectBytes, test.quota)
			if (err != nil) != test.wantError {
				t.Fatalf("validateArtifactBudget() error = %v, wantError %t", err, test.wantError)
			}
		})
	}
}

func TestBackupFreeSpaceDecisionIncludesBoundedOverhead(t *testing.T) {
	const databaseBytes = int64(10 << 20)
	required := backupSpaceRequired(databaseBytes)
	if required != 11<<20 {
		t.Fatalf("backup requirement = %d", required)
	}
	if err := validateBackupSpace(required, databaseBytes); err != nil {
		t.Fatalf("exact backup space rejected: %v", err)
	}
	if err := validateBackupSpace(required-1, databaseBytes); err == nil || !strings.Contains(err.Error(), "insufficient free space") {
		t.Fatalf("insufficient backup space error = %v", err)
	}
	if got := backupSpaceRequired(-1); got != int64(^uint64(0)>>1) {
		t.Fatalf("negative size did not fail closed: %d", got)
	}
}

func TestLegacyImportInjectedFailureRollsBackCompletely(t *testing.T) {
	projectId, err := project.GenerateProjectID()
	if err != nil {
		t.Fatal(err)
	}
	registration := project.Registration{ProjectId: projectId, RepositoryRoot: t.TempDir(), SchemaVersion: 1, CreatedAt: time.Now().UTC()}
	store, err := Open(filepath.Join(t.TempDir(), "praetor"), registration)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	records := make([]audit.LegacyRecord, 2)
	for index := range records {
		event, eventError := audit.NewEvent("LEGACY_TEST", string(projectId), "legacy-change", registration.RepositoryRoot, map[string]any{"index": index}, time.Now().UTC().Add(time.Duration(index)*time.Second))
		if eventError != nil {
			t.Fatal(eventError)
		}
		records[index] = audit.LegacyRecord{Event: event, RawJSON: []byte(`{"legacy":true}`), Sequence: uint64(index + 1)}
	}
	injected := errors.New("injected mid-import failure")
	err = store.importLegacyAudit(records, "sha256:"+strings.Repeat("a", 64), func(imported int) error {
		if imported == 1 {
			return injected
		}
		return nil
	})
	if !errors.Is(err, injected) {
		t.Fatalf("migration error = %v", err)
	}
	for _, query := range []string{`SELECT COUNT(*) FROM audit_events`, `SELECT COUNT(*) FROM legacy_imports`} {
		var count int
		if err := store.db.QueryRow(query).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != 0 {
			t.Fatalf("failed migration left %d rows for %q", count, query)
		}
	}
}
