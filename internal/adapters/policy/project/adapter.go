// Package project loads the fixed Project Policy Manifest V1 from governed source.
package project

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"

	"github.com/Eu-Pedro0ficial/praetor/internal/policy"
	"github.com/Eu-Pedro0ficial/praetor/internal/verification"
	"go.yaml.in/yaml/v3"
)

const (
	ManifestPath         = "engineering/policies/praetor.yaml"
	maximumManifestBytes = 64 * 1024
	maximumNodeDepth     = 16
)

type Adapter struct{}

func New() Adapter { return Adapter{} }

type manifestV1 struct {
	SchemaVersion int      `yaml:"schema_version"`
	Bundle        bundleV1 `yaml:"bundle"`
}
type bundleV1 struct {
	ID       string     `yaml:"id"`
	Version  string     `yaml:"version"`
	Policies []policyV1 `yaml:"policies"`
}
type policyV1 struct {
	ID                        string `yaml:"id"`
	Version                   string `yaml:"version"`
	Family                    string `yaml:"family"`
	Description               string `yaml:"description"`
	Severity                  string `yaml:"severity"`
	Outcome                   string `yaml:"outcome"`
	RequiredEvidence          string `yaml:"required_evidence"`
	NonOverridable            bool   `yaml:"non_overridable"`
	ExceptionCandidateAllowed bool   `yaml:"exception_candidate_allowed"`
}

func (Adapter) Load(repositoryRoot string) (policy.PolicyBundle, error) {
	root, err := filepath.Abs(repositoryRoot)
	if err != nil || strings.TrimSpace(repositoryRoot) == "" {
		return policy.PolicyBundle{}, fmt.Errorf("resolve Project Policy repository root")
	}
	manifestPath := filepath.Join(root, filepath.FromSlash(ManifestPath))
	resolved, err := filepath.EvalSymlinks(manifestPath)
	if err != nil {
		return policy.PolicyBundle{}, fmt.Errorf("load Project Policy Manifest %q: %w", ManifestPath, err)
	}
	if filepath.Clean(resolved) != filepath.Clean(manifestPath) {
		return policy.PolicyBundle{}, fmt.Errorf("Project Policy Manifest must not use symlinks")
	}
	info, err := os.Lstat(manifestPath)
	if err != nil {
		return policy.PolicyBundle{}, fmt.Errorf("inspect Project Policy Manifest: %w", err)
	}
	if !info.Mode().IsRegular() {
		return policy.PolicyBundle{}, fmt.Errorf("Project Policy Manifest must be a regular file")
	}
	if info.Size() > maximumManifestBytes {
		return policy.PolicyBundle{}, fmt.Errorf("Project Policy Manifest exceeds %d bytes", maximumManifestBytes)
	}
	file, err := os.Open(manifestPath)
	if err != nil {
		return policy.PolicyBundle{}, fmt.Errorf("open Project Policy Manifest: %w", err)
	}
	defer file.Close()
	payload, err := io.ReadAll(io.LimitReader(file, maximumManifestBytes+1))
	if err != nil {
		return policy.PolicyBundle{}, fmt.Errorf("read Project Policy Manifest: %w", err)
	}
	if len(payload) > maximumManifestBytes {
		return policy.PolicyBundle{}, fmt.Errorf("Project Policy Manifest exceeds %d bytes", maximumManifestBytes)
	}
	return decode(payload)
}

func decode(payload []byte) (policy.PolicyBundle, error) {
	var root yaml.Node
	decoder := yaml.NewDecoder(bytes.NewReader(payload))
	if err := decoder.Decode(&root); err != nil {
		return policy.PolicyBundle{}, fmt.Errorf("parse Project Policy Manifest: %w", err)
	}
	if len(root.Content) != 1 || root.Content[0].Kind != yaml.MappingNode {
		return policy.PolicyBundle{}, fmt.Errorf("Project Policy Manifest must contain one mapping document")
	}
	if err := inspectNode(root.Content[0], 1); err != nil {
		return policy.PolicyBundle{}, err
	}
	if err := validateSchemaNodes(root.Content[0]); err != nil {
		return policy.PolicyBundle{}, err
	}
	var extra yaml.Node
	if err := decoder.Decode(&extra); err != io.EOF {
		if err == nil {
			return policy.PolicyBundle{}, fmt.Errorf("Project Policy Manifest must contain exactly one YAML document")
		}
		return policy.PolicyBundle{}, fmt.Errorf("parse trailing Project Policy document: %w", err)
	}
	var decoded manifestV1
	strict := yaml.NewDecoder(bytes.NewReader(payload))
	strict.KnownFields(true)
	if err := strict.Decode(&decoded); err != nil {
		return policy.PolicyBundle{}, fmt.Errorf("validate Project Policy Manifest V1 fields: %w", err)
	}
	if decoded.SchemaVersion != 1 {
		return policy.PolicyBundle{}, fmt.Errorf("unsupported Project Policy schema_version %d", decoded.SchemaVersion)
	}
	policies := make([]policy.Policy, 0, len(decoded.Bundle.Policies))
	for index, item := range decoded.Bundle.Policies {
		rule, err := policy.NewPolicy(policy.PolicyId(item.ID), policy.PolicyVersion(item.Version), policy.PolicyFamily(item.Family), item.Description, policy.Severity(item.Severity), policy.EnforcementOutcome(item.Outcome), verification.StepKind(item.RequiredEvidence), item.NonOverridable, item.ExceptionCandidateAllowed)
		if err != nil {
			return policy.PolicyBundle{}, fmt.Errorf("policy[%d]: %w", index, err)
		}
		policies = append(policies, rule)
	}
	digest := sha256.Sum256(payload)
	return policy.NewBundle(policy.PolicyId(decoded.Bundle.ID), policy.PolicyVersion(decoded.Bundle.Version), hex.EncodeToString(digest[:]), policies)
}

func validateSchemaNodes(root *yaml.Node) error {
	rootFields, err := mappingFields(root, map[string]yaml.Kind{"schema_version": yaml.ScalarNode, "bundle": yaml.MappingNode})
	if err != nil {
		return err
	}
	if rootFields["schema_version"].Tag != "!!int" {
		return fmt.Errorf("schema_version must be an integer")
	}
	bundleFields, err := mappingFields(rootFields["bundle"], map[string]yaml.Kind{"id": yaml.ScalarNode, "version": yaml.ScalarNode, "policies": yaml.SequenceNode})
	if err != nil {
		return err
	}
	if bundleFields["id"].Tag != "!!str" || bundleFields["version"].Tag != "!!str" {
		return fmt.Errorf("bundle id and version must be strings")
	}
	for _, item := range bundleFields["policies"].Content {
		fields, fieldError := mappingFields(item, map[string]yaml.Kind{"id": yaml.ScalarNode, "version": yaml.ScalarNode, "family": yaml.ScalarNode, "description": yaml.ScalarNode, "severity": yaml.ScalarNode, "outcome": yaml.ScalarNode, "required_evidence": yaml.ScalarNode, "non_overridable": yaml.ScalarNode, "exception_candidate_allowed": yaml.ScalarNode})
		if fieldError != nil {
			return fieldError
		}
		for _, name := range []string{"id", "version", "family", "description", "severity", "outcome", "required_evidence"} {
			if fields[name].Tag != "!!str" {
				return fmt.Errorf("policy field %q must be a string", name)
			}
		}
		for _, name := range []string{"non_overridable", "exception_candidate_allowed"} {
			if fields[name].Tag != "!!bool" {
				return fmt.Errorf("policy field %q must be a boolean", name)
			}
		}
	}
	return nil
}

func mappingFields(node *yaml.Node, expected map[string]yaml.Kind) (map[string]*yaml.Node, error) {
	if node == nil || node.Kind != yaml.MappingNode {
		return nil, fmt.Errorf("Project Policy schema requires a mapping")
	}
	fields := map[string]*yaml.Node{}
	for index := 0; index < len(node.Content); index += 2 {
		name, value := node.Content[index].Value, node.Content[index+1]
		kind, known := expected[name]
		if !known {
			return nil, fmt.Errorf("unknown Project Policy field %q", name)
		}
		if value.Kind != kind {
			return nil, fmt.Errorf("Project Policy field %q has invalid YAML node type", name)
		}
		fields[name] = value
	}
	for name := range expected {
		if fields[name] == nil {
			return nil, fmt.Errorf("required Project Policy field %q is missing", name)
		}
	}
	return fields, nil
}

func inspectNode(node *yaml.Node, depth int) error {
	if depth > maximumNodeDepth {
		return fmt.Errorf("Project Policy Manifest exceeds maximum YAML nesting depth")
	}
	if node.Kind == yaml.AliasNode || node.Anchor != "" {
		return fmt.Errorf("Project Policy Manifest aliases and anchors are not allowed")
	}
	if node.Kind == yaml.MappingNode {
		seen := map[string]struct{}{}
		for index := 0; index < len(node.Content); index += 2 {
			key := node.Content[index]
			if key.Kind != yaml.ScalarNode || key.Tag != "!!str" {
				return fmt.Errorf("Project Policy Manifest keys must be strings")
			}
			if _, exists := seen[key.Value]; exists {
				return fmt.Errorf("Project Policy Manifest contains duplicate key %q", key.Value)
			}
			seen[key.Value] = struct{}{}
		}
	}
	for _, child := range node.Content {
		if err := inspectNode(child, depth+1); err != nil {
			return err
		}
	}
	return nil
}
