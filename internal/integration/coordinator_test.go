package integration_test

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"

	sqliteadapter "github.com/Eu-Pedro0ficial/praetor/internal/adapters/persistence/sqlite"
	"github.com/Eu-Pedro0ficial/praetor/internal/approval"
	"github.com/Eu-Pedro0ficial/praetor/internal/artifact"
	"github.com/Eu-Pedro0ficial/praetor/internal/audit"
	"github.com/Eu-Pedro0ficial/praetor/internal/authority"
	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"github.com/Eu-Pedro0ficial/praetor/internal/integration"
	"github.com/Eu-Pedro0ficial/praetor/internal/project"
	"github.com/Eu-Pedro0ficial/praetor/internal/proposal"
	"github.com/Eu-Pedro0ficial/praetor/internal/workflow"
)

type recoveryCanonical struct {
	condition integration.ExternalCondition
	proof     integration.CanonicalProof
	applies   int
}

func (c *recoveryCanonical) Preflight(integration.ApplicationRequest) error { return nil }
func (c *recoveryCanonical) Apply(integration.ApplicationRequest) (integration.CanonicalProof, bool, error) {
	c.applies++
	c.condition = integration.ConditionPOST
	return c.proof, true, nil
}
func (c *recoveryCanonical) Classify(integration.ApplicationRequest) (integration.ExternalCondition, error) {
	return c.condition, nil
}
func (c *recoveryCanonical) PostProof(integration.ApplicationRequest) (integration.CanonicalProof, error) {
	if c.condition != integration.ConditionPOST {
		return integration.CanonicalProof{}, errors.New("not POST")
	}
	return c.proof, nil
}
func (c *recoveryCanonical) ClassifyDurable(integration.RecoveryRequest) (integration.ExternalCondition, error) {
	return c.condition, nil
}
func (c *recoveryCanonical) PostProofDurable(integration.RecoveryRequest) (integration.CanonicalProof, error) {
	if c.condition != integration.ConditionPOST {
		return integration.CanonicalProof{}, errors.New("not POST")
	}
	return c.proof, nil
}

type completionFailureStore struct {
	authority.Store
	fail bool
}

func (s *completionFailureStore) CompleteOperation(id authority.OperationId, digest string, result []byte, events []audit.Event) (authority.Operation, error) {
	if s.fail {
		return authority.Operation{}, errors.New("injected completion failure")
	}
	return s.Store.CompleteOperation(id, digest, result, events)
}

func TestCanonicalCoordinatorRecoversExactPostWithoutReapply(t *testing.T) {
	fixture := newIntegrationFixture(t, approval.DecisionApprove)
	registration := project.Registration{ProjectId: fixture.currentChange.ProjectId(), RepositoryRoot: fixture.currentProposal.Workspace().CanonicalRoot(), SchemaVersion: 1, CreatedAt: time.Now()}
	data := t.TempDir()
	store, err := sqliteadapter.Open(data, registration)
	if err != nil {
		t.Fatal(err)
	}
	seedDurableApproved(t, store, fixture.currentChange.ChangeId(), fixture.currentChange.Intent(), registration.ProjectId)
	persistRecoveryArtifacts(t, store, fixture.currentProposal)
	patch, _ := fixture.currentProposal.PatchArtifact()
	proof, err := integration.NewCanonicalProof(patch.BaseRevision(), patch.PatchDigest(), patch.ChangedPaths(), true)
	if err != nil {
		t.Fatal(err)
	}
	canonical := &recoveryCanonical{condition: integration.ConditionPRE, proof: proof}
	failing := &completionFailureStore{Store: store, fail: true}
	coordinator, err := integration.NewCoordinatedCanonical(canonical, canonical, failing, registration.ProjectId, t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	request := integration.ApplicationRequest{Proposal: fixture.currentProposal}
	if _, mutated, err := coordinator.Apply(request); err == nil || !mutated {
		t.Fatalf("apply mutated=%t err=%v", mutated, err)
	}
	operations, err := store.ListIncompleteOperations()
	if err != nil || len(operations) != 1 {
		t.Fatalf("operations=%#v err=%v", operations, err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	store, err = sqliteadapter.Open(data, registration)
	if err != nil {
		t.Fatal(err)
	}
	defer store.Close()
	restartedCanonical := &recoveryCanonical{condition: integration.ConditionPOST, proof: proof}
	stateDirectory := t.TempDir()
	restarted, err := integration.NewCoordinatedCanonical(restartedCanonical, restartedCanonical, store, registration.ProjectId, stateDirectory)
	if err != nil {
		t.Fatal(err)
	}
	finalizerObservedLock := false
	condition, recoveredProof, err := restarted.RecoverPersisted(operations[0].Id, func(operation authority.Operation, _ integration.CanonicalProof) error {
		lockPath := filepath.Join(stateDirectory, "projects", string(registration.ProjectId), "canonical-mutation.lock")
		contender, openError := os.OpenFile(lockPath, os.O_RDWR, 0o600)
		if openError != nil {
			return openError
		}
		defer contender.Close()
		lockError := syscall.Flock(int(contender.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if !errors.Is(lockError, syscall.EWOULDBLOCK) && !errors.Is(lockError, syscall.EAGAIN) {
			return errors.New("recovery finalizer ran outside Project exclusion lock")
		}
		finalizerObservedLock = true
		_, completeError := store.CompleteOperation(operation.Id, operation.RequestDigest, operation.Result, nil)
		return completeError
	})
	if err != nil || condition != integration.ConditionPOST || recoveredProof.PatchDigest() != patch.PatchDigest() {
		t.Fatalf("recover condition=%s proof=%#v err=%v", condition, recoveredProof, err)
	}
	if canonical.applies != 1 {
		t.Fatalf("recovery reapplied external effect %d times", canonical.applies)
	}
	if !finalizerObservedLock {
		t.Fatal("POST recovery did not hold the Project lock through durable finalization")
	}
	if remaining, _ := store.ListIncompleteOperations(); len(remaining) != 0 {
		t.Fatalf("remaining=%#v", remaining)
	}
}

func persistRecoveryArtifacts(t *testing.T, store *sqliteadapter.Store, current proposal.Proposal) {
	t.Helper()
	changeValue, _, err := store.GetChange(current.Workspace().ChangeId())
	if err != nil {
		t.Fatal(err)
	}
	snapshot := current.CanonicalSource()
	tracked := make([]string, 0, len(snapshot.TrackedPaths()))
	for _, path := range snapshot.TrackedPaths() {
		tracked = append(tracked, string(path))
	}
	sourcePayload, err := artifact.EncodeSourceSnapshotPayload(artifact.SourceSnapshotPayload{RepositoryRoot: snapshot.RepositoryRoot(), WorkspaceId: string(current.Workspace().WorkspaceId()), WorkspaceRoot: current.Workspace().Root(), HeadRevision: snapshot.HeadRevision(), TrackedPaths: tracked, SourceStateDigest: string(snapshot.SourceStateDigest())})
	if err != nil {
		t.Fatal(err)
	}
	patch, ok := current.PatchArtifact()
	if !ok {
		t.Fatal("fixture has no PatchArtifact")
	}
	patchPayload, err := artifact.EncodePatchPayload(artifact.PatchPayload{WorkspaceId: string(patch.WorkspaceId()), BaseRevision: patch.BaseRevision(), SourceStateDigest: string(patch.SourceStateDigest()), ChangedPaths: patch.ChangedPaths(), PatchDigest: patch.PatchDigest(), Content: patch.Content(), CreatedAt: patch.CreatedAt().Format(time.RFC3339Nano)})
	if err != nil {
		t.Fatal(err)
	}
	createdAt := time.Now().UTC()
	sourceId, _ := artifact.GenerateId()
	patchId, _ := artifact.GenerateId()
	sourceArtifact, err := artifact.New(sourceId, changeValue.ProjectId(), changeValue.ChangeId(), artifact.KindSourceSnapshot, 1, 1, "application/json", createdAt, artifact.Producer{Component: "test"}, sourcePayload, false)
	if err != nil {
		t.Fatal(err)
	}
	patchArtifact, err := artifact.New(patchId, changeValue.ProjectId(), changeValue.ChangeId(), artifact.KindPatch, 1, 1, "application/json", createdAt, artifact.Producer{Component: "test"}, patchPayload, false)
	if err != nil {
		t.Fatal(err)
	}
	bindings := []authority.ArtifactBinding{{Role: "source-snapshot", ArtifactId: sourceId, Revision: changeValue.Revision()}, {Role: "patch", ArtifactId: patchId, Revision: changeValue.Revision()}}
	if err := store.CommitArtifacts(changeValue.ChangeId(), changeValue.Revision(), []artifact.Artifact{sourceArtifact, patchArtifact}, nil, nil, bindings, nil); err != nil {
		t.Fatal(err)
	}
}

func TestCanonicalCoordinatorRecoveryPREAndAmbiguousDoNotReplay(t *testing.T) {
	for _, test := range []struct {
		name      string
		condition integration.ExternalCondition
		wantError bool
	}{{"pre", integration.ConditionPRE, false}, {"ambiguous", integration.ConditionAmbiguous, true}} {
		t.Run(test.name, func(t *testing.T) {
			fixture := newIntegrationFixture(t, approval.DecisionApprove)
			registration := project.Registration{ProjectId: fixture.currentChange.ProjectId(), RepositoryRoot: fixture.currentProposal.Workspace().CanonicalRoot(), SchemaVersion: 1, CreatedAt: time.Now()}
			store, err := sqliteadapter.Open(t.TempDir(), registration)
			if err != nil {
				t.Fatal(err)
			}
			defer store.Close()
			seedDurableApproved(t, store, fixture.currentChange.ChangeId(), fixture.currentChange.Intent(), registration.ProjectId)
			patch, _ := fixture.currentProposal.PatchArtifact()
			proof, _ := integration.NewCanonicalProof(patch.BaseRevision(), patch.PatchDigest(), patch.ChangedPaths(), true)
			canonical := &recoveryCanonical{condition: test.condition, proof: proof}
			coordinator, _ := integration.NewCoordinatedCanonical(canonical, canonical, store, registration.ProjectId, t.TempDir())
			request := integration.ApplicationRequest{Proposal: fixture.currentProposal}
			digest := testCanonicalDigest(patch)
			operationId, _ := authority.GenerateOperationId()
			now := time.Now().UTC()
			operation, _, err := store.ReserveOperation(authority.Operation{Id: operationId, ProjectId: registration.ProjectId, ChangeId: fixture.currentChange.ChangeId(), Kind: "canonical-git-apply", RequestDigest: digest, ExpectedRevision: fixture.currentChange.Revision(), State: authority.OperationReserved, Result: []byte{}, CreatedAt: now, UpdatedAt: now})
			if err != nil {
				t.Fatal(err)
			}
			condition, _, err := coordinator.Recover(request, operation.Id)
			if (err != nil) != test.wantError || condition != test.condition {
				t.Fatalf("condition=%s err=%v", condition, err)
			}
			if canonical.applies != 0 {
				t.Fatal("recovery replayed external effect")
			}
			remaining, _ := store.ListIncompleteOperations()
			if len(remaining) != 1 {
				t.Fatalf("remaining=%#v", remaining)
			}
		})
	}
}

func testCanonicalDigest(patch proposal.PatchArtifact) string {
	values := []string{"praetor-canonical-git-operation-v1", string(patch.ProjectId()), string(patch.ChangeId()), patch.BaseRevision(), string(patch.SourceStateDigest()), patch.PatchDigest()}
	sum := sha256.Sum256([]byte(strings.Join(values, "\x1f")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func seedDurableApproved(t *testing.T, store *sqliteadapter.Store, id change.ChangeId, intent change.ChangeIntent, projectId project.ProjectId) {
	t.Helper()
	current, err := change.New(id, projectId, intent, time.Now().UTC())
	if err != nil {
		t.Fatal(err)
	}
	snapshot, _ := workflow.CoreV0Snapshot()
	event := workflow.LifecycleEvent{EventType: workflow.EventChangeCreated, ChangeId: id, ProjectId: projectId, ResultingState: current.State(), OccurredAt: current.CreatedAt(), Context: "seed", Intent: intent}
	if err := store.CreateChange(current, snapshot, event); err != nil {
		t.Fatal(err)
	}
	for _, state := range []change.ChangeState{change.StatePlanned, change.StateIsolated, change.StateValidated, change.StateApproved} {
		previous := current
		transition, err := current.Transition(state, time.Now().UTC(), "seed")
		if err != nil {
			t.Fatal(err)
		}
		event = workflow.LifecycleEvent{EventType: workflow.EventChangeTransition, ChangeId: id, ProjectId: projectId, PreviousState: transition.PreviousState, ResultingState: state, OccurredAt: transition.OccurredAt, Context: "seed"}
		if err := store.CommitTransition(previous.Revision(), current, event); err != nil {
			t.Fatal(err)
		}
	}
}
