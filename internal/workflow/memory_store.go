package workflow

import (
	"fmt"
	"sync"

	"github.com/Eu-Pedro0ficial/praetor/internal/change"
)

// MemoryStore is the deliberately process-local M0.2 Change state adapter.
type MemoryStore struct {
	mutex   sync.RWMutex
	changes map[change.ChangeId]change.Change
}

// NewMemoryStore creates an empty process-local Change store.
func NewMemoryStore() *MemoryStore {
	return &MemoryStore{changes: make(map[change.ChangeId]change.Change)}
}

func (store *MemoryStore) create(
	createdChange change.Change,
	beforeCommit func() error,
) error {
	store.mutex.Lock()
	defer store.mutex.Unlock()

	changeId := createdChange.ChangeId()
	if _, exists := store.changes[changeId]; exists {
		return fmt.Errorf("Change %q already exists", changeId)
	}
	if err := beforeCommit(); err != nil {
		return err
	}
	store.changes[changeId] = createdChange
	return nil
}

func (store *MemoryStore) update(
	changeId change.ChangeId,
	update func(*change.Change) error,
) (change.Change, error) {
	store.mutex.Lock()
	defer store.mutex.Unlock()

	current, exists := store.changes[changeId]
	if !exists {
		return change.Change{}, fmt.Errorf("Change %q was not found", changeId)
	}
	candidate := current
	if err := update(&candidate); err != nil {
		return change.Change{}, err
	}
	store.changes[changeId] = candidate
	return candidate, nil
}

func (store *MemoryStore) get(changeId change.ChangeId) (change.Change, error) {
	store.mutex.RLock()
	defer store.mutex.RUnlock()

	storedChange, exists := store.changes[changeId]
	if !exists {
		return change.Change{}, fmt.Errorf("Change %q was not found", changeId)
	}
	return storedChange, nil
}
