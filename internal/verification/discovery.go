package verification

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"unicode"
)

const (
	maximumDiscoveryFiles       = 64
	maximumDiscoveryFileBytes   = 32 << 10
	maximumDiscoveryTotalBytes  = 256 << 10
	maximumDiscoveryWalkEntries = 10000
)

// Discover inspects open-ended repository evidence in an isolated workspace.
// Ecosystem-specific recognition is expressed as independent evidence rules,
// not a language switch in verification orchestration.
func Discover(workspaceRoot string) (DiscoveryResult, error) {
	root, err := resolveWorkspaceRoot(workspaceRoot)
	if err != nil {
		return DiscoveryResult{}, err
	}
	paths, walkIssues, err := discoverEvidencePaths(root)
	if err != nil {
		return DiscoveryResult{}, err
	}

	var candidates []VerificationCandidate
	var evidence []RepositoryEvidence
	issues := append([]string(nil), walkIssues...)
	needsPlanning := len(walkIssues) > 0
	totalBytes := 0
	for _, relativePath := range paths {
		absolutePath := filepath.Join(root, filepath.FromSlash(relativePath))
		info, statError := os.Lstat(absolutePath)
		if statError != nil {
			issues = append(issues, fmt.Sprintf("inspect %s: %v", relativePath, statError))
			needsPlanning = true
			continue
		}
		if !info.Mode().IsRegular() {
			issues = append(issues, fmt.Sprintf("ignore non-regular verification evidence %s", relativePath))
			needsPlanning = true
			continue
		}
		if info.Size() > maximumDiscoveryFileBytes {
			issues = append(issues, fmt.Sprintf("verification evidence %s exceeds 32 KiB", relativePath))
			needsPlanning = true
			continue
		}
		content, readError := os.ReadFile(absolutePath)
		if readError != nil {
			issues = append(issues, fmt.Sprintf("read %s: %v", relativePath, readError))
			needsPlanning = true
			continue
		}
		totalBytes += len(content)
		if totalBytes > maximumDiscoveryTotalBytes {
			issues = append(issues, "verification repository evidence exceeds 256 KiB")
			needsPlanning = true
			break
		}
		kind := evidenceKind(relativePath)
		item, evidenceError := newRepositoryEvidence(relativePath, kind, string(content))
		if evidenceError != nil {
			issues = append(issues, fmt.Sprintf("normalize evidence %s: %v", relativePath, evidenceError))
			needsPlanning = true
			continue
		}
		evidence = append(evidence, item)

		discovered, ambiguous, ruleIssues := candidatesFromEvidence(item)
		candidates = append(candidates, discovered...)
		needsPlanning = needsPlanning || ambiguous
		issues = append(issues, ruleIssues...)
	}
	if len(evidence) == 0 || len(candidates) == 0 {
		needsPlanning = true
	}
	return newDiscoveryResult(candidates, evidence, needsPlanning, issues), nil
}

func resolveWorkspaceRoot(value string) (string, error) {
	if strings.TrimSpace(value) == "" {
		return "", fmt.Errorf("ProposalWorkspace root is required for verification discovery")
	}
	absolute, err := filepath.Abs(value)
	if err != nil {
		return "", fmt.Errorf("resolve ProposalWorkspace root: %w", err)
	}
	resolved, err := filepath.EvalSymlinks(absolute)
	if err != nil {
		return "", fmt.Errorf("resolve ProposalWorkspace root symlinks: %w", err)
	}
	info, err := os.Stat(resolved)
	if err != nil {
		return "", fmt.Errorf("inspect ProposalWorkspace root: %w", err)
	}
	if !info.IsDir() {
		return "", fmt.Errorf("ProposalWorkspace root is not a directory")
	}
	return filepath.Clean(resolved), nil
}

func discoverEvidencePaths(root string) ([]string, []string, error) {
	var paths []string
	var issues []string
	entries := 0
	err := filepath.WalkDir(root, func(currentPath string, entry fs.DirEntry, walkError error) error {
		if walkError != nil {
			return walkError
		}
		entries++
		if entries > maximumDiscoveryWalkEntries {
			return fmt.Errorf("verification discovery exceeded %d repository entries", maximumDiscoveryWalkEntries)
		}
		relativePath, err := filepath.Rel(root, currentPath)
		if err != nil {
			return err
		}
		relativePath = filepath.ToSlash(relativePath)
		if entry.IsDir() {
			if relativePath == ".git" || strings.HasPrefix(relativePath, ".git/") {
				return filepath.SkipDir
			}
			return nil
		}
		if !isVerificationEvidencePath(relativePath) {
			return nil
		}
		if len(paths) >= maximumDiscoveryFiles {
			issues = append(issues, "verification discovery evidence exceeds 64 files")
			return nil
		}
		paths = append(paths, relativePath)
		return nil
	})
	if err != nil {
		return nil, nil, fmt.Errorf("discover repository verification evidence: %w", err)
	}
	sort.Strings(paths)
	return paths, issues, nil
}

func isVerificationEvidencePath(relativePath string) bool {
	base := filepath.Base(relativePath)
	switch base {
	case "go.mod", "package.json", "pom.xml", "build.gradle", "build.gradle.kts",
		"pyproject.toml", "requirements.txt", "Cargo.toml", "composer.json",
		"Makefile", "Taskfile", "Taskfile.yml", "Taskfile.yaml", ".gitlab-ci.yml":
		return true
	}
	extension := strings.ToLower(filepath.Ext(base))
	if strings.HasPrefix(relativePath, ".github/workflows/") && (extension == ".yml" || extension == ".yaml") {
		return true
	}
	if strings.HasSuffix(base, ".csproj") || strings.HasSuffix(base, ".sln") {
		return true
	}
	lower := strings.ToLower(base)
	if !strings.Contains(lower, "lint") && !strings.Contains(lower, "test") && !strings.Contains(lower, "toolchain") {
		return false
	}
	switch extension {
	case ".json", ".yaml", ".yml", ".toml", ".ini", ".cfg", ".conf":
		return true
	case ".js", ".cjs", ".mjs":
		return strings.Contains(lower, "config")
	default:
		return false
	}
}

func evidenceKind(relativePath string) string {
	base := filepath.Base(relativePath)
	switch base {
	case "package.json", "go.mod", "pom.xml", "build.gradle", "build.gradle.kts",
		"pyproject.toml", "requirements.txt", "Cargo.toml", "composer.json":
		return "manifest"
	case "Makefile", "Taskfile", "Taskfile.yml", "Taskfile.yaml":
		return "task-definition"
	default:
		if strings.Contains(relativePath, "/workflows/") || base == ".gitlab-ci.yml" {
			return "ci-configuration"
		}
		return "tool-configuration"
	}
}

func candidatesFromEvidence(evidence RepositoryEvidence) ([]VerificationCandidate, bool, []string) {
	base := filepath.Base(string(evidence.Path()))
	workingDirectory := filepath.ToSlash(filepath.Dir(string(evidence.Path())))
	if workingDirectory == "" {
		workingDirectory = "."
	}
	switch base {
	case "go.mod":
		candidate, err := NewCandidate(
			KindTest,
			"go",
			[]string{"test", "./..."},
			workingDirectory,
			OriginDeterministicallyInferred,
			[]string{string(evidence.Path())},
		)
		if err != nil {
			return nil, true, []string{err.Error()}
		}
		return []VerificationCandidate{candidate}, false, nil
	case "package.json":
		return candidatesFromPackageJSON(evidence, workingDirectory)
	case "Makefile":
		return candidatesFromMakefile(evidence, workingDirectory)
	default:
		return nil, true, nil
	}
}

func candidatesFromPackageJSON(evidence RepositoryEvidence, workingDirectory string) ([]VerificationCandidate, bool, []string) {
	var manifest struct {
		Scripts map[string]json.RawMessage `json:"scripts"`
	}
	if err := json.Unmarshal([]byte(evidence.Content()), &manifest); err != nil {
		return nil, true, []string{fmt.Sprintf("parse %s: malformed package.json", evidence.Path())}
	}
	var candidates []VerificationCandidate
	var issues []string
	for _, script := range []struct {
		name string
		kind StepKind
	}{
		{name: "build", kind: KindBuild},
		{name: "typecheck", kind: KindTypeCheck},
		{name: "type-check", kind: KindTypeCheck},
		{name: "lint", kind: KindLint},
		{name: "test", kind: KindTest},
	} {
		raw, exists := manifest.Scripts[script.name]
		if !exists {
			continue
		}
		var declared string
		if err := json.Unmarshal(raw, &declared); err != nil || strings.TrimSpace(declared) == "" {
			issues = append(issues, fmt.Sprintf("ignore malformed package.json script %q", script.name))
			continue
		}
		candidate, err := NewCandidate(
			script.kind,
			"npm",
			[]string{"run", script.name},
			workingDirectory,
			OriginRepositoryDeclared,
			[]string{string(evidence.Path())},
		)
		if err != nil {
			issues = append(issues, err.Error())
			continue
		}
		candidates = append(candidates, candidate)
	}
	return candidates, len(candidates) == 0 || len(issues) > 0, issues
}

func candidatesFromMakefile(evidence RepositoryEvidence, workingDirectory string) ([]VerificationCandidate, bool, []string) {
	targetKinds := map[string]StepKind{
		"build":     KindBuild,
		"typecheck": KindTypeCheck,
		"lint":      KindLint,
		"test":      KindTest,
		"verify":    KindRepositoryCheck,
		"check":     KindRepositoryCheck,
	}
	seen := make(map[string]struct{})
	var candidates []VerificationCandidate
	for _, line := range strings.Split(evidence.Content(), "\n") {
		if line == "" || unicode.IsSpace(rune(line[0])) || strings.HasPrefix(line, "#") {
			continue
		}
		separator := strings.IndexByte(line, ':')
		if separator <= 0 {
			continue
		}
		for _, target := range strings.Fields(line[:separator]) {
			kind, known := targetKinds[target]
			if !known {
				continue
			}
			if _, duplicate := seen[target]; duplicate {
				continue
			}
			seen[target] = struct{}{}
			candidate, err := NewCandidate(
				kind,
				"make",
				[]string{target},
				workingDirectory,
				OriginRepositoryDeclared,
				[]string{string(evidence.Path())},
			)
			if err != nil {
				return candidates, true, []string{err.Error()}
			}
			candidates = append(candidates, candidate)
		}
	}
	return candidates, len(candidates) == 0, nil
}
