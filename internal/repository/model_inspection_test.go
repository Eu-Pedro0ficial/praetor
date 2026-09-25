package repository

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/Eu-Pedro0ficial/praetor/internal/repositorymodel"
)

func TestModelFingerprintTracksContentModesLinksAndUntrackedNames(t *testing.T) {
	root := committedRepositoryFixture(t)
	configuration := repositorymodel.DefaultConfiguration()
	baseline, err := InspectModelSource(repositoryTestProjectId, root, configuration)
	if err != nil {
		t.Fatal(err)
	}
	untracked := filepath.Join(root, "scratch.txt")
	if err := os.WriteFile(untracked, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	withUntracked, err := InspectModelSource(repositoryTestProjectId, root, configuration)
	if err != nil {
		t.Fatal(err)
	}
	if withUntracked.Fingerprint.Digest == baseline.Fingerprint.Digest || len(withUntracked.Fingerprint.Untracked) != 1 {
		t.Fatal("untracked path presence did not affect comparable fingerprint")
	}
	if err := os.WriteFile(untracked, []byte("different bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	byteChanged, err := InspectModelSource(repositoryTestProjectId, root, configuration)
	if err != nil {
		t.Fatal(err)
	}
	if byteChanged.Fingerprint.Digest != withUntracked.Fingerprint.Digest {
		t.Fatal("excluded untracked byte-only change altered fingerprint")
	}
	if err := os.Rename(untracked, filepath.Join(root, "renamed.txt")); err != nil {
		t.Fatal(err)
	}
	renamed, err := InspectModelSource(repositoryTestProjectId, root, configuration)
	if err != nil {
		t.Fatal(err)
	}
	if renamed.Fingerprint.Digest == byteChanged.Fingerprint.Digest {
		t.Fatal("untracked rename did not alter source condition")
	}
	if err := os.Remove(filepath.Join(root, "renamed.txt")); err != nil {
		t.Fatal(err)
	}
	deleted, err := InspectModelSource(repositoryTestProjectId, root, configuration)
	if err != nil {
		t.Fatal(err)
	}
	if deleted.Fingerprint.Digest == renamed.Fingerprint.Digest || len(deleted.Fingerprint.Untracked) != 0 {
		t.Fatal("untracked deletion did not alter source condition")
	}

	trackedPath := filepath.Join(root, "internal", "service", "service.go")
	if err := os.WriteFile(trackedPath, []byte("package service\n\nfunc Changed() {}\n"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(trackedPath, 0o700); err != nil {
		t.Fatal(err)
	}
	dirty, err := InspectModelSource(repositoryTestProjectId, root, configuration)
	if err != nil {
		t.Fatal(err)
	}
	if dirty.Fingerprint.Digest == deleted.Fingerprint.Digest {
		t.Fatal("tracked content/mode change did not alter fingerprint")
	}
	foundMode := false
	for _, entry := range dirty.Fingerprint.Tracked {
		if entry.Path == "internal/service/service.go" {
			foundMode = entry.GitMode == "100755"
		}
	}
	if !foundMode {
		t.Fatal("working-tree Git mode was not represented")
	}
}

func TestModelFingerprintDoesNotFollowTrackedSymlink(t *testing.T) {
	root := committedRepositoryFixture(t)
	outside := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(outside, []byte("first secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "link.txt")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, root, "add", "link.txt")
	runGitTest(t, root, "-c", "user.name=Praetor Test", "-c", "user.email=praetor@example.invalid", "commit", "--quiet", "-m", "link")
	first, err := InspectModelSource(repositoryTestProjectId, root, repositorymodel.DefaultConfiguration())
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(outside, []byte("changed secret"), 0o600); err != nil {
		t.Fatal(err)
	}
	second, err := InspectModelSource(repositoryTestProjectId, root, repositorymodel.DefaultConfiguration())
	if err != nil {
		t.Fatal(err)
	}
	if first.Fingerprint.Digest != second.Fingerprint.Digest {
		t.Fatal("symlink target content was followed into fingerprint")
	}
	for _, entry := range first.Fingerprint.Tracked {
		if entry.Path == "link.txt" && (entry.Kind != "symlink" || entry.ContentDigest == "") {
			t.Fatalf("symlink entry=%#v", entry)
		}
	}
}

func TestModelFingerprintRepresentsGitlinkIdentityWithoutTraversal(t *testing.T) {
	root := committedRepositoryFixture(t)
	head := strings.TrimSpace(runGitTest(t, root, "rev-parse", "HEAD"))
	runGitTest(t, root, "update-index", "--add", "--cacheinfo", "160000,"+head+",third_party/module")
	inspected, err := InspectModelSource(repositoryTestProjectId, root, repositorymodel.DefaultConfiguration())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, entry := range inspected.Fingerprint.Tracked {
		if entry.Path == "third_party/module" {
			found = entry.Kind == "gitlink" && entry.ObjectId == head && entry.Available
		}
	}
	if !found {
		t.Fatalf("Gitlink identity missing from tracked manifest: %#v", inspected.Fingerprint.Tracked)
	}
}

func TestModelInspectionBoundsAggregateAnalyzerContent(t *testing.T) {
	root := committedRepositoryFixture(t)
	configuration := repositorymodel.DefaultConfiguration()
	configuration.Limits.MaximumTotalBytes = 1
	inspected, err := InspectModelSource(repositoryTestProjectId, root, configuration)
	if err != nil {
		t.Fatal(err)
	}
	available := int64(0)
	bounded := false
	for _, file := range inspected.Files {
		if file.Available {
			available += file.Size
		}
		if strings.Contains(file.Exclusion, "aggregate analyzer limit") {
			bounded = true
		}
	}
	if available > configuration.Limits.MaximumTotalBytes || !bounded || !inspected.Fingerprint.Complete {
		t.Fatalf("aggregate bound available=%d bounded=%t complete=%t", available, bounded, inspected.Fingerprint.Complete)
	}
}

func TestModelInspectionRejectsMetadataBeyondConfiguredBound(t *testing.T) {
	root := committedRepositoryFixture(t)
	configuration := repositorymodel.DefaultConfiguration()
	configuration.Limits.MaximumMetadataBytes = 1
	_, err := InspectModelSource(repositoryTestProjectId, root, configuration)
	if err == nil || !strings.Contains(err.Error(), "repository metadata exceeds") {
		t.Fatalf("metadata bound error=%v", err)
	}
}

func TestModelInspectionRejectsIntermediateAndNestedSymlinkEscapes(t *testing.T) {
	for _, testCase := range []struct {
		name      string
		component string
	}{
		{name: "intermediate", component: "internal"},
		{name: "nested", component: "internal/service"},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			root := committedRepositoryFixture(t)
			componentPath := filepath.Join(root, filepath.FromSlash(testCase.component))
			outside := filepath.Join(t.TempDir(), "outside")
			if err := os.Rename(componentPath, outside); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(outside, componentPath); err != nil {
				t.Fatal(err)
			}
			inspected, err := InspectModelSource(repositoryTestProjectId, root, repositorymodel.DefaultConfiguration())
			if err != nil {
				t.Fatal(err)
			}
			assertTrackedUnavailable(t, inspected, "internal/service/service.go")
			if inspected.Fingerprint.Complete {
				t.Fatal("symlinked parent produced a complete fingerprint")
			}
		})
	}
}

func TestModelInspectionPreservesFinalSymlinkWithoutReadingTarget(t *testing.T) {
	root := committedRepositoryFixture(t)
	secret := []byte("outside-secret-that-must-not-be-read")
	outside := filepath.Join(t.TempDir(), "secret")
	if err := os.WriteFile(outside, secret, 0o600); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(root, "tracked-link")
	if err := os.Symlink(outside, link); err != nil {
		t.Fatal(err)
	}
	runGitTest(t, root, "add", "tracked-link")
	inspected, err := InspectModelSource(repositoryTestProjectId, root, repositorymodel.DefaultConfiguration())
	if err != nil {
		t.Fatal(err)
	}
	for _, entry := range inspected.Fingerprint.Tracked {
		if entry.Path == "tracked-link" {
			if entry.Kind != "symlink" || !entry.Available || entry.ContentDigest != digestBytes([]byte(outside)) {
				t.Fatalf("final symlink entry=%#v", entry)
			}
			return
		}
	}
	t.Fatal("tracked final symlink missing")
}

func TestModelInspectionFailsClosedOnConcurrentPathReplacement(t *testing.T) {
	for _, testCase := range []struct {
		name       string
		checkpoint string
		mutate     func(t *testing.T, root string)
	}{
		{
			name:       "parent component replacement",
			checkpoint: "parent-after-lstat",
			mutate: func(t *testing.T, root string) {
				original := filepath.Join(root, "internal")
				saved := filepath.Join(root, "internal.saved")
				if err := os.Rename(original, saved); err != nil {
					t.Fatal(err)
				}
				if err := os.Symlink(saved, original); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name:       "final inode swap",
			checkpoint: "regular-after-lstat",
			mutate: func(t *testing.T, root string) {
				pathValue := filepath.Join(root, "internal", "service", "service.go")
				if err := os.Rename(pathValue, pathValue+".saved"); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(pathValue, []byte("package hostile\n"), 0o600); err != nil {
					t.Fatal(err)
				}
			},
		},
		{
			name:       "ABA path replacement",
			checkpoint: "regular-after-read",
			mutate: func(t *testing.T, root string) {
				pathValue := filepath.Join(root, "internal", "service", "service.go")
				saved := pathValue + ".saved"
				if err := os.Rename(pathValue, saved); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(pathValue, []byte("package hostile\n"), 0o600); err != nil {
					t.Fatal(err)
				}
				if err := os.Remove(pathValue); err != nil {
					t.Fatal(err)
				}
				if err := os.Rename(saved, pathValue); err != nil {
					t.Fatal(err)
				}
			},
		},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			root := committedRepositoryFixture(t)
			secureRoot, err := os.OpenRoot(root)
			if err != nil {
				t.Fatal(err)
			}
			defer secureRoot.Close()
			mutated := false
			entry, file, gap := inspectTrackedEntryWithHook(secureRoot, stageEntry{mode: "100644", objectId: strings.Repeat("a", 40), path: "internal/service/service.go"}, repositorymodel.DefaultConfiguration(), func(checkpoint, repositoryPath string) {
				if !mutated && checkpoint == testCase.checkpoint {
					mutated = true
					testCase.mutate(t, root)
				}
			})
			if !mutated {
				t.Fatalf("checkpoint %q was not reached", testCase.checkpoint)
			}
			if entry.Available || file.Available || len(file.Content) != 0 || gap == nil {
				t.Fatalf("replacement did not fail closed: entry=%#v file=%#v gap=%#v", entry, file, gap)
			}
		})
	}
}

func TestModelInspectionFingerprintDigestMatchesAnalyzerBytes(t *testing.T) {
	root := committedRepositoryFixture(t)
	inspected, err := InspectModelSource(repositoryTestProjectId, root, repositorymodel.DefaultConfiguration())
	if err != nil {
		t.Fatal(err)
	}
	entries := make(map[string]repositorymodel.TrackedEntry, len(inspected.Fingerprint.Tracked))
	for _, entry := range inspected.Fingerprint.Tracked {
		entries[entry.Path] = entry
	}
	for _, file := range inspected.Files {
		if !file.Available || file.Kind != "regular" {
			continue
		}
		sum := sha256.Sum256(file.Content)
		want := "sha256:" + hex.EncodeToString(sum[:])
		if file.ContentDigest != want || entries[file.Path].ContentDigest != want {
			t.Fatalf("%s fingerprint=%s analyzer=%s content=%s", file.Path, entries[file.Path].ContentDigest, file.ContentDigest, want)
		}
	}
}

func assertTrackedUnavailable(t *testing.T, inspected repositorymodel.SourceInspection, pathValue string) {
	t.Helper()
	for _, entry := range inspected.Fingerprint.Tracked {
		if entry.Path == pathValue {
			if entry.Available {
				t.Fatalf("%s unexpectedly available: %#v", pathValue, entry)
			}
			for _, file := range inspected.Files {
				if file.Path == pathValue && (file.Available || len(file.Content) != 0) {
					t.Fatalf("%s analyzer bytes escaped confinement: %#v", pathValue, file)
				}
			}
			return
		}
	}
	t.Fatalf("tracked path %s missing", pathValue)
}

func TestModelInspectionNeverMixesDigestAndAnalyzerBytesDuringAtomicReplacement(t *testing.T) {
	root := committedRepositoryFixture(t)
	pathValue := filepath.Join(root, "internal", "service", "service.go")
	contentA := []byte("package service\n\nfunc Value() string { return \"A\" }\n")
	contentB := []byte("package service\n\nfunc Value() string { return \"B\" }\n")
	if err := os.WriteFile(pathValue, contentA, 0o600); err != nil {
		t.Fatal(err)
	}
	secureRoot, err := os.OpenRoot(root)
	if err != nil {
		t.Fatal(err)
	}
	defer secureRoot.Close()

	stop := make(chan struct{})
	var replacementError error
	var replacementMutex sync.Mutex
	done := make(chan struct{})
	go func() {
		defer close(done)
		content := contentB
		for {
			select {
			case <-stop:
				return
			default:
			}
			replacement := pathValue + ".replacement"
			if err := os.WriteFile(replacement, content, 0o600); err != nil {
				replacementMutex.Lock()
				replacementError = err
				replacementMutex.Unlock()
				return
			}
			if err := os.Rename(replacement, pathValue); err != nil {
				replacementMutex.Lock()
				replacementError = err
				replacementMutex.Unlock()
				return
			}
			if string(content) == string(contentA) {
				content = contentB
			} else {
				content = contentA
			}
		}
	}()

	successes := 0
	for range 200 {
		entry, file, _ := inspectTrackedEntry(secureRoot, stageEntry{mode: "100644", objectId: strings.Repeat("a", 40), path: "internal/service/service.go"}, repositorymodel.DefaultConfiguration())
		if !entry.Available || !file.Available {
			continue
		}
		successes++
		sum := sha256.Sum256(file.Content)
		if got := "sha256:" + hex.EncodeToString(sum[:]); entry.ContentDigest != got || file.ContentDigest != got {
			close(stop)
			<-done
			t.Fatalf("mixed observation: entry=%s file=%s bytes=%s", entry.ContentDigest, file.ContentDigest, got)
		}
	}
	close(stop)
	<-done
	replacementMutex.Lock()
	err = replacementError
	replacementMutex.Unlock()
	if err != nil {
		t.Fatal(err)
	}
	if successes == 0 {
		entry, file, gap := inspectTrackedEntry(secureRoot, stageEntry{mode: "100644", objectId: strings.Repeat("a", 40), path: "internal/service/service.go"}, repositorymodel.DefaultConfiguration())
		if !entry.Available || !file.Available || gap != nil {
			t.Fatalf("stable inspection failed after replacement stopped: entry=%#v file=%#v gap=%#v", entry, file, gap)
		}
	}
}
