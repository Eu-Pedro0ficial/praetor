package integration

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"github.com/Eu-Pedro0ficial/praetor/internal/artifact"
	"github.com/Eu-Pedro0ficial/praetor/internal/authority"
	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"github.com/Eu-Pedro0ficial/praetor/internal/project"
)

type ExternalCondition string

const (
	ConditionPRE       ExternalCondition = "PRE"
	ConditionPOST      ExternalCondition = "POST"
	ConditionAmbiguous ExternalCondition = "AMBIGUOUS"
)

// RecoveryProbe classifies exact canonical source reality without mutation.
type RecoveryProbe interface {
	Classify(ApplicationRequest) (ExternalCondition, error)
	PostProof(ApplicationRequest) (CanonicalProof, error)
}

type RecoveryPort interface {
	RecoverPersisted(authority.OperationId, RecoveryFinalizer) (ExternalCondition, CanonicalProof, error)
}

// RecoveryFinalizer commits terminal durable authority while the Project
// source-exclusion lock is still held.
type RecoveryFinalizer func(authority.Operation, CanonicalProof) error

type coordinatedCompletion interface {
	ApplyCoordinated(ApplicationRequest, RecoveryFinalizer) (CanonicalProof, bool, error)
}

// RecoveryRequest contains only durable authority required to classify Git
// reality after the proposal workspace and process-local objects are gone.
type RecoveryRequest struct {
	ProjectId         project.ProjectId
	ChangeId          change.ChangeId
	RepositoryRoot    string
	BaseRevision      string
	SourceStateDigest string
	TrackedPaths      []string
	ChangedPaths      []string
	PatchDigest       string
	PatchContent      []byte
}

// DurableRecoveryProbe classifies canonical source from durable artifacts.
type DurableRecoveryProbe interface {
	ClassifyDurable(RecoveryRequest) (ExternalCondition, error)
	PostProofDurable(RecoveryRequest) (CanonicalProof, error)
}

// CoordinatedCanonical adds durable reservation and Project-scoped OS exclusion
// around the existing exact Git adapter. It holds no SQLite transaction during Git.
type CoordinatedCanonical struct {
	canonical      CanonicalSourcePort
	probe          RecoveryProbe
	durableProbe   DurableRecoveryProbe
	store          authority.Store
	projectId      project.ProjectId
	stateDirectory string
}

func NewCoordinatedCanonical(canonical CanonicalSourcePort, probe RecoveryProbe, store authority.Store, projectId project.ProjectId, stateDirectory string) (*CoordinatedCanonical, error) {
	if canonical == nil || probe == nil || store == nil {
		return nil, fmt.Errorf("canonical coordinator dependencies are required")
	}
	if !projectId.IsValid() {
		return nil, fmt.Errorf("valid ProjectId is required")
	}
	if strings.TrimSpace(stateDirectory) == "" {
		return nil, fmt.Errorf("Praetor state directory is required")
	}
	durableProbe, _ := probe.(DurableRecoveryProbe)
	return &CoordinatedCanonical{canonical: canonical, probe: probe, durableProbe: durableProbe, store: store, projectId: projectId, stateDirectory: stateDirectory}, nil
}
func (c *CoordinatedCanonical) Preflight(request ApplicationRequest) error {
	return c.canonical.Preflight(request)
}

func (c *CoordinatedCanonical) Apply(request ApplicationRequest) (CanonicalProof, bool, error) {
	return c.apply(request, nil)
}

func (c *CoordinatedCanonical) ApplyCoordinated(request ApplicationRequest, finalizer RecoveryFinalizer) (CanonicalProof, bool, error) {
	if finalizer == nil {
		return CanonicalProof{}, false, fmt.Errorf("canonical authority finalizer is required")
	}
	return c.apply(request, finalizer)
}

func (c *CoordinatedCanonical) apply(request ApplicationRequest, finalizer RecoveryFinalizer) (CanonicalProof, bool, error) {
	patch, ok := request.Proposal.PatchArtifact()
	if !ok {
		return CanonicalProof{}, false, fmt.Errorf("canonical operation requires PatchArtifact")
	}
	if patch.ProjectId() != c.projectId {
		return CanonicalProof{}, false, fmt.Errorf("cross-Project canonical operation rejected")
	}
	current, _, err := c.store.GetChange(patch.ChangeId())
	if err != nil {
		return CanonicalProof{}, false, err
	}
	requestDigest := canonicalRequestDigest(request)
	operation, replayed, err := c.reserveOrFind(patch.ChangeId(), current.Revision(), requestDigest)
	if err != nil {
		return CanonicalProof{}, false, err
	}
	lock, err := acquireProjectLock(c.stateDirectory, c.projectId)
	if err != nil {
		return CanonicalProof{}, false, err
	}
	defer releaseProjectLock(lock)
	condition, err := c.probe.Classify(request)
	if err != nil {
		return CanonicalProof{}, false, err
	}
	if replayed && operation.State == authority.OperationCompleted {
		if condition != ConditionPOST {
			return CanonicalProof{}, false, fmt.Errorf("%w: completed canonical operation %s no longer has exact POST", authority.ErrCorrupt, operation.Id)
		}
		proof, err := c.probe.PostProof(request)
		if err != nil {
			return CanonicalProof{}, false, err
		}
		if err := verifyCompletedResult(operation, proof); err != nil {
			return CanonicalProof{}, false, err
		}
		if finalizer != nil {
			if err := finalizer(operation, proof); err != nil {
				return proof, false, err
			}
		}
		return proof, true, nil
	}
	if condition != ConditionPRE {
		return CanonicalProof{}, false, fmt.Errorf("canonical operation %s classified %s; explicit recovery required", operation.Id, condition)
	}
	proof, mutated, err := c.canonical.Apply(request)
	if err != nil {
		return proof, mutated, err
	}
	condition, err = c.probe.Classify(request)
	if err != nil {
		return proof, true, err
	}
	if condition != ConditionPOST {
		return proof, true, fmt.Errorf("canonical operation %s postcondition is %s", operation.Id, condition)
	}
	encoded, err := encodeProof(proof)
	if err != nil {
		return proof, true, err
	}
	if finalizer != nil {
		operation.State = authority.OperationCompleted
		operation.Result = encoded
		operation.UpdatedAt = time.Now().UTC()
		if err := finalizer(operation, proof); err != nil {
			return proof, true, err
		}
	} else if _, err := c.store.CompleteOperation(operation.Id, requestDigest, encoded, nil); err != nil {
		return proof, true, err
	}
	return proof, true, nil
}

// Recover explicitly classifies and, only for exact POST, finalizes a reserved
// canonical mutation. PRE is never replayed and AMBIGUOUS always blocks.
func (c *CoordinatedCanonical) RecoverPersisted(operationId authority.OperationId, finalizer RecoveryFinalizer) (ExternalCondition, CanonicalProof, error) {
	if c.durableProbe == nil {
		return ConditionAmbiguous, CanonicalProof{}, fmt.Errorf("canonical adapter does not support durable recovery")
	}
	operation, err := c.store.GetOperation(operationId)
	if err != nil {
		return ConditionAmbiguous, CanonicalProof{}, err
	}
	request, err := c.recoveryRequest(operation.ChangeId)
	if err != nil {
		return ConditionAmbiguous, CanonicalProof{}, err
	}
	requestDigest := canonicalRecoveryRequestDigest(request)
	if operation.ProjectId != c.projectId || operation.ChangeId != request.ChangeId || operation.RequestDigest != requestDigest {
		return ConditionAmbiguous, CanonicalProof{}, authority.ErrOperationConflict
	}
	lock, err := acquireProjectLock(c.stateDirectory, c.projectId)
	if err != nil {
		return ConditionAmbiguous, CanonicalProof{}, err
	}
	defer releaseProjectLock(lock)
	condition, err := c.durableProbe.ClassifyDurable(request)
	if err != nil {
		return ConditionAmbiguous, CanonicalProof{}, err
	}
	if operation.State == authority.OperationCompleted {
		if condition != ConditionPOST {
			return condition, CanonicalProof{}, fmt.Errorf("%w: completed operation classified %s instead of exact POST", authority.ErrCorrupt, condition)
		}
		proof, err := c.durableProbe.PostProofDurable(request)
		if err == nil {
			err = verifyCompletedResult(operation, proof)
		}
		if err == nil && finalizer != nil {
			err = finalizer(operation, proof)
		}
		return condition, proof, err
	}
	switch condition {
	case ConditionPRE:
		return condition, CanonicalProof{}, nil
	case ConditionPOST:
		proof, err := c.durableProbe.PostProofDurable(request)
		if err != nil {
			return condition, CanonicalProof{}, err
		}
		encoded, err := encodeProof(proof)
		if err != nil {
			return condition, CanonicalProof{}, err
		}
		operation.State = authority.OperationCompleted
		operation.Result = encoded
		operation.UpdatedAt = time.Now().UTC()
		if finalizer != nil {
			if err := finalizer(operation, proof); err != nil {
				return condition, CanonicalProof{}, err
			}
		} else if _, err := c.store.CompleteOperation(operationId, requestDigest, encoded, nil); err != nil {
			return condition, CanonicalProof{}, err
		}
		return condition, proof, nil
	default:
		return condition, CanonicalProof{}, fmt.Errorf("canonical operation %s is AMBIGUOUS and requires human remediation", operationId)
	}
}

// Recover retains the narrow live-proposal seam used by adapter unit tests;
// runtime recovery uses RecoverPersisted so it survives process restart.
func (c *CoordinatedCanonical) Recover(request ApplicationRequest, operationId authority.OperationId) (ExternalCondition, CanonicalProof, error) {
	operation, err := c.store.GetOperation(operationId)
	if err != nil {
		return ConditionAmbiguous, CanonicalProof{}, err
	}
	requestDigest := canonicalRequestDigest(request)
	if operation.ProjectId != c.projectId || operation.ChangeId != request.Proposal.Workspace().ChangeId() || operation.RequestDigest != requestDigest {
		return ConditionAmbiguous, CanonicalProof{}, authority.ErrOperationConflict
	}
	lock, err := acquireProjectLock(c.stateDirectory, c.projectId)
	if err != nil {
		return ConditionAmbiguous, CanonicalProof{}, err
	}
	defer releaseProjectLock(lock)
	condition, err := c.probe.Classify(request)
	if err != nil {
		return ConditionAmbiguous, CanonicalProof{}, err
	}
	if operation.State == authority.OperationCompleted {
		if condition != ConditionPOST {
			return condition, CanonicalProof{}, fmt.Errorf("%w: completed operation no longer has exact POST", authority.ErrCorrupt)
		}
		proof, err := c.probe.PostProof(request)
		if err == nil {
			err = verifyCompletedResult(operation, proof)
		}
		return condition, proof, err
	}
	switch condition {
	case ConditionPRE:
		return condition, CanonicalProof{}, nil
	case ConditionPOST:
		proof, err := c.probe.PostProof(request)
		if err != nil {
			return condition, CanonicalProof{}, err
		}
		encoded, err := encodeProof(proof)
		if err != nil {
			return condition, CanonicalProof{}, err
		}
		if _, err := c.store.CompleteOperation(operationId, requestDigest, encoded, nil); err != nil {
			return condition, CanonicalProof{}, err
		}
		return condition, proof, nil
	default:
		return condition, CanonicalProof{}, fmt.Errorf("canonical operation %s is AMBIGUOUS and requires human remediation", operationId)
	}
}

func (c *CoordinatedCanonical) recoveryRequest(changeId change.ChangeId) (RecoveryRequest, error) {
	bindings, err := c.store.ListBindings(changeId)
	if err != nil {
		return RecoveryRequest{}, err
	}
	var sourceId, patchId artifact.ArtifactId
	for _, binding := range bindings {
		switch binding.Role {
		case "source-snapshot":
			sourceId = binding.ArtifactId
		case "patch":
			patchId = binding.ArtifactId
		}
	}
	if sourceId == "" || patchId == "" {
		return RecoveryRequest{}, fmt.Errorf("canonical recovery requires durable source-snapshot and patch bindings")
	}
	sourceArtifact, err := c.store.GetArtifact(changeId, sourceId, true)
	if err != nil {
		return RecoveryRequest{}, err
	}
	patchArtifact, err := c.store.GetArtifact(changeId, patchId, true)
	if err != nil {
		return RecoveryRequest{}, err
	}
	sourcePayload, err := artifact.DecodeSourceSnapshotPayload(sourceArtifact.Payload())
	if err != nil {
		return RecoveryRequest{}, fmt.Errorf("%w: %v", authority.ErrCorrupt, err)
	}
	patchPayload, err := artifact.DecodePatchPayload(patchArtifact.Payload())
	if err != nil {
		return RecoveryRequest{}, fmt.Errorf("%w: %v", authority.ErrCorrupt, err)
	}
	if sourcePayload.HeadRevision != patchPayload.BaseRevision || sourcePayload.SourceStateDigest != patchPayload.SourceStateDigest {
		return RecoveryRequest{}, fmt.Errorf("%w: durable source and patch authority disagree", authority.ErrCorrupt)
	}
	return RecoveryRequest{ProjectId: c.projectId, ChangeId: changeId, RepositoryRoot: sourcePayload.RepositoryRoot, BaseRevision: patchPayload.BaseRevision, SourceStateDigest: patchPayload.SourceStateDigest, TrackedPaths: append([]string(nil), sourcePayload.TrackedPaths...), ChangedPaths: append([]string(nil), patchPayload.ChangedPaths...), PatchDigest: patchPayload.PatchDigest, PatchContent: append([]byte(nil), patchPayload.Content...)}, nil
}

func encodeProof(proof CanonicalProof) ([]byte, error) {
	return canonicalResult(proof).Encode()
}

func canonicalResult(proof CanonicalProof) authority.CanonicalResult {
	return authority.CanonicalResult{Head: proof.HeadRevision(), Patch: proof.PatchDigest(), Paths: proof.ChangedPaths(), Index: proof.IndexUnchanged()}
}

func verifyCompletedResult(operation authority.Operation, proof CanonicalProof) error {
	stored, err := authority.DecodeCanonicalResult(operation.Result)
	if err != nil {
		return fmt.Errorf("%w: operation %s: %v", authority.ErrCorrupt, operation.Id, err)
	}
	if !stored.Equal(canonicalResult(proof)) {
		return fmt.Errorf("%w: completed operation %s result disagrees with exact canonical POST", authority.ErrCorrupt, operation.Id)
	}
	return nil
}

func (c *CoordinatedCanonical) reserveOrFind(changeId change.ChangeId, revision uint64, requestDigest string) (authority.Operation, bool, error) {
	incomplete, err := c.store.ListIncompleteOperations()
	if err != nil {
		return authority.Operation{}, false, err
	}
	for _, item := range incomplete {
		if string(item.ChangeId) == string(changeId) && item.Kind == "canonical-git-apply" {
			if item.RequestDigest != requestDigest {
				return authority.Operation{}, false, authority.ErrOperationConflict
			}
			return item, true, nil
		}
	}
	id, err := authority.GenerateOperationId()
	if err != nil {
		return authority.Operation{}, false, err
	}
	now := time.Now().UTC()
	value := authority.Operation{Id: id, ProjectId: c.projectId, ChangeId: changeId, Kind: "canonical-git-apply", RequestDigest: requestDigest, ExpectedRevision: revision, State: authority.OperationReserved, Result: []byte{}, CreatedAt: now, UpdatedAt: now}
	return c.store.ReserveOperation(value)
}

func canonicalRequestDigest(request ApplicationRequest) string {
	patch, _ := request.Proposal.PatchArtifact()
	values := []string{"praetor-canonical-git-operation-v1", string(patch.ProjectId()), string(patch.ChangeId()), patch.BaseRevision(), string(patch.SourceStateDigest()), patch.PatchDigest()}
	sum := sha256.Sum256([]byte(strings.Join(values, "\x1f")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func canonicalRecoveryRequestDigest(request RecoveryRequest) string {
	values := []string{"praetor-canonical-git-operation-v1", string(request.ProjectId), string(request.ChangeId), request.BaseRevision, request.SourceStateDigest, request.PatchDigest}
	sum := sha256.Sum256([]byte(strings.Join(values, "\x1f")))
	return "sha256:" + hex.EncodeToString(sum[:])
}

func acquireProjectLock(stateDirectory string, projectId project.ProjectId) (*os.File, error) {
	base, err := filepath.Abs(stateDirectory)
	if err != nil {
		return nil, err
	}
	projects := filepath.Join(base, "projects")
	projectDirectory := filepath.Join(projects, string(projectId))
	for _, path := range []string{base, projects, projectDirectory} {
		if info, err := os.Lstat(path); err == nil {
			if info.Mode()&os.ModeSymlink != 0 || !info.IsDir() {
				return nil, fmt.Errorf("mutation lock directory is unsafe")
			}
		} else if os.IsNotExist(err) {
			if err := os.MkdirAll(path, 0o700); err != nil {
				return nil, err
			}
		} else {
			return nil, err
		}
		if err := os.Chmod(path, 0o700); err != nil {
			return nil, err
		}
	}
	path := filepath.Join(projectDirectory, "canonical-mutation.lock")
	if info, err := os.Lstat(path); err == nil {
		if !info.Mode().IsRegular() {
			return nil, fmt.Errorf("mutation lock path is not a regular file")
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_RDWR, 0o600)
	if err != nil {
		return nil, err
	}
	if info, err := file.Stat(); err != nil || !info.Mode().IsRegular() {
		file.Close()
		return nil, fmt.Errorf("mutation lock path changed to an unsafe file type")
	}
	deadline := time.Now().Add(5 * time.Second)
	for {
		err = syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return file, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
			file.Close()
			return nil, err
		}
		if time.Now().After(deadline) {
			file.Close()
			return nil, fmt.Errorf("canonical mutation lock timed out")
		}
		time.Sleep(10 * time.Millisecond)
	}
}
func releaseProjectLock(file *os.File) {
	if file == nil {
		return
	}
	_ = syscall.Flock(int(file.Fd()), syscall.LOCK_UN)
	_ = file.Close()
}
