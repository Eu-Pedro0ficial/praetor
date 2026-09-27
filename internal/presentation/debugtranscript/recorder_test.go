package debugtranscript

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const testProjectID = "01890c29-7a78-7abc-8def-0123456789ab"

func TestRecorderCreatesPrivateCollisionSafeTranscriptWithSessionMetadata(t *testing.T) {
	stateDirectory := t.TempDir()
	startedAt := time.Date(2026, time.September, 27, 12, 34, 56, 123456789, time.UTC)
	clockValues := []time.Time{startedAt, startedAt.Add(2 * time.Second)}
	clockIndex := 0
	clock := func() time.Time {
		value := clockValues[clockIndex]
		if clockIndex < len(clockValues)-1 {
			clockIndex++
		}
		return value
	}

	first, err := New(Config{
		ProjectID:      testProjectID,
		StateDirectory: stateDirectory,
		Build:          "test-build",
		Clock:          clock,
	})
	if err != nil {
		t.Fatalf("New() error = %v", err)
	}
	if !strings.Contains(filepath.Base(first.Path()), "20260927-123456.123456789-"+testProjectID) {
		t.Fatalf("transcript filename = %q", first.Path())
	}
	info, err := os.Stat(first.Path())
	if err != nil {
		t.Fatal(err)
	}
	if got := info.Mode().Perm(); got != 0o600 {
		t.Fatalf("transcript mode = %o, want 600", got)
	}
	if err := first.Close(nil); err != nil {
		t.Fatalf("Close() error = %v", err)
	}

	second, err := New(Config{
		ProjectID:      testProjectID,
		StateDirectory: stateDirectory,
		Build:          "test-build",
		Clock:          func() time.Time { return startedAt },
	})
	if err != nil {
		t.Fatalf("second New() error = %v", err)
	}
	if first.Path() == second.Path() || !strings.HasSuffix(second.Path(), "-01.txt") {
		t.Fatalf("collision path = %q after %q", second.Path(), first.Path())
	}
	if err := second.Close(nil); err != nil {
		t.Fatal(err)
	}

	payload, err := os.ReadFile(first.Path())
	if err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{
		"PRAETOR DEVELOPMENT DEBUG TRANSCRIPT",
		"started_at: 2026-09-27T12:34:56.123456789Z",
		"project_id: " + testProjectID,
		"build: test-build",
		"session_end:",
		"duration: 2s",
		"result: success",
	} {
		if !strings.Contains(string(payload), expected) {
			t.Fatalf("transcript lacks %q:\n%s", expected, payload)
		}
	}
}

func TestRecorderOrdersMultipleCommandsOutputsAndErrors(t *testing.T) {
	now := time.Date(2026, time.January, 2, 3, 4, 5, 0, time.UTC)
	recorder, err := New(Config{
		ProjectID:      testProjectID,
		StateDirectory: t.TempDir(),
		Build:          "test",
		Clock:          func() time.Time { return now },
	})
	if err != nil {
		t.Fatal(err)
	}
	path := recorder.Path()

	first, err := recorder.BeginCommand("status")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(first, "Project ready\n")
	if err := recorder.EndCommand(); err != nil {
		t.Fatal(err)
	}
	second, err := recorder.BeginCommand("unknown")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(second, "praetor: unknown command \"unknown\"\n")
	if err := recorder.EndCommand(); err != nil {
		t.Fatal(err)
	}
	runFailure := errors.New("terminal read failed")
	if err := recorder.Close(runFailure); err != nil {
		t.Fatal(err)
	}

	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(payload)
	ordered := []string{"> status", "Project ready", "> unknown", "praetor: unknown command", "result: error: terminal read failed"}
	position := -1
	for _, value := range ordered {
		next := strings.Index(text, value)
		if next <= position {
			t.Fatalf("transcript order for %q after byte %d:\n%s", value, position, text)
		}
		position = next
	}
}

func TestRecorderRedactsCommonSecretsAndNeutralizesTerminalControls(t *testing.T) {
	recorder, err := New(Config{
		ProjectID:      testProjectID,
		StateDirectory: t.TempDir(),
		Build:          "test",
		Clock:          time.Now,
	})
	if err != nil {
		t.Fatal(err)
	}
	path := recorder.Path()
	output, err := recorder.BeginCommand("configure Authorization: Bearer abc123 api_key=\"key-value\" OPENAI_API_KEY=openai-value GITHUB_TOKEN=github-value password=hunter2 sk-1234567890")
	if err != nil {
		t.Fatal(err)
	}
	_, _ = io.WriteString(output, "Authorization: Basic basic-value access_token=visible secret='do not retain'\x1b[2J\x00done\n")
	if err := recorder.EndCommand(); err != nil {
		t.Fatal(err)
	}
	if err := recorder.Close(nil); err != nil {
		t.Fatal(err)
	}
	payload, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := string(payload)
	for _, forbidden := range []string{"abc123", "key-value", "openai-value", "github-value", "basic-value", "hunter2", "sk-1234567890", "visible", "do not retain", "\x1b", "\x00"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("transcript retained %q:\n%s", forbidden, text)
		}
	}
	for _, expected := range []string{"Authorization: [REDACTED]", "api_key=[REDACTED]", "OPENAI_API_KEY=[REDACTED]", "GITHUB_TOKEN=[REDACTED]", "password=[REDACTED]", "[REDACTED_OPENAI_KEY]", "access_token=[REDACTED]", "secret=[REDACTED]"} {
		if !strings.Contains(text, expected) {
			t.Fatalf("transcript lacks redaction %q:\n%s", expected, text)
		}
	}
}

func TestRecorderCreateAndWriteFailuresAreExplicit(t *testing.T) {
	stateFile := filepath.Join(t.TempDir(), "not-a-directory")
	if err := os.WriteFile(stateFile, []byte("occupied"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := New(Config{ProjectID: testProjectID, StateDirectory: stateFile}); err == nil || !strings.Contains(err.Error(), "create debug transcript directory") {
		t.Fatalf("New() error = %v", err)
	}

	writeFailure := errors.New("disk unavailable")
	writer := &failingWriteCloser{failAtCall: 2, failure: writeFailure}
	recorder := &Recorder{
		writer:    writer,
		clock:     time.Now,
		startedAt: time.Now(),
	}
	output, err := recorder.BeginCommand("status")
	if err != nil {
		t.Fatalf("BeginCommand() error = %v", err)
	}
	_, _ = io.WriteString(output, "result")
	if err := recorder.EndCommand(); !errors.Is(err, writeFailure) {
		t.Fatalf("EndCommand() error = %v", err)
	}
}

type failingWriteCloser struct {
	calls      int
	failAtCall int
	failure    error
}

func (writer *failingWriteCloser) Write(value []byte) (int, error) {
	writer.calls++
	if writer.calls >= writer.failAtCall {
		return 0, writer.failure
	}
	return len(value), nil
}

func (*failingWriteCloser) Close() error { return nil }
