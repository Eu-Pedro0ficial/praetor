package impact

import (
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"github.com/Eu-Pedro0ficial/praetor/internal/project"
	"github.com/Eu-Pedro0ficial/praetor/internal/repositorymodel"
	"github.com/Eu-Pedro0ficial/praetor/internal/source"
)

const impactProjectId project.ProjectId = "01890c29-7a78-7abc-8def-0123456789ab"

func TestExplainableImpactIsBoundedCycleSafeAndDoesNotExpandApprovedScope(t *testing.T) {
	model := impactFixture(t, false)
	current, err := change.New("impact-change", impactProjectId, "change core", time.Unix(100, 0))
	if err != nil {
		t.Fatal(err)
	}
	report, err := Analyze(current, model, Request{Expected: []string{"a.go"}, Protected: []string{"protected.go"}}, Configuration{MaximumDepth: 10, MaximumItems: 20, MaximumPathSteps: 10}, time.Unix(200, 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(report.Items) > len(model.Nodes()) {
		t.Fatalf("cycle expanded impact indefinitely: %d", len(report.Items))
	}
	for _, pathValue := range []string{"a.go", "b.go", "c.go", "a_test.go", "protected.go"} {
		if !hasImpactPath(report.Items, pathValue) {
			t.Fatalf("missing impacted path %s: %#v", pathValue, report.Items)
		}
	}
	for _, item := range report.Items {
		if item.Element.Path == "b.go" && len(item.Explanation) == 0 {
			t.Fatal("dependency impact has no explanation path")
		}
		if item.Basis != repositorymodel.EvidenceInferred && item.Confidence != "" {
			t.Fatal("deterministic impact has heuristic confidence")
		}
	}
	protected := itemForPath(t, report.Items, "protected.go")
	if protected.Classification != Protected {
		t.Fatalf("protected classification=%s", protected.Classification)
	}
	testItem := itemForPath(t, report.Items, "a_test.go")
	if !containsRelation(testItem.Explanation, repositorymodel.EdgeTests) {
		t.Fatal("affected test lacks TESTS evidence")
	}
	if !report.Risk.Advisory || report.Risk.Overall != RiskHigh {
		t.Fatalf("risk=%#v", report.Risk)
	}

	snapshot := sourceSnapshot(t)
	analysis, err := source.AnalyzeImpact(current, snapshot, source.ScopeRequest{Expected: []string{"a.go"}})
	if err != nil {
		t.Fatal(err)
	}
	approved, err := source.EstablishApprovedScope(analysis)
	if err != nil {
		t.Fatal(err)
	}
	if !slices.Equal(approved.Surface().ExpectedPaths(), []source.RepositoryPath{"a.go"}) || len(approved.Surface().PossiblePaths()) != 0 {
		t.Fatal("impact analysis mutated ApprovedScope")
	}
}

func TestKnowledgeGapProducesUncertainImpactAndUnorderedRisk(t *testing.T) {
	model := impactFixture(t, true)
	current, _ := change.New("gap-change", impactProjectId, "change unknown", time.Unix(100, 0))
	report, err := Analyze(current, model, Request{Expected: []string{"a.go"}}, DefaultConfiguration(), time.Unix(200, 0))
	if err != nil {
		t.Fatal(err)
	}
	if report.Risk.Overall != RiskIndeterminate || len(report.KnowledgeGaps) == 0 {
		t.Fatalf("risk=%s gaps=%d", report.Risk.Overall, len(report.KnowledgeGaps))
	}
	if _, ordered := CompareSeverity(RiskIndeterminate, RiskHigh); ordered {
		t.Fatal("INDETERMINATE entered severity ordering")
	}
	if compared, ordered := CompareSeverity(RiskLow, RiskHigh); !ordered || compared >= 0 {
		t.Fatal("LOW < HIGH ordering is broken")
	}
	for _, dimension := range report.Risk.Dimensions {
		if dimension.Name == "uncertainty" && dimension.Disposition != "insufficient" {
			t.Fatal("gap lowered uncertainty")
		}
	}
}

func TestTraversalLimitCreatesExplicitGap(t *testing.T) {
	model := impactFixture(t, false)
	current, _ := change.New("bounded-change", impactProjectId, "bounded", time.Unix(100, 0))
	report, err := Analyze(current, model, Request{Expected: []string{"a.go"}}, Configuration{MaximumDepth: 1, MaximumItems: 2, MaximumPathSteps: 1}, time.Unix(200, 0))
	if err != nil {
		t.Fatal(err)
	}
	if !report.Truncated || !hasGapCategory(report.KnowledgeGaps, "impact-truncated") || report.Risk.Overall != RiskIndeterminate {
		t.Fatalf("bounded report=%#v", report)
	}
}

func TestIsolatedAndBroadStartingSurfacesRemainVisible(t *testing.T) {
	model := impactFixture(t, false)
	current, _ := change.New("breadth-change", impactProjectId, "compare impact breadth", time.Unix(100, 0))
	isolated, err := Analyze(current, model, Request{Expected: []string{"isolated.go"}}, DefaultConfiguration(), time.Unix(200, 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(isolated.Items) != 1 || isolated.Items[0].Element.Path != "isolated.go" || len(isolated.Items[0].Explanation) != 0 {
		t.Fatalf("isolated impact=%#v", isolated.Items)
	}
	broad, err := Analyze(current, model, Request{Expected: []string{"a.go"}}, DefaultConfiguration(), time.Unix(200, 0))
	if err != nil {
		t.Fatal(err)
	}
	if len(broad.Items) <= 3 || broad.Risk.Overall == RiskLow {
		t.Fatalf("broad dependency impact not visible: items=%d risk=%s", len(broad.Items), broad.Risk.Overall)
	}
}

func TestProtectedClassificationPrecedence(t *testing.T) {
	model := impactFixture(t, false)
	current, _ := change.New("protected-precedence", impactProjectId, "protected precedence", time.Unix(100, 0))
	testCases := []struct {
		name               string
		request            Request
		pathValue          string
		wantClassification Classification
		wantError          bool
	}{
		{name: "expected only", request: Request{Expected: []string{"isolated.go"}}, pathValue: "isolated.go", wantClassification: Expected},
		{name: "possible only", request: Request{Possible: []string{"isolated.go"}}, pathValue: "isolated.go", wantClassification: Possible},
		{name: "protected only is not a start surface", request: Request{Protected: []string{"isolated.go"}}, wantError: true},
		{name: "expected and protected", request: Request{Expected: []string{"isolated.go"}, Protected: []string{"isolated.go"}}, pathValue: "isolated.go", wantClassification: Protected},
		{name: "possible and protected", request: Request{Possible: []string{"isolated.go"}, Protected: []string{"isolated.go"}}, pathValue: "isolated.go", wantClassification: Protected},
		{name: "traversal discovered and protected", request: Request{Expected: []string{"a.go"}, Protected: []string{"b.go"}}, pathValue: "b.go", wantClassification: Protected},
		{name: "duplicate protected entries", request: Request{Expected: []string{"isolated.go"}, Protected: []string{"isolated.go", "isolated.go"}}, pathValue: "isolated.go", wantClassification: Protected},
		{name: "non protected remains expected", request: Request{Expected: []string{"isolated.go"}, Protected: []string{"protected.go"}}, pathValue: "isolated.go", wantClassification: Expected},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			report, err := Analyze(current, model, testCase.request, DefaultConfiguration(), time.Unix(200, 0))
			if testCase.wantError {
				if err == nil {
					t.Fatal("protected-only request unexpectedly succeeded")
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			item := itemForPath(t, report.Items, testCase.pathValue)
			if item.Classification != testCase.wantClassification {
				t.Fatalf("classification=%s want=%s", item.Classification, testCase.wantClassification)
			}
			if testCase.wantClassification == Protected && report.Risk.Overall == RiskLow {
				t.Fatalf("protected overlap produced LOW risk: %#v", report.Risk)
			}
		})
	}
}

func TestMaximumItemsIsHardDeterministicInvariant(t *testing.T) {
	model := impactFixture(t, false)
	current, _ := change.New("maximum-items", impactProjectId, "bounded cardinality", time.Unix(100, 0))
	testCases := []struct {
		name          string
		request       Request
		maximum       int
		wantTruncated bool
		wantProtected bool
	}{
		{name: "maximum one", request: Request{Expected: []string{"isolated.go"}}, maximum: 1},
		{name: "multiple expected", request: Request{Expected: []string{"isolated.go", "isolated2.go"}}, maximum: 1, wantTruncated: true},
		{name: "expected and possible", request: Request{Expected: []string{"isolated.go"}, Possible: []string{"isolated2.go"}}, maximum: 1, wantTruncated: true},
		{name: "duplicates across lists", request: Request{Expected: []string{"isolated.go"}, Possible: []string{"isolated.go", "isolated.go"}}, maximum: 1},
		{name: "missing starts", request: Request{Expected: []string{"missing-a.go", "missing-b.go"}}, maximum: 1, wantTruncated: true},
		{name: "protected overlap", request: Request{Expected: []string{"isolated.go"}, Protected: []string{"isolated.go"}}, maximum: 1, wantProtected: true},
		{name: "traversal expansion", request: Request{Expected: []string{"a.go"}}, maximum: 1, wantTruncated: true},
		{name: "exactly N candidates", request: Request{Expected: []string{"isolated.go", "isolated2.go"}}, maximum: 2},
		{name: "N plus one candidates", request: Request{Expected: []string{"isolated.go", "isolated2.go", "isolated3.go"}}, maximum: 2, wantTruncated: true},
	}
	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			configuration := Configuration{MaximumDepth: 12, MaximumItems: testCase.maximum, MaximumPathSteps: 24}
			first, err := Analyze(current, model, testCase.request, configuration, time.Unix(200, 0))
			if err != nil {
				t.Fatal(err)
			}
			second, err := Analyze(current, model, testCase.request, configuration, time.Unix(200, 0))
			if err != nil {
				t.Fatal(err)
			}
			if len(first.Items) > testCase.maximum {
				t.Fatalf("len(Items)=%d exceeds MaximumItems=%d", len(first.Items), testCase.maximum)
			}
			if first.Truncated != testCase.wantTruncated {
				t.Fatalf("truncated=%t want=%t gaps=%#v items=%#v", first.Truncated, testCase.wantTruncated, first.KnowledgeGaps, first.Items)
			}
			if first.Truncated && !hasGapCategory(first.KnowledgeGaps, "impact-truncated") {
				t.Fatal("truncation lacks explicit impact-truncated KnowledgeGap")
			}
			if !reflect.DeepEqual(first.Items, second.Items) || first.ReportDigest != second.ReportDigest {
				t.Fatal("bounded selection is not deterministic")
			}
			if testCase.wantProtected {
				if itemForPath(t, first.Items, "isolated.go").Classification != Protected || first.Risk.Overall == RiskLow {
					t.Fatalf("protected bounded item/risk=%#v/%s", first.Items, first.Risk.Overall)
				}
			}
		})
	}
}

func TestImpactTraversalPerformanceEvidence(t *testing.T) {
	model := impactFixture(t, false)
	current, _ := change.New("measured-change", impactProjectId, "measure bounded impact", time.Unix(100, 0))
	started := time.Now()
	report, err := Analyze(current, model, Request{Expected: []string{"a.go"}}, DefaultConfiguration(), time.Unix(200, 0))
	if err != nil {
		t.Fatal(err)
	}
	duration := time.Since(started)
	if len(report.Items) == 0 || report.Truncated {
		t.Fatalf("measured traversal items=%d truncated=%t", len(report.Items), report.Truncated)
	}
	t.Logf("production impact evidence: duration=%s nodes=%d edges=%d items=%d gaps=%d", duration, len(model.Nodes()), len(model.Edges()), len(report.Items), len(report.KnowledgeGaps))
}

func impactFixture(t *testing.T, withGap bool) repositorymodel.Model {
	t.Helper()
	paths := []string{"a.go", "a_test.go", "b.go", "c.go", "isolated.go", "isolated2.go", "isolated3.go", "protected.go"}
	tracked := make([]repositorymodel.TrackedEntry, 0, len(paths))
	nodes := []repositorymodel.Node{}
	observed := func(reference string) repositorymodel.Assertion {
		return repositorymodel.Assertion{Class: repositorymodel.EvidenceObserved, Provenance: repositorymodel.Provenance{AnalyzerId: "fixture", AnalyzerVersion: "1", Inputs: []string{reference}, Evidence: []repositorymodel.Evidence{{Kind: "fixture", Reference: reference, Digest: repositorymodel.DigestJSON(reference)}}}}
	}
	rootId := repositorymodel.NodeId(repositorymodel.NodeRepository, string(impactProjectId))
	nodes = append(nodes, repositorymodel.Node{Id: rootId, Kind: repositorymodel.NodeRepository, Name: "repository", Assertion: observed("repository")})
	ids := map[string]string{}
	for _, pathValue := range paths {
		digest := repositorymodel.DigestJSON(pathValue)
		tracked = append(tracked, repositorymodel.TrackedEntry{Path: pathValue, GitMode: "100644", Kind: "regular", ContentDigest: digest, ObjectId: strings.Repeat("a", 40), Size: 10, Available: true})
		id := repositorymodel.NodeId(repositorymodel.NodeFile, pathValue)
		ids[pathValue] = id
		nodes = append(nodes, repositorymodel.Node{Id: id, Kind: repositorymodel.NodeFile, Name: pathValue, Path: pathValue, Language: "Go", Assertion: observed(pathValue)})
	}
	trackedDigest := repositorymodel.DigestJSON(struct {
		Version uint32                         `json:"version"`
		Entries []repositorymodel.TrackedEntry `json:"entries"`
	}{1, tracked})
	untrackedDigest := repositorymodel.DigestJSON(struct {
		Version uint32                           `json:"version"`
		Entries []repositorymodel.UntrackedEntry `json:"entries"`
	}{1, nil})
	sourceDigest := repositorymodel.DigestJSON("source")
	fingerprint := repositorymodel.SourceFingerprint{Version: 1, HeadRevision: strings.Repeat("b", 40), WorkingTreeState: "clean", SourceStateDigest: sourceDigest, TrackedManifestDigest: trackedDigest, UntrackedConditionDigest: untrackedDigest, Complete: true, Tracked: tracked}
	fingerprint.Digest = repositorymodel.DigestJSON(struct {
		Version                                 uint32 `json:"version"`
		Head, State, Source, Tracked, Untracked string
	}{1, fingerprint.HeadRevision, fingerprint.WorkingTreeState, fingerprint.SourceStateDigest, trackedDigest, untrackedDigest})
	key, err := repositorymodel.NewBuildKey(fingerprint, repositorymodel.DigestJSON("analyzers"), repositorymodel.DigestJSON("configuration"))
	if err != nil {
		t.Fatal(err)
	}
	derived := func(reference string) repositorymodel.Assertion {
		return repositorymodel.Assertion{Class: repositorymodel.EvidenceDerived, Provenance: repositorymodel.Provenance{AnalyzerId: "fixture", AnalyzerVersion: "1", Inputs: []string{reference}, Evidence: []repositorymodel.Evidence{{Kind: "relation", Reference: reference}}}}
	}
	inferred := func(reference string) repositorymodel.Assertion {
		value := derived(reference)
		value.Class, value.Confidence = repositorymodel.EvidenceInferred, repositorymodel.ConfidenceMedium
		return value
	}
	edge := func(kind repositorymodel.EdgeKind, from, to string, assertion repositorymodel.Assertion) repositorymodel.Edge {
		return repositorymodel.Edge{Id: repositorymodel.EdgeId(kind, ids[from], ids[to]), Kind: kind, From: ids[from], To: ids[to], Assertion: assertion}
	}
	edges := []repositorymodel.Edge{edge(repositorymodel.EdgeDependsOn, "b.go", "a.go", derived("b->a")), edge(repositorymodel.EdgeDependsOn, "c.go", "b.go", derived("c->b")), edge(repositorymodel.EdgeDependsOn, "a.go", "c.go", derived("cycle")), edge(repositorymodel.EdgeTests, "a_test.go", "a.go", derived("test")), edge(repositorymodel.EdgeDependsOn, "protected.go", "a.go", derived("protected")), edge(repositorymodel.EdgeCoChangesWith, "c.go", "a.go", inferred("history"))}
	var gaps []repositorymodel.KnowledgeGap
	if withGap {
		gaps = append(gaps, repositorymodel.KnowledgeGap{Id: repositorymodel.GapId("unsupported-language", "a.go", "missing semantic evidence"), Category: "unsupported-language", Scope: "a.go", Reason: "missing semantic evidence", Evidence: []repositorymodel.Evidence{{Kind: "fixture", Reference: "a.go"}}, Consequence: "impact may be incomplete", Resolution: "add analyzer"})
	}
	model, err := repositorymodel.New(repositorymodel.Document{SchemaVersion: repositorymodel.SchemaVersion, ProjectId: impactProjectId, RepositoryRoot: "/fixture", Fingerprint: fingerprint, BuildKey: key, GeneratedAt: time.Unix(150, 0), Nodes: nodes, Edges: edges, Gaps: gaps, Limits: repositorymodel.DefaultConfiguration().Limits})
	if err != nil {
		t.Fatal(err)
	}
	return model
}

func sourceSnapshot(t *testing.T) source.SourceSnapshot {
	t.Helper()
	snapshot, err := source.NewSourceSnapshot(impactProjectId, "/fixture", strings.Repeat("b", 40), source.WorkingTreeClean, []string{"a.go", "a_test.go", "b.go", "c.go", "isolated.go", "isolated2.go", "isolated3.go", "protected.go"}, source.SourceStateDigest(repositorymodel.DigestJSON("snapshot")))
	if err != nil {
		t.Fatal(err)
	}
	return snapshot
}
func hasImpactPath(items []Item, pathValue string) bool {
	for _, item := range items {
		if item.Element.Path == pathValue {
			return true
		}
	}
	return false
}
func itemForPath(t *testing.T, items []Item, pathValue string) Item {
	t.Helper()
	for _, item := range items {
		if item.Element.Path == pathValue {
			return item
		}
	}
	t.Fatalf("missing item %s", pathValue)
	return Item{}
}
func containsRelation(steps []ExplanationStep, relation repositorymodel.EdgeKind) bool {
	for _, step := range steps {
		if step.Relation == relation {
			return true
		}
	}
	return false
}
func hasGapCategory(gaps []repositorymodel.KnowledgeGap, category string) bool {
	for _, gap := range gaps {
		if gap.Category == category {
			return true
		}
	}
	return false
}
