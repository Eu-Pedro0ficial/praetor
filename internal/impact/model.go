// Package impact implements bounded explainable M1.2 impact and advisory risk.
package impact

import (
	"encoding/json"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"github.com/Eu-Pedro0ficial/praetor/internal/project"
	"github.com/Eu-Pedro0ficial/praetor/internal/repositorymodel"
	"github.com/Eu-Pedro0ficial/praetor/internal/source"
)

const ReportSchemaVersion uint32 = 1

type Classification string
type RiskLevel string

const (
	Expected  Classification = "EXPECTED"
	Possible  Classification = "POSSIBLE"
	Protected Classification = "PROTECTED"
	Uncertain Classification = "UNCERTAIN"

	RiskLow           RiskLevel = "LOW"
	RiskModerate      RiskLevel = "MODERATE"
	RiskHigh          RiskLevel = "HIGH"
	RiskIndeterminate RiskLevel = "INDETERMINATE"
)

type Configuration struct {
	MaximumDepth     int `json:"maximum_depth"`
	MaximumItems     int `json:"maximum_items"`
	MaximumPathSteps int `json:"maximum_path_steps"`
}

func DefaultConfiguration() Configuration {
	return Configuration{MaximumDepth: 12, MaximumItems: 2000, MaximumPathSteps: 24}
}
func (value Configuration) Digest() string {
	return repositorymodel.DigestJSON(struct {
		Version       uint32        `json:"version"`
		Configuration Configuration `json:"configuration"`
	}{1, value})
}

type Request struct {
	Expected  []string `json:"expected"`
	Possible  []string `json:"possible"`
	Protected []string `json:"protected"`
}

type Element struct {
	Id   string                   `json:"id"`
	Kind repositorymodel.NodeKind `json:"kind"`
	Name string                   `json:"name"`
	Path string                   `json:"path,omitempty"`
}

type ExplanationStep struct {
	From          string                        `json:"from"`
	To            string                        `json:"to"`
	AssertionFrom string                        `json:"assertion_from"`
	AssertionTo   string                        `json:"assertion_to"`
	Relation      repositorymodel.EdgeKind      `json:"relation"`
	Basis         repositorymodel.EvidenceClass `json:"basis"`
	Confidence    repositorymodel.Confidence    `json:"confidence,omitempty"`
	Provenance    repositorymodel.Provenance    `json:"provenance"`
}

type Item struct {
	Element        Element                       `json:"element"`
	Classification Classification                `json:"classification"`
	Explanation    []ExplanationStep             `json:"explanation"`
	Basis          repositorymodel.EvidenceClass `json:"basis"`
	Confidence     repositorymodel.Confidence    `json:"confidence,omitempty"`
	Evidence       []repositorymodel.Evidence    `json:"evidence"`
	GapIds         []string                      `json:"knowledge_gap_ids,omitempty"`
}

type RiskDimension struct {
	Name        string                     `json:"name"`
	Disposition string                     `json:"disposition"`
	Explanation string                     `json:"explanation"`
	Evidence    []repositorymodel.Evidence `json:"evidence"`
}

type RiskProfile struct {
	SchemaVersion uint32          `json:"schema_version"`
	Ruleset       string          `json:"ruleset"`
	Overall       RiskLevel       `json:"overall"`
	Dimensions    []RiskDimension `json:"dimensions"`
	Advisory      bool            `json:"advisory"`
}

type Report struct {
	SchemaVersion               uint32                         `json:"schema_version"`
	ProjectId                   project.ProjectId              `json:"project_id"`
	ChangeId                    change.ChangeId                `json:"change_id"`
	Intent                      string                         `json:"intent"`
	RepositoryModelId           string                         `json:"repository_model_id"`
	RepositoryModelDigest       string                         `json:"repository_model_digest"`
	BuildKey                    repositorymodel.BuildKey       `json:"build_key"`
	SourceFingerprintDigest     string                         `json:"source_fingerprint_digest"`
	AnalysisConfigurationDigest string                         `json:"analysis_configuration_digest"`
	Request                     Request                        `json:"request"`
	Items                       []Item                         `json:"items"`
	KnowledgeGaps               []repositorymodel.KnowledgeGap `json:"knowledge_gaps"`
	Limits                      Configuration                  `json:"limits"`
	Truncated                   bool                           `json:"truncated"`
	Risk                        RiskProfile                    `json:"risk_profile"`
	CreatedAt                   time.Time                      `json:"created_at"`
	ReportDigest                string                         `json:"report_digest"`
}

type traversalState struct {
	id         string
	depth      int
	steps      []ExplanationStep
	basis      repositorymodel.EvidenceClass
	confidence repositorymodel.Confidence
}

func Analyze(currentChange change.Change, model repositorymodel.Model, request Request, configuration Configuration, now time.Time) (Report, error) {
	if currentChange.ChangeId() == "" || currentChange.ProjectId() != model.ProjectId() {
		return Report{}, fmt.Errorf("Change and RepositoryModel Project association mismatch")
	}
	if configuration.MaximumDepth <= 0 || configuration.MaximumItems <= 0 || configuration.MaximumPathSteps <= 0 {
		return Report{}, fmt.Errorf("impact traversal limits must be positive")
	}
	if now.IsZero() {
		return Report{}, fmt.Errorf("impact report timestamp is required")
	}
	normalized, err := normalizeRequest(request)
	if err != nil {
		return Report{}, err
	}
	nodes := model.Nodes()
	edges := model.Edges()
	modelGaps := model.Gaps()
	byId := make(map[string]repositorymodel.Node, len(nodes))
	byPath := make(map[string]repositorymodel.Node)
	for _, node := range nodes {
		byId[node.Id] = node
		if node.Path != "" && node.Kind == repositorymodel.NodeFile {
			byPath[node.Path] = node
		}
	}
	incoming, outgoing := adjacency(edges)
	protectedSet := make(map[string]struct{}, len(normalized.Protected))
	for _, value := range normalized.Protected {
		protectedSet[value] = struct{}{}
	}
	classify := func(pathValue string, classification Classification) Classification {
		if _, protected := protectedSet[pathValue]; protected {
			return Protected
		}
		return classification
	}

	queue := []traversalState{}
	classifications := map[string]Classification{}
	paths := map[string]traversalState{}
	gaps := []repositorymodel.KnowledgeGap{}
	truncated := false
	seed := func(pathValue string, classification Classification) {
		classification = classify(pathValue, classification)
		node, exists := byPath[pathValue]
		id := "missing:" + pathValue
		if exists {
			id = node.Id
		}
		if previous, admitted := classifications[id]; admitted {
			if classificationPriority(classification) > classificationPriority(previous) {
				classifications[id] = classification
			}
			return
		}
		if len(paths) >= configuration.MaximumItems {
			truncated = true
			return
		}
		classifications[id] = classification
		if !exists {
			gap := repositorymodel.KnowledgeGap{Id: repositorymodel.GapId("missing-start-element", pathValue, "requested impact start is absent from RepositoryModel"), Category: "missing-start-element", Scope: pathValue, Reason: "requested impact start is absent from RepositoryModel", Evidence: []repositorymodel.Evidence{{Kind: "impact-request", Reference: pathValue}}, Consequence: "impact from this start is uncertain", Resolution: "rebuild a current model or correct the requested path"}
			gaps = append(gaps, gap)
			paths[id] = traversalState{id: id, basis: repositorymodel.EvidenceDerived}
			return
		}
		item := traversalState{id: node.Id, basis: repositorymodel.EvidenceObserved}
		paths[node.Id] = item
		queue = append(queue, item)
	}
	for _, value := range normalized.Expected {
		seed(value, Expected)
	}
	for _, value := range normalized.Possible {
		seed(value, Possible)
	}

	visitedDepth := map[string]int{}
	for _, item := range queue {
		visitedDepth[item.id] = 0
	}
	for len(queue) > 0 {
		current := queue[0]
		queue = queue[1:]
		if current.depth >= configuration.MaximumDepth {
			if len(incoming[current.id])+len(outgoing[current.id]) > 0 {
				truncated = true
			}
			continue
		}
		candidates := traversalEdges(current.id, incoming, outgoing)
		for _, traversal := range candidates {
			nextId := traversal.next
			if nextId == "" {
				continue
			}
			depth := current.depth + 1
			if knownDepth, seen := visitedDepth[nextId]; seen && knownDepth <= depth {
				continue
			}
			step := ExplanationStep{From: current.id, To: nextId, AssertionFrom: traversal.edge.From, AssertionTo: traversal.edge.To, Relation: traversal.edge.Kind, Basis: traversal.edge.Assertion.Class, Confidence: traversal.edge.Assertion.Confidence, Provenance: traversal.edge.Assertion.Provenance}
			steps := append(append([]ExplanationStep(nil), current.steps...), step)
			if len(steps) > configuration.MaximumPathSteps {
				truncated = true
				continue
			}
			if _, admitted := paths[nextId]; !admitted && len(paths) >= configuration.MaximumItems {
				truncated = true
				queue = nil
				break
			}
			basis, confidence := current.basis, current.confidence
			if traversal.edge.Assertion.Class == repositorymodel.EvidenceInferred {
				basis, confidence = repositorymodel.EvidenceInferred, traversal.edge.Assertion.Confidence
			} else if basis != repositorymodel.EvidenceInferred {
				basis = repositorymodel.EvidenceDerived
			}
			classification := Possible
			if basis == repositorymodel.EvidenceInferred {
				classification = Uncertain
			}
			if node, ok := byId[nextId]; ok {
				classification = classify(node.Path, classification)
			}
			visitedDepth[nextId] = depth
			paths[nextId] = traversalState{id: nextId, depth: depth, steps: steps, basis: basis, confidence: confidence}
			if previous, exists := classifications[nextId]; !exists || classificationPriority(classification) > classificationPriority(previous) {
				classifications[nextId] = classification
			}
			queue = append(queue, paths[nextId])
		}
	}
	for _, gap := range modelGaps {
		if gapRelevant(gap, normalized, paths, byId) {
			gaps = append(gaps, gap)
		}
	}
	gaps = deduplicateGaps(gaps)
	items := make([]Item, 0, len(paths))
	for id, stateValue := range paths {
		node, exists := byId[id]
		if !exists {
			scope := strings.TrimPrefix(id, "missing:")
			gapIds := gapIdsForScope(gaps, scope)
			items = append(items, Item{Element: Element{Id: id, Kind: repositorymodel.NodeUnresolvedReference, Name: scope, Path: scope}, Classification: classifications[id], Basis: repositorymodel.EvidenceDerived, GapIds: gapIds})
			continue
		}
		evidence := []repositorymodel.Evidence{}
		for _, step := range stateValue.steps {
			evidence = append(evidence, step.Provenance.Evidence...)
		}
		items = append(items, Item{Element: Element{Id: node.Id, Kind: node.Kind, Name: node.Name, Path: node.Path}, Classification: classifications[id], Explanation: stateValue.steps, Basis: stateValue.basis, Confidence: stateValue.confidence, Evidence: append([]repositorymodel.Evidence{{Kind: "impact-start-or-path", Reference: node.Path, Digest: model.BuildKey().Digest}}, evidence...), GapIds: gapIdsForScope(gaps, node.Path)})
	}
	if truncated {
		gaps = append(gaps, impactTruncatedGap(configuration, "impact traversal limit reached", "impact traversal reached a configured limit", "additional impacted elements may exist"))
		gaps = deduplicateGaps(gaps)
	}
	for _, gap := range gaps {
		if len(items) >= configuration.MaximumItems {
			truncated = true
			break
		}
		items = append(items, Item{Element: Element{Id: "gap:" + gap.Id, Kind: repositorymodel.NodeUnresolvedReference, Name: gap.Category, Path: gap.Scope}, Classification: Uncertain, Basis: repositorymodel.EvidenceDerived, Evidence: append([]repositorymodel.Evidence(nil), gap.Evidence...), GapIds: []string{gap.Id}})
	}
	if truncated && !containsGapCategory(gaps, "impact-truncated") {
		gaps = append(gaps, impactTruncatedGap(configuration, "impact result cardinality limit reached", "impact result cardinality reached a configured limit", "additional impacted elements or gaps may exist"))
		gaps = deduplicateGaps(gaps)
	}
	slices.SortFunc(items, func(left, right Item) int {
		if compared := strings.Compare(string(left.Classification), string(right.Classification)); compared != 0 {
			return compared
		}
		return strings.Compare(left.Element.Id, right.Element.Id)
	})
	risk := deriveRisk(items, gaps, truncated)
	report := Report{SchemaVersion: ReportSchemaVersion, ProjectId: currentChange.ProjectId(), ChangeId: currentChange.ChangeId(), Intent: string(currentChange.Intent()), RepositoryModelId: model.ModelId(), RepositoryModelDigest: model.ModelDigest(), BuildKey: model.BuildKey(), SourceFingerprintDigest: model.Fingerprint().Digest, AnalysisConfigurationDigest: configuration.Digest(), Request: normalized, Items: items, KnowledgeGaps: gaps, Limits: configuration, Truncated: truncated, Risk: risk, CreatedAt: now.UTC()}
	report.ReportDigest = reportDigest(report)
	return report, nil
}

func impactTruncatedGap(configuration Configuration, idReason, reason, consequence string) repositorymodel.KnowledgeGap {
	return repositorymodel.KnowledgeGap{Id: repositorymodel.GapId("impact-truncated", "repository", idReason), Category: "impact-truncated", Scope: "repository", Reason: reason, Evidence: []repositorymodel.Evidence{{Kind: "impact-configuration", Reference: configuration.Digest()}}, Consequence: consequence, Resolution: "review bounded traversal configuration"}
}

func Decode(payload []byte) (Report, error) {
	var report Report
	if err := json.Unmarshal(payload, &report); err != nil {
		return Report{}, err
	}
	if report.SchemaVersion != ReportSchemaVersion {
		return Report{}, fmt.Errorf("unsupported ImpactReport schema version %d", report.SchemaVersion)
	}
	if report.ReportDigest != reportDigest(report) {
		return Report{}, fmt.Errorf("ImpactReport digest mismatch")
	}
	return report, nil
}

func (report Report) Encode() ([]byte, error) {
	if report.SchemaVersion != ReportSchemaVersion || report.ReportDigest != reportDigest(report) {
		return nil, fmt.Errorf("invalid ImpactReport")
	}
	return json.Marshal(report)
}

func CompareSeverity(left, right RiskLevel) (int, bool) {
	if left == RiskIndeterminate || right == RiskIndeterminate {
		return 0, false
	}
	values := map[RiskLevel]int{RiskLow: 0, RiskModerate: 1, RiskHigh: 2}
	leftValue, leftOk := values[left]
	rightValue, rightOk := values[right]
	if !leftOk || !rightOk {
		return 0, false
	}
	if leftValue < rightValue {
		return -1, true
	}
	if leftValue > rightValue {
		return 1, true
	}
	return 0, true
}

func normalizeRequest(request Request) (Request, error) {
	normalize := func(values []string) ([]string, error) {
		set := map[string]struct{}{}
		for _, value := range values {
			normalized, err := source.NormalizeRepositoryPath(value)
			if err != nil {
				return nil, err
			}
			set[string(normalized)] = struct{}{}
		}
		result := make([]string, 0, len(set))
		for value := range set {
			result = append(result, value)
		}
		slices.Sort(result)
		return result, nil
	}
	var err error
	request.Expected, err = normalize(request.Expected)
	if err != nil {
		return Request{}, err
	}
	request.Possible, err = normalize(request.Possible)
	if err != nil {
		return Request{}, err
	}
	request.Protected, err = normalize(request.Protected)
	if err != nil {
		return Request{}, err
	}
	if len(request.Expected)+len(request.Possible) == 0 {
		return Request{}, fmt.Errorf("impact request requires expected or possible starting scope")
	}
	return request, nil
}

type traversalEdge struct {
	edge repositorymodel.Edge
	next string
}

func adjacency(edges []repositorymodel.Edge) (map[string][]repositorymodel.Edge, map[string][]repositorymodel.Edge) {
	incoming, outgoing := map[string][]repositorymodel.Edge{}, map[string][]repositorymodel.Edge{}
	for _, edge := range edges {
		incoming[edge.To] = append(incoming[edge.To], edge)
		outgoing[edge.From] = append(outgoing[edge.From], edge)
	}
	for key := range incoming {
		slices.SortFunc(incoming[key], func(left, right repositorymodel.Edge) int { return strings.Compare(left.Id, right.Id) })
	}
	for key := range outgoing {
		slices.SortFunc(outgoing[key], func(left, right repositorymodel.Edge) int { return strings.Compare(left.Id, right.Id) })
	}
	return incoming, outgoing
}
func traversalEdges(id string, incoming, outgoing map[string][]repositorymodel.Edge) []traversalEdge {
	var result []traversalEdge
	for _, edge := range incoming[id] {
		switch edge.Kind {
		case repositorymodel.EdgeContains, repositorymodel.EdgeDependsOn, repositorymodel.EdgeReferences, repositorymodel.EdgeTests, repositorymodel.EdgeCoChangesWith:
			result = append(result, traversalEdge{edge, edge.From})
		}
	}
	for _, edge := range outgoing[id] {
		switch edge.Kind {
		case repositorymodel.EdgeDeclares, repositorymodel.EdgeExposes, repositorymodel.EdgeOwnedBy, repositorymodel.EdgeAssociatedWithArchitecture:
			result = append(result, traversalEdge{edge, edge.To})
		}
	}
	slices.SortFunc(result, func(left, right traversalEdge) int {
		if compared := strings.Compare(left.edge.Id, right.edge.Id); compared != 0 {
			return compared
		}
		return strings.Compare(left.next, right.next)
	})
	return result
}
func classificationPriority(value Classification) int {
	switch value {
	case Protected:
		return 4
	case Uncertain:
		return 3
	case Expected:
		return 2
	case Possible:
		return 1
	}
	return 0
}

func gapRelevant(gap repositorymodel.KnowledgeGap, request Request, paths map[string]traversalState, nodes map[string]repositorymodel.Node) bool { // repository and excluded input gaps can widen every analysis.
	if gap.Scope == "repository" || gap.Category == "excluded-untracked-content" || gap.Category == "analysis-truncated" {
		return true
	}
	for _, value := range append(append(append([]string(nil), request.Expected...), request.Possible...), request.Protected...) {
		if gap.Scope == value || strings.HasPrefix(value, gap.Scope+"/") || strings.HasPrefix(gap.Scope, value+"/") {
			return true
		}
	}
	for id := range paths {
		if node, exists := nodes[id]; exists && (node.Path == gap.Scope || node.Name == gap.Scope) {
			return true
		}
	}
	return false
}

func gapIdsForScope(gaps []repositorymodel.KnowledgeGap, scope string) []string {
	var result []string
	for _, gap := range gaps {
		if gap.Scope == "repository" || gap.Scope == scope || strings.HasPrefix(scope, gap.Scope+"/") || strings.HasPrefix(gap.Scope, scope+"/") {
			result = append(result, gap.Id)
		}
	}
	slices.Sort(result)
	return result
}
func deduplicateGaps(values []repositorymodel.KnowledgeGap) []repositorymodel.KnowledgeGap {
	byId := map[string]repositorymodel.KnowledgeGap{}
	for _, value := range values {
		byId[value.Id] = value
	}
	result := make([]repositorymodel.KnowledgeGap, 0, len(byId))
	for _, value := range byId {
		result = append(result, value)
	}
	slices.SortFunc(result, func(left, right repositorymodel.KnowledgeGap) int { return strings.Compare(left.Id, right.Id) })
	return result
}

func containsGapCategory(gaps []repositorymodel.KnowledgeGap, category string) bool {
	for _, gap := range gaps {
		if gap.Category == category {
			return true
		}
	}
	return false
}

func deriveRisk(items []Item, gaps []repositorymodel.KnowledgeGap, truncated bool) RiskProfile {
	counts := map[Classification]int{}
	apiCount, testCount, fileCount, dependencyHops, inferredCount, historyCount := 0, 0, 0, 0, 0, 0
	ownedFiles := map[string]struct{}{}
	for _, item := range items {
		counts[item.Classification]++
		if item.Element.Kind == repositorymodel.NodeAPI {
			apiCount++
		}
		if item.Element.Kind == repositorymodel.NodeTest {
			testCount++
		}
		if item.Element.Kind == repositorymodel.NodeFile {
			fileCount++
		}
		if item.Basis == repositorymodel.EvidenceInferred {
			inferredCount++
		}
		for _, step := range item.Explanation {
			if step.Relation == repositorymodel.EdgeDependsOn || step.Relation == repositorymodel.EdgeReferences {
				dependencyHops++
			}
			if step.Relation == repositorymodel.EdgeOwnedBy {
				ownedFiles[step.AssertionFrom] = struct{}{}
			}
			if step.Relation == repositorymodel.EdgeCoChangesWith {
				historyCount++
			}
		}
	}
	ownershipAmbiguity := max(0, fileCount-len(ownedFiles))
	dimensions := []RiskDimension{
		{Name: "breadth", Disposition: riskDisposition(len(items), 5, 20), Explanation: fmt.Sprintf("%d repository elements are in the bounded impact set", len(items)), Evidence: []repositorymodel.Evidence{{Kind: "impact-count", Reference: fmt.Sprint(len(items))}}},
		{Name: "protected exposure", Disposition: riskDisposition(counts[Protected], 1, 1), Explanation: fmt.Sprintf("%d protected elements are exposed", counts[Protected]), Evidence: []repositorymodel.Evidence{{Kind: "protected-count", Reference: fmt.Sprint(counts[Protected])}}},
		{Name: "dependency reach", Disposition: riskDisposition(dependencyHops, 3, 10), Explanation: fmt.Sprintf("%d dependency/reference hops support impact", dependencyHops), Evidence: []repositorymodel.Evidence{{Kind: "dependency-hop-count", Reference: fmt.Sprint(dependencyHops)}}},
		{Name: "verification relation", Disposition: verificationDisposition(testCount), Explanation: fmt.Sprintf("%d test elements are related", testCount), Evidence: []repositorymodel.Evidence{{Kind: "test-count", Reference: fmt.Sprint(testCount)}}},
		{Name: "API exposure", Disposition: riskDisposition(apiCount, 1, 5), Explanation: fmt.Sprintf("%d API elements are exposed", apiCount), Evidence: []repositorymodel.Evidence{{Kind: "api-count", Reference: fmt.Sprint(apiCount)}}},
		{Name: "ownership ambiguity", Disposition: riskDisposition(ownershipAmbiguity, 1, 5), Explanation: fmt.Sprintf("%d impacted files lack a traversed ownership relation", ownershipAmbiguity), Evidence: []repositorymodel.Evidence{{Kind: "unowned-impact-file-count", Reference: fmt.Sprint(ownershipAmbiguity)}}},
		{Name: "bounded history concentration", Disposition: riskDisposition(historyCount, 3, 10), Explanation: fmt.Sprintf("%d bounded co-change hops are present", historyCount), Evidence: []repositorymodel.Evidence{{Kind: "co-change-hop-count", Reference: fmt.Sprint(historyCount)}}},
		{Name: "uncertainty", Disposition: func() string {
			if len(gaps) > 0 || truncated {
				return "insufficient"
			}
			if inferredCount > 0 {
				return "inferred"
			}
			return "bounded"
		}(), Explanation: fmt.Sprintf("%d gaps, %d inferred items, truncated=%t", len(gaps), inferredCount, truncated), Evidence: []repositorymodel.Evidence{{Kind: "knowledge-gap-count", Reference: fmt.Sprint(len(gaps))}, {Kind: "inferred-impact-count", Reference: fmt.Sprint(inferredCount)}}},
	}
	overall := RiskLow
	if len(gaps) > 0 || truncated {
		overall = RiskIndeterminate
	} else if counts[Protected] > 0 || len(items) >= 20 || apiCount >= 5 {
		overall = RiskHigh
	} else if len(items) >= 5 || dependencyHops >= 3 || apiCount > 0 {
		overall = RiskModerate
	}
	return RiskProfile{SchemaVersion: 1, Ruleset: "praetor-m1.2-risk-v1", Overall: overall, Dimensions: dimensions, Advisory: true}
}
func riskDisposition(value, moderate, high int) string {
	if high > 0 && value >= high {
		return "high"
	}
	if moderate > 0 && value >= moderate {
		return "moderate"
	}
	return "low"
}
func verificationDisposition(testCount int) string {
	if testCount == 0 {
		return "moderate"
	}
	return "low"
}
func max(left, right int) int {
	if left > right {
		return left
	}
	return right
}
func reportDigest(report Report) string {
	report.ReportDigest = ""
	report.CreatedAt = time.Time{}
	return repositorymodel.DigestJSON(report)
}
