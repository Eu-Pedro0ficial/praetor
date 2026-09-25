package repositorymodel

import (
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/Eu-Pedro0ficial/praetor/internal/project"
)

const testProjectId project.ProjectId = "01890c29-7a78-7abc-8def-0123456789ab"

type noOpAnalyzer struct{}

func (noOpAnalyzer) Descriptor() AnalyzerDescriptor {
	return AnalyzerDescriptor{Id: "noop", Version: "1", Capabilities: []string{"fixture"}}
}
func (noOpAnalyzer) Supports(FileInput) bool                 { return false }
func (noOpAnalyzer) Analyze(FileInput) (FileAnalysis, error) { return FileAnalysis{}, nil }

type panicAnalyzer struct{ noOpAnalyzer }

func (panicAnalyzer) Supports(FileInput) bool                 { return true }
func (panicAnalyzer) Analyze(FileInput) (FileAnalysis, error) { panic("untrusted parser panic") }

type oversizedAnalyzer struct{ noOpAnalyzer }

func (oversizedAnalyzer) Supports(FileInput) bool { return true }
func (oversizedAnalyzer) Analyze(file FileInput) (FileAnalysis, error) {
	return FileAnalysis{Path: file.Path, Nodes: []Node{{}, {}}}, nil
}

type memoryCache struct{ modelStored bool }

func (*memoryCache) LoadModel(BuildKey) (Model, error) { return Model{}, ErrCacheMiss }
func (cache *memoryCache) StoreModel(Model) error      { cache.modelStored = true; return nil }
func (*memoryCache) LoadFileAnalysis(string, string, string) (FileAnalysis, error) {
	return FileAnalysis{}, ErrCacheMiss
}
func (*memoryCache) StoreFileAnalysis(FileAnalysis, string) error { return nil }
func (*memoryCache) Close() error                                 { return nil }

func TestBuildRefusesMixedPrePostSourceAndDoesNotPublishCache(t *testing.T) {
	first := testInspection("first", true)
	second := testInspection("second", true)
	calls := 0
	inspector := func(project.ProjectId, string, Configuration) (SourceInspection, error) {
		calls++
		if calls == 1 {
			return first, nil
		}
		return second, nil
	}
	cache := &memoryCache{}
	service, err := NewService(testProjectId, "/fixture", inspector, []Analyzer{noOpAnalyzer{}}, cache, DefaultConfiguration(), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Build(); !errors.Is(err, ErrSourceChanged) {
		t.Fatalf("mixed-source build error=%v", err)
	}
	if cache.modelStored {
		t.Fatal("mixed-source model was published to cache")
	}
}

func TestBuildRefusesIncompleteFingerprintAsCurrent(t *testing.T) {
	inspection := testInspection("incomplete", false)
	service, err := NewService(testProjectId, "/fixture", func(project.ProjectId, string, Configuration) (SourceInspection, error) { return inspection, nil }, []Analyzer{noOpAnalyzer{}}, &memoryCache{}, DefaultConfiguration(), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Build(); !errors.Is(err, ErrSourceIncomplete) {
		t.Fatalf("incomplete build error=%v", err)
	}
}

func TestAnalyzerPanicBecomesKnowledgeGap(t *testing.T) {
	inspection := testInspection("panic", true)
	service, err := NewService(testProjectId, "/fixture", func(project.ProjectId, string, Configuration) (SourceInspection, error) { return inspection, nil }, []Analyzer{panicAnalyzer{}}, &memoryCache{}, DefaultConfiguration(), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	built, err := service.Build()
	if err != nil {
		t.Fatal(err)
	}
	if len(built.Model.Gaps()) != 1 || built.Model.Gaps()[0].Category != "analyzer-failure" {
		t.Fatalf("panic gaps=%#v", built.Model.Gaps())
	}
}

func TestAnalyzerOutputBeyondConfiguredCardinalityBecomesKnowledgeGap(t *testing.T) {
	inspection := testInspection("oversized", true)
	configuration := DefaultConfiguration()
	configuration.Limits.MaximumNodes = 1
	service, err := NewService(testProjectId, "/fixture", func(project.ProjectId, string, Configuration) (SourceInspection, error) { return inspection, nil }, []Analyzer{oversizedAnalyzer{}}, &memoryCache{}, configuration, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	built, err := service.Build()
	if err != nil {
		t.Fatal(err)
	}
	categories := map[string]bool{}
	for _, gap := range built.Model.Gaps() {
		categories[gap.Category] = true
	}
	if !categories["analyzer-failure"] || !categories["analysis-truncated"] {
		t.Fatalf("bounded analyzer gaps=%#v", built.Model.Gaps())
	}
}

func TestBuildRejectsAggregateAnalyzerOutputBeyondConfiguredBound(t *testing.T) {
	inspection := testInspection("aggregate-output", true)
	configuration := DefaultConfiguration()
	configuration.Limits.MaximumTotalAnalyzerBytes = 1
	service, err := NewService(testProjectId, "/fixture", func(project.ProjectId, string, Configuration) (SourceInspection, error) { return inspection, nil }, []Analyzer{noOpAnalyzer{}}, &memoryCache{}, configuration, time.Now)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.Build(); err == nil || !strings.Contains(err.Error(), "aggregate limit") {
		t.Fatalf("aggregate analyzer output error=%v", err)
	}
}

func testInspection(identity string, complete bool) SourceInspection {
	entry := TrackedEntry{Path: "README.md", GitMode: "100644", Kind: "regular", ContentDigest: DigestJSON(identity), ObjectId: strings.Repeat("a", 40), Size: 1, Available: complete}
	tracked := []TrackedEntry{entry}
	trackedDigest := DigestJSON(struct {
		Version uint32         `json:"version"`
		Entries []TrackedEntry `json:"entries"`
	}{1, tracked})
	untrackedDigest := DigestJSON(struct {
		Version uint32           `json:"version"`
		Entries []UntrackedEntry `json:"entries"`
	}{1, nil})
	sourceDigest := DigestJSON("source:" + identity)
	fingerprint := SourceFingerprint{Version: 1, HeadRevision: strings.Repeat("b", 40), WorkingTreeState: "clean", SourceStateDigest: sourceDigest, TrackedManifestDigest: trackedDigest, UntrackedConditionDigest: untrackedDigest, Complete: complete, Tracked: tracked}
	fingerprint.Digest = DigestJSON(struct {
		Version                                 uint32 `json:"version"`
		Head, State, Source, Tracked, Untracked string
	}{1, fingerprint.HeadRevision, fingerprint.WorkingTreeState, fingerprint.SourceStateDigest, trackedDigest, untrackedDigest})
	return SourceInspection{ProjectId: testProjectId, RepositoryRoot: "/fixture", Fingerprint: fingerprint, Files: []FileInput{{Path: entry.Path, GitMode: entry.GitMode, Kind: entry.Kind, ContentDigest: entry.ContentDigest, Content: []byte(identity), Size: 1, Available: complete}}}
}
