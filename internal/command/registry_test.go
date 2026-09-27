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

	sqliteadapter "github.com/Eu-Pedro0ficial/praetor/internal/adapters/persistence/sqlite"
	"github.com/Eu-Pedro0ficial/praetor/internal/audit"
	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"github.com/Eu-Pedro0ficial/praetor/internal/command"
	"github.com/Eu-Pedro0ficial/praetor/internal/composition"
	"github.com/Eu-Pedro0ficial/praetor/internal/source"
)

func TestDefaultRegistryHasUniqueCompleteHierarchicalMetadata(t *testing.T) {
	registry := newTestRegistry(t)
	commands := registry.Commands()
	wantTopLevel := []string{"status", "analysis", "change", "policy", "provider", "configure", "help", "?", "exit"}
	if len(commands) != len(wantTopLevel) {
		t.Fatalf("Commands() = %#v", commands)
	}
	seen := make(map[string]struct{})
	var inspect func([]command.Metadata)
	inspect = func(metadata []command.Metadata) {
		for _, item := range metadata {
			if _, exists := seen[item.Path]; exists {
				t.Fatalf("duplicate command path %q", item.Path)
			}
			seen[item.Path] = struct{}{}
			if strings.TrimSpace(item.Description) == "" || strings.TrimSpace(item.Usage) == "" {
				t.Fatalf("incomplete metadata = %#v", item)
			}
			inspect(item.Children)
		}
	}
	inspect(commands)
	for index, commandPath := range wantTopLevel {
		if commands[index].Path != commandPath {
			t.Fatalf("command %d = %q, want %q", index, commands[index].Path, commandPath)
		}
	}
	if _, exists := seen["analysis impact"]; !exists {
		t.Fatal("registry lacks analysis impact")
	}
	if _, exists := seen["change new"]; !exists {
		t.Fatal("registry lacks change new")
	}
	for _, commandPath := range []string{
		"change isolate", "change implement", "change patch", "change verify", "change approve", "change reject", "change apply", "change close", "change discard",
		"provider list", "provider show", "provider select", "provider model", "configure project",
		"configure layout show", "configure layout sidebar show", "configure layout sidebar identity",
		"configure layout sidebar context", "configure layout sidebar provider", "configure layout sidebar status",
		"configure layout color accent", "configure layout color border", "configure layout color background",
		"configure layout color text", "configure layout reset",
	} {
		if _, exists := seen[commandPath]; !exists {
			t.Fatalf("registry lacks %s", commandPath)
		}
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
				{Name: "same", Description: "first", Usage: "same", Handler: handler},
				{Name: "same", Description: "second", Usage: "same", Handler: handler},
			},
		},
		{name: "missing description", definitions: []command.Definition{{Name: "test", Usage: "test", Handler: handler}}},
		{name: "missing usage", definitions: []command.Definition{{Name: "test", Description: "test", Handler: handler}}},
		{name: "missing handler", definitions: []command.Definition{{Name: "test", Description: "test", Usage: "test"}}},
		{name: "invalid name", definitions: []command.Definition{{Name: "bad/name", Description: "test", Usage: "bad/name", Handler: handler}}},
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

	if _, err := registry.Dispatch(session, "help", &output); err != nil {
		t.Fatalf("help error = %v", err)
	}
	for _, expected := range []string{"status", "analysis", "change", "policy", "provider", "configure", "help", "?", "exit"} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("/help output %q lacks %q", output.String(), expected)
		}
	}
	output.Reset()
	if _, err := registry.Dispatch(session, "help analysis", &output); err != nil {
		t.Fatalf("help analysis error = %v", err)
	}
	if !strings.Contains(output.String(), "analysis [model|impact|report|inspect ...]") || !strings.Contains(output.String(), "report") {
		t.Fatalf("analysis help = %q", output.String())
	}

	output.Reset()
	if _, err := registry.Dispatch(session, "help change", &output); err != nil {
		t.Fatalf("help change error = %v", err)
	}
	if !strings.Contains(output.String(), "Usage: change [command]") ||
		!strings.Contains(output.String(), "isolate") ||
		strings.Contains(output.String(), "change [new|list|") {
		t.Fatalf("change help repeats child syntax: %q", output.String())
	}

	output.Reset()
	for range 2 {
		if _, err := registry.Dispatch(session, "status", &output); err != nil {
			t.Fatalf("status error = %v", err)
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
		`change new change-shell "govern service update" planned`,
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
		`analysis impact change-analysis "bound service update" --expected internal/service/service.go --possible internal/service/service_test.go --protected go.mod --actual internal/service/service_test.go internal/service/service.go`,
		&output,
	)
	if err != nil {
		t.Fatalf("/analysis impact error = %v", err)
	}
	for _, expected := range []string{
		"Change ID: change-analysis\n",
		"Working tree: clean\n",
		"Tracked paths: 6\n",
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
		`analysis impact change-protected "reject manifest update" --expected internal/service/service.go --possible internal/service/service_test.go --protected go.mod --actual internal/service/service.go go.mod`,
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
		`analysis impact change-unsafe "unsafe scope" --expected internal/../go.mod --actual go.mod`,
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

func TestRegistrySimplestIsolationWorkflowDefaultsRepositoryWide(t *testing.T) {
	repositoryRoot, _, session, registry := prepareCommittedCommandTest(t)
	var output bytes.Buffer
	if _, err := registry.Dispatch(
		session,
		"change isolate change-simple \"use the simplest workflow\"",
		&output,
	); err != nil {
		t.Fatalf("simplest isolate error = %v", err)
	}
	proposal, ok := session.CurrentProposal()
	if !ok || proposal.ApprovedScope().Surface().AuthorizationMode() != source.AuthorizationRepositoryWide {
		t.Fatalf("simplest isolate proposal = %#v/%t", proposal, ok)
	}
	for _, expected := range []string{
		"Change: change-simple (isolated)\n",
		"Workspace: ",
		"Scope: repository-wide\n",
	} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("simplest isolate output %q lacks %q", output.String(), expected)
		}
	}
	if strings.Contains(output.String(), "Expected:") ||
		strings.Contains(output.String(), "Possible:") ||
		strings.Contains(output.String(), "Protected:") {
		t.Fatalf("simplest isolate printed empty scope categories: %q", output.String())
	}
	if _, err := registry.Dispatch(session, "change discard", io.Discard); err != nil {
		t.Fatalf("discard simplest proposal: %v", err)
	}
	assertGovernedCommandRepositoryClean(t, repositoryRoot)
}

func TestRegistryRepositoryWideIsolationAllowsDiscoveryAndPreservesProtection(t *testing.T) {
	t.Run("discovered tracked path is accepted", func(t *testing.T) {
		repositoryRoot, dataDirectory, session, registry := prepareCommittedCommandTest(t)
		var output bytes.Buffer
		if _, err := registry.Dispatch(
			session,
			`change isolate change-repository-wide "discover the implementation surface" --protected internal/service`,
			&output,
		); err != nil {
			t.Fatalf("repository-wide isolate error = %v", err)
		}
		for _, expected := range []string{
			"Change: change-repository-wide (isolated)\n",
			"Workspace: ",
			"Scope: repository-wide\n",
			"Protected: internal/service\n",
		} {
			if !strings.Contains(output.String(), expected) {
				t.Fatalf("isolate output %q lacks %q", output.String(), expected)
			}
		}
		for _, redundant := range []string{
			"Authorization mode:",
			"Expected:",
			"Possible:",
			"Project ID:",
			"Workspace ID:",
			"Source state digest:",
			"Isolation boundary:",
		} {
			if strings.Contains(output.String(), redundant) {
				t.Fatalf("concise repository-wide output %q contains %q", output.String(), redundant)
			}
		}
		currentProposal, ok := session.CurrentProposal()
		if !ok {
			t.Fatal("repository-wide isolate did not retain ProposalWorkspace")
		}
		surface := currentProposal.ApprovedScope().Surface()
		if surface.AuthorizationMode() != source.AuthorizationRepositoryWide ||
			len(surface.ExpectedPaths()) != 0 || len(surface.PossiblePaths()) != 0 {
			t.Fatalf("repository-wide ApprovedScope = mode %q expected %#v possible %#v", surface.AuthorizationMode(), surface.ExpectedPaths(), surface.PossiblePaths())
		}
		workspaceRoot := currentProposal.Workspace().Root()
		writeCommandFile(t, workspaceRoot, "cmd/app/main.go", "package main\n\nconst Discovered = true\n")

		output.Reset()
		if _, err := registry.Dispatch(session, "change patch", &output); err != nil {
			t.Fatalf("repository-wide patch error = %v", err)
		}
		if !strings.Contains(output.String(), "Surface valid: true") ||
			!strings.Contains(output.String(), "Changed paths: cmd/app/main.go") {
			t.Fatalf("repository-wide patch output = %q", output.String())
		}
		classified, ok := session.CurrentProposal()
		if !ok {
			t.Fatal("surface-valid repository-wide proposal was not retained")
		}
		patch, ok := classified.PatchArtifact()
		if !ok || len(patch.ChangedPaths()) != 1 || patch.ChangedPaths()[0] != "cmd/app/main.go" {
			t.Fatalf("repository-wide PatchArtifact = %#v, present=%t", patch, ok)
		}
		if contents, err := os.ReadFile(filepath.Join(repositoryRoot, "cmd", "app", "main.go")); err != nil ||
			string(contents) != "package main\n" {
			t.Fatalf("canonical source changed: %q, %v", contents, err)
		}

		events := readCommandAudit(t, dataDirectory)
		var sawEstablished, sawValidated bool
		for _, event := range events {
			switch event.EventType {
			case audit.EventChangeSurfaceEstablished:
				if event.Metadata["authorization_mode"] == "repository-wide" && event.Metadata["repository_scope"] == "." {
					sawEstablished = true
				}
			case audit.EventPatchSurfaceValidated:
				if event.Metadata["approved_authorization_mode"] == "repository-wide" {
					sawValidated = true
				}
			}
		}
		if !sawEstablished || !sawValidated {
			t.Fatalf("repository-wide audit metadata missing: %#v", events)
		}

		output.Reset()
		if _, err := registry.Dispatch(session, "change artifacts change-repository-wide", &output); err != nil {
			t.Fatalf("list repository-wide artifacts: %v", err)
		}
		var approvedScopeArtifact string
		for _, line := range strings.Split(output.String(), "\n") {
			if strings.HasPrefix(line, "binding role=approved-scope artifact=") {
				fields := strings.Fields(line)
				approvedScopeArtifact = strings.TrimPrefix(fields[2], "artifact=")
			}
		}
		if approvedScopeArtifact == "" {
			t.Fatalf("approved-scope binding missing: %q", output.String())
		}
		output.Reset()
		if _, err := registry.Dispatch(session, "change content "+approvedScopeArtifact+" change-repository-wide", &output); err != nil {
			t.Fatalf("inspect repository-wide ApprovedScope: %v", err)
		}
		if !strings.Contains(output.String(), "authorization_mode") || !strings.Contains(output.String(), "repository-wide") {
			t.Fatalf("ApprovedScope payload lacks repository-wide mode: %q", output.String())
		}

		assertGovernedCommandRepositoryClean(t, repositoryRoot)
		if _, err := registry.Dispatch(session, "change discard", io.Discard); err != nil {
			t.Fatalf("discard repository-wide proposal: %v", err)
		}
	})

	t.Run("protected subtree is rejected", func(t *testing.T) {
		repositoryRoot, _, session, registry := prepareCommittedCommandTest(t)
		if _, err := registry.Dispatch(
			session,
			`change isolate change-repository-protected "protect a repository subtree" --protected internal/service`,
			io.Discard,
		); err != nil {
			t.Fatalf("repository-wide isolate error = %v", err)
		}
		currentProposal, _ := session.CurrentProposal()
		workspaceRoot := currentProposal.Workspace().Root()
		writeCommandFile(t, workspaceRoot, "internal/service/service.go", "package service\n\nconst Forbidden = true\n")

		var output bytes.Buffer
		_, err := registry.Dispatch(session, "change patch", &output)
		if err == nil || !strings.Contains(err.Error(), "protected path") {
			t.Fatalf("protected subtree patch error = %v", err)
		}
		if !strings.Contains(output.String(), "- protected: internal/service/service.go") {
			t.Fatalf("protected subtree patch output = %q", output.String())
		}
		if _, ok := session.CurrentProposal(); ok {
			t.Fatal("protected repository-wide proposal remained active")
		}
		if _, err := os.Stat(workspaceRoot); !os.IsNotExist(err) {
			t.Fatalf("protected workspace remains: %v", err)
		}
		assertGovernedCommandRepositoryClean(t, repositoryRoot)
	})
}

func TestRegistryRepositoryRootSyntaxAndScopeConflicts(t *testing.T) {
	t.Run("explicit root form", func(t *testing.T) {
		_, _, session, registry := prepareCommittedCommandTest(t)
		if _, err := registry.Dispatch(session, `change isolate change-root-form "use explicit root" --possible .`, io.Discard); err != nil {
			t.Fatalf("--possible . isolate error = %v", err)
		}
		currentProposal, ok := session.CurrentProposal()
		if !ok || currentProposal.ApprovedScope().Surface().AuthorizationMode() != source.AuthorizationRepositoryWide {
			t.Fatalf("--possible . did not select repository-wide scope: %#v, %t", currentProposal, ok)
		}
		if _, err := registry.Dispatch(session, "change discard", io.Discard); err != nil {
			t.Fatalf("discard explicit-root proposal: %v", err)
		}
	})

	tests := []struct {
		name      string
		options   string
		wantError string
	}{
		{name: "both root forms", options: "--expected . --possible .", wantError: "redundant"},
		{name: "root mixed with explicit path", options: "--expected . --possible README.md", wantError: "cannot be combined"},
		{name: "protected repository root", options: "--protected .", wantError: "complete repository"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, _, session, registry := prepareCommittedCommandTest(t)
			_, err := registry.Dispatch(session, `change isolate change-scope-conflict "reject ambiguous scope" `+test.options, io.Discard)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("scope conflict error = %v, want containing %q", err, test.wantError)
			}
			if _, ok := session.CurrentChange(); ok {
				t.Fatal("scope conflict created a Change")
			}
		})
	}
}

func TestRegistryRepositoryWideIsolationHelpAndErrors(t *testing.T) {
	_, _, session, registry := prepareCommittedCommandTest(t)
	var output bytes.Buffer
	if _, err := registry.Dispatch(session, "help change isolate", &output); err != nil {
		t.Fatalf("help change isolate error = %v", err)
	}
	for _, expected := range []string{
		"Create an isolated proposal; scope is repository-wide unless paths are provided",
		"Usage: isolate <change-id> <intent>",
		"--expected",
		"--possible",
		"--protected",
		"providing paths selects explicit scope",
	} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("change isolate help lacks %q: %q", expected, output.String())
		}
	}

	tests := []struct {
		name      string
		command   string
		wantError string
	}{
		{name: "missing intent", command: "change isolate change-missing-intent", wantError: "usage: isolate <change-id> <intent>"},
		{name: "unknown option", command: `change isolate change-unknown-option "intent" --unknown README.md`, wantError: `unknown surface option "--unknown"`},
		{name: "empty expected", command: `change isolate change-empty-expected "intent" --expected`, wantError: "requires at least one path"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, err := registry.Dispatch(session, test.command, io.Discard)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("error = %v, want containing %q", err, test.wantError)
			}
			if test.name == "missing intent" && err.Error() != "usage: isolate <change-id> <intent>" {
				t.Fatalf("missing-intent error is verbose: %q", err)
			}
		})
	}
	assertSuggestions(t, registry.ContextualHelp(session, "change isolate --"), []string{
		"--expected", "--possible", "--protected",
	})
}

func TestRegistryAnalysisAcceptsPossibleOnlyStrictScope(t *testing.T) {
	_, _, session, registry := prepareCommittedCommandTest(t)
	var output bytes.Buffer
	if _, err := registry.Dispatch(
		session,
		`analysis impact change-possible-only "possible-only strict scope" --possible README.md --actual README.md`,
		&output,
	); err != nil {
		t.Fatalf("possible-only analysis error = %v", err)
	}
	if !strings.Contains(output.String(), "Scope: explicit") ||
		!strings.Contains(output.String(), "Possible: README.md") ||
		!strings.Contains(output.String(), "Possible changes: README.md") {
		t.Fatalf("possible-only analysis output = %q", output.String())
	}
	if strings.Contains(output.String(), "Expected:") ||
		strings.Contains(output.String(), "Protected:") ||
		strings.Contains(output.String(), "Expected changes:") {
		t.Fatalf("possible-only analysis printed empty scope categories: %q", output.String())
	}
}

func TestRegistryM04RetainsSurfaceValidPatchWithoutAdvancingValidation(t *testing.T) {
	repositoryRoot, dataDirectory, session, registry := prepareCommittedCommandTest(t)
	baseRevision := strings.TrimSpace(string(runCommandGitOutput(t, repositoryRoot, "rev-parse", "HEAD")))
	var output bytes.Buffer
	if _, err := registry.Dispatch(session, "change", io.Discard); err != nil {
		t.Fatalf("enter change mode: %v", err)
	}
	_, err := registry.Dispatch(
		session,
		`isolate change-proposal "update service" --expected internal/service/service.go --possible internal/service/service_test.go --protected go.mod`,
		&output,
	)
	if err != nil {
		t.Fatalf("/change isolate error = %v", err)
	}
	currentProposal, ok := session.CurrentProposal()
	if !ok {
		t.Fatal("isolated command did not retain proposal context")
	}
	workspace := currentProposal.Workspace()
	if workspace.BaseRevision() != baseRevision || workspace.ProjectId() != session.Registration().ProjectId || workspace.ChangeId() != "change-proposal" {
		t.Fatalf("proposal linkage = %#v", workspace)
	}
	if relative, relativeError := filepath.Rel(repositoryRoot, workspace.Root()); relativeError != nil ||
		(relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))) {
		t.Fatalf("proposal workspace %q is inside canonical source", workspace.Root())
	}
	for _, expected := range []string{
		"Change: change-proposal (isolated)\n",
		"Workspace: " + workspace.Root() + "\n",
		"Scope: explicit\n",
		"Expected: internal/service/service.go\n",
		"Possible: internal/service/service_test.go\n",
		"Protected: go.mod\n",
	} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("explicit isolation output %q lacks %q", output.String(), expected)
		}
	}

	writeCommandFile(t, workspace.Root(), "internal/service/service.go", "package service\n\nconst Updated = true\n")
	output.Reset()
	if _, err := registry.Dispatch(session, "patch", &output); err != nil {
		t.Fatalf("/change patch error = %v", err)
	}
	classified, ok := session.CurrentProposal()
	if !ok || classified.Workspace().State() != "retained" {
		t.Fatalf("classified proposal = %#v, %t", classified, ok)
	}
	currentChange, ok := session.CurrentChange()
	if !ok || currentChange.State() != "isolated" {
		t.Fatalf("surface-valid patch prematurely advanced Change = %#v, %t", currentChange, ok)
	}
	if !strings.Contains(output.String(), "Surface valid: true\n") ||
		!strings.Contains(output.String(), "retained for later deterministic validation and human approval") {
		t.Fatalf("patch output = %q", output.String())
	}
	assertGovernedCommandRepositoryClean(t, repositoryRoot)

	events := readCommandAudit(t, dataDirectory)
	wantTail := []string{
		audit.EventProposalWorkspaceCreated,
		audit.EventChangeTransition,
		audit.EventArtifactCommitted,
		audit.EventPatchExtracted,
		audit.EventPatchSurfaceValidated,
		audit.EventArtifactCommitted,
	}
	for index, eventType := range wantTail {
		event := events[len(events)-len(wantTail)+index]
		if event.EventType != eventType || event.ChangeID != "change-proposal" || event.ProjectID != string(session.Registration().ProjectId) {
			t.Fatalf("M0.4 event %d = %#v", index, event)
		}
		encoded := fmt.Sprintf("%v", event.Metadata)
		if strings.Contains(encoded, workspace.Root()) || strings.Contains(encoded, "const Updated") {
			t.Fatalf("audit contains workspace path or patch body: %#v", event.Metadata)
		}
	}

	output.Reset()
	if _, err := registry.Dispatch(session, "discard", &output); err != nil {
		t.Fatalf("/change discard error = %v", err)
	}
	if _, ok := session.CurrentProposal(); ok {
		t.Fatal("discard retained proposal context")
	}
	if _, err := os.Stat(workspace.Root()); !os.IsNotExist(err) {
		t.Fatalf("discard did not remove workspace: %v", err)
	}
	currentChange, _ = session.CurrentChange()
	if currentChange.State() != "rejected" {
		t.Fatalf("discarded Change state = %q", currentChange.State())
	}
	if _, err := registry.Dispatch(session, "end", io.Discard); err != nil || session.CurrentMode().Identity != command.ModeRoot {
		t.Fatalf("leave change mode: mode=%q error=%v", session.CurrentMode().Identity, err)
	}
	assertGovernedCommandRepositoryClean(t, repositoryRoot)
}

func TestRegistryM04RejectsForbiddenMixedAndEmptyPatches(t *testing.T) {
	tests := []struct {
		name          string
		changeId      string
		mutate        func(*testing.T, string)
		wantError     string
		wantViolation string
	}{
		{
			name:     "protected",
			changeId: "change-protected-patch",
			mutate: func(t *testing.T, workspaceRoot string) {
				writeCommandFile(t, workspaceRoot, "go.mod", "module forbidden.invalid/change\n")
			},
			wantError:     "protected path",
			wantViolation: "- protected: go.mod",
		},
		{
			name:     "unexpected",
			changeId: "change-unexpected-patch",
			mutate: func(t *testing.T, workspaceRoot string) {
				writeCommandFile(t, workspaceRoot, "README.md", "# Unexpected\n")
			},
			wantError:     "unexpected path",
			wantViolation: "- unexpected: README.md",
		},
		{
			name:     "added unexpected file",
			changeId: "change-added-patch",
			mutate: func(t *testing.T, workspaceRoot string) {
				writeCommandFile(t, workspaceRoot, "new file.go", "package added\n")
			},
			wantError:     "unexpected path",
			wantViolation: "- unexpected: new file.go",
		},
		{
			name:     "mixed",
			changeId: "change-mixed-patch",
			mutate: func(t *testing.T, workspaceRoot string) {
				writeCommandFile(t, workspaceRoot, "internal/service/service.go", "package service\n\nconst AllowedPart = true\n")
				writeCommandFile(t, workspaceRoot, "go.mod", "module forbidden.invalid/mixed\n")
			},
			wantError:     "protected path",
			wantViolation: "Disposition: rejected; no partial acceptance",
		},
		{
			name:          "empty",
			changeId:      "change-empty-patch",
			mutate:        func(*testing.T, string) {},
			wantError:     "produced no patch",
			wantViolation: "Patch: empty",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repositoryRoot, dataDirectory, session, registry := prepareCommittedCommandTest(t)
			commandLine := fmt.Sprintf(
				`change isolate %s "test rejection" --expected internal/service/service.go --possible internal/service/service_test.go --protected go.mod`,
				test.changeId,
			)
			if _, err := registry.Dispatch(session, commandLine, io.Discard); err != nil {
				t.Fatalf("/change isolate error = %v", err)
			}
			currentProposal, _ := session.CurrentProposal()
			workspaceRoot := currentProposal.Workspace().Root()
			test.mutate(t, workspaceRoot)
			var output bytes.Buffer
			_, err := registry.Dispatch(session, "change patch", &output)
			if err == nil || !strings.Contains(err.Error(), test.wantError) {
				t.Fatalf("/change patch error = %v", err)
			}
			if !strings.Contains(output.String(), test.wantViolation) {
				t.Fatalf("patch output %q lacks %q", output.String(), test.wantViolation)
			}
			if _, ok := session.CurrentProposal(); ok {
				t.Fatal("rejected proposal retained session context")
			}
			if _, err := os.Stat(workspaceRoot); !os.IsNotExist(err) {
				t.Fatalf("rejected workspace remains: %v", err)
			}
			currentChange, _ := session.CurrentChange()
			if currentChange.State() != "rejected" {
				t.Fatalf("rejected Change state = %q", currentChange.State())
			}
			events := readCommandAudit(t, dataDirectory)
			if events[len(events)-3].EventType != audit.EventPatchRejected ||
				events[len(events)-2].EventType != audit.EventChangeTransition ||
				events[len(events)-1].EventType != audit.EventProposalWorkspaceDiscarded {
				t.Fatalf("rejection audit tail = %#v", events[len(events)-3:])
			}
			assertGovernedCommandRepositoryClean(t, repositoryRoot)
		})
	}
}

func TestRegistryM04AllowsPossibleAndDeletedFilePatches(t *testing.T) {
	tests := []struct {
		name            string
		changeId        string
		mutate          func(*testing.T, string)
		wantChangedPath string
	}{
		{
			name:     "possible file",
			changeId: "change-possible-patch",
			mutate: func(t *testing.T, workspaceRoot string) {
				writeCommandFile(t, workspaceRoot, "internal/service/service_test.go", "package service_test\n\nconst AdditionalCoverage = true\n")
			},
			wantChangedPath: "internal/service/service_test.go",
		},
		{
			name:     "deleted expected file",
			changeId: "change-deleted-patch",
			mutate: func(t *testing.T, workspaceRoot string) {
				if err := os.Remove(filepath.Join(workspaceRoot, "internal/service/service.go")); err != nil {
					t.Fatalf("delete expected proposal file: %v", err)
				}
			},
			wantChangedPath: "internal/service/service.go",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repositoryRoot, dataDirectory, session, registry := prepareCommittedCommandTest(t)
			commandLine := fmt.Sprintf(
				`change isolate %s "valid patch case" --expected internal/service/service.go --possible internal/service/service_test.go --protected go.mod`,
				test.changeId,
			)
			if _, err := registry.Dispatch(session, commandLine, io.Discard); err != nil {
				t.Fatalf("/change isolate error = %v", err)
			}
			currentProposal, _ := session.CurrentProposal()
			workspaceRoot := currentProposal.Workspace().Root()
			test.mutate(t, workspaceRoot)
			var output bytes.Buffer
			if _, err := registry.Dispatch(session, "change patch", &output); err != nil {
				t.Fatalf("/change patch error = %v", err)
			}
			if !strings.Contains(output.String(), "Surface valid: true\n") ||
				!strings.Contains(output.String(), "Changed paths: "+test.wantChangedPath+"\n") {
				t.Fatalf("patch output = %q", output.String())
			}
			classified, ok := session.CurrentProposal()
			if !ok || classified.Workspace().State() != "retained" ||
				classified.Workspace().ProjectId() != session.Registration().ProjectId ||
				classified.Workspace().ChangeId() != change.ChangeId(test.changeId) {
				t.Fatalf("classified linkage = %#v, %t", classified, ok)
			}
			events := readCommandAudit(t, dataDirectory)
			if events[len(events)-2].EventType != audit.EventPatchSurfaceValidated ||
				events[len(events)-1].EventType != audit.EventArtifactCommitted ||
				events[len(events)-1].ChangeID != test.changeId {
				t.Fatalf("surface-valid audit = %#v", events[len(events)-1])
			}
			assertGovernedCommandRepositoryClean(t, repositoryRoot)
			if _, err := registry.Dispatch(session, "change discard", io.Discard); err != nil {
				t.Fatalf("/change discard error = %v", err)
			}
			if _, err := os.Stat(workspaceRoot); !os.IsNotExist(err) {
				t.Fatalf("discard left workspace: %v", err)
			}
		})
	}
}

func TestRegistryM04DirtyAndExternallyDriftingCanonicalSourceFailClosed(t *testing.T) {
	t.Run("dirty before isolation", func(t *testing.T) {
		repositoryRoot, _, session, registry := prepareCommittedCommandTest(t)
		writeCommandFile(t, repositoryRoot, "README.md", "# Developer edit\n")
		_, err := registry.Dispatch(
			session,
			`change isolate change-dirty "dirty source" --expected internal/service/service.go`,
			io.Discard,
		)
		if err == nil || !strings.Contains(err.Error(), "is dirty") {
			t.Fatalf("dirty isolation error = %v", err)
		}
		if _, ok := session.CurrentProposal(); ok {
			t.Fatal("dirty source created proposal")
		}
		currentChange, ok := session.CurrentChange()
		if !ok || currentChange.State() != change.StateRejected {
			t.Fatalf("dirty proposal Change = %#v, %t", currentChange, ok)
		}
		contents, readError := os.ReadFile(filepath.Join(repositoryRoot, "README.md"))
		if readError != nil || string(contents) != "# Developer edit\n" {
			t.Fatalf("dirty source was repaired: %q, %v", contents, readError)
		}
	})

	t.Run("external drift during proposal", func(t *testing.T) {
		repositoryRoot, _, session, registry := prepareCommittedCommandTest(t)
		if _, err := registry.Dispatch(
			session,
			`change isolate change-drift "external drift" --expected internal/service/service.go`,
			io.Discard,
		); err != nil {
			t.Fatalf("/change isolate error = %v", err)
		}
		currentProposal, _ := session.CurrentProposal()
		workspaceRoot := currentProposal.Workspace().Root()
		writeCommandFile(t, workspaceRoot, "internal/service/service.go", "package service\n\nconst ProposalEdit = true\n")
		writeCommandFile(t, repositoryRoot, "README.md", "# External developer drift\n")
		_, err := registry.Dispatch(session, "change patch", io.Discard)
		if err == nil || !strings.Contains(err.Error(), "canonical source drift") {
			t.Fatalf("drifting patch error = %v", err)
		}
		contents, readError := os.ReadFile(filepath.Join(repositoryRoot, "README.md"))
		if readError != nil || string(contents) != "# External developer drift\n" {
			t.Fatalf("external drift was repaired: %q, %v", contents, readError)
		}
		if closeError := session.Close(); closeError == nil || !strings.Contains(closeError.Error(), "canonical source drift") {
			t.Fatalf("Close() drift error = %v", closeError)
		}
		if _, ok := session.CurrentProposal(); ok {
			t.Fatal("Close() did not clean drifted proposal workspace")
		}
		if _, statError := os.Stat(workspaceRoot); !os.IsNotExist(statError) {
			t.Fatalf("Close() left drifted workspace: %v", statError)
		}
	})
}

func TestRegistryDispatchErrorsExitAndCompletion(t *testing.T) {
	_, _, session, registry := prepareCommittedCommandTest(t)
	tests := []struct {
		line      string
		wantError string
	}{
		{line: "unknown", wantError: "unknown command"},
		{line: "analysis unknown", wantError: "unknown command"},
		{line: `change new change-one "unclosed`, wantError: "unclosed quote"},
		{line: `change new change-one trailing\`, wantError: "trailing escape"},
	}
	for _, test := range tests {
		if _, err := registry.Dispatch(session, test.line, io.Discard); err == nil || !strings.Contains(err.Error(), test.wantError) {
			t.Errorf("Dispatch(%q) error = %v, want %q", test.line, err, test.wantError)
		}
	}
	result, err := registry.Dispatch(session, "exit", io.Discard)
	if err != nil || !result.Exit {
		t.Fatalf("/exit result = %#v, error = %v", result, err)
	}

	assertSuggestions(t, registry.Complete(session, ""), []string{"status", "analysis", "change", "policy", "provider", "configure", "help", "?", "exit"})
	assertSuggestions(t, registry.Complete(session, "ana"), []string{"analysis"})
	assertSuggestions(t, registry.Complete(session, "analysis "), []string{"model", "impact", "report", "inspect"})
	assertSuggestions(t, registry.Complete(session, "analysis im"), []string{"impact"})
	assertSuggestions(t, registry.Complete(session, "change "), []string{"list", "show", "select", "artifacts", "history", "diagnose", "recover", "content", "new", "isolate", "implement", "patch", "verify", "approve", "reject", "apply", "close", "discard"})
	assertSuggestions(t, registry.Complete(session, "change i"), []string{"isolate", "implement"})
	if _, err := registry.Dispatch(session, "/status", io.Discard); err != nil {
		t.Fatalf("small leading-slash compatibility alias failed: %v", err)
	}
}

func TestRegistryContextModesNavigateWithoutDomainMutation(t *testing.T) {
	_, _, session, registry := prepareCommittedCommandTest(t)
	registration := session.Registration()
	if _, err := registry.Dispatch(session, `change new retained-change "retained state" planned`, io.Discard); err != nil {
		t.Fatalf("create retained Change: %v", err)
	}
	retainedChange, ok := session.CurrentChange()
	if !ok {
		t.Fatal("change command did not retain Change")
	}

	assertMetadataNames(t, registry.ContextCommands(session), []string{
		"status", "analysis", "change", "policy", "provider", "configure", "help", "?", "exit",
	})
	if _, err := registry.Dispatch(session, "analysis", io.Discard); err != nil {
		t.Fatalf("enter analysis: %v", err)
	}
	if session.CurrentMode().Identity != command.ModeAnalysis {
		t.Fatalf("current mode = %#v", session.CurrentMode())
	}
	assertMetadataNames(t, registry.ContextCommands(session), []string{"model", "impact", "report", "inspect", "help", "?", "end"})
	if _, err := registry.Dispatch(session, "change", io.Discard); err == nil ||
		!strings.Contains(err.Error(), "analysis mode") {
		t.Fatalf("unavailable command error = %v", err)
	}
	if _, err := registry.Dispatch(session, "exit", io.Discard); err == nil ||
		!strings.Contains(err.Error(), "analysis mode") {
		t.Fatalf("nested exit error = %v", err)
	}
	currentChange, currentChangeExists := session.CurrentChange()
	if session.Registration() != registration || !currentChangeExists || currentChange != retainedChange {
		t.Fatal("analysis mode navigation mutated retained Project or Change")
	}
	if _, err := registry.Dispatch(session, "end", io.Discard); err != nil {
		t.Fatalf("leave analysis: %v", err)
	}
	if session.CurrentMode().Identity != command.ModeRoot {
		t.Fatalf("mode after analysis end = %#v", session.CurrentMode())
	}
	if _, err := registry.Dispatch(session, "end", io.Discard); err == nil ||
		!strings.Contains(err.Error(), "root mode") {
		t.Fatalf("root end error = %v", err)
	}

	if _, err := registry.Dispatch(session, "configure", io.Discard); err != nil {
		t.Fatalf("enter configure: %v", err)
	}
	if _, err := registry.Dispatch(session, "project", io.Discard); err != nil {
		t.Fatalf("enter configure project: %v", err)
	}
	wantStack := []command.ModeIdentity{command.ModeRoot, command.ModeConfigure, command.ModeConfigureProject}
	stack := session.ModeStack()
	if len(stack) != len(wantStack) {
		t.Fatalf("mode stack = %#v", stack)
	}
	for index, want := range wantStack {
		if stack[index].Identity != want {
			t.Fatalf("mode stack %d = %#v, want %q", index, stack[index], want)
		}
	}
	assertMetadataNames(t, registry.ContextCommands(session), []string{"help", "?", "end"})
	if _, err := registry.Dispatch(session, "end", io.Discard); err != nil {
		t.Fatalf("leave configure project: %v", err)
	}
	if session.CurrentMode().Identity != command.ModeConfigure {
		t.Fatalf("first nested end reached %#v", session.CurrentMode())
	}
	if _, err := registry.Dispatch(session, "end", io.Discard); err != nil {
		t.Fatalf("leave configure: %v", err)
	}
	if session.CurrentMode().Identity != command.ModeRoot {
		t.Fatalf("second nested end reached %#v", session.CurrentMode())
	}
	currentChange, currentChangeExists = session.CurrentChange()
	if session.Registration() != registration || !currentChangeExists || currentChange != retainedChange {
		t.Fatal("nested mode navigation mutated retained Project or Change")
	}
}

func TestRegistryDirectAndContextualInvocationUseSameHandler(t *testing.T) {
	_, _, session, _ := prepareCommittedCommandTest(t)
	var invocations []command.Invocation
	handler := func(_ *command.Session, invocation command.Invocation, _ io.Writer) (command.Result, error) {
		invocations = append(invocations, invocation)
		return command.Result{}, nil
	}
	registry, err := command.NewRegistry([]command.Definition{
		{
			Name:        "analysis",
			Description: "Enter analysis",
			Usage:       "analysis [impact ...]",
			Mode:        command.ModeAnalysis,
			Children: []command.Definition{
				{Name: "impact", Description: "Analyze impact", Usage: "impact <value>", Handler: handler},
			},
		},
	})
	if err != nil {
		t.Fatalf("NewRegistry() error = %v", err)
	}
	if _, err := registry.Dispatch(session, "analysis impact same", io.Discard); err != nil {
		t.Fatalf("direct invocation: %v", err)
	}
	if _, err := registry.Dispatch(session, "analysis", io.Discard); err != nil {
		t.Fatalf("enter analysis: %v", err)
	}
	if _, err := registry.Dispatch(session, "impact same", io.Discard); err != nil {
		t.Fatalf("contextual invocation: %v", err)
	}
	if len(invocations) != 2 || invocations[0].CommandPath != "analysis impact" ||
		invocations[1].CommandPath != invocations[0].CommandPath ||
		fmt.Sprint(invocations[0].Arguments) != fmt.Sprint(invocations[1].Arguments) {
		t.Fatalf("resolved invocations = %#v", invocations)
	}
}

func TestRegistryContextualHelpIsDeterministicAndNonMutating(t *testing.T) {
	_, _, session, registry := prepareCommittedCommandTest(t)
	registration := session.Registration()
	root := registry.ContextualHelp(session, "")
	assertSuggestions(t, root, []string{"status", "analysis", "change", "policy", "provider", "configure", "help", "?", "exit"})
	assertSuggestions(t, registry.ContextualHelp(session, "a"), []string{"analysis"})
	assertSuggestions(t, registry.ContextualHelp(session, "analysis "), []string{"model", "impact", "report", "inspect"})
	assertSuggestions(t, registry.ContextualHelp(session, "analysis i"), []string{"impact", "inspect"})
	assertSuggestions(t, registry.ContextualHelp(session, "analysis impact --"), []string{
		"--expected", "--possible", "--protected", "--actual",
	})
	if got := registry.ContextualHelp(session, ""); fmt.Sprint(got) != fmt.Sprint(root) {
		t.Fatalf("contextual order changed: first %#v, second %#v", root, got)
	}

	if _, err := registry.Dispatch(session, "analysis", io.Discard); err != nil {
		t.Fatalf("enter analysis: %v", err)
	}
	beforeStack := session.ModeStack()
	assertSuggestions(t, registry.ContextualHelp(session, ""), []string{"model", "impact", "report", "inspect", "help", "?", "end"})
	var output bytes.Buffer
	if _, err := registry.Dispatch(session, "i?", &output); err != nil {
		t.Fatalf("contextual question mark: %v", err)
	}
	if !strings.Contains(output.String(), "impact") || !strings.Contains(output.String(), "Analyze") {
		t.Fatalf("contextual help output = %q", output.String())
	}
	if fmt.Sprint(session.ModeStack()) != fmt.Sprint(beforeStack) || session.Registration() != registration {
		t.Fatal("contextual help mutated mode or Project context")
	}

	executions := 0
	probeRegistry, err := command.NewRegistry([]command.Definition{{
		Name: "probe", Description: "Probe without execution", Usage: "probe", Handler: func(*command.Session, command.Invocation, io.Writer) (command.Result, error) {
			executions++
			return command.Result{}, nil
		},
	}})
	if err != nil {
		t.Fatalf("probe registry: %v", err)
	}
	if _, err := registry.Dispatch(session, "end", io.Discard); err != nil {
		t.Fatalf("leave analysis: %v", err)
	}
	if suggestions := probeRegistry.ContextualHelp(session, "pro"); len(suggestions) != 1 || suggestions[0].Text != "probe" {
		t.Fatalf("probe contextual help = %#v", suggestions)
	}
	if executions != 0 {
		t.Fatalf("help executed probe %d times", executions)
	}
}

func TestSessionPromptUsesPresentationOnlyRepositoryContext(t *testing.T) {
	repositoryRoot, _, session, registry := prepareCommittedCommandTest(t)
	displayName := strings.ToLower(filepath.Base(repositoryRoot))
	rootPrompt := session.Prompt()
	if !strings.Contains(rootPrompt, "praetor-"+displayName) || strings.Contains(rootPrompt, string(session.Registration().ProjectId)) {
		t.Fatalf("root prompt = %q", rootPrompt)
	}
	if _, err := registry.Dispatch(session, "analysis", io.Discard); err != nil {
		t.Fatalf("enter analysis: %v", err)
	}
	if prompt := session.Prompt(); !strings.Contains(prompt, "praetor-"+displayName+"-analysis") {
		t.Fatalf("analysis prompt = %q", prompt)
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
		"cmd/app/main.go":                   "package main\n",
		"internal/service/service.go":       "package service\n",
		"internal/service/service_test.go":  "package service_test\n",
		"go.mod":                            "module example.invalid/fixture\n\ngo 1.25.1\n",
		"README.md":                         "# Fixture\n",
		"engineering/policies/praetor.yaml": "schema_version: 1\nbundle:\n  id: fixture-policy\n  version: \"1.0\"\n  policies:\n    - id: test-required\n      version: \"1.0\"\n      family: testing\n      description: Require passing tests.\n      severity: HIGH\n      outcome: APPROVAL\n      required_evidence: test\n      non_overridable: true\n      exception_candidate_allowed: true\n    - id: patch-integrity\n      version: \"1.0\"\n      family: change-surface\n      description: Require patch integrity.\n      severity: CRITICAL\n      outcome: AUTO\n      required_evidence: patch-integrity\n      non_overridable: true\n      exception_candidate_allowed: false\n",
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
	t.Setenv("XDG_CACHE_HOME", filepath.Join(t.TempDir(), "cache"))
	t.Setenv("XDG_STATE_HOME", filepath.Join(t.TempDir(), "state"))
	t.Setenv("XDG_CONFIG_HOME", filepath.Join(t.TempDir(), "config"))
	container := composition.New()
	session, err := container.NewInteractiveSession(".")
	if err != nil {
		t.Fatalf("NewInteractiveSession() error = %v", err)
	}
	t.Cleanup(func() {
		if err := session.Close(); err != nil {
			t.Errorf("Session.Close() error = %v", err)
		}
	})
	return repositoryRoot, filepath.Join(xdgDataHome, "praetor"), session, newTestRegistry(t)
}

func runCommandGit(t *testing.T, repositoryRoot string, arguments ...string) {
	t.Helper()
	_ = runCommandGitOutput(t, repositoryRoot, arguments...)
}

func runCommandGitOutput(t *testing.T, repositoryRoot string, arguments ...string) []byte {
	t.Helper()
	commandArguments := append([]string{"-C", repositoryRoot}, arguments...)
	process := exec.Command("git", commandArguments...)
	output, err := process.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", arguments, err, output)
	}
	return output
}

func writeCommandFile(t *testing.T, root string, relativePath string, contents string) {
	t.Helper()
	absolutePath := filepath.Join(root, filepath.FromSlash(relativePath))
	if err := os.MkdirAll(filepath.Dir(absolutePath), 0o755); err != nil {
		t.Fatalf("create fixture directory: %v", err)
	}
	if err := os.WriteFile(absolutePath, []byte(contents), 0o600); err != nil {
		t.Fatalf("write fixture %q: %v", relativePath, err)
	}
}

func readCommandAudit(t *testing.T, dataDirectory string) []audit.Event {
	t.Helper()
	events, err := sqliteadapter.ReadAuditEvents(dataDirectory)
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

func assertMetadataNames(t *testing.T, metadata []command.Metadata, want []string) {
	t.Helper()
	if len(metadata) != len(want) {
		t.Fatalf("metadata = %#v, want names %#v", metadata, want)
	}
	for index, item := range metadata {
		if item.Name != want[index] || strings.TrimSpace(item.Description) == "" {
			t.Fatalf("metadata %d = %#v, want %q with description", index, item, want[index])
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
	for _, suggestion := range registry.Complete(&command.Session{}, "ana") {
		fmt.Printf("%s — %s\n", suggestion.Text, suggestion.Description)
	}
	// Output:
	// analysis — Enter source and Change analysis mode
}
