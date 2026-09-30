package sqlite_test

import (
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/Eu-Pedro0ficial/praetor/internal/adapters/persistence/sqlite"
	"github.com/Eu-Pedro0ficial/praetor/internal/artifact"
	"github.com/Eu-Pedro0ficial/praetor/internal/audit"
	"github.com/Eu-Pedro0ficial/praetor/internal/authority"
	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"github.com/Eu-Pedro0ficial/praetor/internal/project"
	"github.com/Eu-Pedro0ficial/praetor/internal/workflow"
)

func TestChangeArtifactAuthoritySurvivesRestart(t *testing.T) {
	data, registration := fixture(t)
	store := open(t, data, registration)
	created := create(t, store, registration.ProjectId, "durable")
	candidate := created
	transition, err := candidate.Transition(change.StatePlanned, time.Now().UTC(), "planned")
	if err != nil {
		t.Fatal(err)
	}
	id, _ := artifact.GenerateId()
	payload := []byte("exact governed bytes")
	item, err := artifact.New(id, registration.ProjectId, candidate.ChangeId(), artifact.KindApprovedScope, 1, 1, "application/json", time.Now(), artifact.Producer{Component: "test"}, payload, false)
	if err != nil {
		t.Fatal(err)
	}
	event, err := audit.NewEvent(workflow.EventChangeTransition, string(registration.ProjectId), string(candidate.ChangeId()), registration.RepositoryRoot, map[string]any{"context": "planned"}, transition.OccurredAt)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CommitAuthority(authority.AuthorityCommit{ExpectedRevision: created.Revision(), Candidate: candidate, Artifacts: []artifact.Artifact{item}, Bindings: []authority.ArtifactBinding{{Role: "approved-scope", ArtifactId: id, Revision: candidate.Revision()}}, AuditEvents: []audit.Event{event}}); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}

	store = open(t, data, registration)
	defer store.Close()
	recovered, snapshot, err := store.GetChange(candidate.ChangeId())
	if err != nil {
		t.Fatal(err)
	}
	if recovered.State() != change.StatePlanned || recovered.Revision() != 2 {
		t.Fatalf("recovered = %s revision %d", recovered.State(), recovered.Revision())
	}
	core, _ := workflow.CoreV0Snapshot()
	if snapshot.Digest() != core.Digest() || !bytes.Equal(snapshot.ExactYAML(), core.ExactYAML()) {
		t.Fatal("workflow authority changed across restart")
	}
	loaded, err := store.GetArtifact(candidate.ChangeId(), id, true)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(loaded.Payload(), payload) || loaded.ContentDigest() != item.ContentDigest() {
		t.Fatal("artifact authority changed across restart")
	}
	bindings, err := store.ListBindings(candidate.ChangeId())
	if err != nil || len(bindings) != 1 || bindings[0].ArtifactId != id {
		t.Fatalf("bindings = %#v, %v", bindings, err)
	}
}

func TestAuthorityCommitRollbackAndExpectedRevision(t *testing.T) {
	data, registration := fixture(t)
	store := open(t, data, registration)
	defer store.Close()
	created := create(t, store, registration.ProjectId, "atomic")
	candidate := created
	_, _ = candidate.Transition(change.StatePlanned, time.Now(), "planned")
	missing := artifact.ArtifactId("art-00000000000000000000000000000000")
	err := store.CommitAuthority(authority.AuthorityCommit{ExpectedRevision: created.Revision(), Candidate: candidate, Bindings: []authority.ArtifactBinding{{Role: "patch", ArtifactId: missing, Revision: candidate.Revision()}}})
	if err == nil {
		t.Fatal("invalid binding committed")
	}
	current, _, err := store.GetChange(created.ChangeId())
	if err != nil {
		t.Fatal(err)
	}
	if current.Revision() != created.Revision() || current.State() != created.State() {
		t.Fatal("failed transaction leaked Change update")
	}
	if err := store.CommitTransition(created.Revision(), candidate, lifecycle(candidate, change.StateCreated, "planned")); err != nil {
		t.Fatal(err)
	}
	if err := store.CommitTransition(created.Revision(), candidate, lifecycle(candidate, change.StateCreated, "stale")); !errors.Is(err, authority.ErrStaleRevision) {
		t.Fatalf("stale error = %v", err)
	}
}

func TestOperationIdempotencyAndConflict(t *testing.T) {
	data, registration := fixture(t)
	store := open(t, data, registration)
	defer store.Close()
	created := create(t, store, registration.ProjectId, "operation")
	id, _ := authority.GenerateOperationId()
	now := time.Now().UTC()
	request := authority.Operation{Id: id, ProjectId: registration.ProjectId, ChangeId: created.ChangeId(), Kind: "git-apply", RequestDigest: digest("request"), ExpectedRevision: created.Revision(), State: authority.OperationReserved, CreatedAt: now, UpdatedAt: now}
	first, replayed, err := store.ReserveOperation(request)
	if err != nil || replayed || first.Id != id {
		t.Fatalf("first reserve = %#v %v %v", first, replayed, err)
	}
	second, replayed, err := store.ReserveOperation(request)
	if err != nil || !replayed || second.Id != id {
		t.Fatalf("retry = %#v %v %v", second, replayed, err)
	}
	request.RequestDigest = digest("different")
	if _, _, err := store.ReserveOperation(request); !errors.Is(err, authority.ErrOperationConflict) {
		t.Fatalf("conflict = %v", err)
	}
	request.RequestDigest = digest("request")
	request.ChangeId = create(t, store, registration.ProjectId, "other-operation-change").ChangeId()
	if _, _, err := store.ReserveOperation(request); !errors.Is(err, authority.ErrOperationConflict) {
		t.Fatalf("cross-Change replay conflict = %v", err)
	}
	request.ChangeId = created.ChangeId()
	completed, err := store.CompleteOperation(id, digest("request"), []byte(`{"result":"post"}`), nil)
	if err != nil || completed.State != authority.OperationCompleted {
		t.Fatalf("complete = %#v %v", completed, err)
	}
	if _, err := store.CompleteOperation(id, digest("request"), []byte(`{"result":"different"}`), nil); !errors.Is(err, authority.ErrOperationConflict) {
		t.Fatalf("completed outcome replacement error = %v", err)
	}
}

func TestFailedOperationIsTerminalIdempotentAndAudited(t *testing.T) {
	data, registration := fixture(t)
	store := open(t, data, registration)
	defer store.Close()
	created := create(t, store, registration.ProjectId, "failed-operation")
	id, _ := authority.GenerateOperationId()
	now := time.Now().UTC()
	requestDigest := digest("workspace-request")
	operation := authority.Operation{Id: id, ProjectId: registration.ProjectId, ChangeId: created.ChangeId(), Kind: "proposal-workspace-create", RequestDigest: requestDigest, ExpectedRevision: created.Revision(), State: authority.OperationReserved, CreatedAt: now, UpdatedAt: now}
	if _, _, err := store.ReserveOperation(operation); err != nil {
		t.Fatal(err)
	}
	event, err := audit.NewEvent(audit.EventOperationRecovered, string(registration.ProjectId), string(created.ChangeId()), registration.RepositoryRoot, map[string]any{"outcome": "compensated"}, now)
	if err != nil {
		t.Fatal(err)
	}
	result := []byte(`{"outcome":"compensated"}`)
	failed, err := store.FailOperation(id, requestDigest, result, []audit.Event{event})
	if err != nil || failed.State != authority.OperationFailed {
		t.Fatalf("FailOperation() = %#v, %v", failed, err)
	}
	if incomplete, err := store.ListIncompleteOperations(); err != nil || len(incomplete) != 0 {
		t.Fatalf("incomplete after failure = %#v, %v", incomplete, err)
	}
	if replay, err := store.FailOperation(id, requestDigest, result, nil); err != nil || replay.State != authority.OperationFailed {
		t.Fatalf("idempotent FailOperation() = %#v, %v", replay, err)
	}
	if _, err := store.CompleteOperation(id, requestDigest, []byte(`{"outcome":"created"}`), nil); !errors.Is(err, authority.ErrOperationConflict) {
		t.Fatalf("failed operation became completed: %v", err)
	}
	history, err := store.AuditHistory(created.ChangeId())
	if err != nil || history[len(history)-1].EventType != audit.EventOperationRecovered {
		t.Fatalf("recovery audit = %#v, %v", history, err)
	}
}

func TestConcurrentExpectedRevisionHasOneWinner(t *testing.T) {
	data, registration := fixture(t)
	initial := open(t, data, registration)
	created := create(t, initial, registration.ProjectId, "concurrent")
	initial.Close()
	a := open(t, data, registration)
	defer a.Close()
	b := open(t, data, registration)
	defer b.Close()
	candidate := created
	_, _ = candidate.Transition(change.StatePlanned, time.Now(), "planned")
	start := make(chan struct{})
	errs := make(chan error, 2)
	var wait sync.WaitGroup
	wait.Add(2)
	for _, store := range []*sqlite.Store{a, b} {
		go func(store *sqlite.Store) {
			defer wait.Done()
			<-start
			errs <- store.CommitTransition(created.Revision(), candidate, lifecycle(candidate, change.StateCreated, "race"))
		}(store)
	}
	close(start)
	wait.Wait()
	close(errs)
	wins, stale := 0, 0
	for err := range errs {
		if err == nil {
			wins++
		} else if errors.Is(err, authority.ErrStaleRevision) {
			stale++
		} else {
			t.Fatalf("unexpected error: %v", err)
		}
	}
	if wins != 1 || stale != 1 {
		t.Fatalf("wins=%d stale=%d", wins, stale)
	}
}

func TestArtifactLimitsValidationAndBackup(t *testing.T) {
	data, registration := fixture(t)
	store := open(t, data, registration)
	defer store.Close()
	created := create(t, store, registration.ProjectId, "limits")
	id, _ := artifact.GenerateId()
	if _, err := artifact.New(id, registration.ProjectId, created.ChangeId(), artifact.KindPatch, 1, 1, "application/octet-stream", time.Now(), artifact.Producer{Component: "test"}, make([]byte, artifact.LargeArtifactBoundary+1), false); err == nil {
		t.Fatal("large artifact accepted without explicit handling")
	}
	backupPath := filepath.Join(t.TempDir(), "backup.sqlite3")
	if err := store.Backup(backupPath); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(backupPath)
	if err != nil {
		t.Fatal(err)
	}
	if info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("backup permissions = %o", info.Mode().Perm())
	}
	if err := store.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestConfiguredProjectQuotaRejectsBeforeArtifactCommit(t *testing.T) {
	data, registration := fixture(t)
	store, err := sqlite.OpenWithOptions(data, registration, sqlite.Options{ProjectQuotaBytes: 8})
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	created := create(t, store, registration.ProjectId, "quota")
	id, _ := artifact.GenerateId()
	item, err := artifact.New(id, registration.ProjectId, created.ChangeId(), artifact.KindPatch, 1, 1, "application/octet-stream", time.Now(), artifact.Producer{Component: "test"}, []byte("ninebytes"), false)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CommitArtifacts(created.ChangeId(), created.Revision(), []artifact.Artifact{item}, nil, nil, nil, nil); err == nil {
		t.Fatal("configured Project quota was not enforced")
	}
	if total, err := store.ProjectArtifactBytes(); err != nil || total != 0 {
		t.Fatalf("artifact bytes=%d error=%v", total, err)
	}
}

func TestArtifactRelationshipsAndSupersessionRemainAppendOriented(t *testing.T) {
	data, registration := fixture(t)
	store := open(t, data, registration)
	defer store.Close()
	current := create(t, store, registration.ProjectId, "relationships")
	firstId, _ := artifact.GenerateId()
	first, err := artifact.New(firstId, registration.ProjectId, current.ChangeId(), artifact.KindApprovedScope, 1, 1, "application/json", time.Now().UTC(), artifact.Producer{Component: "test"}, []byte("first immutable payload"), false)
	if err != nil {
		t.Fatal(err)
	}
	if err := store.CommitArtifacts(current.ChangeId(), current.Revision(), []artifact.Artifact{first}, nil, nil, []authority.ArtifactBinding{{Role: "approved-scope", ArtifactId: firstId, Revision: current.Revision()}}, nil); err != nil {
		t.Fatal(err)
	}
	previous := current
	if _, err := current.Transition(change.StatePlanned, time.Now().UTC(), "advance binding revision"); err != nil {
		t.Fatal(err)
	}
	if err := store.CommitTransition(previous.Revision(), current, lifecycle(current, previous.State(), "advance binding revision")); err != nil {
		t.Fatal(err)
	}
	secondId, _ := artifact.GenerateId()
	second, err := artifact.New(secondId, registration.ProjectId, current.ChangeId(), artifact.KindApprovedScope, 1, 2, "application/json", time.Now().UTC().Add(time.Second), artifact.Producer{Component: "test"}, []byte("second immutable payload"), false)
	if err != nil {
		t.Fatal(err)
	}
	relation := artifact.Relationship{From: secondId, To: firstId, Kind: artifact.RelationshipParent}
	supersession := artifact.Supersession{Previous: firstId, Current: secondId}
	if err := store.CommitArtifacts(current.ChangeId(), current.Revision(), []artifact.Artifact{second}, []artifact.Relationship{relation}, []artifact.Supersession{supersession}, []authority.ArtifactBinding{{Role: "approved-scope", ArtifactId: secondId, Revision: current.Revision()}}, nil); err != nil {
		t.Fatal(err)
	}
	relationships, err := store.ListRelationships(current.ChangeId())
	if err != nil || len(relationships) != 1 || relationships[0] != relation {
		t.Fatalf("relationships=%#v err=%v", relationships, err)
	}
	bindings, err := store.ListBindings(current.ChangeId())
	if err != nil || len(bindings) != 1 || bindings[0].ArtifactId != secondId || bindings[0].Revision != current.Revision() {
		t.Fatalf("current bindings=%#v err=%v", bindings, err)
	}
	loadedFirst, err := store.GetArtifact(current.ChangeId(), firstId, true)
	if err != nil || string(loadedFirst.Payload()) != "first immutable payload" || loadedFirst.RecordDigest() != first.RecordDigest() {
		t.Fatalf("predecessor changed=%#v err=%v", loadedFirst, err)
	}
	raw, err := sql.Open("sqlite", store.Path())
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	var supersededBy artifact.ArtifactId
	if err := raw.QueryRow(`SELECT current_artifact_id FROM artifact_supersessions WHERE previous_artifact_id=?`, firstId).Scan(&supersededBy); err != nil || supersededBy != secondId {
		t.Fatalf("supersession current=%q err=%v", supersededBy, err)
	}

	other := create(t, store, registration.ProjectId, "other-relationship-change")
	otherId, _ := artifact.GenerateId()
	otherArtifact, _ := artifact.New(otherId, registration.ProjectId, other.ChangeId(), artifact.KindPatch, 1, 1, "application/octet-stream", time.Now().UTC(), artifact.Producer{Component: "test"}, []byte("other"), false)
	if err := store.CommitArtifacts(other.ChangeId(), other.Revision(), []artifact.Artifact{otherArtifact}, nil, nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	if err := store.CommitArtifacts(current.ChangeId(), current.Revision(), nil, []artifact.Relationship{{From: secondId, To: otherId, Kind: artifact.RelationshipDependency}}, nil, nil, nil); err == nil {
		t.Fatal("cross-Change relationship was accepted")
	}
	foreignProject, _ := project.GenerateProjectID()
	foreignId, _ := artifact.GenerateId()
	foreign, _ := artifact.New(foreignId, foreignProject, current.ChangeId(), artifact.KindPatch, 1, 1, "application/octet-stream", time.Now().UTC(), artifact.Producer{Component: "test"}, []byte("foreign"), false)
	if err := store.CommitArtifacts(current.ChangeId(), current.Revision(), []artifact.Artifact{foreign}, nil, nil, nil, nil); err == nil {
		t.Fatal("cross-Project artifact was accepted")
	}
}

func TestWALReaderRemainsAvailableDuringWriteTransaction(t *testing.T) {
	data, registration := fixture(t)
	store := open(t, data, registration)
	defer store.Close()
	created := create(t, store, registration.ProjectId, "wal-reader")
	raw, err := sql.Open("sqlite", store.Path())
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	tx, err := raw.Begin()
	if err != nil {
		t.Fatal(err)
	}
	defer tx.Rollback()
	if _, err := tx.Exec(`UPDATE changes SET intent=intent WHERE change_id=?`, created.ChangeId()); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() {
		values, err := store.ListChanges()
		if err == nil && len(values) != 1 {
			err = errors.New("reader returned unexpected Change count")
		}
		done <- err
	}()
	select {
	case err := <-done:
		if err != nil {
			t.Fatal(err)
		}
	case <-time.After(time.Second):
		t.Fatal("WAL reader blocked behind writer")
	}
}

func TestLegacyAuditMigrationIsIdempotentAndPreservesSource(t *testing.T) {
	data, registration := fixture(t)
	if _, err := audit.AppendChange(data, audit.EventChangeCreated, string(registration.ProjectId), "legacy-change", registration.RepositoryRoot, map[string]any{"intent": "legacy"}); err != nil {
		t.Fatal(err)
	}
	legacyPath := filepath.Join(data, "audit.log")
	before, err := os.ReadFile(legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := sqlite.MigrateLegacyAudit(data, []project.Registration{registration}); err != nil {
		t.Fatal(err)
	}
	after, err := os.ReadFile(legacyPath)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("legacy source was modified")
	}
	store := open(t, data, registration)
	defer store.Close()
	events, err := store.AuditHistory("legacy-change")
	if err != nil || len(events) != 1 {
		t.Fatalf("events=%#v err=%v", events, err)
	}
	if err := sqlite.MigrateLegacyAudit(data, []project.Registration{registration}); err != nil {
		t.Fatal(err)
	}
	if _, err := audit.Append(data, audit.EventInitialization, string(registration.ProjectId), registration.RepositoryRoot, nil); err == nil {
		t.Fatal("retired legacy writer accepted append")
	}
}

func TestMalformedLegacyAuditBlocksMigration(t *testing.T) {
	data, registration := fixture(t)
	if err := os.MkdirAll(data, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(data, "audit.log"), []byte("{malformed}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := sqlite.MigrateLegacyAudit(data, []project.Registration{registration}); err == nil {
		t.Fatal("malformed legacy audit migrated")
	}
	if _, err := os.Stat(filepath.Join(data, "audit-migration-v1.json")); !os.IsNotExist(err) {
		t.Fatalf("retirement marker exists: %v", err)
	}
}

func TestLegacyAuditMigrationRejectsBroadSourcePermissions(t *testing.T) {
	data, registration := fixture(t)
	if err := os.MkdirAll(data, 0o700); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(data, "audit.log")
	if err := os.WriteFile(path, nil, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := sqlite.MigrateLegacyAudit(data, []project.Registration{registration}); err == nil || !strings.Contains(err.Error(), "permissions") {
		t.Fatalf("broad legacy permissions error=%v", err)
	}
	if _, err := os.Stat(filepath.Join(data, "audit-migration-v1.json")); !os.IsNotExist(err) {
		t.Fatalf("retirement marker exists after rejected source: %v", err)
	}
}

func TestRealProcessesCompeteOnExpectedRevision(t *testing.T) {
	data, registration := fixture(t)
	store := open(t, data, registration)
	created := create(t, store, registration.ProjectId, "process-race")
	store.Close()
	gate := filepath.Join(t.TempDir(), "gate")
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	commands := make([]*exec.Cmd, 2)
	for index := range commands {
		commands[index] = exec.Command(executable, "-test.run=TestSQLiteHelperProcess")
		commands[index].Env = append(os.Environ(), "PRAETOR_SQLITE_HELPER=transition", "PRAETOR_HELPER_DATA="+data, "PRAETOR_HELPER_PROJECT="+string(registration.ProjectId), "PRAETOR_HELPER_ROOT="+registration.RepositoryRoot, "PRAETOR_HELPER_CHANGE="+string(created.ChangeId()), "PRAETOR_HELPER_GATE="+gate)
		if err := commands[index].Start(); err != nil {
			t.Fatal(err)
		}
	}
	time.Sleep(100 * time.Millisecond)
	if err := os.WriteFile(gate, []byte("go"), 0o600); err != nil {
		t.Fatal(err)
	}
	success, failed := 0, 0
	for _, command := range commands {
		if err := command.Wait(); err == nil {
			success++
		} else {
			failed++
		}
	}
	if success != 1 || failed != 1 {
		t.Fatalf("process winners=%d failures=%d", success, failed)
	}
	reopened := open(t, data, registration)
	defer reopened.Close()
	current, _, err := reopened.GetChange(created.ChangeId())
	if err != nil || current.State() != change.StatePlanned || current.Revision() != 2 {
		t.Fatalf("current=%#v err=%v", current, err)
	}
}

func TestKilledProcessLeavesDiagnosableOperation(t *testing.T) {
	data, registration := fixture(t)
	store := open(t, data, registration)
	created := create(t, store, registration.ProjectId, "killed-operation")
	store.Close()
	ready := filepath.Join(t.TempDir(), "ready")
	executable, _ := os.Executable()
	command := exec.Command(executable, "-test.run=TestSQLiteHelperProcess")
	command.Env = append(os.Environ(), "PRAETOR_SQLITE_HELPER=reserve", "PRAETOR_HELPER_DATA="+data, "PRAETOR_HELPER_PROJECT="+string(registration.ProjectId), "PRAETOR_HELPER_ROOT="+registration.RepositoryRoot, "PRAETOR_HELPER_CHANGE="+string(created.ChangeId()), "PRAETOR_HELPER_GATE="+ready)
	if err := command.Start(); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		if _, err := os.Stat(ready); err == nil {
			break
		}
		if time.Now().After(deadline) {
			_ = command.Process.Kill()
			t.Fatal("helper did not reserve operation")
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = command.Wait()
	reopened := open(t, data, registration)
	defer reopened.Close()
	operations, err := reopened.ListIncompleteOperations()
	if err != nil || len(operations) != 1 || operations[0].ChangeId != created.ChangeId() {
		t.Fatalf("incomplete=%#v err=%v", operations, err)
	}
}

func TestProcessKillAtTransactionBoundary(t *testing.T) {
	for _, test := range []struct {
		mode     string
		want     change.ChangeState
		revision uint64
	}{{"uncommitted", change.StateCreated, 1}, {"committed", change.StatePlanned, 2}} {
		t.Run(test.mode, func(t *testing.T) {
			data, registration := fixture(t)
			store := open(t, data, registration)
			created := create(t, store, registration.ProjectId, "kill-boundary")
			path := store.Path()
			store.Close()
			ready := filepath.Join(t.TempDir(), "ready")
			executable, _ := os.Executable()
			command := exec.Command(executable, "-test.run=TestSQLiteHelperProcess")
			command.Env = append(os.Environ(), "PRAETOR_SQLITE_HELPER="+test.mode, "PRAETOR_HELPER_DATA="+data, "PRAETOR_HELPER_DB="+path, "PRAETOR_HELPER_PROJECT="+string(registration.ProjectId), "PRAETOR_HELPER_ROOT="+registration.RepositoryRoot, "PRAETOR_HELPER_CHANGE="+string(created.ChangeId()), "PRAETOR_HELPER_GATE="+ready)
			if err := command.Start(); err != nil {
				t.Fatal(err)
			}
			deadline := time.Now().Add(5 * time.Second)
			for {
				if _, err := os.Stat(ready); err == nil {
					break
				}
				if time.Now().After(deadline) {
					_ = command.Process.Kill()
					t.Fatal("helper not ready")
				}
				time.Sleep(10 * time.Millisecond)
			}
			_ = command.Process.Kill()
			_ = command.Wait()
			reopened := open(t, data, registration)
			defer reopened.Close()
			current, _, err := reopened.GetChange(created.ChangeId())
			if err != nil {
				t.Fatal(err)
			}
			if current.State() != test.want || current.Revision() != test.revision {
				t.Fatalf("state=%s revision=%d", current.State(), current.Revision())
			}
		})
	}
}

func TestSQLiteHelperProcess(t *testing.T) {
	mode := os.Getenv("PRAETOR_SQLITE_HELPER")
	if mode == "" {
		return
	}
	registration := project.Registration{ProjectId: project.ProjectId(os.Getenv("PRAETOR_HELPER_PROJECT")), RepositoryRoot: os.Getenv("PRAETOR_HELPER_ROOT"), SchemaVersion: 1, CreatedAt: time.Now()}
	store, err := sqlite.Open(os.Getenv("PRAETOR_HELPER_DATA"), registration)
	if err != nil {
		os.Exit(20)
	}
	defer store.Close()
	id := change.ChangeId(os.Getenv("PRAETOR_HELPER_CHANGE"))
	switch mode {
	case "transition":
		for {
			if _, err := os.Stat(os.Getenv("PRAETOR_HELPER_GATE")); err == nil {
				break
			}
			time.Sleep(5 * time.Millisecond)
		}
		current, _, err := store.GetChange(id)
		if err != nil {
			os.Exit(21)
		}
		candidate := current
		if _, err := candidate.Transition(change.StatePlanned, time.Now(), "process race"); err != nil {
			os.Exit(22)
		}
		if err := store.CommitTransition(current.Revision(), candidate, lifecycle(candidate, current.State(), "process race")); err != nil {
			os.Exit(23)
		}
		os.Exit(0)
	case "reserve":
		current, _, err := store.GetChange(id)
		if err != nil {
			os.Exit(24)
		}
		operationId, _ := authority.GenerateOperationId()
		now := time.Now().UTC()
		_, _, err = store.ReserveOperation(authority.Operation{Id: operationId, ProjectId: registration.ProjectId, ChangeId: id, Kind: "test-effect", RequestDigest: digest("killed"), ExpectedRevision: current.Revision(), State: authority.OperationReserved, Result: []byte{}, CreatedAt: now, UpdatedAt: now})
		if err != nil {
			os.Exit(25)
		}
		if err := os.WriteFile(os.Getenv("PRAETOR_HELPER_GATE"), []byte("ready"), 0o600); err != nil {
			os.Exit(26)
		}
		select {}
	case "uncommitted":
		db, err := sql.Open("sqlite", os.Getenv("PRAETOR_HELPER_DB"))
		if err != nil {
			os.Exit(27)
		}
		tx, err := db.Begin()
		if err != nil {
			os.Exit(28)
		}
		if _, err := tx.Exec(`UPDATE changes SET state='planned',revision=2 WHERE change_id=?`, id); err != nil {
			os.Exit(29)
		}
		if err := os.WriteFile(os.Getenv("PRAETOR_HELPER_GATE"), []byte("ready"), 0o600); err != nil {
			os.Exit(30)
		}
		select {}
	case "committed":
		current, _, err := store.GetChange(id)
		if err != nil {
			os.Exit(33)
		}
		candidate := current
		transition, err := candidate.Transition(change.StatePlanned, time.Now(), "committed before kill")
		if err != nil {
			os.Exit(34)
		}
		if err := store.CommitTransition(current.Revision(), candidate, workflow.LifecycleEvent{EventType: workflow.EventChangeTransition, ChangeId: id, ProjectId: registration.ProjectId, PreviousState: transition.PreviousState, ResultingState: transition.ResultingState, OccurredAt: transition.OccurredAt, Context: transition.Context}); err != nil {
			os.Exit(35)
		}
		if err := os.WriteFile(os.Getenv("PRAETOR_HELPER_GATE"), []byte("ready"), 0o600); err != nil {
			os.Exit(36)
		}
		select {}
	}
}

func TestUnsupportedSchemaAndArtifactCorruptionFailClosed(t *testing.T) {
	t.Run("newer schema", func(t *testing.T) {
		data, registration := fixture(t)
		store := open(t, data, registration)
		path := store.Path()
		store.Close()
		db, err := sql.Open("sqlite", path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`PRAGMA user_version=2`); err != nil {
			t.Fatal(err)
		}
		db.Close()
		if _, err := sqlite.Open(data, registration); !errors.Is(err, authority.ErrIncompatible) {
			t.Fatalf("reopen error=%v", err)
		}
	})
	t.Run("artifact digest", func(t *testing.T) {
		data, registration := fixture(t)
		store := open(t, data, registration)
		created := create(t, store, registration.ProjectId, "corrupt")
		candidate := created
		_, _ = candidate.Transition(change.StatePlanned, time.Now(), "planned")
		id, _ := artifact.GenerateId()
		item, _ := artifact.New(id, registration.ProjectId, created.ChangeId(), artifact.KindPatch, 1, 1, "application/octet-stream", time.Now(), artifact.Producer{Component: "test"}, []byte("original"), false)
		if err := store.CommitAuthority(authority.AuthorityCommit{ExpectedRevision: created.Revision(), Candidate: candidate, Artifacts: []artifact.Artifact{item}}); err != nil {
			t.Fatal(err)
		}
		path := store.Path()
		store.Close()
		db, err := sql.Open("sqlite", path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`UPDATE artifacts SET payload=? WHERE artifact_id=?`, []byte("tampered"), id); err != nil {
			t.Fatal(err)
		}
		db.Close()
		store = open(t, data, registration)
		defer store.Close()
		if err := store.Validate(); !errors.Is(err, authority.ErrCorrupt) {
			t.Fatalf("validation error=%v", err)
		}
	})
}

func TestWorkflowAndForeignKeyCorruptionFailClosed(t *testing.T) {
	t.Run("unsupported workflow schema remains inspectable but not executable", func(t *testing.T) {
		data, registration := fixture(t)
		store := open(t, data, registration)
		created := create(t, store, registration.ProjectId, "workflow-corrupt")
		path := store.Path()
		store.Close()
		db, err := sql.Open("sqlite", path)
		if err != nil {
			t.Fatal(err)
		}
		bad := []byte("schema_version: 99\nworkflow_id: historical-v99\nworkflow_version: 9.0.0\nstates: []\ntransitions: []\n")
		workflowDigest := digest(string(bad))
		if _, err := db.Exec(`INSERT INTO workflow_snapshots(workflow_digest,workflow_id,workflow_version,schema_version,exact_yaml) VALUES(?,?,?,?,?)`, workflowDigest, "historical-v99", "9.0.0", 99, bad); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`UPDATE changes SET workflow_digest=? WHERE change_id=?`, workflowDigest, created.ChangeId()); err != nil {
			t.Fatal(err)
		}
		db.Close()
		store = open(t, data, registration)
		defer store.Close()
		loaded, snapshot, err := store.GetChange(created.ChangeId())
		if err != nil || snapshot.Executable() || snapshot.SchemaVersion() != 99 || snapshot.Digest() != workflowDigest || !bytes.Equal(snapshot.ExactYAML(), bad) {
			t.Fatalf("historical inspection Change=%#v snapshot=%#v error=%v", loaded, snapshot, err)
		}
		service, err := workflow.NewDurable(store, registration.ProjectId, func() time.Time { return time.Now().UTC() })
		if err != nil {
			t.Fatal(err)
		}
		if _, err := service.Transition(created.ChangeId(), change.StatePlanned, "must remain blocked"); !errors.Is(err, workflow.ErrUnsupportedWorkflowSchema) {
			t.Fatalf("unsupported execution error=%v", err)
		}
		after, afterSnapshot, err := store.GetChange(created.ChangeId())
		if err != nil || after.Revision() != created.Revision() || after.State() != created.State() || !bytes.Equal(afterSnapshot.ExactYAML(), bad) {
			t.Fatalf("blocked execution mutated authority: %#v %#v %v", after, afterSnapshot, err)
		}
	})
	t.Run("foreign key violation", func(t *testing.T) {
		data, registration := fixture(t)
		store := open(t, data, registration)
		path := store.Path()
		store.Close()
		db, err := sql.Open("sqlite", path)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`PRAGMA foreign_keys=OFF`); err != nil {
			t.Fatal(err)
		}
		if _, err := db.Exec(`INSERT INTO artifact_bindings(change_id,role,binding_revision,artifact_id) VALUES('missing','patch',1,'missing')`); err != nil {
			t.Fatal(err)
		}
		db.Close()
		store = open(t, data, registration)
		defer store.Close()
		if err := store.Validate(); !errors.Is(err, authority.ErrCorrupt) {
			t.Fatalf("Validate error=%v", err)
		}
	})
}

func TestPerProjectStoresAreIsolated(t *testing.T) {
	data, firstRegistration := fixture(t)
	secondId, _ := project.GenerateProjectID()
	secondRegistration := project.Registration{ProjectId: secondId, RepositoryRoot: t.TempDir(), SchemaVersion: 1, CreatedAt: time.Now()}
	first := open(t, data, firstRegistration)
	defer first.Close()
	second := open(t, data, secondRegistration)
	defer second.Close()
	created := create(t, first, firstRegistration.ProjectId, "private-change")
	if _, _, err := second.GetChange(created.ChangeId()); err == nil {
		t.Fatal("Project B loaded Project A Change")
	}
	if first.Path() == second.Path() {
		t.Fatal("Projects share authority database")
	}
}

func fixture(t *testing.T) (string, project.Registration) {
	t.Helper()
	root := t.TempDir()
	id, err := project.GenerateProjectID()
	if err != nil {
		t.Fatal(err)
	}
	return filepath.Join(t.TempDir(), "praetor"), project.Registration{ProjectId: id, RepositoryRoot: root, SchemaVersion: 1, CreatedAt: time.Now().UTC()}
}
func open(t *testing.T, data string, registration project.Registration) *sqlite.Store {
	t.Helper()
	store, err := sqlite.Open(data, registration)
	if err != nil {
		t.Fatal(err)
	}
	return store
}
func create(t *testing.T, store *sqlite.Store, pid project.ProjectId, id string) change.Change {
	t.Helper()
	created, err := change.New(change.ChangeId(id), pid, "intent", time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	snapshot, err := workflow.CoreV0Snapshot()
	if err != nil {
		t.Fatal(err)
	}
	event := workflow.LifecycleEvent{EventType: workflow.EventChangeCreated, ChangeId: created.ChangeId(), ProjectId: pid, ResultingState: created.State(), OccurredAt: created.CreatedAt(), Context: "create", Intent: created.Intent()}
	if err := store.CreateChange(created, snapshot, event); err != nil {
		t.Fatal(err)
	}
	return created
}
func lifecycle(candidate change.Change, previous change.ChangeState, context string) workflow.LifecycleEvent {
	return workflow.LifecycleEvent{EventType: workflow.EventChangeTransition, ChangeId: candidate.ChangeId(), ProjectId: candidate.ProjectId(), PreviousState: previous, ResultingState: candidate.State(), OccurredAt: candidate.UpdatedAt(), Context: context}
}
func digest(value string) string {
	sum := sha256.Sum256([]byte(value))
	return "sha256:" + hex.EncodeToString(sum[:])
}
