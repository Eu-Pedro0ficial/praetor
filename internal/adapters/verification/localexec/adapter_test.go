package localexec

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/Eu-Pedro0ficial/praetor/internal/verification"
)

func TestAdapterExecutesArgumentVectorWithoutShellAndStripsCredentials(t *testing.T) {
	adapter, err := New(4096)
	if err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	canonicalRoot := t.TempDir()
	literal := "$(touch should-not-exist)"
	t.Setenv("PRAETOR_LOCAL_SECRET", "do-not-inherit")
	result, err := adapter.Run(context.Background(), verification.ProcessInvocation{
		ResolvedExecutable: executable,
		Arguments:          []string{"-test.run=TestLocalExecHelperProcess", "--", "emit", literal},
		Directory:          root,
		WorkspaceRoot:      root,
		CanonicalRoot:      canonicalRoot,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	exitCode, available := result.ExitCode()
	if !available || exitCode != 0 {
		t.Fatalf("exit = %d/%t", exitCode, available)
	}
	output := string(result.StandardOutput())
	if !strings.Contains(output, "argument="+literal) || !strings.Contains(output, "secret=") ||
		strings.Contains(output, "do-not-inherit") {
		t.Fatalf("helper output = %q", output)
	}
	if _, err := os.Stat(filepath.Join(root, "should-not-exist")); !os.IsNotExist(err) {
		t.Fatalf("shell interpolation occurred: %v", err)
	}
}

func TestAdapterBoundsOutputAndReturnsExitStatus(t *testing.T) {
	adapter, _ := New(16)
	executable, _ := os.Executable()
	result, err := adapter.Run(context.Background(), verification.ProcessInvocation{
		ResolvedExecutable: executable,
		Arguments:          []string{"-test.run=TestLocalExecHelperProcess", "--", "fail"},
		Directory:          t.TempDir(),
		WorkspaceRoot:      t.TempDir(),
		CanonicalRoot:      t.TempDir(),
	})
	if err == nil {
		t.Fatal("failing helper returned no error")
	}
	exitCode, available := result.ExitCode()
	if !available || exitCode != 7 || !result.OutputTruncated() ||
		len(result.StandardOutput()) > 16 || len(result.StandardError()) > 16 {
		t.Fatalf("bounded failure result = exit %d/%t stdout=%d stderr=%d truncated=%t",
			exitCode, available, len(result.StandardOutput()), len(result.StandardError()), result.OutputTruncated())
	}
}

func TestAdapterHonorsCancellation(t *testing.T) {
	adapter := NewDefault()
	executable, _ := os.Executable()
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Millisecond)
	defer cancel()
	_, err := adapter.Run(ctx, verification.ProcessInvocation{
		ResolvedExecutable: executable,
		Arguments:          []string{"-test.run=TestLocalExecHelperProcess", "--", "wait"},
		Directory:          t.TempDir(),
		WorkspaceRoot:      t.TempDir(),
		CanonicalRoot:      t.TempDir(),
	})
	if err == nil || ctx.Err() == nil {
		t.Fatalf("cancelled Run() error/context = %v/%v", err, ctx.Err())
	}
}

func TestAdapterSanitizesPathResolutionAndChildEnvironment(t *testing.T) {
	workspaceRoot := t.TempDir()
	canonicalRoot := t.TempDir()
	safeBin := t.TempDir()
	worldWritableBin := t.TempDir()
	if err := os.Chmod(worldWritableBin, 0o777); err != nil {
		t.Fatal(err)
	}
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	toolName := "praetor-localexec-test-tool"
	if err := os.Symlink(executable, filepath.Join(safeBin, toolName)); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(executable, filepath.Join(worldWritableBin, "unsafe-tool")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(executable, filepath.Join(workspaceRoot, "workspace-tool")); err != nil {
		t.Fatal(err)
	}
	t.Setenv("PATH", strings.Join([]string{".", worldWritableBin, workspaceRoot, safeBin, safeBin}, string(os.PathListSeparator)))
	t.Setenv("GOPATH", workspaceRoot)
	t.Setenv("GOMODCACHE", canonicalRoot)
	adapter := NewDefault()
	resolved, err := adapter.Resolve(toolName)
	wantResolved := filepath.Join(safeBin, toolName)
	if err != nil || resolved != wantResolved {
		t.Fatalf("Resolve() = %q/%v, want %q", resolved, err, wantResolved)
	}
	if _, err := adapter.Resolve("unsafe-tool"); err == nil {
		t.Fatal("Resolve() accepted a tool from a world-writable PATH directory")
	}
	workspaceResolved, err := adapter.Resolve("workspace-tool")
	if err != nil || workspaceResolved != filepath.Join(workspaceRoot, "workspace-tool") {
		t.Fatalf("workspace Resolve() = %q/%v", workspaceResolved, err)
	}

	result, err := adapter.Run(context.Background(), verification.ProcessInvocation{
		ResolvedExecutable: executable,
		Arguments:          []string{"-test.run=TestLocalExecHelperProcess", "--", "environment"},
		Directory:          workspaceRoot,
		WorkspaceRoot:      workspaceRoot,
		CanonicalRoot:      canonicalRoot,
	})
	if err != nil {
		t.Fatalf("Run() error = %v", err)
	}
	output := string(result.StandardOutput())
	if strings.Contains(output, workspaceRoot) || strings.Contains(output, worldWritableBin) ||
		strings.Contains(output, canonicalRoot) || strings.Contains(output, ".:") || !strings.Contains(output, safeBin) {
		t.Fatalf("sanitized child PATH = %q", output)
	}
}

func TestLocalExecHelperProcess(t *testing.T) {
	separator := -1
	for index, argument := range os.Args {
		if argument == "--" {
			separator = index
			break
		}
	}
	if separator < 0 || separator+1 >= len(os.Args) {
		return
	}
	mode := os.Args[separator+1]
	switch mode {
	case "emit":
		argument := ""
		if separator+2 < len(os.Args) {
			argument = os.Args[separator+2]
		}
		workingDirectory, _ := os.Getwd()
		fmt.Printf("directory=%s secret=%s argument=%s\n", workingDirectory, os.Getenv("PRAETOR_LOCAL_SECRET"), argument)
	case "fail":
		fmt.Fprintln(os.Stdout, strings.Repeat("o", 64))
		fmt.Fprintln(os.Stderr, strings.Repeat("e", 64))
		os.Exit(7)
	case "wait":
		time.Sleep(10 * time.Second)
	case "environment":
		fmt.Printf("path=%s home=%s tmp=%s gopath=%s gomodcache=%s\n",
			os.Getenv("PATH"), os.Getenv("HOME"), os.Getenv("TMPDIR"), os.Getenv("GOPATH"), os.Getenv("GOMODCACHE"))
	}
}
