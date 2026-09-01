package shell

import (
	"bytes"
	"errors"
	"io"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Eu-Pedro0ficial/praetor/internal/audit"
	"github.com/Eu-Pedro0ficial/praetor/internal/command"
	"github.com/Eu-Pedro0ficial/praetor/internal/composition"
	"github.com/reeflective/readline"
)

type scriptedRead struct {
	line string
	err  error
}

type scriptedEditor struct {
	reads []scriptedRead
	index int
}

func (editor *scriptedEditor) Readline() (string, error) {
	if editor.index >= len(editor.reads) {
		return "", io.EOF
	}
	read := editor.reads[editor.index]
	editor.index++
	return read.line, read.err
}

func TestAdapterRetainsProjectContextAndContinuesAfterCommandErrors(t *testing.T) {
	repositoryRoot, dataDirectory, session, registry := prepareShellTest(t)
	editor := &scriptedEditor{reads: []scriptedRead{
		{line: "/unknown"},
		{line: `/change new change-shell "shell lifecycle" planned`},
		{line: "/status extra"},
		{line: "/status"},
		{line: "/exit"},
	}}
	var output bytes.Buffer
	adapter, err := newWithEditor(registry, session, &output, editor)
	if err != nil {
		t.Fatalf("newWithEditor() error = %v", err)
	}
	if err := adapter.Run(); err != nil {
		t.Fatalf("Run() error = %v", err)
	}

	registration := session.Registration()
	for _, expected := range []string{
		"Praetor\n",
		"Project: " + string(registration.ProjectId) + "\n",
		"Repository: " + repositoryRoot + "\n",
		"Change: none\n",
		"praetor: unknown command",
		"Change ID: change-shell\n",
		"praetor: usage: /status",
		"Current change: change-shell (planned)\n",
	} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("shell output %q lacks %q", output.String(), expected)
		}
	}
	if editor.index != len(editor.reads) {
		t.Fatalf("shell stopped after %d of %d commands", editor.index, len(editor.reads))
	}
	events, err := audit.Read(dataDirectory)
	if err != nil {
		t.Fatalf("audit.Read() error = %v", err)
	}
	if len(events) != 5 {
		t.Fatalf("shell audit contains %d events, want 3 startup + 2 Change", len(events))
	}
	if _, err := filepath.Abs(repositoryRoot); err != nil {
		t.Fatalf("repository root is not operational path metadata: %v", err)
	}
}

func TestAdapterHandlesInterruptAndEOFWithoutDispatchFailure(t *testing.T) {
	_, _, session, registry := prepareShellTest(t)
	tests := []struct {
		name  string
		reads []scriptedRead
	}{
		{name: "interrupt", reads: []scriptedRead{{err: errInterrupted}, {line: "/exit"}}},
		{name: "EOF", reads: []scriptedRead{{err: io.EOF}}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			adapter, err := newWithEditor(registry, session, io.Discard, &scriptedEditor{reads: test.reads})
			if err != nil {
				t.Fatalf("newWithEditor() error = %v", err)
			}
			if err := adapter.Run(); err != nil {
				t.Fatalf("Run() error = %v", err)
			}
		})
	}
}

func TestAdapterPropagatesUnexpectedEditorFailure(t *testing.T) {
	_, _, session, registry := prepareShellTest(t)
	editorFailure := errors.New("terminal unavailable")
	adapter, err := newWithEditor(
		registry,
		session,
		io.Discard,
		&scriptedEditor{reads: []scriptedRead{{err: editorFailure}}},
	)
	if err != nil {
		t.Fatalf("newWithEditor() error = %v", err)
	}
	if err := adapter.Run(); !errors.Is(err, editorFailure) {
		t.Fatalf("Run() error = %v", err)
	}
}

func TestReadlineEditorEnablesLiveRegistryCompletion(t *testing.T) {
	registry, err := command.DefaultRegistry()
	if err != nil {
		t.Fatalf("DefaultRegistry() error = %v", err)
	}
	editor := newReadlineEditor(registry)
	if editor.shell == nil || editor.shell.Completer == nil || editor.shell.History == nil {
		t.Fatal("readline editor lacks registry completion or in-memory history")
	}
	if !editor.shell.Config.GetBool("autocomplete") {
		t.Fatal("readline live autocomplete is disabled")
	}
	tests := []struct {
		input      string
		wantValues []string
	}{
		{input: "/", wantValues: []string{"/status", "/change", "/analysis", "/help", "/exit"}},
		{input: "/ana", wantValues: []string{"/analysis"}},
		{input: "/analysis ", wantValues: []string{"impact"}},
	}
	for _, test := range tests {
		completions := editor.shell.Completer([]rune(test.input), len([]rune(test.input)))
		var candidates []readline.Completion
		completions.EachValue(func(candidate readline.Completion) readline.Completion {
			candidates = append(candidates, candidate)
			return candidate
		})
		if len(candidates) != len(test.wantValues) {
			t.Fatalf("completion %q candidates = %#v", test.input, candidates)
		}
		for index, candidate := range candidates {
			if candidate.Value != test.wantValues[index] || strings.TrimSpace(candidate.Description) == "" {
				t.Fatalf("completion %q candidate %d = %#v", test.input, index, candidate)
			}
		}
	}
}

func prepareShellTest(t *testing.T) (string, string, *command.Session, command.Registry) {
	t.Helper()
	repositoryRoot := t.TempDir()
	process := exec.Command("git", "-C", repositoryRoot, "init", "--quiet")
	if output, err := process.CombinedOutput(); err != nil {
		t.Fatalf("git init: %v: %s", err, output)
	}
	t.Chdir(repositoryRoot)
	xdgDataHome := filepath.Join(t.TempDir(), "xdg")
	t.Setenv("XDG_DATA_HOME", xdgDataHome)
	container := composition.New()
	session, err := container.NewInteractiveSession(".")
	if err != nil {
		t.Fatalf("NewInteractiveSession() error = %v", err)
	}
	registry, err := command.DefaultRegistry()
	if err != nil {
		t.Fatalf("DefaultRegistry() error = %v", err)
	}
	return repositoryRoot, filepath.Join(xdgDataHome, "praetor"), session, registry
}
