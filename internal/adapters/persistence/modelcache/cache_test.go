package modelcache

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/Eu-Pedro0ficial/praetor/internal/project"
	"github.com/Eu-Pedro0ficial/praetor/internal/repositorymodel"
)

func TestMissingCorruptAndIncompatibleCachesRebuildSafely(t *testing.T) {
	directory := t.TempDir()
	registration := project.Registration{ProjectId: "01890c29-7a78-7abc-8def-0123456789ab", RepositoryRoot: t.TempDir()}
	cache, err := Open(directory, registration)
	if err != nil {
		t.Fatal(err)
	}
	path := cache.Path()
	if err := cache.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte("not sqlite"), 0o600); err != nil {
		t.Fatal(err)
	}
	rebuilt, err := Open(directory, registration)
	if err != nil {
		t.Fatalf("corrupt cache was not recoverable: %v", err)
	}
	if rebuilt.Path() != path {
		t.Fatal("rebuild changed per-Project cache location")
	}
	if err := rebuilt.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := sql.Open("sqlite", "file:"+filepath.ToSlash(path))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := raw.Exec(`PRAGMA user_version = 99`); err != nil {
		raw.Close()
		t.Fatal(err)
	}
	if err := raw.Close(); err != nil {
		t.Fatal(err)
	}
	compatible, err := Open(directory, registration)
	if err != nil {
		t.Fatalf("incompatible derived schema was not rebuilt: %v", err)
	}
	defer compatible.Close()
}

func TestProjectCachesArePhysicallyIsolated(t *testing.T) {
	directory := t.TempDir()
	left, err := Open(directory, project.Registration{ProjectId: "01890c29-7a78-7abc-8def-0123456789ab", RepositoryRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer left.Close()
	right, err := Open(directory, project.Registration{ProjectId: "01890c29-7a78-7abc-8def-1123456789ab", RepositoryRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer right.Close()
	if left.Path() == right.Path() || filepath.Dir(left.Path()) == filepath.Dir(right.Path()) {
		t.Fatal("cross-Project cache directories were reused")
	}
}

func TestConcurrentCacheWritersRemainReplaceableAndConsistent(t *testing.T) {
	cache, err := Open(t.TempDir(), project.Registration{ProjectId: "01890c29-7a78-7abc-8def-0123456789ab", RepositoryRoot: t.TempDir()})
	if err != nil {
		t.Fatal(err)
	}
	defer cache.Close()
	var wait sync.WaitGroup
	errorsChannel := make(chan error, 16)
	for index := 0; index < 8; index++ {
		wait.Add(1)
		go func() {
			defer wait.Done()
			analysis := repositorymodel.FileAnalysis{Path: "source.go", ContentKey: "content-key"}
			if err := cache.StoreFileAnalysis(analysis, "analyzer-set"); err != nil {
				errorsChannel <- err
				return
			}
			loaded, err := cache.LoadFileAnalysis("source.go", "content-key", "analyzer-set")
			if err != nil {
				errorsChannel <- err
				return
			}
			if loaded.Path != analysis.Path {
				errorsChannel <- fmt.Errorf("inconsistent concurrent cache read")
			}
		}()
	}
	wait.Wait()
	close(errorsChannel)
	for err := range errorsChannel {
		t.Fatal(err)
	}
}
