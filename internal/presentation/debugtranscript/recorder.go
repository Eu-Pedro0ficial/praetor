// Package debugtranscript records bounded, sanitized developer diagnostics for
// one interactive shell session. It is imported only by debug-enabled builds.
package debugtranscript

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"runtime/debug"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/Eu-Pedro0ficial/praetor/internal/project"
)

const (
	maximumCommandBytes = 16 * 1024
	maximumOutputBytes  = 1024 * 1024
	maximumSessionBytes = 8 * 1024 * 1024
)

var secretPatterns = []struct {
	expression  *regexp.Regexp
	replacement string
}{
	{regexp.MustCompile("(?i)(authorization\\s*:\\s*)(?:(?:bearer|basic)\\s+)?[^\\s]+"), "${1}[REDACTED]"},
	{regexp.MustCompile("(?i)\\bbearer\\s+[A-Za-z0-9._~+/=-]+"), "Bearer [REDACTED]"},
	{regexp.MustCompile("(?i)\\b([a-z0-9_-]*(?:api[_-]?key|access[_-]?token|password|secret|token)[a-z0-9_-]*)\\s*[:=]\\s*(?:\"[^\"\\n]*\"|'[^'\\n]*'|[^\\s,;]+)"), "${1}=[REDACTED]"},
	{regexp.MustCompile("\\bsk-[A-Za-z0-9_-]{8,}"), "[REDACTED_OPENAI_KEY]"},
}

// Config supplies session identity and deterministic test seams.
type Config struct {
	ProjectID      string
	StateDirectory string
	Build          string
	Clock          func() time.Time
}

// Recorder implements shell.InteractionRecorder.
type Recorder struct {
	writer       io.WriteCloser
	path         string
	clock        func() time.Time
	startedAt    time.Time
	output       boundedBuffer
	active       bool
	closed       bool
	writtenBytes int
}

// New creates a private transcript beneath XDG STATE and writes its header.
// Creation failure is returned so explicit --debug startup fails deterministically.
func New(config Config) (*Recorder, error) {
	projectID := strings.TrimSpace(config.ProjectID)
	if projectID == "" {
		return nil, fmt.Errorf("debug transcript ProjectId is required")
	}
	stateDirectory := strings.TrimSpace(config.StateDirectory)
	if stateDirectory == "" {
		resolved, err := project.ResolveStateDir()
		if err != nil {
			return nil, fmt.Errorf("resolve debug transcript state directory: %w", err)
		}
		stateDirectory = resolved
	}
	directory := filepath.Join(stateDirectory, "debug-transcripts")
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, fmt.Errorf("create debug transcript directory %q: %w", directory, err)
	}
	if err := os.Chmod(directory, 0o700); err != nil {
		return nil, fmt.Errorf("protect debug transcript directory %q: %w", directory, err)
	}

	clock := config.Clock
	if clock == nil {
		clock = time.Now
	}
	startedAt := clock().UTC()
	base := fmt.Sprintf(
		"praetor-%s-%s.txt",
		startedAt.Format("20060102-150405.000000000"),
		safeFilenameComponent(projectID),
	)
	var file *os.File
	var path string
	for collision := 0; collision < 100; collision++ {
		name := base
		if collision > 0 {
			name = strings.TrimSuffix(base, ".txt") + fmt.Sprintf("-%02d.txt", collision)
		}
		path = filepath.Join(directory, name)
		opened, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if errors.Is(err, os.ErrExist) {
			continue
		}
		if err != nil {
			return nil, fmt.Errorf("create debug transcript %q: %w", path, err)
		}
		file = opened
		break
	}
	if file == nil {
		return nil, fmt.Errorf("create collision-safe debug transcript in %q", directory)
	}

	recorder := &Recorder{
		writer:    file,
		path:      path,
		clock:     clock,
		startedAt: startedAt,
	}
	build := strings.TrimSpace(config.Build)
	if build == "" {
		build = buildDescription()
	}
	header := fmt.Sprintf(
		"PRAETOR DEVELOPMENT DEBUG TRANSCRIPT\nstarted_at: %s\nproject_id: %s\nbuild: %s\nsecurity: bounded logical shell output; best-effort common-secret redaction\n\n",
		startedAt.Format(time.RFC3339Nano),
		sanitize(projectID, 256),
		sanitize(build, 1024),
	)
	if err := recorder.write(header); err != nil {
		_ = file.Close()
		return nil, fmt.Errorf("write debug transcript header: %w", err)
	}
	return recorder, nil
}

// Path is the absolute transcript path created for this session.
func (recorder *Recorder) Path() string {
	if recorder == nil {
		return ""
	}
	return recorder.path
}

// BeginCommand writes the command marker and returns a bounded logical-output buffer.
func (recorder *Recorder) BeginCommand(commandLine string) (io.Writer, error) {
	if recorder == nil || recorder.writer == nil || recorder.closed {
		return nil, io.ErrClosedPipe
	}
	if recorder.active {
		return nil, fmt.Errorf("previous debug transcript command is still active")
	}
	recorder.output.reset(maximumOutputBytes)
	recorder.active = true
	if err := recorder.write("> " + sanitize(commandLine, maximumCommandBytes) + "\n"); err != nil {
		recorder.active = false
		return nil, err
	}
	return &recorder.output, nil
}

// EndCommand sanitizes and persists the accumulated logical command output.
func (recorder *Recorder) EndCommand() error {
	if recorder == nil || !recorder.active {
		return fmt.Errorf("no active debug transcript command")
	}
	recorder.active = false
	value := sanitize(recorder.output.String(), maximumOutputBytes)
	if recorder.output.truncated {
		value += "\n[transcript output truncated at 1048576 bytes]"
	}
	if value != "" && !strings.HasSuffix(value, "\n") {
		value += "\n"
	}
	if err := recorder.write(value + "\n"); err != nil {
		return err
	}
	return nil
}

// Close records the shell outcome and closes the transcript.
func (recorder *Recorder) Close(runError error) error {
	if recorder == nil || recorder.closed {
		return nil
	}
	var result error
	if recorder.active {
		result = errors.Join(result, recorder.EndCommand())
	}
	outcome := "success"
	if runError != nil {
		outcome = "error: " + sanitize(runError.Error(), 4096)
	}
	endedAt := recorder.clock().UTC()
	result = errors.Join(result, recorder.write(fmt.Sprintf(
		"session_end: %s\nduration: %s\nresult: %s\n",
		endedAt.Format(time.RFC3339Nano),
		endedAt.Sub(recorder.startedAt).Round(time.Millisecond),
		outcome,
	)))
	recorder.closed = true
	result = errors.Join(result, recorder.writer.Close())
	return result
}

func (recorder *Recorder) write(value string) error {
	if recorder.writtenBytes+len(value) > maximumSessionBytes {
		return fmt.Errorf("debug transcript exceeds %d-byte session limit", maximumSessionBytes)
	}
	written, err := io.WriteString(recorder.writer, value)
	recorder.writtenBytes += written
	if err != nil {
		return err
	}
	if written != len(value) {
		return io.ErrShortWrite
	}
	return nil
}

type boundedBuffer struct {
	buffer    bytes.Buffer
	limit     int
	truncated bool
}

func (buffer *boundedBuffer) reset(limit int) {
	buffer.buffer.Reset()
	buffer.limit = limit
	buffer.truncated = false
}

func (buffer *boundedBuffer) Write(value []byte) (int, error) {
	length := len(value)
	remaining := buffer.limit - buffer.buffer.Len()
	if remaining > 0 {
		if remaining > length {
			remaining = length
		}
		_, _ = buffer.buffer.Write(value[:remaining])
	}
	if remaining < length {
		buffer.truncated = true
	}
	return length, nil
}

func (buffer *boundedBuffer) String() string {
	return buffer.buffer.String()
}

func sanitize(value string, maximumBytes int) string {
	value = strings.ToValidUTF8(value, "�")
	value = strings.Map(func(character rune) rune {
		switch character {
		case '\n':
			return '\n'
		case '\t', '\r':
			return ' '
		default:
			if unicode.IsControl(character) {
				return '�'
			}
			return character
		}
	}, value)
	for _, pattern := range secretPatterns {
		value = pattern.expression.ReplaceAllString(value, pattern.replacement)
	}
	return truncateUTF8(value, maximumBytes)
}

func truncateUTF8(value string, maximumBytes int) string {
	if maximumBytes <= 0 {
		return ""
	}
	if len(value) <= maximumBytes {
		return value
	}
	value = value[:maximumBytes]
	for !utf8.ValidString(value) {
		value = value[:len(value)-1]
	}
	return value + "\n[truncated]"
}

func safeFilenameComponent(value string) string {
	var result strings.Builder
	for _, character := range value {
		if unicode.IsLetter(character) || unicode.IsDigit(character) || character == '-' || character == '_' {
			result.WriteRune(character)
		}
	}
	if result.Len() == 0 {
		return "unknown-project"
	}
	return result.String()
}

func buildDescription() string {
	info, ok := debug.ReadBuildInfo()
	if !ok {
		return "development debug build"
	}
	version := strings.TrimSpace(info.Main.Version)
	if version == "" {
		version = "(devel)"
	}
	return info.Main.Path + "@" + version + " debug-tag=praetor_debug"
}
