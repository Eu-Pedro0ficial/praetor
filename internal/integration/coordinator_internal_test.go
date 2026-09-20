package integration

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/Eu-Pedro0ficial/praetor/internal/project"
)

func TestProjectMutationLockExcludesProcessesAndReleasesOnDeath(t *testing.T) {
	state := t.TempDir()
	projectId, _ := project.GenerateProjectID()
	ready := filepath.Join(t.TempDir(), "ready")
	executable, _ := os.Executable()
	command := exec.Command(executable, "-test.run=TestMutationLockHelperProcess")
	command.Env = append(os.Environ(), "PRAETOR_LOCK_HELPER=1", "PRAETOR_LOCK_STATE="+state, "PRAETOR_LOCK_PROJECT="+string(projectId), "PRAETOR_LOCK_READY="+ready)
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
			t.Fatal("lock helper was not ready")
		}
		time.Sleep(10 * time.Millisecond)
	}
	path := filepath.Join(state, "projects", string(projectId), "canonical-mutation.lock")
	contender, err := os.OpenFile(path, os.O_RDWR, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	err = syscall.Flock(int(contender.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
	if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EAGAIN) {
		t.Fatalf("concurrent lock error=%v", err)
	}
	contender.Close()
	if err := command.Process.Kill(); err != nil {
		t.Fatal(err)
	}
	_ = command.Wait()
	lock, err := acquireProjectLock(state, projectId)
	if err != nil {
		t.Fatalf("lock not released after process death: %v", err)
	}
	releaseProjectLock(lock)
}

func TestMutationLockHelperProcess(t *testing.T) {
	if os.Getenv("PRAETOR_LOCK_HELPER") == "" {
		return
	}
	projectId := project.ProjectId(os.Getenv("PRAETOR_LOCK_PROJECT"))
	lock, err := acquireProjectLock(os.Getenv("PRAETOR_LOCK_STATE"), projectId)
	if err != nil {
		os.Exit(31)
	}
	defer releaseProjectLock(lock)
	if err := os.WriteFile(os.Getenv("PRAETOR_LOCK_READY"), []byte("ready"), 0o600); err != nil {
		os.Exit(32)
	}
	select {}
}

func TestProjectMutationLockRejectsUnexpectedPathTypes(t *testing.T) {
	for _, test := range []struct {
		name   string
		create func(string) error
	}{
		{name: "directory", create: func(path string) error { return os.Mkdir(path, 0o700) }},
		{name: "symlink", create: func(path string) error { return os.Symlink("missing-target", path) }},
		{name: "fifo", create: func(path string) error { return syscall.Mkfifo(path, 0o600) }},
	} {
		t.Run(test.name, func(t *testing.T) {
			state := t.TempDir()
			projectId, err := project.GenerateProjectID()
			if err != nil {
				t.Fatal(err)
			}
			path := filepath.Join(state, "projects", string(projectId), "canonical-mutation.lock")
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := test.create(path); err != nil {
				t.Fatal(err)
			}
			lock, err := acquireProjectLock(state, projectId)
			if lock != nil {
				releaseProjectLock(lock)
				t.Fatal("unsafe lock path was accepted")
			}
			if err == nil {
				t.Fatal("unsafe lock path did not fail closed")
			}
		})
	}
}
