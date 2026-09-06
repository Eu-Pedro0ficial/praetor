package verification

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/Eu-Pedro0ficial/praetor/internal/source"
)

func TestDiscoverUsesRepresentativeRepositoryEvidence(t *testing.T) {
	tests := []struct {
		name          string
		files         map[string]string
		wantTools     []string
		wantKinds     []StepKind
		wantOrigin    CandidateOrigin
		needsPlanning bool
	}{
		{
			name: "Go manifest is deterministically inferred",
			files: map[string]string{
				"go.mod":          "module fixture\n\ngo 1.25\n",
				"service_test.go": "package fixture\n",
			},
			wantTools:     []string{"go"},
			wantKinds:     []StepKind{KindTest},
			wantOrigin:    OriginDeterministicallyInferred,
			needsPlanning: false,
		},
		{
			name: "package scripts are repository declared",
			files: map[string]string{
				"web/package.json": `{"scripts":{"build":"vite build","test":"vitest run","unknown":"ignored"}}`,
			},
			wantTools:     []string{"npm", "npm"},
			wantKinds:     []StepKind{KindBuild, KindTest},
			wantOrigin:    OriginRepositoryDeclared,
			needsPlanning: false,
		},
		{
			name: "Make targets are repository declared",
			files: map[string]string{
				"Makefile": "build:\n\tgo build ./...\n\nlint:\n\tgo vet ./...\n",
			},
			wantTools:     []string{"make", "make"},
			wantKinds:     []StepKind{KindBuild, KindLint},
			wantOrigin:    OriginRepositoryDeclared,
			needsPlanning: false,
		},
		{
			name:          "open-ended manifest remains planner evidence",
			files:         map[string]string{"pyproject.toml": "[project]\nname='fixture'\n"},
			needsPlanning: true,
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			root := t.TempDir()
			for name, contents := range test.files {
				path := filepath.Join(root, filepath.FromSlash(name))
				if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
					t.Fatalf("create fixture directory: %v", err)
				}
				if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
					t.Fatalf("write %s: %v", name, err)
				}
			}
			result, err := Discover(root)
			if err != nil {
				t.Fatalf("Discover() error = %v", err)
			}
			if result.NeedsPlanning() != test.needsPlanning {
				t.Fatalf("NeedsPlanning() = %t, want %t; issues=%v", result.NeedsPlanning(), test.needsPlanning, result.Issues())
			}
			candidates := result.Candidates()
			var tools []string
			var kinds []StepKind
			for _, candidate := range candidates {
				tools = append(tools, candidate.Executable())
				kinds = append(kinds, candidate.Kind())
				if candidate.Origin() != test.wantOrigin {
					t.Fatalf("candidate origin = %q, want %q", candidate.Origin(), test.wantOrigin)
				}
			}
			if !reflect.DeepEqual(tools, test.wantTools) || !reflect.DeepEqual(kinds, test.wantKinds) {
				t.Fatalf("discovery tools/kinds = %v/%v, want %v/%v", tools, kinds, test.wantTools, test.wantKinds)
			}
		})
	}
}

func TestDiscoverHandlesMalformedAndEmptyEvidenceWithoutPassing(t *testing.T) {
	root := t.TempDir()
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte(`{"scripts":`), 0o600); err != nil {
		t.Fatal(err)
	}
	malformed, err := Discover(root)
	if err != nil {
		t.Fatalf("Discover() malformed error = %v", err)
	}
	if !malformed.NeedsPlanning() || len(malformed.Candidates()) != 0 || len(malformed.Issues()) == 0 {
		t.Fatalf("malformed discovery = %#v", malformed)
	}
	empty, err := Discover(t.TempDir())
	if err != nil {
		t.Fatalf("Discover() empty error = %v", err)
	}
	if !empty.NeedsPlanning() || len(empty.Candidates()) != 0 {
		t.Fatalf("empty discovery = %#v", empty)
	}
	if _, err := BuildPlan(empty.Candidates(), nil, time.Second); err == nil ||
		!strings.Contains(err.Error(), "no viable deterministic verification") {
		t.Fatalf("empty BuildPlan() error = %v", err)
	}
}

func TestCandidateValidationRejectsArbitraryOrShellLikeExecution(t *testing.T) {
	tests := []struct {
		name       string
		executable string
		arguments  []string
		directory  string
		evidence   []string
	}{
		{name: "shell", executable: "sh", arguments: []string{"-c", "go test ./..."}, directory: ".", evidence: []string{"go.mod"}},
		{name: "operator", executable: "go", arguments: []string{"test", "./...", "&&", "curl"}, directory: ".", evidence: []string{"go.mod"}},
		{name: "arbitrary tool", executable: "rm", arguments: []string{"file"}, directory: ".", evidence: []string{"go.mod"}},
		{name: "unsafe npm mode", executable: "npm", arguments: []string{"exec", "tool"}, directory: ".", evidence: []string{"package.json"}},
		{name: "escape", executable: "go", arguments: []string{"test", "./..."}, directory: "../canonical", evidence: []string{"go.mod"}},
		{name: "uncited", executable: "go", arguments: []string{"test", "./..."}, directory: "."},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, err := NewCandidate(
				KindTest,
				test.executable,
				test.arguments,
				test.directory,
				OriginAIAssisted,
				test.evidence,
			); err == nil {
				t.Fatal("unsafe candidate was accepted")
			}
		})
	}
}

func TestBuildPlanPreservesDeterministicPrecedenceAndCopiesData(t *testing.T) {
	deterministic, _ := NewCandidate(
		KindTest, "go", []string{"test", "./..."}, ".",
		OriginDeterministicallyInferred, []string{"go.mod"},
	)
	contradictoryAI, _ := NewCandidate(
		KindTest, "pytest", nil, ".",
		OriginAIAssisted, []string{"pyproject.toml"},
	)
	additionalAI, _ := NewCandidate(
		KindLint, "ruff", nil, ".",
		OriginAIAssisted, []string{"pyproject.toml"},
	)
	plan, err := BuildPlan(
		[]VerificationCandidate{deterministic, deterministic},
		[]VerificationCandidate{contradictoryAI, additionalAI},
		5*time.Second,
	)
	if err != nil {
		t.Fatalf("BuildPlan() error = %v", err)
	}
	steps := plan.Steps()
	if len(steps) != 2 || steps[0].Kind() != KindLint || steps[0].Executable() != "ruff" ||
		steps[1].Kind() != KindTest || steps[1].Executable() != "go" {
		t.Fatalf("precedence plan = %#v", steps)
	}
	arguments := steps[1].Arguments()
	arguments[0] = "mutated"
	if steps[1].Arguments()[0] != "test" {
		t.Fatal("VerificationStep exposed mutable arguments")
	}
	steps[0] = VerificationStep{}
	if plan.Steps()[0].Id() == "" {
		t.Fatal("VerificationPlan exposed mutable step storage")
	}
}

func TestParsePlanningResponseRequiresBoundedStructuredCitedCandidates(t *testing.T) {
	evidence, err := newRepositoryEvidence("pyproject.toml", "manifest", "[tool.pytest]\n")
	if err != nil {
		t.Fatal(err)
	}
	request := PlanningRequest{evidence: []RepositoryEvidence{evidence}}
	valid := `{"candidates":[{"kind":"test","executable":"pytest","arguments":[],"working_directory":".","supporting_evidence":["pyproject.toml"]}]}`
	candidates, err := ParsePlanningResponse(valid, request)
	if err != nil || len(candidates) != 1 || candidates[0].Origin() != OriginAIAssisted {
		t.Fatalf("ParsePlanningResponse() = %#v/%v", candidates, err)
	}
	invalid := []string{
		"Run pytest",
		"```json\n" + valid + "\n```",
		`{"candidates":[{"kind":"test","executable":"sh","arguments":["-c","pytest"],"working_directory":".","supporting_evidence":["pyproject.toml"]}]}`,
		`{"candidates":[{"kind":"test","executable":"pytest","arguments":[],"working_directory":".","supporting_evidence":["missing.toml"]}]}`,
		`{"candidates":[],"unknown":true}`,
	}
	for _, value := range invalid {
		if _, err := ParsePlanningResponse(value, request); err == nil {
			t.Fatalf("unsafe planner response was accepted: %q", value)
		}
	}
	if _, err := ParsePlanningResponse(strings.Repeat("x", maximumPlannerResponseBytes+1), request); err == nil {
		t.Fatal("unbounded planner response was accepted")
	}
}

func TestRepositoryEvidencePathUsesNormalizedRepositoryIdentity(t *testing.T) {
	if _, err := newRepositoryEvidence("../outside", "manifest", "content"); err == nil {
		t.Fatal("escaping evidence path was accepted")
	}
	evidence, err := newRepositoryEvidence("tools/Makefile", "task-definition", "test:\n")
	if err != nil || evidence.Path() != source.RepositoryPath("tools/Makefile") {
		t.Fatalf("normalized evidence = %#v/%v", evidence, err)
	}
}
