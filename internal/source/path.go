// Package source contains the M0.3 source-state and bounded Change Surface
// domain model. It is independent from Git and filesystem adapters.
package source

import (
	"fmt"
	"path"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

// RepositoryPath is one normalized repository-relative source path.
type RepositoryPath string

// NormalizeRepositoryPath converts an untrusted path into the canonical form
// used for Change Surface comparison. It fails closed on ambiguous or unsafe
// path syntax rather than applying platform-specific filesystem semantics.
func NormalizeRepositoryPath(value string) (RepositoryPath, error) {
	if value == "" {
		return "", fmt.Errorf("repository path is required")
	}
	if strings.TrimSpace(value) != value {
		return "", fmt.Errorf("repository path %q has surrounding whitespace", value)
	}
	if !utf8.ValidString(value) {
		return "", fmt.Errorf("repository path contains invalid UTF-8")
	}
	for _, character := range value {
		if unicode.IsControl(character) {
			return "", fmt.Errorf("repository path %q contains a control character", value)
		}
	}
	if strings.Contains(value, `\`) {
		return "", fmt.Errorf("repository path %q uses an ambiguous path separator", value)
	}
	if path.IsAbs(value) || looksLikeWindowsAbsolutePath(value) {
		return "", fmt.Errorf("repository path %q must be relative", value)
	}
	for _, segment := range strings.Split(value, "/") {
		if segment == ".." {
			return "", fmt.Errorf("repository path %q contains traversal", value)
		}
	}

	normalized := path.Clean(value)
	if normalized == "." || normalized == ".." || strings.HasPrefix(normalized, "../") {
		return "", fmt.Errorf("repository path %q does not name a source path", value)
	}
	return RepositoryPath(normalized), nil
}

func looksLikeWindowsAbsolutePath(value string) bool {
	if len(value) < 2 || value[1] != ':' {
		return false
	}
	first := value[0]
	return (first >= 'a' && first <= 'z') || (first >= 'A' && first <= 'Z')
}

func normalizeRepositoryPaths(values []string) ([]RepositoryPath, error) {
	unique := make(map[RepositoryPath]struct{}, len(values))
	for _, value := range values {
		normalized, err := NormalizeRepositoryPath(value)
		if err != nil {
			return nil, err
		}
		unique[normalized] = struct{}{}
	}

	paths := make([]RepositoryPath, 0, len(unique))
	for repositoryPath := range unique {
		paths = append(paths, repositoryPath)
	}
	sort.Slice(paths, func(left int, right int) bool {
		return paths[left] < paths[right]
	})
	return paths, nil
}

func copyRepositoryPaths(paths []RepositoryPath) []RepositoryPath {
	return append([]RepositoryPath(nil), paths...)
}

func containsRepositoryPath(paths []RepositoryPath, candidate RepositoryPath) bool {
	index := sort.Search(len(paths), func(index int) bool {
		return paths[index] >= candidate
	})
	return index < len(paths) && paths[index] == candidate
}
