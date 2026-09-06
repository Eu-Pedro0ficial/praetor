package command_test

import (
	"bytes"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Eu-Pedro0ficial/praetor/internal/command"
	"github.com/Eu-Pedro0ficial/praetor/internal/composition"
	"github.com/Eu-Pedro0ficial/praetor/internal/presentation/preferences"
)

func TestLayoutCommandsDirectContextualPersistenceAndReset(t *testing.T) {
	repositoryRoot, _, session, registry := prepareCommittedCommandTest(t)
	beforeStatus := session.StatusSnapshot()
	beforeRepository, err := os.ReadDir(repositoryRoot)
	if err != nil {
		t.Fatal(err)
	}
	commands := []string{
		"configure layout sidebar show off",
		"configure layout sidebar identity off",
		"configure layout sidebar context off",
		"configure layout sidebar provider off",
		"configure layout sidebar status off",
		"configure layout color accent green",
		"configure layout color border cyan",
		"configure layout color background black",
		"configure layout color text white",
	}
	for _, line := range commands {
		if _, err := registry.Dispatch(session, line, io.Discard); err != nil {
			t.Fatalf("Dispatch(%q): %v", line, err)
		}
	}
	current := session.LayoutPreferences()
	if current.Sidebar.Visible || current.Sidebar.Identity || current.Sidebar.Context ||
		current.Sidebar.Provider || current.Sidebar.Status || current.Colors.Accent != preferences.ColorGreen ||
		current.Colors.Border != preferences.ColorCyan || current.Colors.Background != preferences.ColorBlack ||
		current.Colors.Text != preferences.ColorWhite {
		t.Fatalf("configured layout = %#v", current)
	}

	var output bytes.Buffer
	if _, err := registry.Dispatch(session, "configure layout show", &output); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"Sidebar       off", "Identity    off", "Accent      green", "Background  black"} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("layout show %q lacks %q", output.String(), expected)
		}
	}
	if _, err := registry.Dispatch(session, "configure", io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Dispatch(session, "layout", io.Discard); err != nil {
		t.Fatal(err)
	}
	if session.CurrentMode().Identity != command.ModeConfigureLayout {
		t.Fatalf("mode = %q", session.CurrentMode().Identity)
	}
	if _, err := registry.Dispatch(session, "sidebar", io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Dispatch(session, "show on", io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Dispatch(session, "end", io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Dispatch(session, "reset", io.Discard); err != nil {
		t.Fatal(err)
	}
	if session.LayoutPreferences() != preferences.Defaults() {
		t.Fatalf("reset layout = %#v", session.LayoutPreferences())
	}
	if !reflect.DeepEqual(beforeStatus, session.StatusSnapshot()) {
		t.Fatal("layout commands changed runtime status")
	}
	afterRepository, err := os.ReadDir(repositoryRoot)
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(entryNames(beforeRepository), entryNames(afterRepository)) {
		t.Fatalf("layout commands changed governed repository: %v -> %v", entryNames(beforeRepository), entryNames(afterRepository))
	}
}

func TestLayoutHelpCompletionQuestionMarkAndInvalidValuesDoNotMutate(t *testing.T) {
	_, _, session, registry := prepareCommittedCommandTest(t)
	before := session.LayoutPreferences()
	var output bytes.Buffer
	for _, line := range []string{"help configure layout", "configure layout ?", "configure layout sidebar ?", "configure layout color ?"} {
		output.Reset()
		if _, err := registry.Dispatch(session, line, &output); err != nil {
			t.Fatalf("Dispatch(%q): %v", line, err)
		}
		if output.Len() == 0 {
			t.Fatalf("Dispatch(%q) produced no discovery output", line)
		}
	}
	for _, input := range []string{"configure layout ", "configure layout sidebar ", "configure layout sidebar show ", "configure layout color accent "} {
		if len(registry.Complete(session, input)) == 0 {
			t.Fatalf("Complete(%q) produced no candidates", input)
		}
	}
	for _, line := range []string{
		"configure layout sidebar show maybe",
		"configure layout color accent \x1b[31m",
		"configure layout color background terminal-injection",
	} {
		if _, err := registry.Dispatch(session, line, io.Discard); err == nil {
			t.Fatalf("Dispatch(%q) accepted invalid preference", line)
		}
	}
	if session.LayoutPreferences() != before {
		t.Fatal("help, completion, ?, or invalid input changed preferences")
	}
}

func TestStatusCommandAndSidebarShareSessionSnapshot(t *testing.T) {
	_, _, session, registry := prepareCommittedCommandTest(t)
	snapshot := session.StatusSnapshot()
	var output bytes.Buffer
	if _, err := registry.Dispatch(session, "status", &output); err != nil {
		t.Fatal(err)
	}
	for _, value := range []string{
		snapshot.Project, snapshot.Repository, snapshot.Change, snapshot.Proposal,
		snapshot.ProviderAdapter, snapshot.ProviderVendor, snapshot.ProviderModel,
		snapshot.Verification, snapshot.HumanDecision,
	} {
		if !strings.Contains(output.String(), value) {
			t.Fatalf("status output lacks snapshot value %q: %q", value, output.String())
		}
	}
}

func TestPresentationPreferencesReloadThroughNewSession(t *testing.T) {
	repositoryRoot, _, first, registry := prepareCommittedCommandTest(t)
	if _, err := registry.Dispatch(first, "configure layout sidebar show off", io.Discard); err != nil {
		t.Fatal(err)
	}
	container := composition.New()
	second, err := container.NewInteractiveSession(repositoryRoot)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = second.Close() })
	if second.LayoutPreferences().Sidebar.Visible {
		t.Fatal("new session did not reload persisted sidebar preference")
	}
	configRoot := os.Getenv("XDG_CONFIG_HOME")
	if configRoot == "" || !strings.HasPrefix(filepath.Clean(preferencesPathForTest(t)), filepath.Clean(configRoot)) {
		t.Fatal("presentation preference path is not under XDG_CONFIG_HOME")
	}
}

func entryNames(entries []os.DirEntry) []string {
	values := make([]string, len(entries))
	for index, entry := range entries {
		values[index] = entry.Name()
	}
	return values
}

func preferencesPathForTest(t *testing.T) string {
	t.Helper()
	directory, err := preferences.ResolveConfigDir()
	if err != nil {
		t.Fatal(err)
	}
	return preferences.Path(directory)
}
