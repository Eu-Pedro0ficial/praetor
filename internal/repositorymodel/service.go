package repositorymodel

import (
	"encoding/json"
	"errors"
	"fmt"
	"path"
	"slices"
	"strings"
	"time"

	"github.com/Eu-Pedro0ficial/praetor/internal/project"
)

var (
	ErrCacheMiss         = errors.New("RepositoryModel cache miss")
	ErrCacheCorrupt      = errors.New("RepositoryModel cache is corrupt")
	ErrCacheIncompatible = errors.New("RepositoryModel cache is incompatible")
	ErrSourceChanged     = errors.New("repository source changed during analysis")
	ErrSourceIncomplete  = errors.New("repository source fingerprint is incomplete")
)

type Configuration struct {
	Limits            Limits   `json:"limits"`
	ExcludedPrefixes  []string `json:"excluded_prefixes"`
	GeneratedSuffixes []string `json:"generated_suffixes"`
}

func DefaultConfiguration() Configuration {
	return Configuration{
		Limits: Limits{
			MaximumFiles:               10000,
			MaximumFileBytes:           8 << 20,
			MaximumMetadataBytes:       64 << 20,
			MaximumTotalBytes:          256 << 20,
			MaximumAnalyzerOutputBytes: 8 << 20,
			MaximumTotalAnalyzerBytes:  256 << 20,
			MaximumBuildDurationMillis: 120000,
			MaximumNodes:               50000,
			MaximumEdges:               100000,
			MaximumGaps:                10000,
			MaximumHistoryCommits:      100,
		},
		ExcludedPrefixes:  []string{"vendor/", "node_modules/", "dist/", "build/", "target/"},
		GeneratedSuffixes: []string{".generated.go", ".gen.go", ".min.js"},
	}
}

func (configuration Configuration) Digest() string {
	copyValue := configuration
	copyValue.ExcludedPrefixes = append([]string(nil), configuration.ExcludedPrefixes...)
	copyValue.GeneratedSuffixes = append([]string(nil), configuration.GeneratedSuffixes...)
	slices.Sort(copyValue.ExcludedPrefixes)
	slices.Sort(copyValue.GeneratedSuffixes)
	return DigestJSON(struct {
		Version       uint32        `json:"version"`
		Configuration Configuration `json:"configuration"`
	}{1, copyValue})
}

type FileInput struct {
	Path          string
	GitMode       string
	Kind          string
	ContentDigest string
	Content       []byte
	Size          int64
	Available     bool
	Excluded      bool
	Exclusion     string
}

type HistoryCommit struct {
	Revision string
	Paths    []string
}

type SourceInspection struct {
	ProjectId      project.ProjectId
	RepositoryRoot string
	Fingerprint    SourceFingerprint
	Files          []FileInput
	History        []HistoryCommit
	Gaps           []KnowledgeGap
}

type SourceInspector func(project.ProjectId, string, Configuration) (SourceInspection, error)

type AnalyzerDescriptor struct {
	Id           string   `json:"id"`
	Version      string   `json:"version"`
	Capabilities []string `json:"capabilities"`
}

type PendingEdge struct {
	Kind       EdgeKind  `json:"kind"`
	From       string    `json:"from"`
	TargetKind NodeKind  `json:"target_kind"`
	Target     string    `json:"target"`
	Assertion  Assertion `json:"assertion"`
}

type OwnerRule struct {
	Pattern   string    `json:"pattern"`
	Owners    []string  `json:"owners"`
	Assertion Assertion `json:"assertion"`
}

type FileAnalysis struct {
	Path         string         `json:"path"`
	ContentKey   string         `json:"content_key"`
	Nodes        []Node         `json:"nodes"`
	Edges        []Edge         `json:"edges"`
	PendingEdges []PendingEdge  `json:"pending_edges"`
	OwnerRules   []OwnerRule    `json:"owner_rules"`
	Gaps         []KnowledgeGap `json:"knowledge_gaps"`
}

type Analyzer interface {
	Descriptor() AnalyzerDescriptor
	Supports(FileInput) bool
	Analyze(FileInput) (FileAnalysis, error)
}

type Cache interface {
	LoadModel(BuildKey) (Model, error)
	StoreModel(Model) error
	LoadFileAnalysis(path, contentKey, analyzerSetDigest string) (FileAnalysis, error)
	StoreFileAnalysis(FileAnalysis, string) error
	Close() error
}

type BuildStatistics struct {
	CacheHit      bool
	ReusedFiles   int
	AnalyzedFiles int
	Duration      time.Duration
}

type BuildResult struct {
	Model      Model
	Statistics BuildStatistics
}

type Service struct {
	projectId      project.ProjectId
	repositoryRoot string
	inspector      SourceInspector
	analyzers      []Analyzer
	cache          Cache
	configuration  Configuration
	clock          func() time.Time
	analyzerDigest string
}

func NewService(projectId project.ProjectId, repositoryRoot string, inspector SourceInspector, analyzers []Analyzer, cache Cache, configuration Configuration, clock func() time.Time) (*Service, error) {
	if !projectId.IsValid() || strings.TrimSpace(repositoryRoot) == "" {
		return nil, fmt.Errorf("repository model service requires Project association")
	}
	if inspector == nil || cache == nil || clock == nil {
		return nil, fmt.Errorf("repository model service dependencies are incomplete")
	}
	if len(analyzers) == 0 {
		return nil, fmt.Errorf("at least one bounded repository analyzer is required")
	}
	if err := validateConfiguration(configuration); err != nil {
		return nil, err
	}
	ordered := append([]Analyzer(nil), analyzers...)
	slices.SortFunc(ordered, func(left, right Analyzer) int { return strings.Compare(left.Descriptor().Id, right.Descriptor().Id) })
	descriptors := make([]AnalyzerDescriptor, len(ordered))
	seen := map[string]struct{}{}
	for index, analyzer := range ordered {
		descriptor := analyzer.Descriptor()
		if strings.TrimSpace(descriptor.Id) == "" || strings.TrimSpace(descriptor.Version) == "" {
			return nil, fmt.Errorf("analyzer descriptor is incomplete")
		}
		if _, duplicate := seen[descriptor.Id]; duplicate {
			return nil, fmt.Errorf("duplicate analyzer %q", descriptor.Id)
		}
		seen[descriptor.Id] = struct{}{}
		descriptor.Capabilities = append([]string(nil), descriptor.Capabilities...)
		slices.Sort(descriptor.Capabilities)
		descriptors[index] = descriptor
	}
	return &Service{projectId: projectId, repositoryRoot: repositoryRoot, inspector: inspector, analyzers: ordered, cache: cache, configuration: configuration, clock: clock, analyzerDigest: DigestJSON(struct {
		Version   uint32               `json:"version"`
		Analyzers []AnalyzerDescriptor `json:"analyzers"`
	}{1, descriptors})}, nil
}

func (service *Service) Build() (BuildResult, error) {
	started := time.Now()
	deadline := started.Add(time.Duration(service.configuration.Limits.MaximumBuildDurationMillis) * time.Millisecond)
	before, err := service.inspector(service.projectId, service.repositoryRoot, service.configuration)
	if err != nil {
		return BuildResult{}, fmt.Errorf("inspect repository before model build: %w", err)
	}
	if err := service.validateInspection(before); err != nil {
		return BuildResult{}, err
	}
	if !before.Fingerprint.Complete {
		return BuildResult{}, ErrSourceIncomplete
	}
	buildKey, err := NewBuildKey(before.Fingerprint, service.analyzerDigest, service.configuration.Digest())
	if err != nil {
		return BuildResult{}, err
	}
	if cached, loadError := service.cache.LoadModel(buildKey); loadError == nil {
		if cached.ProjectId() != service.projectId || cached.RepositoryRoot() != service.repositoryRoot {
			loadError = ErrCacheCorrupt
		} else {
			after, inspectError := service.inspector(service.projectId, service.repositoryRoot, service.configuration)
			if inspectError != nil {
				return BuildResult{}, fmt.Errorf("inspect repository after cache lookup: %w", inspectError)
			}
			if after.Fingerprint.Digest != before.Fingerprint.Digest {
				return BuildResult{}, ErrSourceChanged
			}
			return BuildResult{Model: cached, Statistics: BuildStatistics{CacheHit: true, Duration: time.Since(started)}}, nil
		}
	} else if !errors.Is(loadError, ErrCacheMiss) && !errors.Is(loadError, ErrCacheCorrupt) && !errors.Is(loadError, ErrCacheIncompatible) {
		return BuildResult{}, fmt.Errorf("load RepositoryModel cache: %w", loadError)
	}

	analyses := make([]FileAnalysis, 0, len(before.Files))
	statistics := BuildStatistics{}
	var analyzerOutputBytes int64
	for _, file := range before.Files {
		if time.Now().After(deadline) {
			return BuildResult{}, fmt.Errorf("repository analysis exceeded the configured %d ms build limit", service.configuration.Limits.MaximumBuildDurationMillis)
		}
		if file.Excluded || !file.Available || file.Kind != "regular" {
			continue
		}
		contentKey := DigestJSON(struct {
			Path, Mode, Digest string
			Size               int64
		}{file.Path, file.GitMode, file.ContentDigest, file.Size})
		cached, loadError := service.cache.LoadFileAnalysis(file.Path, contentKey, service.analyzerDigest)
		if loadError == nil {
			cachedBytes, validationError := validateFileAnalysis(cached, file, service.configuration.Limits)
			if validationError == nil && analyzerOutputBytes+cachedBytes <= service.configuration.Limits.MaximumTotalAnalyzerBytes {
				analyzerOutputBytes += cachedBytes
				analyses = append(analyses, cached)
				statistics.ReusedFiles++
				continue
			}
			if validationError == nil {
				return BuildResult{}, fmt.Errorf("repository analyzer output exceeds the configured %d-byte aggregate limit", service.configuration.Limits.MaximumTotalAnalyzerBytes)
			}
			loadError = ErrCacheCorrupt
		}
		if !errors.Is(loadError, ErrCacheMiss) && !errors.Is(loadError, ErrCacheCorrupt) && !errors.Is(loadError, ErrCacheIncompatible) {
			return BuildResult{}, loadError
		}
		analysis := FileAnalysis{Path: file.Path, ContentKey: contentKey}
		supported := false
		for _, analyzer := range service.analyzers {
			if time.Now().After(deadline) {
				return BuildResult{}, fmt.Errorf("repository analysis exceeded the configured %d ms build limit", service.configuration.Limits.MaximumBuildDurationMillis)
			}
			if !analyzer.Supports(file) {
				continue
			}
			supported = true
			partial, analyzeError := safelyAnalyze(analyzer, file)
			if analyzeError != nil {
				analysis.Gaps = append(analysis.Gaps, analyzerFailureGap(file.Path, analyzer.Descriptor(), analyzeError))
				continue
			}
			if validateError := validateAnalyzerPartial(partial, file, service.configuration.Limits); validateError != nil {
				analysis.Gaps = append(analysis.Gaps, analyzerFailureGap(file.Path, analyzer.Descriptor(), validateError))
				continue
			}
			analysis.Nodes = append(analysis.Nodes, partial.Nodes...)
			analysis.Edges = append(analysis.Edges, partial.Edges...)
			analysis.PendingEdges = append(analysis.PendingEdges, partial.PendingEdges...)
			analysis.OwnerRules = append(analysis.OwnerRules, partial.OwnerRules...)
			analysis.Gaps = append(analysis.Gaps, partial.Gaps...)
		}
		if !supported && sourceLike(file.Path) {
			analysis.Gaps = append(analysis.Gaps, KnowledgeGap{Id: GapId("unsupported-language", file.Path, "no bounded analyzer supports this source"), Category: "unsupported-language", Scope: file.Path, Reason: "no bounded analyzer supports this source", Evidence: []Evidence{{Kind: "tracked-file", Reference: file.Path, Digest: file.ContentDigest}}, Consequence: "relationships and impact may be incomplete", Resolution: "configure or implement an approved read-only analyzer"})
		}
		canonicalizeAnalysis(&analysis)
		analysisBytes, validationError := validateFileAnalysis(analysis, file, service.configuration.Limits)
		if validationError != nil {
			return BuildResult{}, fmt.Errorf("validate combined analyzer output for %q: %w", file.Path, validationError)
		}
		if analyzerOutputBytes+analysisBytes > service.configuration.Limits.MaximumTotalAnalyzerBytes {
			return BuildResult{}, fmt.Errorf("repository analyzer output exceeds the configured %d-byte aggregate limit", service.configuration.Limits.MaximumTotalAnalyzerBytes)
		}
		analyzerOutputBytes += analysisBytes
		if storeError := service.cache.StoreFileAnalysis(analysis, service.analyzerDigest); storeError != nil {
			return BuildResult{}, fmt.Errorf("cache file analysis: %w", storeError)
		}
		analyses = append(analyses, analysis)
		statistics.AnalyzedFiles++
	}
	if time.Now().After(deadline) {
		return BuildResult{}, fmt.Errorf("repository analysis exceeded the configured %d ms build limit", service.configuration.Limits.MaximumBuildDurationMillis)
	}

	model, err := assemble(service.projectId, service.repositoryRoot, before, buildKey, analyses, service.configuration.Limits, service.clock().UTC())
	if err != nil {
		return BuildResult{}, err
	}
	after, err := service.inspector(service.projectId, service.repositoryRoot, service.configuration)
	if err != nil {
		return BuildResult{}, fmt.Errorf("inspect repository after model build: %w", err)
	}
	if after.Fingerprint.Digest != before.Fingerprint.Digest {
		return BuildResult{}, ErrSourceChanged
	}
	if !after.Fingerprint.Complete {
		return BuildResult{}, ErrSourceIncomplete
	}
	if err := service.cache.StoreModel(model); err != nil {
		return BuildResult{}, fmt.Errorf("cache RepositoryModel: %w", err)
	}
	statistics.Duration = time.Since(started)
	return BuildResult{Model: model, Statistics: statistics}, nil
}

func safelyAnalyze(analyzer Analyzer, file FileInput) (analysis FileAnalysis, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			err = fmt.Errorf("bounded analyzer panic: %v", recovered)
		}
	}()
	return analyzer.Analyze(file)
}

func (service *Service) Freshness(model Model) (Freshness, error) {
	if model.ProjectId() != service.projectId || model.RepositoryRoot() != service.repositoryRoot {
		return FreshnessUnknown, nil
	}
	inspection, err := service.inspector(service.projectId, service.repositoryRoot, service.configuration)
	if err != nil {
		return FreshnessUnknown, err
	}
	current, keyError := NewBuildKey(inspection.Fingerprint, service.analyzerDigest, service.configuration.Digest())
	if keyError != nil {
		return FreshnessUnknown, nil
	}
	return CompareFreshness(model.BuildKey(), &current, inspection.Fingerprint.Complete), nil
}

func (service *Service) FreshnessForBuildKey(recorded BuildKey) (Freshness, error) {
	inspection, err := service.inspector(service.projectId, service.repositoryRoot, service.configuration)
	if err != nil {
		return FreshnessUnknown, err
	}
	current, keyError := NewBuildKey(inspection.Fingerprint, service.analyzerDigest, service.configuration.Digest())
	if keyError != nil {
		return FreshnessUnknown, nil
	}
	return CompareFreshness(recorded, &current, inspection.Fingerprint.Complete), nil
}

func (service *Service) Close() error {
	if service == nil || service.cache == nil {
		return nil
	}
	return service.cache.Close()
}
func (service *Service) AnalyzerSetDigest() string   { return service.analyzerDigest }
func (service *Service) ConfigurationDigest() string { return service.configuration.Digest() }

func (service *Service) validateInspection(value SourceInspection) error {
	if value.ProjectId != service.projectId || value.RepositoryRoot != service.repositoryRoot {
		return fmt.Errorf("repository inspection association mismatch")
	}
	if len(value.Files) > service.configuration.Limits.MaximumFiles {
		return fmt.Errorf("repository contains %d tracked files; limit is %d", len(value.Files), service.configuration.Limits.MaximumFiles)
	}
	return ValidateFingerprint(value.Fingerprint)
}

func validateConfiguration(value Configuration) error {
	limits := value.Limits
	if limits.MaximumFiles <= 0 || limits.MaximumFileBytes <= 0 || limits.MaximumMetadataBytes <= 0 || limits.MaximumTotalBytes <= 0 || limits.MaximumAnalyzerOutputBytes <= 0 || limits.MaximumTotalAnalyzerBytes <= 0 || limits.MaximumBuildDurationMillis <= 0 || limits.MaximumNodes <= 0 || limits.MaximumEdges <= 0 || limits.MaximumGaps <= 0 || limits.MaximumHistoryCommits < 0 {
		return fmt.Errorf("repository analysis limits must be positive")
	}
	return nil
}

func validateAnalyzerPartial(value FileAnalysis, file FileInput, limits Limits) error {
	if value.Path != file.Path {
		return fmt.Errorf("analyzer output path %q does not match input %q", value.Path, file.Path)
	}
	if len(value.Nodes) > limits.MaximumNodes || len(value.Edges) > limits.MaximumEdges || len(value.PendingEdges) > limits.MaximumEdges || len(value.Gaps) > limits.MaximumGaps || len(value.OwnerRules) > limits.MaximumNodes {
		return fmt.Errorf("analyzer output exceeds configured graph cardinality")
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return fmt.Errorf("encode bounded analyzer output: %w", err)
	}
	if int64(len(payload)) > limits.MaximumAnalyzerOutputBytes {
		return fmt.Errorf("analyzer output exceeds configured %d-byte limit", limits.MaximumAnalyzerOutputBytes)
	}
	return nil
}

func validateFileAnalysis(value FileAnalysis, file FileInput, limits Limits) (int64, error) {
	if value.ContentKey == "" {
		return 0, fmt.Errorf("analyzer output has no exact content key")
	}
	if err := validateAnalyzerPartial(value, file, limits); err != nil {
		return 0, err
	}
	payload, err := json.Marshal(value)
	if err != nil {
		return 0, err
	}
	return int64(len(payload)), nil
}

func analyzerFailureGap(file string, descriptor AnalyzerDescriptor, failure error) KnowledgeGap {
	reason := fmt.Sprintf("analyzer %s failed safely: %s", descriptor.Id, failure)
	if len(reason) > 512 {
		reason = reason[:512]
	}
	return KnowledgeGap{Id: GapId("analyzer-failure", file, reason), Category: "analyzer-failure", Scope: file, Reason: reason, Evidence: []Evidence{{Kind: "analyzer", Reference: descriptor.Id + "@" + descriptor.Version}}, Consequence: "relationships and impact may be incomplete", Resolution: "inspect the bounded analyzer failure and source syntax"}
}

func sourceLike(file string) bool {
	extension := strings.ToLower(path.Ext(file))
	switch extension {
	case ".go", ".java", ".kt", ".kts", ".js", ".jsx", ".ts", ".tsx", ".py", ".rs", ".c", ".cc", ".cpp", ".h", ".hpp", ".rb", ".php", ".swift", ".scala", ".cs":
		return true
	}
	return false
}

func canonicalizeAnalysis(value *FileAnalysis) {
	slices.SortFunc(value.Nodes, func(left, right Node) int { return strings.Compare(left.Id, right.Id) })
	slices.SortFunc(value.Edges, func(left, right Edge) int { return strings.Compare(left.Id, right.Id) })
	slices.SortFunc(value.PendingEdges, func(left, right PendingEdge) int {
		if compared := strings.Compare(left.From, right.From); compared != 0 {
			return compared
		}
		if compared := strings.Compare(string(left.Kind), string(right.Kind)); compared != 0 {
			return compared
		}
		return strings.Compare(left.Target, right.Target)
	})
	slices.SortFunc(value.Gaps, func(left, right KnowledgeGap) int { return strings.Compare(left.Id, right.Id) })
}

func assemble(projectId project.ProjectId, repositoryRoot string, inspection SourceInspection, buildKey BuildKey, analyses []FileAnalysis, limits Limits, generatedAt time.Time) (Model, error) {
	repositoryId := NodeId(NodeRepository, string(projectId))
	rootAssertion := observedAssertion("repository-inspector", "1", repositoryRoot, Evidence{Kind: "git-repository", Reference: repositoryRoot, Digest: inspection.Fingerprint.Digest})
	nodes := []Node{{Id: repositoryId, Kind: NodeRepository, Name: "repository", Assertion: rootAssertion}}
	edges := make([]Edge, 0, len(inspection.Files)*2)
	gaps := append([]KnowledgeGap(nil), inspection.Gaps...)
	fileIds := make(map[string]string, len(inspection.Files))
	for _, file := range inspection.Files {
		id := NodeId(NodeFile, file.Path)
		fileIds[file.Path] = id
		assertion := observedAssertion("repository-inspector", "1", file.Path, Evidence{Kind: "tracked-entry", Reference: file.Path, Digest: file.ContentDigest, Detail: file.GitMode})
		nodes = append(nodes, Node{Id: id, Kind: NodeFile, Name: path.Base(file.Path), Path: file.Path, Assertion: assertion})
		edges = append(edges, Edge{Id: EdgeId(EdgeContains, repositoryId, id), Kind: EdgeContains, From: repositoryId, To: id, Assertion: assertion})
		if file.Excluded {
			gaps = append(gaps, KnowledgeGap{Id: GapId("excluded-input", file.Path, file.Exclusion), Category: "excluded-input", Scope: file.Path, Reason: file.Exclusion, Evidence: []Evidence{{Kind: "tracked-file", Reference: file.Path, Digest: file.ContentDigest}}, Consequence: "relationships and impact may be incomplete", Resolution: "change the analysis configuration under governance or inspect explicitly"})
		}
		if !file.Available {
			gaps = append(gaps, KnowledgeGap{Id: GapId("unavailable-input", file.Path, "tracked content was unavailable"), Category: "unavailable-input", Scope: file.Path, Reason: "tracked content was unavailable or exceeded a bounded input limit", Evidence: []Evidence{{Kind: "tracked-entry", Reference: file.Path, Digest: file.ContentDigest}}, Consequence: "content relationships cannot be established", Resolution: "restore readable bounded tracked content and rebuild"})
		}
	}
	for _, untracked := range inspection.Fingerprint.Untracked {
		gaps = append(gaps, KnowledgeGap{Id: GapId("excluded-untracked-content", untracked.Path, "untracked content is excluded by ADR-039"), Category: "excluded-untracked-content", Scope: untracked.Path, Reason: "untracked content is excluded by ADR-039", Evidence: []Evidence{{Kind: "git-status", Reference: untracked.Path, Detail: untracked.Status}}, Consequence: "Praetor cannot conclude that the entry has no impact", Resolution: "track the entry or remove it, then rebuild"})
	}
	for _, analysis := range analyses {
		nodes = append(nodes, analysis.Nodes...)
		edges = append(edges, analysis.Edges...)
		gaps = append(gaps, analysis.Gaps...)
	}
	nodes = deduplicateNodes(nodes)
	aliases := buildAliases(nodes)
	for _, analysis := range analyses {
		for _, pending := range analysis.PendingEdges {
			target := resolveTarget(pending.TargetKind, pending.Target, aliases)
			if target == "" {
				unresolvedId := NodeId(NodeUnresolvedReference, string(pending.TargetKind)+":"+pending.Target)
				nodes = append(nodes, Node{Id: unresolvedId, Kind: NodeUnresolvedReference, Name: pending.Target, Assertion: pending.Assertion})
				edges = append(edges, Edge{Id: EdgeId(pending.Kind, pending.From, unresolvedId), Kind: pending.Kind, From: pending.From, To: unresolvedId, Assertion: pending.Assertion})
				gaps = append(gaps, KnowledgeGap{Id: GapId("unresolved-reference", pending.Target, pending.From), Category: "unresolved-reference", Scope: pending.Target, Reason: "an analyzer could not resolve a repository relationship target", Evidence: append([]Evidence(nil), pending.Assertion.Provenance.Evidence...), Consequence: "downstream impact may be incomplete", Resolution: "provide supported declarations or inspect the reference"})
				continue
			}
			edges = append(edges, Edge{Id: EdgeId(pending.Kind, pending.From, target), Kind: pending.Kind, From: pending.From, To: target, Assertion: pending.Assertion})
		}
	}
	nodes = deduplicateNodes(nodes)
	for _, node := range nodes {
		if node.Kind == NodeModule {
			edges = append(edges, Edge{Id: EdgeId(EdgeContains, repositoryId, node.Id), Kind: EdgeContains, From: repositoryId, To: node.Id, Assertion: node.Assertion})
		}
	}
	edges = append(edges, ownershipEdges(analyses, nodes)...)
	edges = append(edges, historyEdges(inspection.History, fileIds)...)
	edges = deduplicateEdges(edges)
	gaps = deduplicateGaps(gaps)
	truncated := false
	if len(nodes) > limits.MaximumNodes {
		nodes = nodes[:limits.MaximumNodes]
		truncated = true
	}
	allowed := make(map[string]struct{}, len(nodes))
	for _, node := range nodes {
		allowed[node.Id] = struct{}{}
	}
	edges = slices.DeleteFunc(edges, func(edge Edge) bool { _, from := allowed[edge.From]; _, to := allowed[edge.To]; return !from || !to })
	if len(edges) > limits.MaximumEdges {
		edges = edges[:limits.MaximumEdges]
		truncated = true
	}
	if len(gaps) > limits.MaximumGaps {
		gaps = gaps[:limits.MaximumGaps]
		truncated = true
	}
	if truncated {
		gap := KnowledgeGap{Id: GapId("analysis-truncated", "repository", "configured graph limit reached"), Category: "analysis-truncated", Scope: "repository", Reason: "a configured graph or gap cardinality limit was reached", Evidence: []Evidence{{Kind: "analysis-limit", Reference: buildKey.Digest}}, Consequence: "impact is incomplete beyond the reported bounds", Resolution: "review bounded configuration and repository size"}
		if len(gaps) < limits.MaximumGaps {
			gaps = append(gaps, gap)
		} else if len(gaps) > 0 {
			gaps[len(gaps)-1] = gap
		}
	}
	return New(Document{SchemaVersion: SchemaVersion, ProjectId: projectId, RepositoryRoot: repositoryRoot, Fingerprint: inspection.Fingerprint, BuildKey: buildKey, GeneratedAt: generatedAt, Nodes: nodes, Edges: edges, Gaps: gaps, Limits: limits, Truncated: truncated})
}

func observedAssertion(analyzer, version, input string, evidence Evidence) Assertion {
	return Assertion{Class: EvidenceObserved, Provenance: Provenance{AnalyzerId: analyzer, AnalyzerVersion: version, Inputs: []string{input}, Evidence: []Evidence{evidence}}}
}

func deduplicateNodes(values []Node) []Node {
	byId := map[string]Node{}
	for _, value := range values {
		if _, exists := byId[value.Id]; !exists {
			byId[value.Id] = value
		}
	}
	result := make([]Node, 0, len(byId))
	for _, value := range byId {
		result = append(result, value)
	}
	slices.SortFunc(result, func(left, right Node) int { return strings.Compare(left.Id, right.Id) })
	return result
}
func deduplicateEdges(values []Edge) []Edge {
	byId := map[string]Edge{}
	for _, value := range values {
		if _, exists := byId[value.Id]; !exists {
			byId[value.Id] = value
		}
	}
	result := make([]Edge, 0, len(byId))
	for _, value := range byId {
		result = append(result, value)
	}
	slices.SortFunc(result, func(left, right Edge) int { return strings.Compare(left.Id, right.Id) })
	return result
}
func deduplicateGaps(values []KnowledgeGap) []KnowledgeGap {
	byId := map[string]KnowledgeGap{}
	for _, value := range values {
		if _, exists := byId[value.Id]; !exists {
			byId[value.Id] = value
		}
	}
	result := make([]KnowledgeGap, 0, len(byId))
	for _, value := range byId {
		result = append(result, value)
	}
	slices.SortFunc(result, func(left, right KnowledgeGap) int { return strings.Compare(left.Id, right.Id) })
	return result
}

func buildAliases(nodes []Node) map[string]string {
	result := map[string]string{}
	for _, node := range nodes {
		for _, alias := range []string{string(node.Kind) + ":" + node.Name, string(node.Kind) + ":" + node.Path, node.Path, node.Name} {
			if alias != "" {
				if previous, exists := result[alias]; exists && previous != node.Id {
					result[alias] = ""
				} else if !exists {
					result[alias] = node.Id
				}
			}
		}
	}
	return result
}
func resolveTarget(kind NodeKind, target string, aliases map[string]string) string {
	for _, candidate := range []string{string(kind) + ":" + target, target} {
		if id := aliases[candidate]; id != "" {
			return id
		}
	}
	if kind == NodeFile {
		for _, suffix := range []string{".go", ".java", ".kt", ".ts", ".tsx", ".js", ".jsx", "/index.ts", "/index.js"} {
			if id := aliases[string(kind)+":"+target+suffix]; id != "" {
				return id
			}
		}
	}
	if kind == NodeModule {
		match := ""
		for alias, id := range aliases {
			if id == "" || !strings.HasPrefix(alias, string(kind)+":") {
				continue
			}
			name := strings.TrimPrefix(alias, string(kind)+":")
			if target == name || strings.HasSuffix(target, "/"+name) || strings.HasPrefix(target, name+".") {
				if match != "" && match != id {
					return ""
				}
				match = id
			}
		}
		return match
	}
	return ""
}

func ownershipEdges(analyses []FileAnalysis, nodes []Node) []Edge {
	var rules []OwnerRule
	for _, analysis := range analyses {
		rules = append(rules, analysis.OwnerRules...)
	}
	if len(rules) == 0 {
		return nil
	}
	result := []Edge{}
	for _, node := range nodes {
		if node.Kind != NodeFile {
			continue
		}
		for _, rule := range rules {
			if !ownerPatternMatches(rule.Pattern, node.Path) {
				continue
			}
			for _, owner := range rule.Owners {
				ownerId := NodeId(NodeOwner, owner)
				result = append(result, Edge{Id: EdgeId(EdgeOwnedBy, node.Id, ownerId), Kind: EdgeOwnedBy, From: node.Id, To: ownerId, Assertion: rule.Assertion})
			}
		}
	}
	return result
}

func ownerPatternMatches(patternValue, candidate string) bool {
	patternValue = strings.TrimPrefix(strings.TrimSpace(patternValue), "/")
	if patternValue == "" {
		return false
	}
	if strings.HasSuffix(patternValue, "/") {
		return strings.HasPrefix(candidate, patternValue)
	}
	if matched, err := path.Match(patternValue, candidate); err == nil && matched {
		return true
	}
	return candidate == patternValue || strings.HasPrefix(candidate, strings.TrimSuffix(patternValue, "*"))
}

func historyEdges(commits []HistoryCommit, fileIds map[string]string) []Edge {
	counts := map[string]int{}
	for _, commit := range commits {
		paths := append([]string(nil), commit.Paths...)
		slices.Sort(paths)
		paths = slices.Compact(paths)
		if len(paths) > 32 {
			paths = paths[:32]
		}
		for left := 0; left < len(paths); left++ {
			for right := left + 1; right < len(paths); right++ {
				if fileIds[paths[left]] != "" && fileIds[paths[right]] != "" {
					counts[paths[left]+"\x00"+paths[right]]++
				}
			}
		}
	}
	result := []Edge{}
	for pair, count := range counts {
		if count < 2 {
			continue
		}
		parts := strings.Split(pair, "\x00")
		left, right := fileIds[parts[0]], fileIds[parts[1]]
		confidence := ConfidenceLow
		if count >= 5 {
			confidence = ConfidenceMedium
		}
		if count >= 10 {
			confidence = ConfidenceHigh
		}
		assertion := Assertion{Class: EvidenceInferred, Confidence: confidence, Provenance: Provenance{AnalyzerId: "bounded-git-history", AnalyzerVersion: "1", Inputs: []string{parts[0], parts[1]}, Evidence: []Evidence{{Kind: "co-change-count", Reference: fmt.Sprint(count), Detail: "bounded committed history"}}}}
		result = append(result, Edge{Id: EdgeId(EdgeCoChangesWith, left, right), Kind: EdgeCoChangesWith, From: left, To: right, Assertion: assertion})
		result = append(result, Edge{Id: EdgeId(EdgeCoChangesWith, right, left), Kind: EdgeCoChangesWith, From: right, To: left, Assertion: assertion})
	}
	return result
}
