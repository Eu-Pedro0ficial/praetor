package project

import (
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Eu-Pedro0ficial/praetor/internal/policy"
)

const validManifest = `schema_version: 1
bundle:
  id: fixture
  version: "1.0"
  policies:
    - id: tests
      version: "1.0"
      family: testing
      description: Require passing tests.
      severity: HIGH
      outcome: APPROVAL
      required_evidence: test
      non_overridable: true
      exception_candidate_allowed: true
`

func TestLoadValidManifestIsStrictBoundedAndReadOnly(t *testing.T) {
	root := writeManifest(t, validManifest)
	before, _ := os.ReadFile(filepath.Join(root, filepath.FromSlash(ManifestPath)))
	bundle, err := New().Load(root)
	if err != nil {
		t.Fatal(err)
	}
	wantDigest := sha256.Sum256([]byte(validManifest))
	if bundle.Id() != "fixture" || bundle.Version() != "1.0" || bundle.Digest() != hex.EncodeToString(wantDigest[:]) || len(bundle.Policies()) != 1 || bundle.Policies()[0].Severity() != policy.SeverityHigh {
		t.Fatalf("bundle = %#v", bundle)
	}
	after, _ := os.ReadFile(filepath.Join(root, filepath.FromSlash(ManifestPath)))
	if string(before) != string(after) {
		t.Fatal("loading mutated governed source")
	}
}

func TestDecodeRejectsInvalidManifestForms(t *testing.T) {
	tests := map[string]string{
		"invalid yaml":        "schema_version: [",
		"unsupported version": strings.Replace(validManifest, "schema_version: 1", "schema_version: 2", 1),
		"unknown field":       validManifest + "unknown: true\n",
		"duplicate key":       strings.Replace(validManifest, "schema_version: 1", "schema_version: 1\nschema_version: 1", 1),
		"multiple documents":  validManifest + "---\nschema_version: 1\n",
		"anchor":              strings.Replace(validManifest, "id: fixture", "id: &bundle fixture", 1),
		"alias":               strings.Replace(validManifest, "id: tests", "id: &name tests", 1) + "alias: *name\n",
		"invalid severity":    strings.Replace(validManifest, "severity: HIGH", "severity: WARNING", 1),
		"invalid outcome":     strings.Replace(validManifest, "outcome: APPROVAL", "outcome: ACCEPT", 1),
		"missing identity":    strings.Replace(validManifest, "id: tests", "id: ''", 1),
		"missing field":       strings.Replace(validManifest, "      severity: HIGH\n", "", 1),
		"wrong node type":     strings.Replace(validManifest, "version: \"1.0\"", "version: 1.0", 1),
		"duplicate policy":    strings.Replace(validManifest, "    - id: tests", "    - id: tests\n      version: \"1.0\"\n      family: testing\n      description: Duplicate policy.\n      severity: LOW\n      outcome: AUTO\n      required_evidence: test\n      non_overridable: false\n      exception_candidate_allowed: false\n    - id: tests", 1),
		"executable payload":  validManifest + "command: rm\n",
	}
	for name, payload := range tests {
		t.Run(name, func(t *testing.T) {
			if _, err := decode([]byte(payload)); err == nil {
				t.Fatal("invalid manifest accepted")
			}
		})
	}
}

func TestDecodeRejectsExcessiveNesting(t *testing.T) {
	payload := "nested: " + strings.Repeat("[", maximumNodeDepth+1) + "0" + strings.Repeat("]", maximumNodeDepth+1) + "\n"
	if _, err := decode([]byte(payload)); err == nil || !strings.Contains(err.Error(), "nesting depth") {
		t.Fatalf("deep manifest error = %v", err)
	}
}

func TestLoadRejectsOversizedAndSymlinkManifest(t *testing.T) {
	root := writeManifest(t, strings.Repeat("x", maximumManifestBytes+1))
	if _, err := New().Load(root); err == nil {
		t.Fatal("oversized manifest accepted")
	}
	realRoot := t.TempDir()
	real := filepath.Join(realRoot, "real.yaml")
	if err := os.WriteFile(real, []byte(validManifest), 0o600); err != nil {
		t.Fatal(err)
	}
	linkRoot := t.TempDir()
	path := filepath.Join(linkRoot, filepath.FromSlash(ManifestPath))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(real, path); err != nil {
		t.Fatal(err)
	}
	if _, err := New().Load(linkRoot); err == nil {
		t.Fatal("symlink manifest accepted")
	}
}

func writeManifest(t *testing.T, contents string) string {
	t.Helper()
	root := t.TempDir()
	path := filepath.Join(root, filepath.FromSlash(ManifestPath))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
		t.Fatal(err)
	}
	return root
}
