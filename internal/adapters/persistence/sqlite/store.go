package sqlite

import (
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"syscall"
	"time"

	"github.com/Eu-Pedro0ficial/praetor/internal/artifact"
	"github.com/Eu-Pedro0ficial/praetor/internal/audit"
	"github.com/Eu-Pedro0ficial/praetor/internal/authority"
	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"github.com/Eu-Pedro0ficial/praetor/internal/project"
	"github.com/Eu-Pedro0ficial/praetor/internal/workflow"
	_ "modernc.org/sqlite"
)

const (
	schemaVersion = 1
	databaseName  = "change-store.sqlite3"
)

type Store struct {
	db             *sql.DB
	projectId      project.ProjectId
	repositoryRoot string
	path           string
	dataDirectory  string
	projectQuota   int64
}

// Options contains optional per-Project storage policy. Zero quota means no
// configured quota; the hard artifact/Change limits and free-space checks remain.
type Options struct {
	ProjectQuotaBytes int64
}

// MigrateLegacyAudit imports the complete historical global ledger into its
// registered per-Project authorities before retiring the legacy writer.
func MigrateLegacyAudit(dataDirectory string, registrations []project.Registration) error {
	byProject := make(map[string]project.Registration, len(registrations))
	for _, registration := range registrations {
		if !registration.ProjectId.IsValid() {
			return fmt.Errorf("legacy audit migration has invalid registered ProjectId")
		}
		byProject[string(registration.ProjectId)] = registration
	}
	return audit.MigrateLegacy(dataDirectory, func(records []audit.LegacyRecord, sourceDigest string) error {
		grouped := make(map[string][]audit.LegacyRecord)
		for _, record := range records {
			if _, exists := byProject[record.Event.ProjectID]; !exists {
				return fmt.Errorf("legacy audit event %q references unregistered ProjectId %q", record.Event.EventID, record.Event.ProjectID)
			}
			grouped[record.Event.ProjectID] = append(grouped[record.Event.ProjectID], record)
		}
		for _, registration := range registrations {
			store, err := Open(dataDirectory, registration)
			if err != nil {
				return err
			}
			err = store.ImportLegacyAudit(grouped[string(registration.ProjectId)], sourceDigest)
			closeErr := store.Close()
			if err != nil {
				return err
			}
			if closeErr != nil {
				return closeErr
			}
		}
		return nil
	})
}

func Open(dataDirectory string, registration project.Registration) (*Store, error) {
	return OpenWithOptions(dataDirectory, registration, Options{})
}

func OpenWithOptions(dataDirectory string, registration project.Registration, options Options) (*Store, error) {
	if options.ProjectQuotaBytes < 0 {
		return nil, fmt.Errorf("Project artifact quota cannot be negative")
	}
	if strings.TrimSpace(dataDirectory) == "" {
		return nil, fmt.Errorf("Praetor data directory is required")
	}
	if !registration.ProjectId.IsValid() {
		return nil, fmt.Errorf("valid ProjectId is required")
	}
	if strings.TrimSpace(registration.RepositoryRoot) == "" {
		return nil, fmt.Errorf("RepositoryRoot is required")
	}
	base, err := filepath.Abs(dataDirectory)
	if err != nil {
		return nil, fmt.Errorf("resolve data directory: %w", err)
	}
	if err := ensurePrivateDirectory(base); err != nil {
		return nil, err
	}
	projects := filepath.Join(base, "projects")
	if err := ensurePrivateDirectory(projects); err != nil {
		return nil, err
	}
	projectDirectory := filepath.Join(projects, string(registration.ProjectId))
	if filepath.Dir(projectDirectory) != projects {
		return nil, fmt.Errorf("unsafe Project store path")
	}
	if err := ensurePrivateDirectory(projectDirectory); err != nil {
		return nil, err
	}
	if err := requireLocalFilesystem(projectDirectory); err != nil {
		return nil, err
	}
	databasePath := filepath.Join(projectDirectory, databaseName)
	if info, statErr := os.Lstat(databasePath); statErr == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.Mode().IsRegular() {
			return nil, fmt.Errorf("Project store path is not a regular file")
		}
	} else if !os.IsNotExist(statErr) {
		return nil, fmt.Errorf("inspect Project store: %w", statErr)
	}
	dsn := databaseDSN(databasePath, "")
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, fmt.Errorf("open Project store: %w", err)
	}
	db.SetMaxOpenConns(8)
	db.SetConnMaxLifetime(0)
	store := &Store{db: db, projectId: registration.ProjectId, repositoryRoot: registration.RepositoryRoot, path: databasePath, dataDirectory: base, projectQuota: options.ProjectQuotaBytes}
	if err := store.configureAndVerify(); err != nil {
		db.Close()
		return nil, err
	}
	if err := store.migrate(); err != nil {
		db.Close()
		return nil, err
	}
	var storedProject string
	if err := db.QueryRow(`SELECT value FROM store_metadata WHERE key='project_id'`).Scan(&storedProject); err != nil {
		db.Close()
		return nil, fmt.Errorf("%w: read Project store identity: %v", authority.ErrCorrupt, err)
	}
	if storedProject != string(registration.ProjectId) {
		db.Close()
		return nil, fmt.Errorf("%w: Project store identity mismatch", authority.ErrCorrupt)
	}
	if err := os.Chmod(databasePath, 0o600); err != nil {
		db.Close()
		return nil, fmt.Errorf("set Project store permissions: %w", err)
	}
	return store, nil
}

func (s *Store) Path() string { return s.path }
func (s *Store) Close() error {
	if s == nil || s.db == nil {
		return nil
	}
	return s.db.Close()
}

func (s *Store) configureAndVerify() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	conn, err := s.db.Conn(ctx)
	if err != nil {
		return normalize(err)
	}
	defer conn.Close()
	checks := []struct {
		pragma string
		want   string
	}{{"foreign_keys", "1"}, {"journal_mode", "wal"}, {"synchronous", "2"}, {"busy_timeout", "5000"}}
	for _, item := range checks {
		var got string
		if err := conn.QueryRowContext(ctx, "PRAGMA "+item.pragma).Scan(&got); err != nil {
			return fmt.Errorf("read SQLite %s: %w", item.pragma, normalize(err))
		}
		if strings.ToLower(got) != item.want {
			return fmt.Errorf("SQLite %s=%s, require %s", item.pragma, got, item.want)
		}
	}
	return nil
}

func (s *Store) migrate() error {
	var userVersion int
	if err := s.db.QueryRow("PRAGMA user_version").Scan(&userVersion); err != nil {
		return normalize(err)
	}
	if userVersion > schemaVersion {
		return fmt.Errorf("%w: schema version %d is newer than supported %d", authority.ErrIncompatible, userVersion, schemaVersion)
	}
	if userVersion == schemaVersion {
		return nil
	}
	if userVersion != 0 {
		return fmt.Errorf("%w: unsupported schema migration from %d", authority.ErrIncompatible, userVersion)
	}
	tx, err := s.db.Begin()
	if err != nil {
		return normalize(err)
	}
	defer tx.Rollback()
	statements := []string{
		`CREATE TABLE store_metadata (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
		`CREATE TABLE workflow_snapshots (workflow_digest TEXT PRIMARY KEY, workflow_id TEXT NOT NULL, workflow_version TEXT NOT NULL, schema_version INTEGER NOT NULL, exact_yaml BLOB NOT NULL, UNIQUE(workflow_id, workflow_version))`,
		`CREATE TABLE changes (change_id TEXT PRIMARY KEY, project_id TEXT NOT NULL, intent TEXT NOT NULL, state TEXT NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL, revision INTEGER NOT NULL CHECK(revision > 0), workflow_digest TEXT NOT NULL REFERENCES workflow_snapshots(workflow_digest))`,
		`CREATE TABLE change_revisions (change_id TEXT NOT NULL REFERENCES changes(change_id), revision INTEGER NOT NULL, state TEXT NOT NULL, updated_at TEXT NOT NULL, context TEXT NOT NULL, PRIMARY KEY(change_id, revision))`,
		`CREATE TABLE artifacts (artifact_id TEXT PRIMARY KEY, change_id TEXT NOT NULL REFERENCES changes(change_id), project_id TEXT NOT NULL, kind TEXT NOT NULL, envelope_schema_version INTEGER NOT NULL, kind_schema_version INTEGER NOT NULL, media_type TEXT NOT NULL, byte_length INTEGER NOT NULL CHECK(byte_length >= 0 AND byte_length <= 52428800), created_at TEXT NOT NULL, producer_component TEXT NOT NULL, producer_operation_id TEXT NOT NULL, payload BLOB NOT NULL, content_digest TEXT NOT NULL, record_digest TEXT NOT NULL UNIQUE)`,
		`CREATE INDEX artifacts_change_idx ON artifacts(change_id, created_at, artifact_id)`,
		`CREATE TABLE artifact_relationships (from_artifact_id TEXT NOT NULL REFERENCES artifacts(artifact_id), to_artifact_id TEXT NOT NULL REFERENCES artifacts(artifact_id), kind TEXT NOT NULL, PRIMARY KEY(from_artifact_id,to_artifact_id,kind))`,
		`CREATE TABLE artifact_supersessions (previous_artifact_id TEXT PRIMARY KEY REFERENCES artifacts(artifact_id), current_artifact_id TEXT NOT NULL REFERENCES artifacts(artifact_id))`,
		`CREATE TABLE artifact_bindings (change_id TEXT NOT NULL REFERENCES changes(change_id), role TEXT NOT NULL, binding_revision INTEGER NOT NULL, artifact_id TEXT NOT NULL REFERENCES artifacts(artifact_id), PRIMARY KEY(change_id,role,binding_revision))`,
		`CREATE INDEX artifact_bindings_current_idx ON artifact_bindings(change_id,role,binding_revision DESC)`,
		`CREATE TABLE operations (operation_id TEXT PRIMARY KEY, change_id TEXT REFERENCES changes(change_id), project_id TEXT NOT NULL, kind TEXT NOT NULL, request_digest TEXT NOT NULL, expected_revision INTEGER NOT NULL, state TEXT NOT NULL, result BLOB NOT NULL, created_at TEXT NOT NULL, updated_at TEXT NOT NULL)`,
		`CREATE INDEX operations_incomplete_idx ON operations(state,change_id,created_at)`,
		`CREATE TABLE audit_events (sequence INTEGER PRIMARY KEY AUTOINCREMENT, event_id TEXT NOT NULL UNIQUE, event_type TEXT NOT NULL, timestamp TEXT NOT NULL, project_id TEXT NOT NULL, change_id TEXT NOT NULL, repository_root TEXT NOT NULL, schema_version INTEGER NOT NULL, metadata_json BLOB NOT NULL, raw_json BLOB, legacy_sequence INTEGER, source_digest TEXT)`,
		`CREATE INDEX audit_change_idx ON audit_events(change_id,sequence)`,
		`CREATE TABLE legacy_imports (source_digest TEXT PRIMARY KEY, event_count INTEGER NOT NULL, imported_at TEXT NOT NULL)`,
		`INSERT INTO store_metadata(key,value) VALUES ('project_id', ?), ('schema_version', '1')`,
	}
	for _, statement := range statements {
		var execErr error
		if strings.Contains(statement, "?") {
			_, execErr = tx.Exec(statement, string(s.projectId))
		} else {
			_, execErr = tx.Exec(statement)
		}
		if execErr != nil {
			return fmt.Errorf("initialize Project store schema: %w", normalize(execErr))
		}
	}
	if _, err := tx.Exec(`PRAGMA user_version = 1`); err != nil {
		return normalize(err)
	}
	if err := tx.Commit(); err != nil {
		return normalize(err)
	}
	return nil
}

func (s *Store) CreateChange(current change.Change, snapshot workflow.WorkflowSnapshot, event workflow.LifecycleEvent) error {
	if err := audit.ValidateRetiredLegacy(s.dataDirectory); err != nil {
		return fmt.Errorf("validate legacy audit evidence: %w", err)
	}
	if current.ProjectId() != s.projectId || event.ProjectId != s.projectId || event.ChangeId != current.ChangeId() {
		return fmt.Errorf("cross-Project Change creation rejected")
	}
	if current.Revision() != 1 || current.State() != change.StateCreated {
		return fmt.Errorf("durable Change creation requires revision 1 CREATED")
	}
	if err := verifySnapshot(snapshot); err != nil {
		return err
	}
	tx, err := s.db.Begin()
	if err != nil {
		return normalize(err)
	}
	defer tx.Rollback()
	if err := insertWorkflow(tx, snapshot); err != nil {
		return err
	}
	if _, err := tx.Exec(`INSERT INTO changes(change_id,project_id,intent,state,created_at,updated_at,revision,workflow_digest) VALUES(?,?,?,?,?,?,?,?)`, current.ChangeId(), current.ProjectId(), current.Intent(), current.State(), timestamp(current.CreatedAt()), timestamp(current.UpdatedAt()), current.Revision(), snapshot.Digest()); err != nil {
		return normalize(err)
	}
	if _, err := tx.Exec(`INSERT INTO change_revisions(change_id,revision,state,updated_at,context) VALUES(?,?,?,?,?)`, current.ChangeId(), current.Revision(), current.State(), timestamp(current.UpdatedAt()), event.Context); err != nil {
		return normalize(err)
	}
	auditEvent, err := lifecycleAudit(event, s.repositoryRoot)
	if err != nil {
		return err
	}
	if err := insertAudit(tx, auditEvent, nil, 0, ""); err != nil {
		return err
	}
	return normalize(tx.Commit())
}

func (s *Store) CommitTransition(expectedRevision uint64, candidate change.Change, event workflow.LifecycleEvent) error {
	if err := audit.ValidateRetiredLegacy(s.dataDirectory); err != nil {
		return fmt.Errorf("validate legacy audit evidence: %w", err)
	}
	if candidate.ProjectId() != s.projectId || event.ProjectId != s.projectId || event.ChangeId != candidate.ChangeId() {
		return fmt.Errorf("cross-Project Change transition rejected")
	}
	if candidate.Revision() != expectedRevision+1 {
		return fmt.Errorf("candidate revision must follow ExpectedRevision")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return normalize(err)
	}
	defer tx.Rollback()
	result, err := tx.Exec(`UPDATE changes SET state=?,updated_at=?,revision=? WHERE change_id=? AND project_id=? AND revision=?`, candidate.State(), timestamp(candidate.UpdatedAt()), candidate.Revision(), candidate.ChangeId(), s.projectId, expectedRevision)
	if err != nil {
		return normalize(err)
	}
	rows, err := result.RowsAffected()
	if err != nil {
		return normalize(err)
	}
	if rows != 1 {
		return fmt.Errorf("%w: Change %q expected revision %d", authority.ErrStaleRevision, candidate.ChangeId(), expectedRevision)
	}
	if _, err := tx.Exec(`INSERT INTO change_revisions(change_id,revision,state,updated_at,context) VALUES(?,?,?,?,?)`, candidate.ChangeId(), candidate.Revision(), candidate.State(), timestamp(candidate.UpdatedAt()), event.Context); err != nil {
		return normalize(err)
	}
	auditEvent, err := lifecycleAudit(event, s.repositoryRoot)
	if err != nil {
		return err
	}
	if err := insertAudit(tx, auditEvent, nil, 0, ""); err != nil {
		return err
	}
	return normalize(tx.Commit())
}

func (s *Store) GetChange(id change.ChangeId) (change.Change, workflow.WorkflowSnapshot, error) {
	row := s.db.QueryRow(`SELECT c.project_id,c.intent,c.state,c.created_at,c.updated_at,c.revision,w.workflow_id,w.workflow_version,w.schema_version,w.workflow_digest,w.exact_yaml FROM changes c JOIN workflow_snapshots w ON w.workflow_digest=c.workflow_digest WHERE c.change_id=? AND c.project_id=?`, id, s.projectId)
	var projectValue, intent, state, created, updated string
	var workflowId, workflowVersion, workflowDigest string
	var workflowSchema uint32
	var revision uint64
	var exact []byte
	if err := row.Scan(&projectValue, &intent, &state, &created, &updated, &revision, &workflowId, &workflowVersion, &workflowSchema, &workflowDigest, &exact); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return change.Change{}, workflow.WorkflowSnapshot{}, fmt.Errorf("%w: %q", workflow.ErrChangeNotFound, id)
		}
		return change.Change{}, workflow.WorkflowSnapshot{}, normalize(err)
	}
	parsedState, err := change.ParseState(state)
	if err != nil {
		return change.Change{}, workflow.WorkflowSnapshot{}, fmt.Errorf("%w: %v", authority.ErrCorrupt, err)
	}
	createdAt, err := parseTimestamp(created)
	if err != nil {
		return change.Change{}, workflow.WorkflowSnapshot{}, fmt.Errorf("%w: %v", authority.ErrCorrupt, err)
	}
	updatedAt, err := parseTimestamp(updated)
	if err != nil {
		return change.Change{}, workflow.WorkflowSnapshot{}, fmt.Errorf("%w: %v", authority.ErrCorrupt, err)
	}
	current, err := change.Rehydrate(id, project.ProjectId(projectValue), change.ChangeIntent(intent), parsedState, createdAt, updatedAt, revision)
	if err != nil {
		return change.Change{}, workflow.WorkflowSnapshot{}, fmt.Errorf("%w: %v", authority.ErrCorrupt, err)
	}
	snapshot, err := workflow.InspectSnapshot(workflowId, workflowVersion, workflowSchema, workflowDigest, exact)
	if err != nil {
		return change.Change{}, workflow.WorkflowSnapshot{}, fmt.Errorf("%w: %v", authority.ErrCorrupt, err)
	}
	return current, snapshot, nil
}

func (s *Store) ListChanges() ([]change.Change, error) {
	rows, err := s.db.Query(`SELECT change_id FROM changes WHERE project_id=? ORDER BY updated_at DESC,change_id`, s.projectId)
	if err != nil {
		return nil, normalize(err)
	}
	defer rows.Close()
	var values []change.Change
	for rows.Next() {
		var id change.ChangeId
		if err := rows.Scan(&id); err != nil {
			return nil, normalize(err)
		}
		current, _, err := s.GetChange(id)
		if err != nil {
			return nil, err
		}
		values = append(values, current)
	}
	return values, normalize(rows.Err())
}

func (s *Store) CommitAuthority(commit authority.AuthorityCommit) error {
	if err := audit.ValidateRetiredLegacy(s.dataDirectory); err != nil {
		return fmt.Errorf("validate legacy audit evidence: %w", err)
	}
	if commit.Candidate.ProjectId() != s.projectId || commit.Candidate.Revision() != commit.ExpectedRevision+1 {
		return fmt.Errorf("invalid authority commit Change revision")
	}
	if commit.Operation != nil && (commit.Operation.ProjectId != s.projectId || (commit.Operation.ChangeId != "" && commit.Operation.ChangeId != commit.Candidate.ChangeId())) {
		return fmt.Errorf("authority operation scope does not match candidate Change")
	}
	if err := validateCanonicalCompletionCommit(commit); err != nil {
		return err
	}
	for _, event := range commit.AuditEvents {
		if err := s.validateAuthorityAudit(event, commit.Candidate.ChangeId()); err != nil {
			return err
		}
	}
	tx, err := s.db.Begin()
	if err != nil {
		return normalize(err)
	}
	defer tx.Rollback()
	var existingBytes int64
	if err := tx.QueryRow(`SELECT COALESCE(SUM(byte_length),0) FROM artifacts WHERE change_id=?`, commit.Candidate.ChangeId()).Scan(&existingBytes); err != nil {
		return normalize(err)
	}
	var added int64
	for _, item := range commit.Artifacts {
		if item.ProjectId() != s.projectId || item.ChangeId() != commit.Candidate.ChangeId() {
			return fmt.Errorf("cross-Project or cross-Change artifact rejected")
		}
		added += item.ByteLength()
	}
	var projectBytes int64
	if err := tx.QueryRow(`SELECT COALESCE(SUM(byte_length),0) FROM artifacts`).Scan(&projectBytes); err != nil {
		return normalize(err)
	}
	if err := validateArtifactBudget(existingBytes, added, projectBytes, s.projectQuota); err != nil {
		return err
	}
	if err := ensureFreeSpace(s.path, added); err != nil {
		return err
	}
	result, err := tx.Exec(`UPDATE changes SET state=?,updated_at=?,revision=? WHERE change_id=? AND project_id=? AND revision=?`, commit.Candidate.State(), timestamp(commit.Candidate.UpdatedAt()), commit.Candidate.Revision(), commit.Candidate.ChangeId(), s.projectId, commit.ExpectedRevision)
	if err != nil {
		return normalize(err)
	}
	count, err := result.RowsAffected()
	if err != nil {
		return normalize(err)
	}
	if count != 1 {
		return fmt.Errorf("%w: Change %q expected revision %d", authority.ErrStaleRevision, commit.Candidate.ChangeId(), commit.ExpectedRevision)
	}
	for _, item := range commit.Artifacts {
		if err := insertArtifact(tx, item); err != nil {
			return err
		}
	}
	for _, rel := range commit.Relationships {
		if err := artifact.ValidateRelationship(rel); err != nil {
			return err
		}
		if err := ensureArtifactPairChange(tx, commit.Candidate.ChangeId(), rel.From, rel.To); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO artifact_relationships(from_artifact_id,to_artifact_id,kind) VALUES(?,?,?)`, rel.From, rel.To, rel.Kind); err != nil {
			return normalize(err)
		}
	}
	for _, sup := range commit.Supersessions {
		if sup.Previous == sup.Current {
			return fmt.Errorf("artifact cannot supersede itself")
		}
		if err := ensureArtifactPairChange(tx, commit.Candidate.ChangeId(), sup.Previous, sup.Current); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO artifact_supersessions(previous_artifact_id,current_artifact_id) VALUES(?,?)`, sup.Previous, sup.Current); err != nil {
			return normalize(err)
		}
	}
	for _, binding := range commit.Bindings {
		role := strings.TrimSpace(binding.Role)
		if role == "" || binding.Revision != commit.Candidate.Revision() {
			return fmt.Errorf("invalid artifact binding")
		}
		result, err := tx.Exec(`INSERT INTO artifact_bindings(change_id,role,binding_revision,artifact_id) SELECT ?,?,?,artifact_id FROM artifacts WHERE artifact_id=? AND change_id=?`, commit.Candidate.ChangeId(), role, binding.Revision, binding.ArtifactId, commit.Candidate.ChangeId())
		if err != nil {
			return normalize(err)
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return normalize(err)
		}
		if rows != 1 {
			return fmt.Errorf("binding artifact %q does not belong to Change %q", binding.ArtifactId, commit.Candidate.ChangeId())
		}
	}
	if commit.Operation != nil {
		if err := insertOrCompleteOperation(tx, *commit.Operation); err != nil {
			return err
		}
	}
	for _, event := range commit.AuditEvents {
		if err := insertAudit(tx, event, nil, 0, ""); err != nil {
			return err
		}
	}
	if _, err := tx.Exec(`INSERT INTO change_revisions(change_id,revision,state,updated_at,context) VALUES(?,?,?,?,?)`, commit.Candidate.ChangeId(), commit.Candidate.Revision(), commit.Candidate.State(), timestamp(commit.Candidate.UpdatedAt()), "atomic authority commit"); err != nil {
		return normalize(err)
	}
	return normalize(tx.Commit())
}

func (s *Store) CommitArtifacts(changeId change.ChangeId, expectedRevision uint64, items []artifact.Artifact, relationships []artifact.Relationship, supersessions []artifact.Supersession, bindings []authority.ArtifactBinding, events []audit.Event) error {
	if err := audit.ValidateRetiredLegacy(s.dataDirectory); err != nil {
		return fmt.Errorf("validate legacy audit evidence: %w", err)
	}
	for _, event := range events {
		if err := s.validateAuthorityAudit(event, changeId); err != nil {
			return err
		}
	}
	tx, err := s.db.Begin()
	if err != nil {
		return normalize(err)
	}
	defer tx.Rollback()
	var revision uint64
	if err := tx.QueryRow(`SELECT revision FROM changes WHERE change_id=? AND project_id=?`, changeId, s.projectId).Scan(&revision); err != nil {
		return normalize(err)
	}
	if revision != expectedRevision {
		return fmt.Errorf("%w: Change %q expected revision %d", authority.ErrStaleRevision, changeId, expectedRevision)
	}
	var existingBytes int64
	if err := tx.QueryRow(`SELECT COALESCE(SUM(byte_length),0) FROM artifacts WHERE change_id=?`, changeId).Scan(&existingBytes); err != nil {
		return normalize(err)
	}
	var added int64
	for _, item := range items {
		if item.ProjectId() != s.projectId || item.ChangeId() != changeId {
			return fmt.Errorf("cross-Project or cross-Change artifact rejected")
		}
		added += item.ByteLength()
	}
	var projectBytes int64
	if err := tx.QueryRow(`SELECT COALESCE(SUM(byte_length),0) FROM artifacts`).Scan(&projectBytes); err != nil {
		return normalize(err)
	}
	if err := validateArtifactBudget(existingBytes, added, projectBytes, s.projectQuota); err != nil {
		return err
	}
	if err := ensureFreeSpace(s.path, added); err != nil {
		return err
	}
	for _, item := range items {
		if err := insertArtifact(tx, item); err != nil {
			return err
		}
	}
	for _, relation := range relationships {
		if err := artifact.ValidateRelationship(relation); err != nil {
			return err
		}
		if err := ensureArtifactPairChange(tx, changeId, relation.From, relation.To); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO artifact_relationships(from_artifact_id,to_artifact_id,kind) VALUES(?,?,?)`, relation.From, relation.To, relation.Kind); err != nil {
			return normalize(err)
		}
	}
	for _, supersession := range supersessions {
		if supersession.Previous == supersession.Current {
			return fmt.Errorf("artifact cannot supersede itself")
		}
		if err := ensureArtifactPairChange(tx, changeId, supersession.Previous, supersession.Current); err != nil {
			return err
		}
		if _, err := tx.Exec(`INSERT INTO artifact_supersessions(previous_artifact_id,current_artifact_id) VALUES(?,?)`, supersession.Previous, supersession.Current); err != nil {
			return normalize(err)
		}
	}
	for _, binding := range bindings {
		if binding.Revision != expectedRevision || strings.TrimSpace(binding.Role) == "" {
			return fmt.Errorf("invalid artifact binding")
		}
		result, err := tx.Exec(`INSERT INTO artifact_bindings(change_id,role,binding_revision,artifact_id) SELECT ?,?,?,artifact_id FROM artifacts WHERE artifact_id=? AND change_id=?`, changeId, binding.Role, binding.Revision, binding.ArtifactId, changeId)
		if err != nil {
			return normalize(err)
		}
		rows, err := result.RowsAffected()
		if err != nil {
			return normalize(err)
		}
		if rows != 1 {
			return fmt.Errorf("binding artifact %q does not belong to Change %q", binding.ArtifactId, changeId)
		}
	}
	for _, event := range events {
		if err := insertAudit(tx, event, nil, 0, ""); err != nil {
			return err
		}
	}
	return normalize(tx.Commit())
}

func ensureArtifactPairChange(tx *sql.Tx, changeId change.ChangeId, left, right artifact.ArtifactId) error {
	var count int
	if err := tx.QueryRow(`SELECT COUNT(*) FROM artifacts WHERE change_id=? AND artifact_id IN (?,?)`, changeId, left, right).Scan(&count); err != nil {
		return normalize(err)
	}
	if count != 2 {
		return fmt.Errorf("artifact relationship crosses Change boundary or references missing artifact")
	}
	return nil
}

func (s *Store) AppendAudit(event audit.Event) error {
	if err := audit.ValidateRetiredLegacy(s.dataDirectory); err != nil {
		return fmt.Errorf("validate legacy audit evidence: %w", err)
	}
	if event.ProjectID != string(s.projectId) {
		return fmt.Errorf("cross-Project audit event rejected")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return normalize(err)
	}
	defer tx.Rollback()
	if err := insertAudit(tx, event, nil, 0, ""); err != nil {
		return err
	}
	return normalize(tx.Commit())
}

// ImportLegacyAudit imports one Project's records as one idempotent transaction.
func (s *Store) ImportLegacyAudit(records []audit.LegacyRecord, sourceDigest string) error {
	return s.importLegacyAudit(records, sourceDigest, nil)
}

func (s *Store) importLegacyAudit(records []audit.LegacyRecord, sourceDigest string, afterRecord func(int) error) error {
	if len(sourceDigest) != 71 || !strings.HasPrefix(sourceDigest, "sha256:") {
		return fmt.Errorf("legacy source digest is invalid")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return normalize(err)
	}
	defer tx.Rollback()
	var existingCount int
	err = tx.QueryRow(`SELECT event_count FROM legacy_imports WHERE source_digest=?`, sourceDigest).Scan(&existingCount)
	if err == nil {
		if existingCount != len(records) {
			return fmt.Errorf("legacy import digest count mismatch")
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return normalize(err)
	}
	for index, record := range records {
		if record.Event.ProjectID != string(s.projectId) {
			return fmt.Errorf("cross-Project legacy audit record rejected")
		}
		if len(record.RawJSON) == 0 || record.Sequence == 0 {
			return fmt.Errorf("legacy audit provenance is incomplete")
		}
		if err := insertAudit(tx, record.Event, record.RawJSON, record.Sequence, sourceDigest); err != nil {
			return err
		}
		if afterRecord != nil {
			if err := afterRecord(index + 1); err != nil {
				return err
			}
		}
	}
	if _, err := tx.Exec(`INSERT INTO legacy_imports(source_digest,event_count,imported_at) VALUES(?,?,?)`, sourceDigest, len(records), timestamp(time.Now().UTC())); err != nil {
		return normalize(err)
	}
	return normalize(tx.Commit())
}

func (s *Store) AuditHistory(id change.ChangeId) ([]audit.Event, error) {
	rows, err := s.db.Query(`SELECT event_id,event_type,timestamp,project_id,change_id,repository_root,schema_version,metadata_json FROM audit_events WHERE change_id=? ORDER BY sequence`, id)
	if err != nil {
		return nil, normalize(err)
	}
	defer rows.Close()
	var events []audit.Event
	for rows.Next() {
		var event audit.Event
		var timestampValue string
		var metadata []byte
		if err := rows.Scan(&event.EventID, &event.EventType, &timestampValue, &event.ProjectID, &event.ChangeID, &event.RepositoryRoot, &event.SchemaVersion, &metadata); err != nil {
			return nil, normalize(err)
		}
		event.Timestamp, err = parseTimestamp(timestampValue)
		if err != nil {
			return nil, err
		}
		if err := json.Unmarshal(metadata, &event.Metadata); err != nil {
			return nil, fmt.Errorf("%w: audit metadata: %v", authority.ErrCorrupt, err)
		}
		if err := audit.ValidateEvent(event); err != nil {
			return nil, fmt.Errorf("%w: %v", authority.ErrCorrupt, err)
		}
		events = append(events, event)
	}
	return events, normalize(rows.Err())
}

func (s *Store) ListArtifacts(id change.ChangeId) ([]authority.ArtifactMetadata, error) {
	rows, err := s.db.Query(`SELECT artifact_id,kind,envelope_schema_version,kind_schema_version,media_type,byte_length,created_at,producer_component,producer_operation_id,content_digest,record_digest FROM artifacts WHERE change_id=? ORDER BY created_at,artifact_id`, id)
	if err != nil {
		return nil, normalize(err)
	}
	defer rows.Close()
	var result []authority.ArtifactMetadata
	for rows.Next() {
		var item authority.ArtifactMetadata
		var created string
		if err := rows.Scan(&item.Id, &item.Kind, &item.EnvelopeSchemaVersion, &item.KindSchemaVersion, &item.MediaType, &item.ByteLength, &created, &item.Producer.Component, &item.Producer.OperationId, &item.ContentDigest, &item.RecordDigest); err != nil {
			return nil, normalize(err)
		}
		item.CreatedAt, err = parseTimestamp(created)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, normalize(rows.Err())
}

func (s *Store) GetArtifact(changeId change.ChangeId, id artifact.ArtifactId, includePayload bool) (artifact.Artifact, error) {
	if !includePayload {
		return artifact.Artifact{}, fmt.Errorf("use ListArtifacts for metadata-only inspection")
	}
	columns := `artifact_id,project_id,change_id,kind,envelope_schema_version,kind_schema_version,media_type,created_at,producer_component,producer_operation_id,content_digest,record_digest`
	columns += `,payload`
	row := s.db.QueryRow(`SELECT `+columns+` FROM artifacts WHERE artifact_id=? AND change_id=?`, id, changeId)
	var aid artifact.ArtifactId
	var pid project.ProjectId
	var cid change.ChangeId
	var kind artifact.Kind
	var ev, kv uint32
	var media, created, component, operation, content, record string
	var payload []byte
	if err := row.Scan(&aid, &pid, &cid, &kind, &ev, &kv, &media, &created, &component, &operation, &content, &record, &payload); err != nil {
		return artifact.Artifact{}, normalize(err)
	}
	createdAt, err := parseTimestamp(created)
	if err != nil {
		return artifact.Artifact{}, err
	}
	return artifact.Rehydrate(aid, pid, cid, kind, ev, kv, media, createdAt, artifact.Producer{Component: component, OperationId: operation}, payload, content, record)
}

func (s *Store) ListBindings(id change.ChangeId) ([]authority.ArtifactBinding, error) {
	rows, err := s.db.Query(`SELECT b.role,b.artifact_id,b.binding_revision FROM artifact_bindings b JOIN (SELECT role,MAX(binding_revision) revision FROM artifact_bindings WHERE change_id=? GROUP BY role) c ON c.role=b.role AND c.revision=b.binding_revision WHERE b.change_id=? ORDER BY b.role`, id, id)
	if err != nil {
		return nil, normalize(err)
	}
	defer rows.Close()
	var result []authority.ArtifactBinding
	for rows.Next() {
		var item authority.ArtifactBinding
		if err := rows.Scan(&item.Role, &item.ArtifactId, &item.Revision); err != nil {
			return nil, normalize(err)
		}
		result = append(result, item)
	}
	return result, normalize(rows.Err())
}

func (s *Store) ListRelationships(id change.ChangeId) ([]artifact.Relationship, error) {
	rows, err := s.db.Query(`SELECT r.from_artifact_id,r.to_artifact_id,r.kind FROM artifact_relationships r JOIN artifacts a ON a.artifact_id=r.from_artifact_id WHERE a.change_id=? ORDER BY r.from_artifact_id,r.to_artifact_id,r.kind`, id)
	if err != nil {
		return nil, normalize(err)
	}
	defer rows.Close()
	var result []artifact.Relationship
	for rows.Next() {
		var item artifact.Relationship
		if err := rows.Scan(&item.From, &item.To, &item.Kind); err != nil {
			return nil, normalize(err)
		}
		if err := artifact.ValidateRelationship(item); err != nil {
			return nil, fmt.Errorf("%w: %v", authority.ErrCorrupt, err)
		}
		result = append(result, item)
	}
	return result, normalize(rows.Err())
}

func (s *Store) ProjectArtifactBytes() (int64, error) {
	var total int64
	if err := s.db.QueryRow(`SELECT COALESCE(SUM(byte_length),0) FROM artifacts WHERE project_id=?`, s.projectId).Scan(&total); err != nil {
		return 0, normalize(err)
	}
	return total, nil
}

func (s *Store) ReserveOperation(value authority.Operation) (authority.Operation, bool, error) {
	if err := authority.ValidateOperation(value); err != nil {
		return authority.Operation{}, false, err
	}
	if value.ProjectId != s.projectId {
		return authority.Operation{}, false, fmt.Errorf("cross-Project operation rejected")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return authority.Operation{}, false, normalize(err)
	}
	defer tx.Rollback()
	existing, found, err := getOperationTx(tx, value.Id)
	if err != nil {
		return authority.Operation{}, false, err
	}
	if found {
		if !sameOperationRequest(existing, value) {
			return authority.Operation{}, false, authority.ErrOperationConflict
		}
		return existing, true, nil
	}
	if value.ChangeId != "" {
		var revision uint64
		if err := tx.QueryRow(`SELECT revision FROM changes WHERE change_id=? AND project_id=?`, value.ChangeId, s.projectId).Scan(&revision); err != nil {
			return authority.Operation{}, false, normalize(err)
		}
		if revision != value.ExpectedRevision {
			return authority.Operation{}, false, fmt.Errorf("%w: Change %q expected revision %d", authority.ErrStaleRevision, value.ChangeId, value.ExpectedRevision)
		}
	}
	if err := insertOperation(tx, value); err != nil {
		return authority.Operation{}, false, err
	}
	if err := tx.Commit(); err != nil {
		return authority.Operation{}, false, normalize(err)
	}
	return value, false, nil
}

func (s *Store) GetOperation(id authority.OperationId) (authority.Operation, error) {
	row := s.db.QueryRow(`SELECT operation_id,project_id,change_id,kind,request_digest,expected_revision,state,result,created_at,updated_at FROM operations WHERE operation_id=? AND project_id=?`, id, s.projectId)
	return scanOperation(row)
}
func (s *Store) ListOperations(changeId change.ChangeId) ([]authority.Operation, error) {
	rows, err := s.db.Query(`SELECT operation_id,project_id,change_id,kind,request_digest,expected_revision,state,result,created_at,updated_at FROM operations WHERE project_id=? AND change_id=? ORDER BY created_at,operation_id`, s.projectId, changeId)
	if err != nil {
		return nil, normalize(err)
	}
	defer rows.Close()
	var result []authority.Operation
	for rows.Next() {
		value, err := scanOperation(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, value)
	}
	return result, normalize(rows.Err())
}
func (s *Store) ListIncompleteOperations() ([]authority.Operation, error) {
	rows, err := s.db.Query(`SELECT operation_id,project_id,change_id,kind,request_digest,expected_revision,state,result,created_at,updated_at FROM operations WHERE project_id=? AND state=? ORDER BY created_at`, s.projectId, authority.OperationReserved)
	if err != nil {
		return nil, normalize(err)
	}
	defer rows.Close()
	var result []authority.Operation
	for rows.Next() {
		item, err := scanOperation(rows)
		if err != nil {
			return nil, err
		}
		result = append(result, item)
	}
	return result, normalize(rows.Err())
}
func (s *Store) CompleteOperation(id authority.OperationId, requestDigest string, result []byte, events []audit.Event) (authority.Operation, error) {
	if err := audit.ValidateRetiredLegacy(s.dataDirectory); err != nil {
		return authority.Operation{}, fmt.Errorf("validate legacy audit evidence: %w", err)
	}
	if len(result) > 64<<10 {
		return authority.Operation{}, fmt.Errorf("operation result exceeds bounded limit")
	}
	tx, err := s.db.Begin()
	if err != nil {
		return authority.Operation{}, normalize(err)
	}
	defer tx.Rollback()
	current, found, err := getOperationTx(tx, id)
	if err != nil {
		return authority.Operation{}, err
	}
	if !found {
		return authority.Operation{}, fmt.Errorf("operation %q was not found", id)
	}
	if current.RequestDigest != requestDigest {
		return authority.Operation{}, authority.ErrOperationConflict
	}
	for _, event := range events {
		if err := s.validateAuthorityAudit(event, current.ChangeId); err != nil {
			return authority.Operation{}, err
		}
	}
	if current.State == authority.OperationCompleted {
		if !sameCompletedResult(current.Kind, current.Result, result) {
			return authority.Operation{}, authority.ErrOperationConflict
		}
		return current, nil
	}
	now := time.Now().UTC()
	updated, err := tx.Exec(`UPDATE operations SET state=?,result=?,updated_at=? WHERE operation_id=? AND state=?`, authority.OperationCompleted, result, timestamp(now), id, authority.OperationReserved)
	if err != nil {
		return authority.Operation{}, normalize(err)
	}
	rows, err := updated.RowsAffected()
	if err != nil {
		return authority.Operation{}, normalize(err)
	}
	if rows != 1 {
		return authority.Operation{}, fmt.Errorf("%w: operation %q is no longer reserved", authority.ErrOperationConflict, id)
	}
	for _, event := range events {
		if err := insertAudit(tx, event, nil, 0, ""); err != nil {
			return authority.Operation{}, err
		}
	}
	if err := tx.Commit(); err != nil {
		return authority.Operation{}, normalize(err)
	}
	current.State = authority.OperationCompleted
	current.Result = append([]byte(nil), result...)
	current.UpdatedAt = now
	return current, nil
}

func (s *Store) validateAuthorityAudit(event audit.Event, changeId change.ChangeId) error {
	if event.ProjectID != string(s.projectId) {
		return fmt.Errorf("cross-Project audit event rejected")
	}
	if event.ChangeID != "" && event.ChangeID != string(changeId) {
		return fmt.Errorf("cross-Change audit event rejected")
	}
	return audit.ValidateEvent(event)
}

func (s *Store) Validate() error {
	if err := s.ValidateAttachment(); err != nil {
		return err
	}
	artifactRows, err := s.db.Query(`SELECT artifact_id FROM artifacts`)
	if err != nil {
		return normalize(err)
	}
	var ids []artifact.ArtifactId
	for artifactRows.Next() {
		var id artifact.ArtifactId
		if err := artifactRows.Scan(&id); err != nil {
			artifactRows.Close()
			return normalize(err)
		}
		ids = append(ids, id)
	}
	if err := artifactRows.Err(); err != nil {
		artifactRows.Close()
		return normalize(err)
	}
	if err := artifactRows.Close(); err != nil {
		return normalize(err)
	}
	for _, id := range ids {
		var cid change.ChangeId
		if err := s.db.QueryRow(`SELECT change_id FROM artifacts WHERE artifact_id=?`, id).Scan(&cid); err != nil {
			return normalize(err)
		}
		if _, err := s.GetArtifact(cid, id, true); err != nil {
			return fmt.Errorf("%w: %v", authority.ErrCorrupt, err)
		}
	}
	return nil
}

// ValidateAttachment performs bounded structural and compatibility checks
// without eagerly loading or hashing every historical artifact payload.
func (s *Store) ValidateAttachment() error {
	var integrity string
	if err := s.db.QueryRow(`PRAGMA integrity_check`).Scan(&integrity); err != nil {
		return normalize(err)
	}
	if integrity != "ok" {
		return fmt.Errorf("%w: SQLite integrity_check: %s", authority.ErrCorrupt, integrity)
	}
	rows, err := s.db.Query(`PRAGMA foreign_key_check`)
	if err != nil {
		return normalize(err)
	}
	defer rows.Close()
	if rows.Next() {
		return fmt.Errorf("%w: SQLite foreign_key_check failed", authority.ErrCorrupt)
	}
	var pid string
	if err := s.db.QueryRow(`SELECT value FROM store_metadata WHERE key='project_id'`).Scan(&pid); err != nil {
		return fmt.Errorf("%w: store metadata: %v", authority.ErrCorrupt, err)
	}
	if pid != string(s.projectId) {
		return fmt.Errorf("%w: ProjectId metadata mismatch", authority.ErrCorrupt)
	}
	for _, check := range []struct {
		query  string
		detail string
	}{
		{`SELECT COUNT(*) FROM artifacts a JOIN changes c ON c.change_id=a.change_id WHERE a.project_id<>c.project_id OR a.project_id<>?`, "artifact Project/Change ownership mismatch"},
		{`SELECT COUNT(*) FROM artifact_bindings b JOIN artifacts a ON a.artifact_id=b.artifact_id WHERE b.change_id<>a.change_id`, "artifact binding crosses Change boundary"},
		{`SELECT COUNT(*) FROM artifact_relationships r JOIN artifacts a ON a.artifact_id=r.from_artifact_id JOIN artifacts b ON b.artifact_id=r.to_artifact_id WHERE a.change_id<>b.change_id`, "artifact relationship crosses Change boundary"},
		{`SELECT COUNT(*) FROM artifact_supersessions s JOIN artifacts a ON a.artifact_id=s.previous_artifact_id JOIN artifacts b ON b.artifact_id=s.current_artifact_id WHERE a.change_id<>b.change_id`, "artifact supersession crosses Change boundary"},
	} {
		var count int
		arguments := []any{}
		if strings.Contains(check.query, "?") {
			arguments = append(arguments, s.projectId)
		}
		if err := s.db.QueryRow(check.query, arguments...).Scan(&count); err != nil {
			return normalize(err)
		}
		if count != 0 {
			return fmt.Errorf("%w: %s", authority.ErrCorrupt, check.detail)
		}
	}
	workflowRows, err := s.db.Query(`SELECT workflow_digest,workflow_id,workflow_version,schema_version,exact_yaml FROM workflow_snapshots`)
	if err != nil {
		return normalize(err)
	}
	for workflowRows.Next() {
		var digest, id, version string
		var schema uint32
		var exact []byte
		if err := workflowRows.Scan(&digest, &id, &version, &schema, &exact); err != nil {
			workflowRows.Close()
			return normalize(err)
		}
		snapshot, err := workflow.InspectSnapshot(id, version, schema, digest, exact)
		if err != nil || snapshot.Digest() != digest || snapshot.WorkflowId() != id || snapshot.WorkflowVersion() != version || snapshot.SchemaVersion() != schema {
			workflowRows.Close()
			return fmt.Errorf("%w: WorkflowSnapshot identity or digest mismatch", authority.ErrCorrupt)
		}
	}
	if err := workflowRows.Err(); err != nil {
		workflowRows.Close()
		return normalize(err)
	}
	if err := workflowRows.Close(); err != nil {
		return normalize(err)
	}
	changes, err := s.ListChanges()
	if err != nil {
		return err
	}
	for _, current := range changes {
		if current.ProjectId() != s.projectId {
			return fmt.Errorf("%w: cross-Project Change row", authority.ErrCorrupt)
		}
	}
	operations, err := s.db.Query(`SELECT operation_id,project_id,change_id,kind,request_digest,expected_revision,state,result,created_at,updated_at FROM operations`)
	if err != nil {
		return normalize(err)
	}
	defer operations.Close()
	for operations.Next() {
		value, err := scanOperation(operations)
		if err != nil {
			return err
		}
		if value.ProjectId != s.projectId {
			return fmt.Errorf("%w: cross-Project operation", authority.ErrCorrupt)
		}
		if value.ChangeId != "" {
			var count int
			if err := s.db.QueryRow(`SELECT COUNT(*) FROM changes WHERE change_id=?`, value.ChangeId).Scan(&count); err != nil {
				return normalize(err)
			}
			if count != 1 {
				return fmt.Errorf("%w: operation references missing Change", authority.ErrCorrupt)
			}
		}
	}
	if err := operations.Err(); err != nil {
		return normalize(err)
	}
	return nil
}

func (s *Store) Backup(destination string) error {
	destination, err := filepath.Abs(destination)
	if err != nil {
		return err
	}
	if _, err := os.Lstat(destination); err == nil {
		return fmt.Errorf("backup destination already exists")
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := ensurePrivateDirectory(filepath.Dir(destination)); err != nil {
		return err
	}
	var pageCount, pageSize int64
	if err := s.db.QueryRow(`PRAGMA page_count`).Scan(&pageCount); err != nil {
		return normalize(err)
	}
	if err := s.db.QueryRow(`PRAGMA page_size`).Scan(&pageSize); err != nil {
		return normalize(err)
	}
	if err := ensureBackupFreeSpace(destination, pageCount*pageSize); err != nil {
		return err
	}
	escaped := strings.ReplaceAll(destination, "'", "''")
	if _, err := s.db.Exec(`VACUUM INTO '` + escaped + `'`); err != nil {
		return fmt.Errorf("create SQLite snapshot: %w", normalize(err))
	}
	if err := os.Chmod(destination, 0o600); err != nil {
		return err
	}
	registration := project.Registration{ProjectId: s.projectId, RepositoryRoot: s.repositoryRoot}
	backup, err := openExisting(destination, registration)
	if err != nil {
		return fmt.Errorf("open backup independently: %w", err)
	}
	defer backup.Close()
	if err := backup.Validate(); err != nil {
		return fmt.Errorf("validate backup: %w", err)
	}
	return nil
}

func openExisting(path string, registration project.Registration) (*Store, error) {
	dsn := databaseDSN(path, "mode=rw&")
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	store := &Store{db: db, projectId: registration.ProjectId, repositoryRoot: registration.RepositoryRoot, path: path, dataDirectory: filepath.Dir(filepath.Dir(filepath.Dir(path)))}
	if err := store.configureAndVerify(); err != nil {
		db.Close()
		return nil, err
	}
	return store, nil
}

func insertWorkflow(tx *sql.Tx, snapshot workflow.WorkflowSnapshot) error {
	var existing string
	err := tx.QueryRow(`SELECT workflow_digest FROM workflow_snapshots WHERE workflow_id=? AND workflow_version=?`, snapshot.WorkflowId(), snapshot.WorkflowVersion()).Scan(&existing)
	if err == nil {
		if existing != snapshot.Digest() {
			return fmt.Errorf("workflow identity/version digest conflict")
		}
		return nil
	}
	if !errors.Is(err, sql.ErrNoRows) {
		return normalize(err)
	}
	_, err = tx.Exec(`INSERT INTO workflow_snapshots(workflow_digest,workflow_id,workflow_version,schema_version,exact_yaml) VALUES(?,?,?,?,?)`, snapshot.Digest(), snapshot.WorkflowId(), snapshot.WorkflowVersion(), snapshot.SchemaVersion(), snapshot.ExactYAML())
	return normalize(err)
}
func insertArtifact(tx *sql.Tx, item artifact.Artifact) error {
	_, err := tx.Exec(`INSERT INTO artifacts(artifact_id,change_id,project_id,kind,envelope_schema_version,kind_schema_version,media_type,byte_length,created_at,producer_component,producer_operation_id,payload,content_digest,record_digest) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?)`, item.Id(), item.ChangeId(), item.ProjectId(), item.Kind(), item.EnvelopeSchemaVersion(), item.KindSchemaVersion(), item.MediaType(), item.ByteLength(), timestamp(item.CreatedAt()), item.Producer().Component, item.Producer().OperationId, item.Payload(), item.ContentDigest(), item.RecordDigest())
	return normalize(err)
}
func insertAudit(tx *sql.Tx, event audit.Event, raw []byte, legacySeq uint64, sourceDigest string) error {
	if err := audit.ValidateEvent(event); err != nil {
		return err
	}
	metadata, err := json.Marshal(event.Metadata)
	if err != nil {
		return err
	}
	_, err = tx.Exec(`INSERT INTO audit_events(event_id,event_type,timestamp,project_id,change_id,repository_root,schema_version,metadata_json,raw_json,legacy_sequence,source_digest) VALUES(?,?,?,?,?,?,?,?,?,?,?)`, event.EventID, event.EventType, timestamp(event.Timestamp), event.ProjectID, event.ChangeID, event.RepositoryRoot, event.SchemaVersion, metadata, nullableBytes(raw), nullableUint(legacySeq), nullableString(sourceDigest))
	return normalize(err)
}
func lifecycleAudit(event workflow.LifecycleEvent, repositoryRoot string) (audit.Event, error) {
	metadata := map[string]any{"resulting_state": string(event.ResultingState), "transition_timestamp": event.OccurredAt.Format(time.RFC3339Nano), "context": event.Context}
	if event.PreviousState != "" {
		metadata["previous_state"] = string(event.PreviousState)
	}
	if event.Intent != "" {
		metadata["intent"] = string(event.Intent)
	}
	return audit.NewEvent(event.EventType, string(event.ProjectId), string(event.ChangeId), repositoryRoot, metadata, event.OccurredAt)
}
func verifySnapshot(snapshot workflow.WorkflowSnapshot) error {
	parsed, err := workflow.ParseSnapshot(snapshot.ExactYAML())
	if err != nil {
		return err
	}
	if parsed.Digest() != snapshot.Digest() || parsed.WorkflowId() != snapshot.WorkflowId() || parsed.WorkflowVersion() != snapshot.WorkflowVersion() {
		return fmt.Errorf("workflow snapshot identity mismatch")
	}
	return nil
}
func insertOperation(tx *sql.Tx, value authority.Operation) error {
	result := value.Result
	if result == nil {
		result = []byte{}
	}
	_, err := tx.Exec(`INSERT INTO operations(operation_id,project_id,change_id,kind,request_digest,expected_revision,state,result,created_at,updated_at) VALUES(?,?,?,?,?,?,?,?,?,?)`, value.Id, value.ProjectId, nullableString(string(value.ChangeId)), value.Kind, value.RequestDigest, value.ExpectedRevision, value.State, result, timestamp(value.CreatedAt), timestamp(value.UpdatedAt))
	return normalize(err)
}
func insertOrCompleteOperation(tx *sql.Tx, value authority.Operation) error {
	if err := authority.ValidateOperation(value); err != nil {
		return err
	}
	existing, found, err := getOperationTx(tx, value.Id)
	if err != nil {
		return err
	}
	if found {
		if !sameOperationRequest(existing, value) {
			return authority.ErrOperationConflict
		}
		if existing.State == authority.OperationCompleted {
			if value.State == authority.OperationCompleted && sameCompletedResult(existing.Kind, existing.Result, value.Result) {
				return nil
			}
			return authority.ErrOperationConflict
		}
		if existing.State != authority.OperationReserved || value.State == authority.OperationReserved {
			return nil
		}
		result, updateError := tx.Exec(`UPDATE operations SET state=?,result=?,updated_at=? WHERE operation_id=? AND state=?`, value.State, value.Result, timestamp(value.UpdatedAt), value.Id, authority.OperationReserved)
		if updateError != nil {
			return normalize(updateError)
		}
		rows, rowsError := result.RowsAffected()
		if rowsError != nil {
			return normalize(rowsError)
		}
		if rows != 1 {
			return fmt.Errorf("%w: operation %q is no longer reserved", authority.ErrOperationConflict, value.Id)
		}
		return nil
	}
	return insertOperation(tx, value)
}

// A completed canonical operation and the ApplicationResult being published
// must describe the same POST within the same short authority transaction.
func validateCanonicalCompletionCommit(commit authority.AuthorityCommit) error {
	operation := commit.Operation
	if operation == nil || operation.Kind != "canonical-git-apply" || operation.State != authority.OperationCompleted || commit.Candidate.State() != change.StateAuditLocked {
		return nil
	}
	result, err := authority.DecodeCanonicalResult(operation.Result)
	if err != nil {
		return err
	}
	var resultId artifact.ArtifactId
	for _, binding := range commit.Bindings {
		if binding.Role == "application-result" {
			if resultId != "" {
				return fmt.Errorf("%w: duplicate application-result binding", authority.ErrCorrupt)
			}
			resultId = binding.ArtifactId
		}
	}
	for _, item := range commit.Artifacts {
		if item.Id() != resultId || item.Kind() != artifact.KindApplicationResult {
			continue
		}
		var payload struct {
			OperationId    authority.OperationId `json:"operation_id"`
			CanonicalHead  string                `json:"canonical_head"`
			PatchDigest    string                `json:"patch_digest"`
			ChangedPaths   []string              `json:"changed_paths"`
			IndexUnchanged bool                  `json:"index_unchanged"`
		}
		if err := json.Unmarshal(item.Payload(), &payload); err != nil || payload.OperationId != operation.Id || payload.CanonicalHead != result.Head || payload.PatchDigest != result.Patch || !slices.Equal(payload.ChangedPaths, result.Paths) || payload.IndexUnchanged != result.Index {
			return fmt.Errorf("%w: ApplicationResult contradicts completed canonical operation", authority.ErrCorrupt)
		}
		return nil
	}
	return fmt.Errorf("%w: completed canonical operation requires matching ApplicationResult", authority.ErrCorrupt)
}

func sameOperationRequest(left, right authority.Operation) bool {
	return left.Id == right.Id && left.ProjectId == right.ProjectId && left.ChangeId == right.ChangeId && left.Kind == right.Kind && left.RequestDigest == right.RequestDigest && left.ExpectedRevision == right.ExpectedRevision
}

func sameCompletedResult(kind string, persisted, candidate []byte) bool {
	if kind != "canonical-git-apply" {
		return bytes.Equal(persisted, candidate)
	}
	left, leftError := authority.DecodeCanonicalResult(persisted)
	right, rightError := authority.DecodeCanonicalResult(candidate)
	return leftError == nil && rightError == nil && left.Equal(right)
}

type scanner interface{ Scan(...any) error }

func scanOperation(row scanner) (authority.Operation, error) {
	var value authority.Operation
	var changeId sql.NullString
	var created, updated string
	if err := row.Scan(&value.Id, &value.ProjectId, &changeId, &value.Kind, &value.RequestDigest, &value.ExpectedRevision, &value.State, &value.Result, &created, &updated); err != nil {
		return value, normalize(err)
	}
	value.ChangeId = change.ChangeId(changeId.String)
	var err error
	value.CreatedAt, err = parseTimestamp(created)
	if err != nil {
		return value, err
	}
	value.UpdatedAt, err = parseTimestamp(updated)
	if err != nil {
		return value, err
	}
	if err := authority.ValidateOperation(value); err != nil {
		return value, fmt.Errorf("%w: %v", authority.ErrCorrupt, err)
	}
	return value, nil
}
func getOperationTx(tx *sql.Tx, id authority.OperationId) (authority.Operation, bool, error) {
	value, err := scanOperation(tx.QueryRow(`SELECT operation_id,project_id,change_id,kind,request_digest,expected_revision,state,result,created_at,updated_at FROM operations WHERE operation_id=?`, id))
	if errors.Is(err, sql.ErrNoRows) {
		return authority.Operation{}, false, nil
	}
	return value, err == nil, err
}
func timestamp(value time.Time) string { return value.UTC().Format(time.RFC3339Nano) }
func parseTimestamp(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid durable timestamp %q: %w", value, err)
	}
	return parsed.UTC(), nil
}
func nullableBytes(value []byte) any {
	if len(value) == 0 {
		return nil
	}
	return value
}
func nullableUint(value uint64) any {
	if value == 0 {
		return nil
	}
	return value
}
func nullableString(value string) any {
	if value == "" {
		return nil
	}
	return value
}
func normalize(err error) error {
	if err == nil {
		return nil
	}
	text := strings.ToLower(err.Error())
	if strings.Contains(text, "database is locked") || strings.Contains(text, "sqlite_busy") {
		return fmt.Errorf("%w: %v", authority.ErrBusy, err)
	}
	return err
}
func ensurePrivateDirectory(path string) error {
	if info, err := os.Lstat(path); err == nil {
		if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
			return fmt.Errorf("path %q is not a regular directory", path)
		}
		if err := os.Chmod(path, 0o700); err != nil {
			return err
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(path, 0o700); err != nil {
		return fmt.Errorf("create private directory %q: %w", path, err)
	}
	return os.Chmod(path, 0o700)
}
func requireLocalFilesystem(path string) error {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(path, &stat); err != nil {
		return fmt.Errorf("inspect Project store filesystem: %w", err)
	}
	unsupported := map[int64]string{0x6969: "NFS", 0x517B: "SMB", 0x9fa0: "9P"}
	if name, ok := unsupported[int64(stat.Type)]; ok {
		return fmt.Errorf("unsupported non-local filesystem %s", name)
	}
	return nil
}
func ensureFreeSpace(path string, required int64) error {
	if required <= 0 {
		return nil
	}
	var stat syscall.Statfs_t
	if err := syscall.Statfs(filepath.Dir(path), &stat); err != nil {
		return fmt.Errorf("inspect free space: %w", err)
	}
	available := int64(stat.Bavail) * int64(stat.Bsize)
	if available < required {
		return fmt.Errorf("insufficient free space for %d artifact bytes", required)
	}
	return nil
}

func validateArtifactBudget(changeBytes, added, projectBytes, quota int64) error {
	if changeBytes < 0 || added < 0 || projectBytes < 0 || changeBytes > artifact.ChangeAggregateLimit-added {
		return fmt.Errorf("Change artifact payloads exceed %d-byte aggregate limit", artifact.ChangeAggregateLimit)
	}
	if quota > 0 && (projectBytes > quota-added) {
		return fmt.Errorf("Project artifact quota of %d bytes would be exceeded", quota)
	}
	return nil
}

func ensureBackupFreeSpace(destination string, databaseBytes int64) error {
	var stat syscall.Statfs_t
	if err := syscall.Statfs(filepath.Dir(destination), &stat); err != nil {
		return fmt.Errorf("inspect backup free space: %w", err)
	}
	available := int64(stat.Bavail) * int64(stat.Bsize)
	return validateBackupSpace(available, databaseBytes)
}

func validateBackupSpace(available, databaseBytes int64) error {
	required := backupSpaceRequired(databaseBytes)
	if available < required {
		return fmt.Errorf("insufficient free space for SQLite backup: require at least %d bytes, have %d", required, available)
	}
	return nil
}

func backupSpaceRequired(databaseBytes int64) int64 {
	if databaseBytes < 0 {
		return int64(^uint64(0) >> 1)
	}
	overhead := databaseBytes / 10
	if overhead < 1<<20 {
		overhead = 1 << 20
	}
	if databaseBytes > int64(^uint64(0)>>1)-overhead {
		return int64(^uint64(0) >> 1)
	}
	return databaseBytes + overhead
}
func databaseDSN(path, prefix string) string {
	// SQLite accepts an absolute file URI; query values are fixed by Praetor.
	return "file:" + filepath.ToSlash(path) + "?" + prefix + "_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=synchronous(FULL)&_pragma=busy_timeout(5000)"
}
