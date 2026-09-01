package source

import (
	"encoding/hex"
	"fmt"
	"strings"

	"github.com/Eu-Pedro0ficial/praetor/internal/project"
)

// WorkingTreeState is the minimal Git working-tree condition captured by M0.3.
type WorkingTreeState string

const (
	WorkingTreeClean WorkingTreeState = "clean"
	WorkingTreeDirty WorkingTreeState = "dirty"
)

// SourceStateDigest is versioned integrity metadata for one captured source
// state. It is not ProjectId, repository identity, or reassociation evidence.
type SourceStateDigest string

// SourceSnapshot identifies the source state used for repository analysis.
type SourceSnapshot struct {
	projectId         project.ProjectId
	repositoryRoot    string
	headRevision      string
	workingTreeState  WorkingTreeState
	trackedPaths      []RepositoryPath
	sourceStateDigest SourceStateDigest
}

// NewSourceSnapshot validates the adapter-produced source-state facts.
func NewSourceSnapshot(
	projectId project.ProjectId,
	repositoryRoot string,
	headRevision string,
	workingTreeState WorkingTreeState,
	trackedPathValues []string,
	sourceStateDigest SourceStateDigest,
) (SourceSnapshot, error) {
	if !projectId.IsValid() {
		return SourceSnapshot{}, fmt.Errorf("valid ProjectId is required")
	}
	if strings.TrimSpace(repositoryRoot) == "" {
		return SourceSnapshot{}, fmt.Errorf("RepositoryRoot is required")
	}
	if strings.TrimSpace(headRevision) == "" {
		return SourceSnapshot{}, fmt.Errorf("HEAD revision is required")
	}
	if workingTreeState != WorkingTreeClean && workingTreeState != WorkingTreeDirty {
		return SourceSnapshot{}, fmt.Errorf("unknown working-tree state %q", workingTreeState)
	}
	trackedPaths, err := normalizeRepositoryPaths(trackedPathValues)
	if err != nil {
		return SourceSnapshot{}, fmt.Errorf("tracked source inventory: %w", err)
	}
	if err := validateSourceStateDigest(sourceStateDigest); err != nil {
		return SourceSnapshot{}, err
	}

	return SourceSnapshot{
		projectId:         projectId,
		repositoryRoot:    repositoryRoot,
		headRevision:      strings.TrimSpace(headRevision),
		workingTreeState:  workingTreeState,
		trackedPaths:      trackedPaths,
		sourceStateDigest: sourceStateDigest,
	}, nil
}

// ProjectId returns the logical Project associated with the snapshot.
func (snapshot SourceSnapshot) ProjectId() project.ProjectId {
	return snapshot.projectId
}

// RepositoryRoot returns operational repository association metadata.
func (snapshot SourceSnapshot) RepositoryRoot() string {
	return snapshot.repositoryRoot
}

// HeadRevision returns the captured Git HEAD revision.
func (snapshot SourceSnapshot) HeadRevision() string {
	return snapshot.headRevision
}

// WorkingTreeState returns whether Git reported any tracked or untracked
// working-tree change when the snapshot was captured.
func (snapshot SourceSnapshot) WorkingTreeState() WorkingTreeState {
	return snapshot.workingTreeState
}

// TrackedPaths returns a defensive copy of the deterministic source inventory.
func (snapshot SourceSnapshot) TrackedPaths() []RepositoryPath {
	return copyRepositoryPaths(snapshot.trackedPaths)
}

// SourceStateDigest returns source-state integrity metadata.
func (snapshot SourceSnapshot) SourceStateDigest() SourceStateDigest {
	return snapshot.sourceStateDigest
}

// ContainsTrackedPath reports whether the source inventory contains path.
func (snapshot SourceSnapshot) ContainsTrackedPath(repositoryPath RepositoryPath) bool {
	return containsRepositoryPath(snapshot.trackedPaths, repositoryPath)
}

func validateSourceStateDigest(digest SourceStateDigest) error {
	const prefix = "sha256:"
	value := string(digest)
	if !strings.HasPrefix(value, prefix) {
		return fmt.Errorf("SourceStateDigest must use the sha256 prefix")
	}
	encoded := strings.TrimPrefix(value, prefix)
	if len(encoded) != 64 {
		return fmt.Errorf("SourceStateDigest must contain a SHA-256 value")
	}
	if _, err := hex.DecodeString(encoded); err != nil {
		return fmt.Errorf("SourceStateDigest contains invalid SHA-256 text: %w", err)
	}
	return nil
}
