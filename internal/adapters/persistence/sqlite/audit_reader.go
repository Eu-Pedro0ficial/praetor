package sqlite

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"time"

	"github.com/Eu-Pedro0ficial/praetor/internal/audit"
)

// ReadAuditEvents supplies compatibility inspection for callers that need a
// merged view after the SQLite cutover. Concrete driver knowledge remains in
// this adapter.
func ReadAuditEvents(dataDirectory string) ([]audit.Event, error) {
	paths, err := filepath.Glob(filepath.Join(dataDirectory, "projects", "*", databaseName))
	if err != nil {
		return nil, err
	}
	if len(paths) == 0 {
		return audit.Read(dataDirectory)
	}
	type sequenced struct {
		event    audit.Event
		path     string
		sequence int64
	}
	var values []sequenced
	for _, path := range paths {
		db, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path)+"?mode=ro&_pragma=busy_timeout(5000)")
		if err != nil {
			return nil, err
		}
		rows, err := db.Query(`SELECT sequence,event_id,event_type,timestamp,project_id,change_id,repository_root,schema_version,metadata_json FROM audit_events ORDER BY sequence`)
		if err != nil {
			db.Close()
			return nil, err
		}
		for rows.Next() {
			var value sequenced
			var rawTimestamp string
			var metadata []byte
			value.path = path
			if err := rows.Scan(&value.sequence, &value.event.EventID, &value.event.EventType, &rawTimestamp, &value.event.ProjectID, &value.event.ChangeID, &value.event.RepositoryRoot, &value.event.SchemaVersion, &metadata); err != nil {
				rows.Close()
				db.Close()
				return nil, err
			}
			value.event.Timestamp, err = time.Parse(time.RFC3339Nano, rawTimestamp)
			if err != nil {
				rows.Close()
				db.Close()
				return nil, err
			}
			if err := json.Unmarshal(metadata, &value.event.Metadata); err != nil {
				rows.Close()
				db.Close()
				return nil, err
			}
			values = append(values, value)
		}
		rowError := rows.Err()
		closeRowsError := rows.Close()
		closeDBError := db.Close()
		if rowError != nil {
			return nil, rowError
		}
		if closeRowsError != nil {
			return nil, closeRowsError
		}
		if closeDBError != nil {
			return nil, closeDBError
		}
	}
	sort.SliceStable(values, func(i, j int) bool {
		if values[i].event.Timestamp.Equal(values[j].event.Timestamp) {
			if values[i].path == values[j].path {
				return values[i].sequence < values[j].sequence
			}
			return values[i].path < values[j].path
		}
		return values[i].event.Timestamp.Before(values[j].event.Timestamp)
	})
	result := make([]audit.Event, len(values))
	for index := range values {
		if err := audit.ValidateEvent(values[index].event); err != nil {
			return nil, fmt.Errorf("invalid durable audit event: %w", err)
		}
		result[index] = values[index].event
	}
	return result, nil
}
