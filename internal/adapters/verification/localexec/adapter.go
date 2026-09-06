// Package localexec adapts direct operating-system processes to Praetor's
// deterministic Verification Port. It never invokes an intermediary shell.
package localexec

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"github.com/Eu-Pedro0ficial/praetor/internal/verification"
)

const defaultMaximumOutputBytes = 64 << 10

// Adapter executes bounded direct child processes with an explicit minimal
// environment. It is not hostile-code containment.
type Adapter struct {
	maximumOutputBytes    int
	executableDirectories []string
}

// NewDefault constructs the Core V0 local verification adapter.
func NewDefault() *Adapter {
	return newAdapter(defaultMaximumOutputBytes)
}

// New constructs an adapter with a bounded per-stream capture limit.
func New(maximumOutputBytes int) (*Adapter, error) {
	if maximumOutputBytes <= 0 || maximumOutputBytes > 1<<20 {
		return nil, fmt.Errorf("verification output limit must be between zero and 1 MiB")
	}
	return newAdapter(maximumOutputBytes), nil
}

func newAdapter(maximumOutputBytes int) *Adapter {
	return &Adapter{
		maximumOutputBytes:    maximumOutputBytes,
		executableDirectories: sanitizedExecutableDirectories(os.Getenv("PATH")),
	}
}

func (adapter *Adapter) MaximumOutputBytes() int {
	if adapter == nil {
		return 0
	}
	return adapter.maximumOutputBytes
}

// Resolve performs explicit resolution against the absolute, existing,
// non-world-writable PATH directories captured when the adapter was created.
func (adapter *Adapter) Resolve(executable string) (string, error) {
	if adapter == nil {
		return "", fmt.Errorf("local verification adapter is required")
	}
	if executable == "" || strings.TrimSpace(executable) != executable ||
		filepath.Base(executable) != executable || strings.ContainsAny(executable, `/\`) {
		return "", fmt.Errorf("verification executable must be a simple program name")
	}
	for _, directory := range adapter.executableDirectories {
		candidate := filepath.Join(directory, executable)
		resolved, err := filepath.EvalSymlinks(candidate)
		if err != nil {
			continue
		}
		info, err := os.Stat(resolved)
		if err != nil || !info.Mode().IsRegular() || info.Mode().Perm()&0o111 == 0 ||
			info.Mode().Perm()&0o002 != 0 {
			continue
		}
		// Preserve the selected PATH entry for the engine's governed-source
		// boundary check. The engine resolves symlinks only after checking this
		// original selection path.
		return filepath.Clean(candidate), nil
	}
	return "", fmt.Errorf("verification executable %q was not found on sanitized PATH: %w", executable, exec.ErrNotFound)
}

// Run executes the already-resolved executable directly with its argument
// vector and no shell interpolation.
func (adapter *Adapter) Run(
	ctx context.Context,
	invocation verification.ProcessInvocation,
) (verification.ProcessResult, error) {
	if adapter == nil {
		return verification.ProcessResult{}, fmt.Errorf("local verification adapter is required")
	}
	if ctx == nil {
		return verification.ProcessResult{}, fmt.Errorf("verification process context is required")
	}
	if !filepath.IsAbs(invocation.ResolvedExecutable) {
		return verification.ProcessResult{}, fmt.Errorf("verification executable must be resolved before execution")
	}
	if strings.TrimSpace(invocation.Directory) == "" {
		return verification.ProcessResult{}, fmt.Errorf("verification process directory is required")
	}
	protectedRoots := []string{invocation.WorkspaceRoot, invocation.CanonicalRoot}
	for _, root := range protectedRoots {
		if !filepath.IsAbs(root) {
			return verification.ProcessResult{}, fmt.Errorf("verification protected roots must be resolved before execution")
		}
	}
	temporaryHome, err := os.MkdirTemp("", "praetor-verification-environment-")
	if err != nil {
		return verification.ProcessResult{}, fmt.Errorf("create isolated verification environment: %w", err)
	}
	defer os.RemoveAll(temporaryHome)

	command := exec.CommandContext(ctx, invocation.ResolvedExecutable, invocation.Arguments...)
	command.Dir = invocation.Directory
	command.Env = adapter.safeEnvironment(temporaryHome, protectedRoots)
	command.WaitDelay = 5 * time.Second
	standardOutput := newBoundedBuffer(adapter.maximumOutputBytes)
	standardError := newBoundedBuffer(adapter.maximumOutputBytes)
	command.Stdout = standardOutput
	command.Stderr = standardError

	runError := command.Run()
	exitCode := 0
	hasExitCode := runError == nil
	if runError != nil {
		var exitError *exec.ExitError
		if errors.As(runError, &exitError) {
			exitCode = exitError.ExitCode()
			hasExitCode = true
		}
	}
	return verification.NewProcessResult(
		exitCode,
		hasExitCode,
		standardOutput.Bytes(),
		standardError.Bytes(),
		standardOutput.Exceeded() || standardError.Exceeded(),
	), runError
}

func (adapter *Adapter) safeEnvironment(temporaryHome string, protectedRoots []string) []string {
	values := map[string]string{
		"PATH":           strings.Join(filterProtectedDirectories(adapter.executableDirectories, protectedRoots), string(os.PathListSeparator)),
		"HOME":           temporaryHome,
		"XDG_CACHE_HOME": filepath.Join(temporaryHome, "cache"),
		"TMPDIR":         temporaryHome,
		"GOCACHE":        filepath.Join(temporaryHome, "go-build"),
		"GOPATH":         filepath.Join(temporaryHome, "go"),
	}
	for _, name := range []string{
		"LANG", "LC_ALL", "TZ",
		"GOTOOLCHAIN", "CGO_ENABLED",
	} {
		if value, available := os.LookupEnv(name); available {
			values[name] = value
		}
	}
	if value, available := safePathListEnvironment("GOPATH", protectedRoots); available {
		values["GOPATH"] = value
	}
	if value, available := safePathListEnvironment("GOMODCACHE", protectedRoots); available {
		values["GOMODCACHE"] = value
	}
	result := make([]string, 0, len(values))
	for _, name := range []string{
		"PATH", "HOME", "XDG_CACHE_HOME", "TMPDIR", "LANG", "LC_ALL", "TZ",
		"GOCACHE", "GOMODCACHE", "GOPATH", "GOTOOLCHAIN", "CGO_ENABLED",
	} {
		if value, available := values[name]; available {
			result = append(result, name+"="+value)
		}
	}
	return result
}

func safePathListEnvironment(name string, protectedRoots []string) (string, bool) {
	value, available := os.LookupEnv(name)
	if !available || value == "" {
		return "", false
	}
	paths := filepath.SplitList(value)
	result := make([]string, 0, len(paths))
	for _, candidate := range paths {
		if candidate == "" || !filepath.IsAbs(candidate) {
			return "", false
		}
		candidate = filepath.Clean(candidate)
		if resolved, err := filepath.EvalSymlinks(candidate); err == nil {
			candidate = filepath.Clean(resolved)
		}
		for _, root := range protectedRoots {
			if pathWithin(root, candidate) {
				return "", false
			}
		}
		result = append(result, candidate)
	}
	return strings.Join(result, string(os.PathListSeparator)), true
}

func sanitizedExecutableDirectories(value string) []string {
	seen := make(map[string]struct{})
	result := make([]string, 0)
	for _, directory := range filepath.SplitList(value) {
		if directory == "" || !filepath.IsAbs(directory) {
			continue
		}
		resolved, err := filepath.EvalSymlinks(filepath.Clean(directory))
		if err != nil {
			continue
		}
		info, err := os.Stat(resolved)
		if err != nil || !info.IsDir() || info.Mode().Perm()&0o002 != 0 {
			continue
		}
		resolved = filepath.Clean(resolved)
		if _, duplicate := seen[resolved]; duplicate {
			continue
		}
		seen[resolved] = struct{}{}
		result = append(result, resolved)
	}
	return result
}

func filterProtectedDirectories(directories []string, protectedRoots []string) []string {
	result := make([]string, 0, len(directories))
	for _, directory := range directories {
		protected := false
		for _, root := range protectedRoots {
			if pathWithin(root, directory) {
				protected = true
				break
			}
		}
		if !protected {
			result = append(result, directory)
		}
	}
	return result
}

func pathWithin(parent string, candidate string) bool {
	relative, err := filepath.Rel(parent, candidate)
	return err == nil && (relative == "." || (relative != ".." && !strings.HasPrefix(relative, ".."+string(filepath.Separator))))
}

type boundedBuffer struct {
	buffer   bytes.Buffer
	limit    int
	exceeded bool
}

func newBoundedBuffer(limit int) *boundedBuffer { return &boundedBuffer{limit: limit} }

func (buffer *boundedBuffer) Write(value []byte) (int, error) {
	originalLength := len(value)
	remaining := buffer.limit - buffer.buffer.Len()
	if remaining <= 0 {
		buffer.exceeded = buffer.exceeded || originalLength > 0
		return originalLength, nil
	}
	if len(value) > remaining {
		buffer.exceeded = true
		value = value[:remaining]
	}
	_, _ = buffer.buffer.Write(value)
	return originalLength, nil
}

func (buffer *boundedBuffer) Bytes() []byte  { return append([]byte(nil), buffer.buffer.Bytes()...) }
func (buffer *boundedBuffer) Exceeded() bool { return buffer.exceeded }
