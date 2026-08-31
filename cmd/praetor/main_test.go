package main

import (
	"bytes"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Eu-Pedro0ficial/praetor/internal/audit"
	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"github.com/Eu-Pedro0ficial/praetor/internal/project"
)

func TestRunStatusPreservesM01Behavior(t *testing.T) {
	repositoryRoot, dataDirectory := prepareRuntimeTest(t)
	var output bytes.Buffer

	if err := runWithOutput([]string{"status"}, &output); err != nil {
		t.Fatalf("runWithOutput(status) error = %v", err)
	}
	registrations, err := project.LoadRegistrations(dataDirectory)
	if err != nil {
		t.Fatalf("project.LoadRegistrations() error = %v", err)
	}
	if len(registrations) != 1 {
		t.Fatalf("project registry contains %d entries, want 1", len(registrations))
	}
	expectedOutput := "Project ID: " + string(registrations[0].ProjectId) + "\n" +
		"Repository root: " + repositoryRoot + "\n" +
		"Git repository: true\n"
	if output.String() != expectedOutput {
		t.Fatalf("status output = %q, want %q", output.String(), expectedOutput)
	}

	events, err := audit.Read(dataDirectory)
	if err != nil {
		t.Fatalf("audit.Read() error = %v", err)
	}
	if len(events) != 3 {
		t.Fatalf("status recorded %d events, want 3", len(events))
	}
	for _, event := range events {
		if event.ChangeID != "" || event.ProjectID != string(registrations[0].ProjectId) {
			t.Fatalf("M0.1 event changed unexpectedly: %#v", event)
		}
	}
	assertGovernedRepositoryClean(t, repositoryRoot)
}

func TestRunChangeNewExecutesCompleteApprovedLifecycle(t *testing.T) {
	repositoryRoot, dataDirectory := prepareRuntimeTest(t)
	var output bytes.Buffer
	args := []string{
		"change", "new", "change-cli-happy", "prove M0.2",
		"planned", "isolated", "validated", "approved", "audit-locked",
	}

	if err := runWithOutput(args, &output); err != nil {
		t.Fatalf("runWithOutput(change new) error = %v", err)
	}
	registrations, err := project.LoadRegistrations(dataDirectory)
	if err != nil || len(registrations) != 1 {
		t.Fatalf("project registration = %#v, error = %v", registrations, err)
	}
	expectedOutput := "Change ID: change-cli-happy\n" +
		"Project ID: " + string(registrations[0].ProjectId) + "\n" +
		"Intent: prove M0.2\n" +
		"State: audit-locked\n"
	if output.String() != expectedOutput {
		t.Fatalf("change output = %q, want %q", output.String(), expectedOutput)
	}

	events, err := audit.Read(dataDirectory)
	if err != nil {
		t.Fatalf("audit.Read() error = %v", err)
	}
	if len(events) != 9 {
		t.Fatalf("complete lifecycle recorded %d events, want 9", len(events))
	}
	changeEvents := events[3:]
	wantStates := []string{"created", "planned", "isolated", "validated", "approved", "audit-locked"}
	for index, event := range changeEvents {
		if event.ChangeID != "change-cli-happy" || event.ProjectID != string(registrations[0].ProjectId) {
			t.Fatalf("Change event %d has incorrect identity linkage: %#v", index, event)
		}
		if event.Metadata["resulting_state"] != wantStates[index] {
			t.Fatalf("Change event %d resulting state = %#v, want %q", index, event.Metadata["resulting_state"], wantStates[index])
		}
		if strings.TrimSpace(event.Metadata["transition_timestamp"].(string)) == "" || strings.TrimSpace(event.Metadata["context"].(string)) == "" {
			t.Fatalf("Change event %d lacks provenance: %#v", index, event.Metadata)
		}
		if index > 0 && event.Metadata["previous_state"] != wantStates[index-1] {
			t.Fatalf("Change event %d previous state = %#v, want %q", index, event.Metadata["previous_state"], wantStates[index-1])
		}
	}
	assertGovernedRepositoryClean(t, repositoryRoot)
}

func TestRunChangeNewRejectsForbiddenTransitionDeterministically(t *testing.T) {
	repositoryRoot, dataDirectory := prepareRuntimeTest(t)
	var output bytes.Buffer

	err := runWithOutput(
		[]string{"change", "new", "change-cli-forbidden", "prove rejection", "validated"},
		&output,
	)
	var transitionError change.InvalidTransitionError
	if !errors.As(err, &transitionError) {
		t.Fatalf("runWithOutput() error = %v, want InvalidTransitionError", err)
	}
	if transitionError.ChangeId != "change-cli-forbidden" || transitionError.From != change.StateCreated || transitionError.To != change.StateValidated {
		t.Fatalf("InvalidTransitionError = %#v", transitionError)
	}
	if output.Len() != 0 {
		t.Fatalf("failed command emitted success output %q", output.String())
	}

	events, readError := audit.Read(dataDirectory)
	if readError != nil {
		t.Fatalf("audit.Read() error = %v", readError)
	}
	if len(events) != 4 {
		t.Fatalf("forbidden lifecycle recorded %d events, want 3 startup plus creation", len(events))
	}
	if events[3].EventType != audit.EventChangeCreated || events[3].ChangeID != "change-cli-forbidden" {
		t.Fatalf("creation event missing after forbidden transition: %#v", events[3])
	}
	for _, event := range events {
		if event.EventType == audit.EventChangeTransition {
			t.Fatalf("forbidden transition was audited as successful: %#v", event)
		}
	}
	assertGovernedRepositoryClean(t, repositoryRoot)
}

func TestRunRejectsUnknownCommandBeforeRuntimeMutation(t *testing.T) {
	_, dataDirectory := prepareRuntimeTest(t)
	var output bytes.Buffer

	if err := runWithOutput([]string{"change", "show"}, &output); err == nil {
		t.Fatal("runWithOutput() accepted an unsupported command")
	}
	if output.Len() != 0 {
		t.Fatalf("unsupported command emitted output %q", output.String())
	}
	if _, err := os.Stat(dataDirectory); !os.IsNotExist(err) {
		t.Fatalf("unsupported command mutated runtime data: %v", err)
	}
}

func prepareRuntimeTest(t *testing.T) (string, string) {
	t.Helper()
	repositoryRoot := t.TempDir()
	command := exec.Command("git", "-C", repositoryRoot, "init", "--quiet")
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	t.Chdir(repositoryRoot)
	xdgDataHome := filepath.Join(t.TempDir(), "xdg")
	t.Setenv("XDG_DATA_HOME", xdgDataHome)
	return repositoryRoot, filepath.Join(xdgDataHome, "praetor")
}

func assertGovernedRepositoryClean(t *testing.T, repositoryRoot string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(repositoryRoot, ".praetor")); !os.IsNotExist(err) {
		t.Fatalf("runtime metadata appeared in governed repository: %v", err)
	}
	command := exec.Command("git", "-C", repositoryRoot, "status", "--short")
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git status: %v: %s", err, output)
	}
	if len(output) != 0 {
		t.Fatalf("governed repository is dirty after runtime command: %s", output)
	}
}
