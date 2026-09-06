package gitproposal

import (
	"bytes"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func TestCanonicalProofExtractionIncludesAddedTextAndBinaryFiles(t *testing.T) {
	repositoryRoot := t.TempDir()
	runCanonicalTestGit(t, repositoryRoot, "init", "--quiet")
	if err := os.WriteFile(filepath.Join(repositoryRoot, "tracked.txt"), []byte("baseline\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	runCanonicalTestGit(t, repositoryRoot, "add", "tracked.txt")
	runCanonicalTestGit(t, repositoryRoot,
		"-c", "user.name=Praetor Test", "-c", "user.email=praetor@example.invalid",
		"commit", "--quiet", "-m", "baseline",
	)
	baseRevision := strings.TrimSpace(string(runCanonicalTestGit(t, repositoryRoot, "rev-parse", "HEAD")))
	if err := os.WriteFile(filepath.Join(repositoryRoot, "added.txt"), []byte("added\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(repositoryRoot, "added.bin"), []byte{0x00, 0xff, 0x01}, 0o600); err != nil {
		t.Fatal(err)
	}

	patch, err := extractCanonicalPatch(
		t.TempDir(),
		repositoryRoot,
		baseRevision,
		[]string{"added.bin", "added.txt"},
	)
	if err != nil {
		t.Fatalf("extractCanonicalPatch() error = %v", err)
	}
	if !bytes.Contains(patch, []byte("new file mode")) ||
		!bytes.Contains(patch, []byte("GIT binary patch")) ||
		!bytes.Contains(patch, []byte("+added")) {
		t.Fatalf("added-file canonical proof patch is incomplete:\n%s", patch)
	}
	if staged := runCanonicalTestGit(t, repositoryRoot, "diff", "--cached", "--name-only"); len(staged) != 0 {
		t.Fatalf("proof extraction changed canonical index: %q", staged)
	}
	status := string(runCanonicalTestGit(t, repositoryRoot, "status", "--porcelain=v1"))
	if status != "?? added.bin\n?? added.txt\n" {
		t.Fatalf("proof extraction changed canonical working tree: %q", status)
	}
}

func runCanonicalTestGit(t *testing.T, repositoryRoot string, arguments ...string) []byte {
	t.Helper()
	commandArguments := append([]string{"-C", repositoryRoot}, arguments...)
	command := exec.Command("git", commandArguments...)
	output, err := command.CombinedOutput()
	if err != nil {
		t.Fatalf("git %v: %v: %s", arguments, err, output)
	}
	return output
}
