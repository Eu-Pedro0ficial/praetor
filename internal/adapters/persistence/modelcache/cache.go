// Package modelcache implements the replaceable per-Project ADR-039 SQLite
// cache. It is deliberately separate from the M1.1 XDG DATA authority store.
package modelcache

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/Eu-Pedro0ficial/praetor/internal/project"
	"github.com/Eu-Pedro0ficial/praetor/internal/repositorymodel"
	_ "modernc.org/sqlite"
)

const (
	databaseName                = "repository-model-cache.sqlite3"
	maximumModelPayloadBytes    = 256 << 20
	maximumAnalysisPayloadBytes = 16 << 20
)

type Cache struct {
	db        *sql.DB
	path      string
	projectId project.ProjectId
}

func Open(cacheDirectory string, registration project.Registration) (*Cache, error) {
	if strings.TrimSpace(cacheDirectory) == "" || !registration.ProjectId.IsValid() {
		return nil, fmt.Errorf("valid cache directory and ProjectId are required")
	}
	base, err := filepath.Abs(cacheDirectory)
	if err != nil {
		return nil, err
	}
	projects := filepath.Join(base, "projects")
	projectDirectory := filepath.Join(projects, string(registration.ProjectId))
	if filepath.Dir(projectDirectory) != projects {
		return nil, fmt.Errorf("unsafe Project cache path")
	}
	for _, directory := range []string{base, projects, projectDirectory} {
		if err := ensurePrivateDirectory(directory); err != nil {
			return nil, err
		}
	}
	databasePath := filepath.Join(projectDirectory, databaseName)
	if info, statError := os.Lstat(databasePath); statError == nil && (!info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0) {
		return nil, fmt.Errorf("RepositoryModel cache path is not a regular file")
	} else if statError != nil && !os.IsNotExist(statError) {
		return nil, statError
	}
	cache, err := open(databasePath, registration.ProjectId)
	if err == nil {
		return cache, nil
	}
	// Cache is replaceable. A structurally corrupt or incompatible cache is
	// discarded, while its canonical inputs remain the governed repository.
	for _, candidate := range []string{databasePath, databasePath + "-wal", databasePath + "-shm"} {
		if info, statError := os.Lstat(candidate); statError == nil {
			if !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
				return nil, fmt.Errorf("refuse to replace unsafe cache file %q", candidate)
			}
			if removeError := os.Remove(candidate); removeError != nil {
				return nil, fmt.Errorf("discard invalid RepositoryModel cache: %w", removeError)
			}
		} else if !os.IsNotExist(statError) {
			return nil, statError
		}
	}
	return open(databasePath, registration.ProjectId)
}

func open(databasePath string, projectId project.ProjectId) (*Cache, error) {
	dsn := "file:" + filepath.ToSlash(databasePath) + "?_pragma=foreign_keys(1)&_pragma=journal_mode(WAL)&_pragma=synchronous(FULL)&_pragma=busy_timeout(5000)"
	db, err := sql.Open("sqlite", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(4)
	cache := &Cache{db: db, path: databasePath, projectId: projectId}
	if err := cache.initialize(); err != nil {
		db.Close()
		return nil, err
	}
	if err := os.Chmod(databasePath, 0o600); err != nil {
		db.Close()
		return nil, err
	}
	return cache, nil
}

func (cache *Cache) initialize() error {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	var integrity string
	if err := cache.db.QueryRowContext(ctx, `PRAGMA integrity_check`).Scan(&integrity); err != nil || integrity != "ok" {
		return fmt.Errorf("%w: SQLite integrity check failed", repositorymodel.ErrCacheCorrupt)
	}
	var version int
	if err := cache.db.QueryRowContext(ctx, `PRAGMA user_version`).Scan(&version); err != nil {
		return err
	}
	if version > 1 {
		return fmt.Errorf("%w: schema version %d", repositorymodel.ErrCacheIncompatible, version)
	}
	if version == 0 {
		tx, err := cache.db.BeginTx(ctx, nil)
		if err != nil {
			return err
		}
		defer tx.Rollback()
		statements := []string{
			`CREATE TABLE cache_metadata (key TEXT PRIMARY KEY, value TEXT NOT NULL)`,
			`CREATE TABLE models (build_key TEXT PRIMARY KEY, schema_version INTEGER NOT NULL, model_digest TEXT NOT NULL, payload_digest TEXT NOT NULL, payload BLOB NOT NULL)`,
			`CREATE TABLE file_analyses (path TEXT NOT NULL, content_key TEXT NOT NULL, analyzer_set_digest TEXT NOT NULL, payload_digest TEXT NOT NULL, payload BLOB NOT NULL, PRIMARY KEY(path,content_key,analyzer_set_digest))`,
			`INSERT INTO cache_metadata(key,value) VALUES ('project_id', ?), ('schema_version', '1')`,
			`PRAGMA user_version = 1`,
		}
		for _, statement := range statements {
			var execError error
			if strings.Contains(statement, "?") {
				_, execError = tx.ExecContext(ctx, statement, string(cache.projectId))
			} else {
				_, execError = tx.ExecContext(ctx, statement)
			}
			if execError != nil {
				return execError
			}
		}
		if err := tx.Commit(); err != nil {
			return err
		}
	}
	var stored string
	if err := cache.db.QueryRowContext(ctx, `SELECT value FROM cache_metadata WHERE key='project_id'`).Scan(&stored); err != nil {
		return fmt.Errorf("%w: missing Project metadata", repositorymodel.ErrCacheCorrupt)
	}
	if stored != string(cache.projectId) {
		return fmt.Errorf("%w: ProjectId mismatch", repositorymodel.ErrCacheCorrupt)
	}
	return nil
}

func (cache *Cache) LoadModel(key repositorymodel.BuildKey) (repositorymodel.Model, error) {
	if err := repositorymodel.ValidateBuildKey(key); err != nil {
		return repositorymodel.Model{}, err
	}
	var schema uint32
	var modelDigest, payloadDigest string
	var payload []byte
	var payloadLength int64
	err := cache.db.QueryRow(`SELECT schema_version,model_digest,payload_digest,length(payload),CASE WHEN length(payload)<=? THEN payload ELSE NULL END FROM models WHERE build_key=?`, maximumModelPayloadBytes, key.Digest).Scan(&schema, &modelDigest, &payloadDigest, &payloadLength, &payload)
	if errors.Is(err, sql.ErrNoRows) {
		return repositorymodel.Model{}, repositorymodel.ErrCacheMiss
	}
	if err != nil {
		return repositorymodel.Model{}, err
	}
	if payloadLength > maximumModelPayloadBytes {
		return repositorymodel.Model{}, repositorymodel.ErrCacheCorrupt
	}
	if schema != repositorymodel.SchemaVersion {
		return repositorymodel.Model{}, repositorymodel.ErrCacheIncompatible
	}
	if digest(payload) != payloadDigest {
		return repositorymodel.Model{}, repositorymodel.ErrCacheCorrupt
	}
	var model repositorymodel.Model
	if err := json.Unmarshal(payload, &model); err != nil {
		return repositorymodel.Model{}, fmt.Errorf("%w: %v", repositorymodel.ErrCacheCorrupt, err)
	}
	if model.ProjectId() != cache.projectId || model.BuildKey().Digest != key.Digest || model.ModelDigest() != modelDigest {
		return repositorymodel.Model{}, repositorymodel.ErrCacheCorrupt
	}
	return model, nil
}

func (cache *Cache) StoreModel(model repositorymodel.Model) error {
	if model.ProjectId() != cache.projectId {
		return fmt.Errorf("cross-Project RepositoryModel cache write rejected")
	}
	payload, err := json.Marshal(model)
	if err != nil {
		return err
	}
	if len(payload) > maximumModelPayloadBytes {
		return fmt.Errorf("RepositoryModel cache payload exceeds %d-byte limit", maximumModelPayloadBytes)
	}
	_, err = cache.db.Exec(`INSERT INTO models(build_key,schema_version,model_digest,payload_digest,payload) VALUES(?,?,?,?,?) ON CONFLICT(build_key) DO UPDATE SET schema_version=excluded.schema_version,model_digest=excluded.model_digest,payload_digest=excluded.payload_digest,payload=excluded.payload`, model.BuildKey().Digest, repositorymodel.SchemaVersion, model.ModelDigest(), digest(payload), payload)
	return err
}

func (cache *Cache) LoadFileAnalysis(pathValue, contentKey, analyzerSetDigest string) (repositorymodel.FileAnalysis, error) {
	var payloadDigest string
	var payload []byte
	var payloadLength int64
	err := cache.db.QueryRow(`SELECT payload_digest,length(payload),CASE WHEN length(payload)<=? THEN payload ELSE NULL END FROM file_analyses WHERE path=? AND content_key=? AND analyzer_set_digest=?`, maximumAnalysisPayloadBytes, pathValue, contentKey, analyzerSetDigest).Scan(&payloadDigest, &payloadLength, &payload)
	if errors.Is(err, sql.ErrNoRows) {
		return repositorymodel.FileAnalysis{}, repositorymodel.ErrCacheMiss
	}
	if err != nil {
		return repositorymodel.FileAnalysis{}, err
	}
	if payloadLength > maximumAnalysisPayloadBytes {
		return repositorymodel.FileAnalysis{}, repositorymodel.ErrCacheCorrupt
	}
	if digest(payload) != payloadDigest {
		return repositorymodel.FileAnalysis{}, repositorymodel.ErrCacheCorrupt
	}
	var analysis repositorymodel.FileAnalysis
	if err := json.Unmarshal(payload, &analysis); err != nil {
		return repositorymodel.FileAnalysis{}, repositorymodel.ErrCacheCorrupt
	}
	if analysis.Path != pathValue || analysis.ContentKey != contentKey {
		return repositorymodel.FileAnalysis{}, repositorymodel.ErrCacheCorrupt
	}
	return analysis, nil
}

func (cache *Cache) StoreFileAnalysis(analysis repositorymodel.FileAnalysis, analyzerSetDigest string) error {
	payload, err := json.Marshal(analysis)
	if err != nil {
		return err
	}
	if len(payload) > maximumAnalysisPayloadBytes {
		return fmt.Errorf("file analysis cache payload exceeds %d-byte limit", maximumAnalysisPayloadBytes)
	}
	_, err = cache.db.Exec(`INSERT INTO file_analyses(path,content_key,analyzer_set_digest,payload_digest,payload) VALUES(?,?,?,?,?) ON CONFLICT(path,content_key,analyzer_set_digest) DO UPDATE SET payload_digest=excluded.payload_digest,payload=excluded.payload`, analysis.Path, analysis.ContentKey, analyzerSetDigest, digest(payload), payload)
	return err
}

func (cache *Cache) Path() string { return cache.path }
func (cache *Cache) Close() error {
	if cache == nil || cache.db == nil {
		return nil
	}
	return cache.db.Close()
}

func digest(value []byte) string {
	sum := sha256.Sum256(value)
	return "sha256:" + hex.EncodeToString(sum[:])
}
func ensurePrivateDirectory(value string) error {
	if info, err := os.Lstat(value); err == nil {
		if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
			return fmt.Errorf("cache path %q is not a directory", value)
		}
		if err := os.Chmod(value, 0o700); err != nil {
			return err
		}
		return nil
	} else if !os.IsNotExist(err) {
		return err
	}
	if err := os.MkdirAll(value, 0o700); err != nil {
		return err
	}
	return os.Chmod(value, 0o700)
}
