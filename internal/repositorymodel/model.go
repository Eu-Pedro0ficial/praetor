// Package repositorymodel defines the presentation-neutral M1.2 repository
// intelligence model. Model values are immutable snapshots: callers receive
// defensive document copies and cannot mutate the value retained by a Model.
package repositorymodel

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Eu-Pedro0ficial/praetor/internal/project"
)

const SchemaVersion uint32 = 1

type EvidenceClass string
type Confidence string
type NodeKind string
type EdgeKind string
type Freshness string

const (
	EvidenceObserved EvidenceClass = "OBSERVED"
	EvidenceDerived  EvidenceClass = "DERIVED"
	EvidenceInferred EvidenceClass = "INFERRED"

	ConfidenceHigh   Confidence = "HIGH"
	ConfidenceMedium Confidence = "MEDIUM"
	ConfidenceLow    Confidence = "LOW"

	NodeRepository          NodeKind = "Repository"
	NodeModule              NodeKind = "Module"
	NodeFile                NodeKind = "File"
	NodeSymbol              NodeKind = "Symbol"
	NodeTest                NodeKind = "Test"
	NodeAPI                 NodeKind = "API"
	NodeExternalDependency  NodeKind = "ExternalDependency"
	NodeOwner               NodeKind = "Owner"
	NodeArchitectureElement NodeKind = "ArchitectureElement"
	NodeUnresolvedReference NodeKind = "UnresolvedReference"

	EdgeContains                   EdgeKind = "CONTAINS"
	EdgeDeclares                   EdgeKind = "DECLARES"
	EdgeDependsOn                  EdgeKind = "DEPENDS_ON"
	EdgeReferences                 EdgeKind = "REFERENCES"
	EdgeTests                      EdgeKind = "TESTS"
	EdgeExposes                    EdgeKind = "EXPOSES"
	EdgeOwnedBy                    EdgeKind = "OWNED_BY"
	EdgeAssociatedWithArchitecture EdgeKind = "ASSOCIATED_WITH_ARCHITECTURE"
	EdgeCoChangesWith              EdgeKind = "CO_CHANGES_WITH"

	FreshnessCurrent Freshness = "CURRENT"
	FreshnessStale   Freshness = "STALE"
	FreshnessUnknown Freshness = "UNKNOWN"
)

type Evidence struct {
	Kind      string `json:"kind"`
	Reference string `json:"reference"`
	Digest    string `json:"digest,omitempty"`
	Detail    string `json:"detail,omitempty"`
}

type Provenance struct {
	AnalyzerId      string     `json:"analyzer_id"`
	AnalyzerVersion string     `json:"analyzer_version"`
	Inputs          []string   `json:"inputs"`
	Evidence        []Evidence `json:"evidence"`
}

type Assertion struct {
	Class      EvidenceClass `json:"class"`
	Confidence Confidence    `json:"confidence,omitempty"`
	Provenance Provenance    `json:"provenance"`
}

type Node struct {
	Id        string    `json:"id"`
	Kind      NodeKind  `json:"kind"`
	Name      string    `json:"name"`
	Path      string    `json:"path,omitempty"`
	Language  string    `json:"language,omitempty"`
	Assertion Assertion `json:"assertion"`
}

type Edge struct {
	Id        string    `json:"id"`
	Kind      EdgeKind  `json:"kind"`
	From      string    `json:"from"`
	To        string    `json:"to"`
	Assertion Assertion `json:"assertion"`
}

type KnowledgeGap struct {
	Id          string     `json:"id"`
	Category    string     `json:"category"`
	Scope       string     `json:"scope"`
	Reason      string     `json:"reason"`
	Evidence    []Evidence `json:"evidence"`
	Consequence string     `json:"consequence"`
	Resolution  string     `json:"resolution"`
}

type TrackedEntry struct {
	Path          string `json:"path"`
	GitMode       string `json:"git_mode"`
	Kind          string `json:"kind"`
	ContentDigest string `json:"content_digest"`
	ObjectId      string `json:"object_id,omitempty"`
	Size          int64  `json:"size"`
	Available     bool   `json:"available"`
}

type UntrackedEntry struct {
	Status string `json:"status"`
	Path   string `json:"path"`
}

type SourceFingerprint struct {
	Version                  uint32           `json:"version"`
	HeadRevision             string           `json:"head_revision"`
	WorkingTreeState         string           `json:"working_tree_state"`
	SourceStateDigest        string           `json:"source_state_digest"`
	TrackedManifestDigest    string           `json:"tracked_manifest_digest"`
	UntrackedConditionDigest string           `json:"untracked_condition_digest"`
	Digest                   string           `json:"digest"`
	Complete                 bool             `json:"complete"`
	Tracked                  []TrackedEntry   `json:"tracked"`
	Untracked                []UntrackedEntry `json:"untracked"`
}

type BuildKey struct {
	SourceFingerprintDigest     string `json:"source_fingerprint_digest"`
	ModelSchemaVersion          uint32 `json:"model_schema_version"`
	AnalyzerSetDigest           string `json:"analyzer_set_digest"`
	AnalysisConfigurationDigest string `json:"analysis_configuration_digest"`
	Digest                      string `json:"digest"`
}

type Limits struct {
	MaximumFiles               int   `json:"maximum_files"`
	MaximumFileBytes           int64 `json:"maximum_file_bytes"`
	MaximumMetadataBytes       int64 `json:"maximum_metadata_bytes"`
	MaximumTotalBytes          int64 `json:"maximum_total_bytes"`
	MaximumAnalyzerOutputBytes int64 `json:"maximum_analyzer_output_bytes"`
	MaximumTotalAnalyzerBytes  int64 `json:"maximum_total_analyzer_bytes"`
	MaximumBuildDurationMillis int64 `json:"maximum_build_duration_millis"`
	MaximumNodes               int   `json:"maximum_nodes"`
	MaximumEdges               int   `json:"maximum_edges"`
	MaximumGaps                int   `json:"maximum_gaps"`
	MaximumHistoryCommits      int   `json:"maximum_history_commits"`
}

type Document struct {
	SchemaVersion  uint32            `json:"schema_version"`
	ModelId        string            `json:"model_id"`
	ProjectId      project.ProjectId `json:"project_id"`
	RepositoryRoot string            `json:"repository_root"`
	Fingerprint    SourceFingerprint `json:"source_fingerprint"`
	BuildKey       BuildKey          `json:"build_key"`
	ModelDigest    string            `json:"model_digest"`
	GeneratedAt    time.Time         `json:"generated_at"`
	Nodes          []Node            `json:"nodes"`
	Edges          []Edge            `json:"edges"`
	Gaps           []KnowledgeGap    `json:"knowledge_gaps"`
	Limits         Limits            `json:"limits"`
	Truncated      bool              `json:"truncated"`
}

type Model struct{ document Document }

func New(document Document) (Model, error) {
	document = cloneDocument(document)
	if document.SchemaVersion != SchemaVersion {
		return Model{}, fmt.Errorf("unsupported RepositoryModel schema version %d", document.SchemaVersion)
	}
	if !document.ProjectId.IsValid() || strings.TrimSpace(document.RepositoryRoot) == "" {
		return Model{}, fmt.Errorf("RepositoryModel requires valid ProjectId and RepositoryRoot")
	}
	if document.GeneratedAt.IsZero() {
		return Model{}, fmt.Errorf("RepositoryModel GeneratedAt is required")
	}
	if err := ValidateFingerprint(document.Fingerprint); err != nil {
		return Model{}, err
	}
	if err := ValidateBuildKey(document.BuildKey); err != nil {
		return Model{}, err
	}
	if err := validateGraph(document.Nodes, document.Edges, document.Gaps); err != nil {
		return Model{}, err
	}
	sortDocument(&document)
	expected := digestDocument(document)
	if document.ModelDigest != "" && document.ModelDigest != expected {
		return Model{}, fmt.Errorf("RepositoryModel digest mismatch")
	}
	document.ModelDigest = expected
	expectedId := "model-" + strings.TrimPrefix(expected, "sha256:")[:32]
	if document.ModelId != "" && document.ModelId != expectedId {
		return Model{}, fmt.Errorf("RepositoryModel identity mismatch")
	}
	document.ModelId = expectedId
	return Model{document: document}, nil
}

func (model Model) Document() Document           { return cloneDocument(model.document) }
func (model Model) ModelId() string              { return model.document.ModelId }
func (model Model) ProjectId() project.ProjectId { return model.document.ProjectId }
func (model Model) RepositoryRoot() string       { return model.document.RepositoryRoot }
func (model Model) Fingerprint() SourceFingerprint {
	return cloneFingerprint(model.document.Fingerprint)
}
func (model Model) BuildKey() BuildKey     { return model.document.BuildKey }
func (model Model) ModelDigest() string    { return model.document.ModelDigest }
func (model Model) GeneratedAt() time.Time { return model.document.GeneratedAt }
func (model Model) Nodes() []Node          { return cloneNodes(model.document.Nodes) }
func (model Model) Edges() []Edge          { return cloneEdges(model.document.Edges) }
func (model Model) Gaps() []KnowledgeGap   { return cloneGaps(model.document.Gaps) }
func (model Model) Limits() Limits         { return model.document.Limits }
func (model Model) Truncated() bool        { return model.document.Truncated }

func (model Model) MarshalJSON() ([]byte, error) { return json.Marshal(model.Document()) }

func (model *Model) UnmarshalJSON(data []byte) error {
	var document Document
	if err := json.Unmarshal(data, &document); err != nil {
		return err
	}
	value, err := New(document)
	if err != nil {
		return err
	}
	*model = value
	return nil
}

func ValidateFingerprint(value SourceFingerprint) error {
	if value.Version != 1 || strings.TrimSpace(value.HeadRevision) == "" {
		return fmt.Errorf("SourceFingerprint identity is incomplete")
	}
	for _, digest := range []string{value.SourceStateDigest, value.TrackedManifestDigest, value.UntrackedConditionDigest, value.Digest} {
		if !validDigest(digest) {
			return fmt.Errorf("SourceFingerprint contains invalid digest")
		}
	}
	if !slices.IsSortedFunc(value.Tracked, func(left, right TrackedEntry) int { return strings.Compare(left.Path, right.Path) }) ||
		!slices.IsSortedFunc(value.Untracked, func(left, right UntrackedEntry) int {
			if compared := strings.Compare(left.Path, right.Path); compared != 0 {
				return compared
			}
			return strings.Compare(left.Status, right.Status)
		}) {
		return fmt.Errorf("SourceFingerprint manifests must be deterministically sorted")
	}
	expectedTracked := DigestJSON(struct {
		Version uint32         `json:"version"`
		Entries []TrackedEntry `json:"entries"`
	}{1, value.Tracked})
	expectedUntracked := DigestJSON(struct {
		Version uint32           `json:"version"`
		Entries []UntrackedEntry `json:"entries"`
	}{1, value.Untracked})
	if value.TrackedManifestDigest != expectedTracked || value.UntrackedConditionDigest != expectedUntracked {
		return fmt.Errorf("SourceFingerprint manifest digest mismatch")
	}
	expected := DigestJSON(struct {
		Version                                 uint32 `json:"version"`
		Head, State, Source, Tracked, Untracked string
	}{1, value.HeadRevision, value.WorkingTreeState, value.SourceStateDigest, value.TrackedManifestDigest, value.UntrackedConditionDigest})
	if value.Digest != expected {
		return fmt.Errorf("SourceFingerprint digest mismatch")
	}
	return nil
}

func NewBuildKey(fingerprint SourceFingerprint, analyzerSetDigest, configurationDigest string) (BuildKey, error) {
	if err := ValidateFingerprint(fingerprint); err != nil {
		return BuildKey{}, err
	}
	if !validDigest(analyzerSetDigest) || !validDigest(configurationDigest) {
		return BuildKey{}, fmt.Errorf("RepositoryModel build-key inputs are invalid")
	}
	value := BuildKey{SourceFingerprintDigest: fingerprint.Digest, ModelSchemaVersion: SchemaVersion, AnalyzerSetDigest: analyzerSetDigest, AnalysisConfigurationDigest: configurationDigest}
	value.Digest = DigestJSON(struct {
		Version       uint32 `json:"version"`
		Source        string `json:"source"`
		Schema        uint32 `json:"schema"`
		Analyzers     string `json:"analyzers"`
		Configuration string `json:"configuration"`
	}{1, value.SourceFingerprintDigest, value.ModelSchemaVersion, value.AnalyzerSetDigest, value.AnalysisConfigurationDigest})
	return value, nil
}

func ValidateBuildKey(value BuildKey) error {
	if value.ModelSchemaVersion != SchemaVersion || !validDigest(value.SourceFingerprintDigest) || !validDigest(value.AnalyzerSetDigest) || !validDigest(value.AnalysisConfigurationDigest) || !validDigest(value.Digest) {
		return fmt.Errorf("RepositoryModel BuildKey is invalid or incompatible")
	}
	expected := DigestJSON(struct {
		Version       uint32 `json:"version"`
		Source        string `json:"source"`
		Schema        uint32 `json:"schema"`
		Analyzers     string `json:"analyzers"`
		Configuration string `json:"configuration"`
	}{1, value.SourceFingerprintDigest, value.ModelSchemaVersion, value.AnalyzerSetDigest, value.AnalysisConfigurationDigest})
	if expected != value.Digest {
		return fmt.Errorf("RepositoryModel BuildKey digest mismatch")
	}
	return nil
}

func CompareFreshness(recorded BuildKey, current *BuildKey, evidenceComplete bool) Freshness {
	if current == nil || !evidenceComplete || ValidateBuildKey(recorded) != nil || ValidateBuildKey(*current) != nil {
		return FreshnessUnknown
	}
	if recorded.Digest == current.Digest {
		return FreshnessCurrent
	}
	return FreshnessStale
}

func NodeId(kind NodeKind, identity string) string { return stableId("node", string(kind), identity) }
func EdgeId(kind EdgeKind, from, to string) string { return stableId("edge", string(kind), from, to) }
func GapId(category, scope, reason string) string  { return stableId("gap", category, scope, reason) }

func DigestJSON(value any) string {
	payload, err := json.Marshal(value)
	if err != nil {
		panic(fmt.Sprintf("canonical JSON value cannot be encoded: %v", err))
	}
	sum := sha256.Sum256(payload)
	return "sha256:" + hex.EncodeToString(sum[:])
}

func stableId(prefix string, values ...string) string {
	joined := strings.Join(append([]string{"praetor-" + prefix + "-v1"}, values...), "\x1f")
	sum := sha256.Sum256([]byte(joined))
	return prefix + "-" + hex.EncodeToString(sum[:16])
}

func validDigest(value string) bool {
	if len(value) != 71 || !strings.HasPrefix(value, "sha256:") {
		return false
	}
	_, err := hex.DecodeString(strings.TrimPrefix(value, "sha256:"))
	return err == nil
}

func validateGraph(nodes []Node, edges []Edge, gaps []KnowledgeGap) error {
	known := make(map[string]struct{}, len(nodes))
	for _, node := range nodes {
		if node.Id == "" || node.Name == "" || !knownNodeKind(node.Kind) {
			return fmt.Errorf("RepositoryModel contains invalid node")
		}
		if _, duplicate := known[node.Id]; duplicate {
			return fmt.Errorf("RepositoryModel contains duplicate node %q", node.Id)
		}
		known[node.Id] = struct{}{}
		if err := validateAssertion(node.Assertion); err != nil {
			return fmt.Errorf("node %q: %w", node.Id, err)
		}
	}
	edgeIds := make(map[string]struct{}, len(edges))
	for _, edge := range edges {
		if !knownEdgeKind(edge.Kind) {
			return fmt.Errorf("RepositoryModel contains invalid edge kind %q", edge.Kind)
		}
		if _, ok := known[edge.From]; !ok {
			return fmt.Errorf("edge %q has missing source", edge.Id)
		}
		if _, ok := known[edge.To]; !ok {
			return fmt.Errorf("edge %q has missing target", edge.Id)
		}
		if _, duplicate := edgeIds[edge.Id]; duplicate {
			return fmt.Errorf("RepositoryModel contains duplicate edge %q", edge.Id)
		}
		edgeIds[edge.Id] = struct{}{}
		if err := validateAssertion(edge.Assertion); err != nil {
			return fmt.Errorf("edge %q: %w", edge.Id, err)
		}
	}
	for _, gap := range gaps {
		if gap.Id == "" || strings.TrimSpace(gap.Category) == "" || strings.TrimSpace(gap.Scope) == "" || strings.TrimSpace(gap.Reason) == "" || strings.TrimSpace(gap.Consequence) == "" || strings.TrimSpace(gap.Resolution) == "" {
			return fmt.Errorf("RepositoryModel contains incomplete KnowledgeGap")
		}
	}
	return nil
}

func validateAssertion(assertion Assertion) error {
	if assertion.Class != EvidenceObserved && assertion.Class != EvidenceDerived && assertion.Class != EvidenceInferred {
		return fmt.Errorf("invalid evidence class %q", assertion.Class)
	}
	if assertion.Class == EvidenceInferred {
		if assertion.Confidence != ConfidenceHigh && assertion.Confidence != ConfidenceMedium && assertion.Confidence != ConfidenceLow {
			return fmt.Errorf("inferred assertion requires bounded confidence")
		}
	} else if assertion.Confidence != "" {
		return fmt.Errorf("deterministic assertion cannot carry confidence")
	}
	if strings.TrimSpace(assertion.Provenance.AnalyzerId) == "" || strings.TrimSpace(assertion.Provenance.AnalyzerVersion) == "" {
		return fmt.Errorf("assertion provenance is incomplete")
	}
	if len(assertion.Provenance.Inputs) == 0 || len(assertion.Provenance.Evidence) == 0 {
		return fmt.Errorf("assertion provenance requires inputs and evidence")
	}
	return nil
}

func knownNodeKind(kind NodeKind) bool {
	switch kind {
	case NodeRepository, NodeModule, NodeFile, NodeSymbol, NodeTest, NodeAPI, NodeExternalDependency, NodeOwner, NodeArchitectureElement, NodeUnresolvedReference:
		return true
	}
	return false
}
func knownEdgeKind(kind EdgeKind) bool {
	switch kind {
	case EdgeContains, EdgeDeclares, EdgeDependsOn, EdgeReferences, EdgeTests, EdgeExposes, EdgeOwnedBy, EdgeAssociatedWithArchitecture, EdgeCoChangesWith:
		return true
	}
	return false
}

func digestDocument(document Document) string {
	document.ModelDigest = ""
	document.ModelId = ""
	document.GeneratedAt = time.Time{}
	return DigestJSON(document)
}

func sortDocument(document *Document) {
	slices.SortFunc(document.Nodes, func(left, right Node) int { return strings.Compare(left.Id, right.Id) })
	slices.SortFunc(document.Edges, func(left, right Edge) int { return strings.Compare(left.Id, right.Id) })
	slices.SortFunc(document.Gaps, func(left, right KnowledgeGap) int { return strings.Compare(left.Id, right.Id) })
}

func cloneDocument(value Document) Document {
	value.Fingerprint = cloneFingerprint(value.Fingerprint)
	value.Nodes = cloneNodes(value.Nodes)
	value.Edges = cloneEdges(value.Edges)
	value.Gaps = cloneGaps(value.Gaps)
	return value
}
func cloneFingerprint(value SourceFingerprint) SourceFingerprint {
	value.Tracked = append([]TrackedEntry(nil), value.Tracked...)
	value.Untracked = append([]UntrackedEntry(nil), value.Untracked...)
	return value
}
func cloneNodes(values []Node) []Node {
	result := append([]Node(nil), values...)
	for i := range result {
		result[i].Assertion = cloneAssertion(result[i].Assertion)
	}
	return result
}
func cloneEdges(values []Edge) []Edge {
	result := append([]Edge(nil), values...)
	for i := range result {
		result[i].Assertion = cloneAssertion(result[i].Assertion)
	}
	return result
}
func cloneGaps(values []KnowledgeGap) []KnowledgeGap {
	result := append([]KnowledgeGap(nil), values...)
	for i := range result {
		result[i].Evidence = append([]Evidence(nil), result[i].Evidence...)
	}
	return result
}
func cloneAssertion(value Assertion) Assertion {
	value.Provenance.Inputs = append([]string(nil), value.Provenance.Inputs...)
	value.Provenance.Evidence = append([]Evidence(nil), value.Provenance.Evidence...)
	return value
}
