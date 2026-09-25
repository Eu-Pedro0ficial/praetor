package repository

import (
	"bufio"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"
	"strings"

	"github.com/Eu-Pedro0ficial/praetor/internal/project"
	"github.com/Eu-Pedro0ficial/praetor/internal/repositorymodel"
	"github.com/Eu-Pedro0ficial/praetor/internal/source"
)

// InspectModelSource captures the bounded, read-only ADR-039 source inputs.
// It never follows tracked symlinks and never reads untracked content.
func InspectModelSource(projectId project.ProjectId, startPath string, configuration repositorymodel.Configuration) (repositorymodel.SourceInspection, error) {
	if !projectId.IsValid() {
		return repositorymodel.SourceInspection{}, fmt.Errorf("valid ProjectId is required")
	}
	context, err := Discover(startPath)
	if err != nil {
		return repositorymodel.SourceInspection{}, err
	}
	headBytes, err := runGit(context.Root, "rev-parse", "--verify", "HEAD")
	if err != nil {
		return repositorymodel.SourceInspection{}, fmt.Errorf("capture RepositoryModel HEAD: %w", err)
	}
	head := strings.TrimSpace(string(headBytes))
	stageBytes, err := runGit(context.Root, "ls-files", "--stage", "--full-name", "-z")
	if err != nil {
		return repositorymodel.SourceInspection{}, fmt.Errorf("capture tracked manifest: %w", err)
	}
	statusBytes, err := runGit(context.Root, "status", "--porcelain=v1", "-z", "--untracked-files=all", "--no-renames")
	if err != nil {
		return repositorymodel.SourceInspection{}, fmt.Errorf("capture source condition: %w", err)
	}
	staged, err := parseStageEntries(stageBytes)
	if err != nil {
		return repositorymodel.SourceInspection{}, err
	}
	if len(staged) > configuration.Limits.MaximumFiles {
		return repositorymodel.SourceInspection{}, fmt.Errorf("tracked file count %d exceeds configured limit %d", len(staged), configuration.Limits.MaximumFiles)
	}
	untracked, err := parseUntrackedEntries(statusBytes)
	if err != nil {
		return repositorymodel.SourceInspection{}, err
	}
	if len(staged)+len(untracked) > configuration.Limits.MaximumFiles {
		return repositorymodel.SourceInspection{}, fmt.Errorf("repository entry count %d exceeds configured limit %d", len(staged)+len(untracked), configuration.Limits.MaximumFiles)
	}
	if int64(len(stageBytes)+len(statusBytes)) > configuration.Limits.MaximumMetadataBytes {
		return repositorymodel.SourceInspection{}, fmt.Errorf("repository metadata exceeds configured %d-byte aggregate limit", configuration.Limits.MaximumMetadataBytes)
	}
	legacy, err := Inspect(projectId, context.Root)
	if err != nil {
		return repositorymodel.SourceInspection{}, err
	}
	repositoryRoot, err := os.OpenRoot(context.Root)
	if err != nil {
		return repositorymodel.SourceInspection{}, fmt.Errorf("open repository root securely: %w", err)
	}
	defer repositoryRoot.Close()

	tracked := make([]repositorymodel.TrackedEntry, 0, len(staged))
	files := make([]repositorymodel.FileInput, 0, len(staged))
	var gaps []repositorymodel.KnowledgeGap
	complete := true
	var analyzedBytes int64
	for _, stagedEntry := range staged {
		entry, file, gap := inspectTrackedEntry(repositoryRoot, stagedEntry, configuration)
		if file.Available && analyzedBytes+file.Size > configuration.Limits.MaximumTotalBytes {
			file.Available = false
			file.Content = nil
			file.Exclusion = fmt.Sprintf("tracked inputs exceed %d-byte aggregate analyzer limit", configuration.Limits.MaximumTotalBytes)
		} else if file.Available {
			analyzedBytes += file.Size
		}
		tracked = append(tracked, entry)
		files = append(files, file)
		if gap != nil {
			gaps = append(gaps, *gap)
		}
		if !entry.Available {
			complete = false
		}
	}
	slices.SortFunc(tracked, func(left, right repositorymodel.TrackedEntry) int { return strings.Compare(left.Path, right.Path) })
	slices.SortFunc(files, func(left, right repositorymodel.FileInput) int { return strings.Compare(left.Path, right.Path) })
	slices.SortFunc(untracked, func(left, right repositorymodel.UntrackedEntry) int {
		if compared := strings.Compare(left.Path, right.Path); compared != 0 {
			return compared
		}
		return strings.Compare(left.Status, right.Status)
	})
	trackedDigest := repositorymodel.DigestJSON(struct {
		Version uint32                         `json:"version"`
		Entries []repositorymodel.TrackedEntry `json:"entries"`
	}{1, tracked})
	untrackedDigest := repositorymodel.DigestJSON(struct {
		Version uint32                           `json:"version"`
		Entries []repositorymodel.UntrackedEntry `json:"entries"`
	}{1, untracked})
	workingState := string(source.WorkingTreeClean)
	if len(statusBytes) > 0 {
		workingState = string(source.WorkingTreeDirty)
	}
	fingerprint := repositorymodel.SourceFingerprint{
		Version: 1, HeadRevision: head, WorkingTreeState: workingState, SourceStateDigest: string(legacy.SourceStateDigest()),
		TrackedManifestDigest: trackedDigest, UntrackedConditionDigest: untrackedDigest, Complete: complete,
		Tracked: tracked, Untracked: untracked,
	}
	fingerprint.Digest = repositorymodel.DigestJSON(struct {
		Version                                 uint32 `json:"version"`
		Head, State, Source, Tracked, Untracked string
	}{1, head, workingState, fingerprint.SourceStateDigest, trackedDigest, untrackedDigest})
	history := inspectBoundedHistory(context.Root, configuration.Limits.MaximumHistoryCommits)
	return repositorymodel.SourceInspection{ProjectId: projectId, RepositoryRoot: context.Root, Fingerprint: fingerprint, Files: files, History: history, Gaps: gaps}, nil
}

type stageEntry struct{ mode, objectId, path string }

func parseStageEntries(output []byte) ([]stageEntry, error) {
	values := splitNullTerminated(output)
	result := make([]stageEntry, 0, len(values))
	seen := map[string]struct{}{}
	for _, value := range values {
		tab := strings.IndexByte(value, '\t')
		if tab < 0 {
			return nil, fmt.Errorf("malformed Git tracked manifest entry")
		}
		fields := strings.Fields(value[:tab])
		if len(fields) != 3 {
			return nil, fmt.Errorf("malformed Git tracked manifest metadata")
		}
		if fields[2] != "0" {
			return nil, fmt.Errorf("unmerged tracked path %q cannot produce a complete RepositoryModel fingerprint", value[tab+1:])
		}
		normalized, err := source.NormalizeRepositoryPath(value[tab+1:])
		if err != nil {
			return nil, fmt.Errorf("tracked manifest path: %w", err)
		}
		pathValue := string(normalized)
		if _, duplicate := seen[pathValue]; duplicate {
			return nil, fmt.Errorf("duplicate tracked path %q", pathValue)
		}
		seen[pathValue] = struct{}{}
		result = append(result, stageEntry{mode: fields[0], objectId: fields[1], path: pathValue})
	}
	slices.SortFunc(result, func(left, right stageEntry) int { return strings.Compare(left.path, right.path) })
	return result, nil
}

func parseUntrackedEntries(output []byte) ([]repositorymodel.UntrackedEntry, error) {
	var result []repositorymodel.UntrackedEntry
	for _, value := range splitNullTerminated(output) {
		if len(value) < 4 || value[2] != ' ' {
			return nil, fmt.Errorf("malformed Git status entry")
		}
		status := value[:2]
		if status != "??" {
			continue
		}
		normalized, err := source.NormalizeRepositoryPath(value[3:])
		if err != nil {
			return nil, fmt.Errorf("untracked status path: %w", err)
		}
		result = append(result, repositorymodel.UntrackedEntry{Status: status, Path: string(normalized)})
	}
	return result, nil
}

type modelInspectionHook func(checkpoint, repositoryPath string)

func inspectTrackedEntry(root *os.Root, staged stageEntry, configuration repositorymodel.Configuration) (repositorymodel.TrackedEntry, repositorymodel.FileInput, *repositorymodel.KnowledgeGap) {
	return inspectTrackedEntryWithHook(root, staged, configuration, nil)
}

func inspectTrackedEntryWithHook(root *os.Root, staged stageEntry, configuration repositorymodel.Configuration, hook modelInspectionHook) (repositorymodel.TrackedEntry, repositorymodel.FileInput, *repositorymodel.KnowledgeGap) {
	entry := repositorymodel.TrackedEntry{Path: staged.path, GitMode: staged.mode, ObjectId: staged.objectId}
	file := repositorymodel.FileInput{Path: staged.path, GitMode: staged.mode}
	switch staged.mode {
	case "160000":
		entry.Kind, entry.Available, entry.ContentDigest = "gitlink", true, digestBytes([]byte(staged.objectId))
		file.Kind, file.Available, file.ContentDigest = entry.Kind, false, entry.ContentDigest
		return entry, file, nil
	case "120000":
		parent, name, closeParent, err := secureTrackedParent(root, staged.path, hook)
		if err != nil {
			return unavailableEntry(entry, file, "tracked symlink parent is unavailable or unsafe")
		}
		defer closeParent()
		parentState, parentErr := parent.Stat(".")
		info, err := parent.Lstat(name)
		if parentErr != nil || err != nil || info.Mode()&os.ModeSymlink == 0 {
			return unavailableEntry(entry, file, "tracked symlink is unavailable or changed type")
		}
		if hook != nil {
			hook("symlink-after-lstat", staged.path)
		}
		target, err := parent.Readlink(name)
		if err != nil {
			return unavailableEntry(entry, file, "tracked symlink link text is unreadable")
		}
		after, err := parent.Lstat(name)
		parentAfter, parentAfterErr := parent.Stat(".")
		if err != nil || parentAfterErr != nil || after.Mode()&os.ModeSymlink == 0 || !os.SameFile(info, after) || !stableDirectory(parentState, parentAfter) {
			return unavailableEntry(entry, file, "tracked symlink changed while being inspected")
		}
		entry.Kind, entry.Available, entry.Size, entry.ContentDigest = "symlink", true, int64(len(target)), digestBytes([]byte(target))
		file.Kind, file.Available, file.Size, file.ContentDigest = entry.Kind, false, entry.Size, entry.ContentDigest
		return entry, file, nil
	default:
		parent, name, closeParent, err := secureTrackedParent(root, staged.path, hook)
		if err != nil {
			return unavailableEntry(entry, file, "tracked content parent is unavailable or unsafe")
		}
		defer closeParent()
		parentState, parentErr := parent.Stat(".")
		info, err := parent.Lstat(name)
		if parentErr != nil || err != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
			return unavailableEntry(entry, file, "tracked regular content is unavailable or changed type")
		}
		if hook != nil {
			hook("regular-after-lstat", staged.path)
		}
		handle, err := parent.Open(name)
		if err != nil {
			return unavailableEntry(entry, file, "tracked content is unreadable")
		}
		opened, err := handle.Stat()
		if err != nil || !opened.Mode().IsRegular() || !os.SameFile(info, opened) {
			_ = handle.Close()
			return unavailableEntry(entry, file, "tracked content changed before it could be inspected")
		}
		mode := "100644"
		if opened.Mode().Perm()&0o111 != 0 {
			mode = "100755"
		}
		entry.GitMode, file.GitMode = mode, mode
		var digest string
		var content []byte
		if opened.Size() <= configuration.Limits.MaximumFileBytes {
			content, err = io.ReadAll(handle)
			if err == nil {
				digest = digestBytes(content)
			}
		} else {
			hasher := sha256.New()
			_, err = io.Copy(hasher, handle)
			if err == nil {
				digest = "sha256:" + hex.EncodeToString(hasher.Sum(nil))
			}
		}
		if hook != nil {
			hook("regular-after-read", staged.path)
		}
		readState, stateErr := handle.Stat()
		closeErr := handle.Close()
		after, pathErr := parent.Lstat(name)
		parentAfter, parentAfterErr := parent.Stat(".")
		if err != nil || stateErr != nil || closeErr != nil || pathErr != nil || parentAfterErr != nil || !stableRegularFile(opened, readState) || !stableRegularFile(opened, after) || !stableDirectory(parentState, parentAfter) {
			return unavailableEntry(entry, file, "tracked content changed while being inspected")
		}
		entry.Kind, entry.Available, entry.Size, entry.ContentDigest = "regular", true, opened.Size(), digest
		file.Kind, file.Size, file.ContentDigest = entry.Kind, entry.Size, entry.ContentDigest
		if opened.Size() > configuration.Limits.MaximumFileBytes {
			file.Available = false
			file.Exclusion = fmt.Sprintf("tracked file exceeds %d-byte analyzer input limit", configuration.Limits.MaximumFileBytes)
		} else {
			file.Available = true
			file.Content = content
		}
		file.Excluded, file.Exclusion = excluded(staged.path, configuration, file.Exclusion)
		return entry, file, nil
	}
}

func secureTrackedParent(root *os.Root, repositoryPath string, hook modelInspectionHook) (*os.Root, string, func(), error) {
	components := strings.Split(repositoryPath, "/")
	if len(components) == 0 {
		return nil, "", func() {}, fmt.Errorf("empty tracked path")
	}
	for _, component := range components {
		if component == "" || component == "." || component == ".." {
			return nil, "", func() {}, fmt.Errorf("invalid tracked path component %q", component)
		}
	}
	current := root
	openedRoots := []*os.Root{}
	closeOpened := func() {
		for index := len(openedRoots) - 1; index >= 0; index-- {
			_ = openedRoots[index].Close()
		}
	}
	for _, component := range components[:len(components)-1] {
		before, err := current.Lstat(component)
		if err != nil || !before.IsDir() || before.Mode()&os.ModeSymlink != 0 {
			closeOpened()
			return nil, "", func() {}, fmt.Errorf("unsafe parent component %q", component)
		}
		if hook != nil {
			hook("parent-after-lstat", strings.Join(components[:len(openedRoots)+1], "/"))
		}
		child, err := current.OpenRoot(component)
		if err != nil {
			closeOpened()
			return nil, "", func() {}, fmt.Errorf("open parent component %q: %w", component, err)
		}
		opened, statErr := child.Stat(".")
		after, lstatErr := current.Lstat(component)
		if statErr != nil || lstatErr != nil || !opened.IsDir() || !after.IsDir() || after.Mode()&os.ModeSymlink != 0 || !os.SameFile(before, opened) || !os.SameFile(opened, after) {
			_ = child.Close()
			closeOpened()
			return nil, "", func() {}, fmt.Errorf("parent component %q changed during inspection", component)
		}
		openedRoots = append(openedRoots, child)
		current = child
	}
	return current, components[len(components)-1], closeOpened, nil
}

func stableRegularFile(expected, actual os.FileInfo) bool {
	return actual != nil && actual.Mode().IsRegular() && actual.Mode()&os.ModeSymlink == 0 && os.SameFile(expected, actual) && expected.Size() == actual.Size() && expected.Mode() == actual.Mode() && expected.ModTime() == actual.ModTime()
}

func stableDirectory(expected, actual os.FileInfo) bool {
	return actual != nil && actual.IsDir() && actual.Mode()&os.ModeSymlink == 0 && os.SameFile(expected, actual) && expected.ModTime() == actual.ModTime()
}

func unavailableEntry(entry repositorymodel.TrackedEntry, file repositorymodel.FileInput, reason string) (repositorymodel.TrackedEntry, repositorymodel.FileInput, *repositorymodel.KnowledgeGap) {
	entry.Kind, entry.Available, entry.ContentDigest = "unavailable", false, digestBytes([]byte("unavailable:"+entry.Path+":"+reason))
	file.Kind, file.Available, file.ContentDigest, file.Exclusion = entry.Kind, false, entry.ContentDigest, reason
	gap := repositorymodel.KnowledgeGap{Id: repositorymodel.GapId("unavailable-input", entry.Path, reason), Category: "unavailable-input", Scope: entry.Path, Reason: reason, Evidence: []repositorymodel.Evidence{{Kind: "git-index", Reference: entry.Path, Detail: entry.GitMode + " " + entry.ObjectId}}, Consequence: "the source fingerprint is incomplete and freshness is UNKNOWN", Resolution: "restore a regular tracked entry and rebuild"}
	return entry, file, &gap
}

func excluded(file string, configuration repositorymodel.Configuration, existing string) (bool, string) {
	for _, prefix := range configuration.ExcludedPrefixes {
		if strings.HasPrefix(file, strings.TrimPrefix(prefix, "./")) {
			return true, "tracked path is excluded by analysis configuration: " + prefix
		}
	}
	for _, suffix := range configuration.GeneratedSuffixes {
		if strings.HasSuffix(file, suffix) {
			return true, "tracked generated path is excluded by analysis configuration: " + suffix
		}
	}
	return false, existing
}

func digestBytes(value []byte) string {
	sum := sha256.Sum256(value)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func inspectBoundedHistory(root string, limit int) []repositorymodel.HistoryCommit {
	if limit <= 0 {
		return nil
	}
	output, err := runGit(root, "-c", "core.quotePath=false", "log", "--no-renames", "--format=commit:%H", "--name-only", "-n", strconv.Itoa(limit))
	if err != nil {
		return nil
	}
	var result []repositorymodel.HistoryCommit
	var current *repositorymodel.HistoryCommit
	scanner := bufio.NewScanner(strings.NewReader(string(output)))
	scanner.Buffer(make([]byte, 4096), 1<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		if strings.HasPrefix(line, "commit:") {
			result = append(result, repositorymodel.HistoryCommit{Revision: strings.TrimPrefix(line, "commit:")})
			current = &result[len(result)-1]
			continue
		}
		if current == nil || len(current.Paths) >= 64 {
			continue
		}
		normalized, pathError := source.NormalizeRepositoryPath(line)
		if pathError == nil {
			current.Paths = append(current.Paths, string(normalized))
		}
	}
	for index := range result {
		slices.Sort(result[index].Paths)
		result[index].Paths = slices.Compact(result[index].Paths)
	}
	return result
}
