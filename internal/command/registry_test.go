package command_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Eu-Pedro0ficial/praetor/internal/audit"
	"github.com/Eu-Pedro0ficial/praetor/internal/command"
	"github.com/Eu-Pedro0ficial/praetor/internal/composition"
	"github.com/Eu-Pedro0ficial/praetor/internal/source"
)

func TestDefaultRegistryHasUniqueCompleteSlashMetadata(t *testing.T) {
	registry := newTestRegistry(t)
	commands := registry.Commands()
	wantTopLevel := []string{"/status", "/change", "/analysis", "/help", "/exit"}
	if len(commands) != len(wantTopLevel) {
		t.Fatalf("Commands() = %#v", commands)
	}
	seen := make(map[string]struct{})
	var inspect func([]command.Metadata)
	inspect = func(metadata []command.Metadata) {
		for _, item := range metadata {
			if _, exists := seen[item.SlashPath]; exists {
				t.Fatalf("duplicate slash path %q", item.SlashPath)
			}
			seen[item.SlashPath] = struct{}{}
			if strings.TrimSpace(item.Description) == "" || strings.TrimSpace(item.Usage) == "" {
				t.Fatalf("incomplete metadata = %#v", item)
			}
			inspect(item.Subcommands)
		}
	}
	inspect(commands)
	for index, slashPath := range wantTopLevel {
		if commands[index].SlashPath != slashPath {
			t.Fatalf("command %d = %q, want %q", index, commands[index].SlashPath, slashPath)
		}
	}
	if _, exists := seen["/analysis impact"]; !exists {
		t.Fatal("registry lacks /analysis impact")
	}
	if _, exists := seen["/change new"]; !exists {
		t.Fatal("registry lacks /change new")
	}
}

func TestNewRegistryRejectsDuplicateAndIncompleteDefinitions(t *testing.T) {
	handler := func(*command.Session, command.Invocation, io.Writer) (command.Result, error) {
		return command.Result{}, nil
	}
	tests := []struct {
		name        string
		definitions []command.Definition
	}{
		{
			name: "duplicate",
			definitions: []command.Definition{
				{Name: "same", Description: "first", Usage: "/same", Handler: handler},
				{Name: "same", Description: "second", Usage: "/same", Handler: handler},
			},
		},
		{name: "missing description", definitions: []command.Definition{{Name: "test", Usage: "/test", Handler: handler}}},
		{name: "missing usage", definitions: []command.Definition{{Name: "test", Description: "test", Handler: handler}}},
		{name: "missing handler", definitions: []command.Definition{{Name: "test", Description: "test", Usage: "/test"}}},
		{name: "invalid name", definitions: []command.Definition{{Name: "bad/name", Description: "test", Usage: "/bad/name", Handler: handler}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := command.NewRegistry(test.definitions); err == nil {
				t.Fatal("NewRegistry() accepted invalid definitions")
			}
		})
	}
}

func TestRegistryHelpStatusAndActiveProjectContextReuse(t *testing.T) {
	repositoryRoot, dataDirectory, session, registry := prepareCommittedCommandTest(t)
	registration := session.Registration()
	var output bytes.Buffer

	if _, err := registry.Dispatch(session, "/help", &output); err != nil {
		t.Fatalf("/help error = %v", err)
	}
	for _, expected := range []string{"/status", "/change", "/analysis", "/help", "/exit"} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("/help output %q lacks %q", output.String(), expected)
		}
	}
	output.Reset()
	if _, err := registry.Dispatch(session, "/help /analysis", &output); err != nil {
		t.Fatalf("/help /analysis error = %v", err)
	}
	if !strings.Contains(output.String(), "/analysis <subcommand>") || !strings.Contains(output.String(), "impact") {
		t.Fatalf("analysis help = %q", output.String())
	}

	output.Reset()
	for range 2 {
		if _, err := registry.Dispatch(session, "/status", &output); err != nil {
			t.Fatalf("/status error = %v", err)
		}
	}
	if strings.Count(output.String(), string(registration.ProjectId)) != 2 ||
		strings.Count(output.String(), repositoryRoot) != 2 {
		t.Fatalf("retained status context = %q", output.String())
	}
	events := readCommandAudit(t, dataDirectory)
	if len(events) != 3 {
		t.Fatalf("status reused context but audit contains %d startup events", len(events))
	}
	assertGovernedCommandRepositoryClean(t, repositoryRoot)
}

func TestRegistryChangeNewPreservesM02Lifecycle(t *testing.T) {
	_, dataDirectory, session, registry := prepareCommittedCommandTest(t)
	var output bytes.Buffer
	result, err := registry.Dispatch(
		session,
		`/change new change-shell "govern service update" planned`,
		&output,
	)
	if err != nil || result.Exit {
		t.Fatalf("/change new result = %#v, error = %v", result, err)
	}
	for _, expected := range []string{
		"Change ID: change-shell\n",
		"Intent: govern service update\n",
		"State: planned\n",
	} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("change output %q lacks %q", output.String(), expected)
		}
	}
	currentChange, ok := session.CurrentChange()
	if !ok || currentChange.ChangeId() != "change-shell" || currentChange.State() != "planned" {
		t.Fatalf("current Change = %#v, %t", currentChange, ok)
	}
	events := readCommandAudit(t, dataDirectory)
	if len(events) != 5 || events[3].EventType != audit.EventChangeCreated || events[4].EventType != audit.EventChangeTransition {
		t.Fatalf("M0.2 shell audit = %#v", events)
	}
	if events[3].ProjectID != string(session.Registration().ProjectId) || events[3].ChangeID != "change-shell" {
		t.Fatalf("M0.2 shell linkage = %#v", events[3])
	}
}

func TestRegistryAnalysisImpactAllowsExpectedAndPossiblePaths(t *testing.T) {
	repositoryRoot, dataDirectory, session, registry := prepareCommittedCommandTest(t)
	var output bytes.Buffer
	_, err := registry.Dispatch(
		session,
		`/analysis impact change-analysis "bound service update" --expected internal/service/service.go --possible internal/service/service_test.go --protected go.mod --actual internal/service/service_test.go internal/service/service.go`,
		&output,
	)
	if err != nil {
		t.Fatalf("/analysis impact error = %v", err)
	}
	for _, expected := range []string{
		"Change ID: change-analysis\n",
		"Working tree: clean\n",
		"Tracked paths: 5\n",
		"Expected: internal/service/service.go\n",
		"Possible: internal/service/service_test.go\n",
		"Protected: go.mod\n",
		"Allowed: true\n",
		"Violations: none\n",
	} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("analysis output %q lacks %q", output.String(), expected)
		}
	}
	events := readCommandAudit(t, dataDirectory)
	if len(events) != 8 {
		t.Fatalf("analysis audit contains %d events, want 8", len(events))
	}
	wantTypes := []string{
		audit.EventSourceSnapshotCaptured,
		audit.EventImpactAnalysisProduced,
		audit.EventChangeSurfaceEstablished,
		audit.EventChangeSurfaceValidated,
	}
	for index, eventType := range wantTypes {
		event := events[index+4]
		if event.EventType != eventType || event.ChangeID != "change-analysis" ||
			event.ProjectID != string(session.Registration().ProjectId) {
			t.Fatalf("M0.3 event %d linkage = %#v", index, event)
		}
		if strings.TrimSpace(event.Metadata["source_state_digest"].(string)) == "" {
			t.Fatalf("M0.3 event %d lacks source context", index)
		}
	}
	assertGovernedCommandRepositoryClean(t, repositoryRoot)
}

func TestRegistryAnalysisImpactRejectsProtectedMixedSurface(t *testing.T) {
	repositoryRoot, dataDirectory, session, registry := prepareCommittedCommandTest(t)
	var output bytes.Buffer
	_, err := registry.Dispatch(
		session,
		`/analysis impact change-protected "reject manifest update" --expected internal/service/service.go --possible internal/service/service_test.go --protected go.mod --actual internal/service/service.go go.mod`,
		&output,
	)
	var validationError *source.SurfaceValidationError
	if !errors.As(err, &validationError) {
		t.Fatalf("protected error = %v", err)
	}
	if !strings.Contains(output.String(), "Allowed: false\n") ||
		!strings.Contains(output.String(), "Expected changes: internal/service/service.go\n") ||
		!strings.Contains(output.String(), "- protected: go.mod") {
		t.Fatalf("protected output = %q", output.String())
	}
	events := readCommandAudit(t, dataDirectory)
	if len(events) != 8 || events[7].EventType != audit.EventChangeSurfaceViolation ||
		events[7].ChangeID != "change-protected" {
		t.Fatalf("protected audit = %#v", events)
	}
	assertGovernedCommandRepositoryClean(t, repositoryRoot)
}

func TestRegistryRejectsUnsafeAnalysisBeforeChangeMutation(t *testing.T) {
	_, dataDirectory, session, registry := prepareCommittedCommandTest(t)
	_, err := registry.Dispatch(
		session,
		`/analysis impact change-unsafe "unsafe scope" --expected internal/../go.mod --actual go.mod`,
		io.Discard,
	)
	if err == nil || !strings.Contains(err.Error(), "traversal") {
		t.Fatalf("unsafe analysis error = %v", err)
	}
	events := readCommandAudit(t, dataDirectory)
	if len(events) != 3 {
		t.Fatalf("unsafe analysis created runtime events: %#v", events)
	}
}

func TestRegistryDispatchErrorsExitAndCompletion(t *testing.T) {
	_, _, session, registry := prepareCommittedCommandTest(t)
	tests := []struct {
		line      string
		wantError string
	}{
		{line: "status", wantError: "slash syntax"},
		{line: "/unknown", wantError: "unknown command"},
		{line: "/analysis", wantError: "requires a subcommand"},
		{line: "/analysis unknown", wantError: "unknown subcommand"},
		{line: `/change new change-one "unclosed`, wantError: "unclosed quote"},
		{line: `/change new change-one trailing\`, wantError: "trailing escape"},
	}
	for _, test := range tests {
		if _, err := registry.Dispatch(session, test.line, io.Discard); err == nil || !strings.Contains(err.Error(), test.wantError) {
			t.Errorf("Dispatch(%q) error = %v, want %q", test.line, err, test.wantError)
		}
	}
	result, err := registry.Dispatch(session, "/exit", io.Discard)
	if err != nil || !result.Exit {
		t.Fatalf("/exit result = %#v, error = %v", result, err)
	}

	assertSuggestions(t, registry.Complete("/"), []string{"/status", "/change", "/analysis", "/help", "/exit"})
	assertSuggestions(t, registry.Complete("/ana"), []string{"/analysis"})
	assertSuggestions(t, registry.Complete("/analysis "), []string{"impact"})
	assertSuggestions(t, registry.Complete("/analysis im"), []string{"impact"})
	assertSuggestions(t, registry.Complete("/change "), []string{"new"})
	if suggestions := registry.Complete("analysis"); len(suggestions) != 0 {
		t.Fatalf("non-slash completion = %#v", suggestions)
	}
}

func newTestRegistry(t *testing.T) command.Registry {
	t.Helper()
	registry, err := command.DefaultRegistry()
	if err != nil {
		t.Fatalf("DefaultRegistry() error = %v", err)
	}
	return registry
}

func prepareCommittedCommandTest(t *testing.T) (string, string, *command.Session, command.Registry) {
	t.Helper()
	repositoryRoot := t.TempDir()
	runCommandGit(t, repositoryRoot, "init", "--quiet")
	files := map[string]string{
		"cmd/app/main.go":                  "package main\n",
		"internal/service/service.go":      "package service\n",
		"internal/service/service_test.go": "package service_test\n",
		"go.mod":                           "module example.invalid/fixture\n\ngo 1.25.1\n",
		"README.md":                        "# Fixture\n",
	}
	for relativePath, contents := range files {
		absolutePath := filepath.Join(repositoryRoot, filepath.FromSlash(relativePath))
		if err := os.MkdirAll(filepath.Dir(absolutePath), 0o755); err != nil {
			t.Fatalf("create fixture directory: %v", err)
		}
		if err := os.WriteFile(absolutePath, []byte(contents), 0o600); err != nil {
			t.Fatalf("write fixture %q: %v", relativePath, err)
		}
	}
	runCommandGit(t, repositoryRoot, "add", ".")
	runCommandGit(t, repositoryRoot, "-c", "user.name=Praetor Test", "-c", "user.email=praetor@example.invalid", "commit", "--quiet", "-m", "baseline")
	t.Chdir(repositoryRoot)
	xdgDataHome := filepath.Join(t.TempDir(), "xdg")
	t.Setenv("XDG_DATA_HOME", xdgDataHome)
	container := composition.New()
	session, err := container.NewInteractiveSession(".")
	if err != nil {
		t.Fatalf("NewInteractiveSession() error = %v", err)
	}
	return repositoryRoot, filepath.Join(xdgDataHome, "praetor"), session, newTestRegistry(t)
}

func runCommandGit(t *testing.T, repositoryRoot string, arguments ...string) {
	t.Helper()
	commandArguments := append([]string{"-C", repositoryRoot}, arguments...)
	process := exec.Command("git", commandArguments...)
	if output, err := process.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", arguments, err, output)
	}
}

func readCommandAudit(t *testing.T, dataDirectory string) []audit.Event {
	t.Helper()
	events, err := audit.Read(dataDirectory)
	if err != nil {
		t.Fatalf("audit.Read() error = %v", err)
	}
	return events
}

func assertSuggestions(t *testing.T, suggestions []command.Suggestion, want []string) {
	t.Helper()
	if len(suggestions) != len(want) {
		t.Fatalf("suggestions = %#v, want %#v", suggestions, want)
	}
	for index, suggestion := range suggestions {
		if suggestion.Text != want[index] || strings.TrimSpace(suggestion.Description) == "" {
			t.Fatalf("suggestion %d = %#v, want %q with description", index, suggestion, want[index])
		}
	}
}

func assertGovernedCommandRepositoryClean(t *testing.T, repositoryRoot string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(repositoryRoot, ".praetor")); !os.IsNotExist(err) {
		t.Fatalf("runtime metadata appeared in governed repository: %v", err)
	}
	process := exec.Command("git", "-C", repositoryRoot, "status", "--short")
	output, err := process.CombinedOutput()
	if err != nil {
		t.Fatalf("git status: %v: %s", err, output)
	}
	if len(output) != 0 {
		t.Fatalf("governed repository is dirty: %s", output)
	}
}

func ExampleRegistry_Complete() {
	registry, _ := command.DefaultRegistry()
	for _, suggestion := range registry.Complete("/ana") {
		fmt.Printf("%s — %s\n", suggestion.Text, suggestion.Description)
	}
	// Output:
	// /analysis — Analyze source and Change impact
}
