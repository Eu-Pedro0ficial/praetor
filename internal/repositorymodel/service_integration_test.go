package repositorymodel_test

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	modelcache "github.com/Eu-Pedro0ficial/praetor/internal/adapters/persistence/modelcache"
	"github.com/Eu-Pedro0ficial/praetor/internal/adapters/repositoryanalysis"
	"github.com/Eu-Pedro0ficial/praetor/internal/project"
	"github.com/Eu-Pedro0ficial/praetor/internal/repository"
	"github.com/Eu-Pedro0ficial/praetor/internal/repositorymodel"
)

const modelProjectId project.ProjectId = "01890c29-7a78-7abc-8def-0123456789ab"

func TestHeterogeneousModelCacheIncrementalEquivalenceAndFreshness(t *testing.T) {
	root := heterogeneousRepository(t)
	registration := project.Registration{ProjectId: modelProjectId, RepositoryRoot: root}
	cache, err := modelcache.Open(filepath.Join(t.TempDir(), "cache"), registration)
	if err != nil {
		t.Fatal(err)
	}
	service, err := repositorymodel.NewService(modelProjectId, root, repository.InspectModelSource, []repositorymodel.Analyzer{repositoryanalysis.New()}, cache, repositorymodel.DefaultConfiguration(), func() time.Time { return time.Unix(1700000000, 0).UTC() })
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = service.Close() })
	first, err := service.Build()
	if err != nil {
		t.Fatal(err)
	}
	if len(first.Model.Nodes()) == 0 || len(first.Model.Edges()) == 0 {
		t.Fatal("heterogeneous model graph is empty")
	}
	assertGraphSignals(t, first.Model)
	if !slices.IsSortedFunc(first.Model.Nodes(), func(left, right repositorymodel.Node) int { return strings.Compare(left.Id, right.Id) }) {
		t.Fatal("nodes are not deterministic")
	}
	assertEvidenceSemantics(t, first.Model)
	if !hasGap(first.Model.Gaps(), "unsupported-language") {
		t.Fatal("unsupported source did not produce KnowledgeGap")
	}
	second, err := service.Build()
	if err != nil {
		t.Fatal(err)
	}
	if !second.Statistics.CacheHit || second.Model.ModelDigest() != first.Model.ModelDigest() {
		t.Fatal("exact build key did not reuse deterministic model")
	}

	goSource := filepath.Join(root, "internal", "core", "core.go")
	if err := os.WriteFile(goSource, []byte("package core\n\nfunc Value() int { return 2 }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	freshness, err := service.Freshness(first.Model)
	if err != nil || freshness != repositorymodel.FreshnessStale {
		t.Fatalf("freshness=%s err=%v", freshness, err)
	}
	incremental, err := service.Build()
	if err != nil {
		t.Fatal(err)
	}
	if incremental.Statistics.ReusedFiles == 0 || incremental.Statistics.AnalyzedFiles == 0 {
		t.Fatalf("incremental statistics=%#v", incremental.Statistics)
	}

	fullCache, err := modelcache.Open(filepath.Join(t.TempDir(), "full-cache"), registration)
	if err != nil {
		t.Fatal(err)
	}
	fullService, err := repositorymodel.NewService(modelProjectId, root, repository.InspectModelSource, []repositorymodel.Analyzer{repositoryanalysis.New()}, fullCache, repositorymodel.DefaultConfiguration(), func() time.Time { return time.Unix(1800000000, 0).UTC() })
	if err != nil {
		t.Fatal(err)
	}
	defer fullService.Close()
	full, err := fullService.Build()
	if err != nil {
		t.Fatal(err)
	}
	if full.Model.ModelDigest() != incremental.Model.ModelDigest() {
		t.Fatalf("incremental digest %s differs from full %s", incremental.Model.ModelDigest(), full.Model.ModelDigest())
	}
	if _, err := os.Stat(filepath.Join(root, ".praetor")); !os.IsNotExist(err) {
		t.Fatalf("repository runtime metadata exists: %v", err)
	}
}

func TestIncrementalAndFullBuildsRemainEquivalentAcrossMutationClasses(t *testing.T) {
	type mutationCase struct {
		name             string
		mutate           func(*testing.T, string)
		assertSemantics  func(*testing.T, repositorymodel.Model)
		wantAnalyzedFile bool
	}
	testCases := []mutationCase{
		{
			name: "local symbol mutation",
			mutate: func(t *testing.T, root string) {
				writeFixtureFile(t, root, "internal/core/core.go", "package core\n\nfunc Value() int { return 3 }\nfunc localValue() int { return 4 }\n")
			},
			assertSemantics: func(t *testing.T, model repositorymodel.Model) {
				assertModelNode(t, model, repositorymodel.NodeSymbol, "localValue", "internal/core/core.go")
			},
			wantAnalyzedFile: true,
		},
		{
			name: "dependency relationship mutation",
			mutate: func(t *testing.T, root string) {
				writeFixtureFile(t, root, "web/api.test.ts", "import dependency from 'new-dependency'; dependency();\n")
			},
			assertSemantics: func(t *testing.T, model repositorymodel.Model) {
				assertModelEdge(t, model, repositorymodel.EdgeDependsOn, repositorymodel.NodeFile, "web/api.test.ts", repositorymodel.NodeExternalDependency, "new-dependency")
			},
			wantAnalyzedFile: true,
		},
		{
			name: "Go module manifest mutation",
			mutate: func(t *testing.T, root string) {
				writeFixtureFile(t, root, "go.mod", "module example.invalid/changed\n\ngo 1.25.1\n")
			},
			assertSemantics: func(t *testing.T, model repositorymodel.Model) {
				assertModelNode(t, model, repositorymodel.NodeModule, "example.invalid/changed", ".")
			},
			wantAnalyzedFile: true,
		},
		{
			name: "test relationship mutation",
			mutate: func(t *testing.T, root string) {
				oldPath := filepath.Join(root, "internal", "core", "core_test.go")
				newPath := filepath.Join(root, "internal", "core", "value_test.go")
				if err := os.Rename(oldPath, newPath); err != nil {
					t.Fatal(err)
				}
				git(t, root, "add", "-A")
			},
			assertSemantics: func(t *testing.T, model repositorymodel.Model) {
				assertModelNode(t, model, repositorymodel.NodeTest, "value", "internal/core/value_test.go")
				assertModelGap(t, model, "unresolved-reference", "internal/core/value.go")
			},
			wantAnalyzedFile: true,
		},
		{
			name: "CODEOWNERS mutation",
			mutate: func(t *testing.T, root string) {
				writeFixtureFile(t, root, "CODEOWNERS", "/internal/ @platform\n/web/ @frontend\n")
			},
			assertSemantics: func(t *testing.T, model repositorymodel.Model) {
				assertModelNode(t, model, repositorymodel.NodeOwner, "@platform", "")
			},
			wantAnalyzedFile: true,
		},
		{
			name: "API mutation",
			mutate: func(t *testing.T, root string) {
				writeFixtureFile(t, root, "internal/core/core.go", "package core\n\nfunc Updated() int { return 3 }\n")
			},
			assertSemantics: func(t *testing.T, model repositorymodel.Model) {
				assertModelNode(t, model, repositorymodel.NodeAPI, "Updated", "internal/core/core.go")
			},
			wantAnalyzedFile: true,
		},
		{
			name: "global package manifest mutation",
			mutate: func(t *testing.T, root string) {
				writeFixtureFile(t, root, "package.json", "{\"name\":\"poly\",\"dependencies\":{\"left-pad\":\"1.0.0\",\"manifest-added\":\"2.0.0\"}}")
			},
			assertSemantics: func(t *testing.T, model repositorymodel.Model) {
				assertModelNode(t, model, repositorymodel.NodeExternalDependency, "manifest-added", "")
			},
			wantAnalyzedFile: true,
		},
		{
			name: "tracked file deletion",
			mutate: func(t *testing.T, root string) {
				if err := os.Remove(filepath.Join(root, "scripts", "unknown.py")); err != nil {
					t.Fatal(err)
				}
				git(t, root, "add", "-A")
			},
			assertSemantics: func(t *testing.T, model repositorymodel.Model) {
				if modelHasPath(model, "scripts/unknown.py") || modelHasGap(model, "unsupported-language", "scripts/unknown.py") {
					t.Fatal("deleted tracked file or its gap remained in the model")
				}
			},
		},
		{
			name: "tracked file rename",
			mutate: func(t *testing.T, root string) {
				oldPath := filepath.Join(root, "docs", "system.puml")
				newPath := filepath.Join(root, "docs", "platform.puml")
				if err := os.Rename(oldPath, newPath); err != nil {
					t.Fatal(err)
				}
				git(t, root, "add", "-A")
			},
			assertSemantics: func(t *testing.T, model repositorymodel.Model) {
				if modelHasPath(model, "docs/system.puml") || !modelHasPath(model, "docs/platform.puml") {
					t.Fatal("tracked rename was not reflected in model file nodes")
				}
				assertModelNode(t, model, repositorymodel.NodeArchitectureElement, "Core", "docs/platform.puml")
			},
			wantAnalyzedFile: true,
		},
	}

	for _, testCase := range testCases {
		t.Run(testCase.name, func(t *testing.T) {
			root := heterogeneousRepository(t)
			registration := project.Registration{ProjectId: modelProjectId, RepositoryRoot: root}
			incrementalCache, err := modelcache.Open(filepath.Join(t.TempDir(), "incremental-cache"), registration)
			if err != nil {
				t.Fatal(err)
			}
			incrementalService, err := repositorymodel.NewService(modelProjectId, root, repository.InspectModelSource, []repositorymodel.Analyzer{repositoryanalysis.New()}, incrementalCache, repositorymodel.DefaultConfiguration(), func() time.Time { return time.Unix(1700000000, 0).UTC() })
			if err != nil {
				t.Fatal(err)
			}
			defer incrementalService.Close()
			before, err := incrementalService.Build()
			if err != nil {
				t.Fatal(err)
			}

			testCase.mutate(t, root)
			incremental, err := incrementalService.Build()
			if err != nil {
				t.Fatal(err)
			}
			if incremental.Model.ModelDigest() == before.Model.ModelDigest() {
				t.Fatal("representative mutation did not change the RepositoryModel digest")
			}
			if incremental.Statistics.ReusedFiles == 0 {
				t.Fatalf("incremental build reused no unchanged file analyses: %#v", incremental.Statistics)
			}
			if testCase.wantAnalyzedFile && incremental.Statistics.AnalyzedFiles == 0 {
				t.Fatalf("incremental build analyzed no changed file: %#v", incremental.Statistics)
			}
			testCase.assertSemantics(t, incremental.Model)

			fullCache, err := modelcache.Open(filepath.Join(t.TempDir(), "full-cache"), registration)
			if err != nil {
				t.Fatal(err)
			}
			fullService, err := repositorymodel.NewService(modelProjectId, root, repository.InspectModelSource, []repositorymodel.Analyzer{repositoryanalysis.New()}, fullCache, repositorymodel.DefaultConfiguration(), func() time.Time { return time.Unix(1800000000, 0).UTC() })
			if err != nil {
				t.Fatal(err)
			}
			defer fullService.Close()
			full, err := fullService.Build()
			if err != nil {
				t.Fatal(err)
			}
			testCase.assertSemantics(t, full.Model)
			if incremental.Model.ModelDigest() != full.Model.ModelDigest() {
				t.Fatalf("incremental digest %s differs from full %s", incremental.Model.ModelDigest(), full.Model.ModelDigest())
			}
		})
	}
}

func writeFixtureFile(t *testing.T, root, repositoryPath, content string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(root, filepath.FromSlash(repositoryPath)), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func assertModelNode(t *testing.T, model repositorymodel.Model, kind repositorymodel.NodeKind, name, pathValue string) {
	t.Helper()
	for _, node := range model.Nodes() {
		if node.Kind == kind && node.Name == name && node.Path == pathValue {
			return
		}
	}
	t.Fatalf("model lacks %s node name=%q path=%q", kind, name, pathValue)
}

func assertModelGap(t *testing.T, model repositorymodel.Model, category, scope string) {
	t.Helper()
	if !modelHasGap(model, category, scope) {
		t.Fatalf("model lacks gap category=%q scope=%q", category, scope)
	}
}

func modelHasGap(model repositorymodel.Model, category, scope string) bool {
	for _, gap := range model.Gaps() {
		if gap.Category == category && gap.Scope == scope {
			return true
		}
	}
	return false
}

func modelHasPath(model repositorymodel.Model, pathValue string) bool {
	for _, node := range model.Nodes() {
		if node.Kind == repositorymodel.NodeFile && node.Path == pathValue {
			return true
		}
	}
	return false
}

func assertModelEdge(t *testing.T, model repositorymodel.Model, kind repositorymodel.EdgeKind, fromKind repositorymodel.NodeKind, fromName string, toKind repositorymodel.NodeKind, toName string) {
	t.Helper()
	fromId := repositorymodel.NodeId(fromKind, fromName)
	toId := repositorymodel.NodeId(toKind, toName)
	for _, edge := range model.Edges() {
		if edge.Kind == kind && edge.From == fromId && edge.To == toId {
			return
		}
	}
	t.Fatalf("model lacks %s edge from %s to %s", kind, fromId, toId)
}

func TestUntrackedExclusionBecomesGapWithoutContentAnalysis(t *testing.T) {
	root := heterogeneousRepository(t)
	registration := project.Registration{ProjectId: modelProjectId, RepositoryRoot: root}
	untracked := filepath.Join(root, "secret.untracked")
	if err := os.WriteFile(untracked, []byte("credential-like bytes"), 0o600); err != nil {
		t.Fatal(err)
	}
	cache, err := modelcache.Open(filepath.Join(t.TempDir(), "cache"), registration)
	if err != nil {
		t.Fatal(err)
	}
	service, err := repositorymodel.NewService(modelProjectId, root, repository.InspectModelSource, []repositorymodel.Analyzer{repositoryanalysis.New()}, cache, repositorymodel.DefaultConfiguration(), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	built, err := service.Build()
	if err != nil {
		t.Fatal(err)
	}
	if !hasGap(built.Model.Gaps(), "excluded-untracked-content") {
		t.Fatal("untracked exclusion was converted into silent absence")
	}
	for _, node := range built.Model.Nodes() {
		if node.Path == "secret.untracked" {
			t.Fatal("untracked content entered RepositoryModel graph")
		}
	}
}

func TestMalformedManifestAndGeneratedInputDegradeToExplicitGaps(t *testing.T) {
	root := heterogeneousRepository(t)
	if err := os.WriteFile(filepath.Join(root, "package.json"), []byte("{malformed"), 0o600); err != nil {
		t.Fatal(err)
	}
	registration := project.Registration{ProjectId: modelProjectId, RepositoryRoot: root}
	cache, err := modelcache.Open(t.TempDir(), registration)
	if err != nil {
		t.Fatal(err)
	}
	service, err := repositorymodel.NewService(modelProjectId, root, repository.InspectModelSource, []repositorymodel.Analyzer{repositoryanalysis.New()}, cache, repositorymodel.DefaultConfiguration(), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	built, err := service.Build()
	if err != nil {
		t.Fatal(err)
	}
	if !hasGap(built.Model.Gaps(), "analyzer-failure") || !hasGap(built.Model.Gaps(), "excluded-input") {
		t.Fatalf("safe degradation gaps=%#v", built.Model.Gaps())
	}
}

func TestRepositoryModelPerformanceEvidence(t *testing.T) {
	root := t.TempDir()
	git(t, root, "init", "--quiet")
	if err := os.WriteFile(filepath.Join(root, "go.mod"), []byte("module example.invalid/large\n\ngo 1.25.1\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	for index := 0; index < 533; index++ {
		name := filepath.Join(root, "pkg", "p"+fmt.Sprint(index), "source.go")
		if err := os.MkdirAll(filepath.Dir(name), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(name, []byte("package p\n\nfunc Value() int { return 1 }\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	git(t, root, "add", ".")
	git(t, root, "-c", "user.name=Praetor Test", "-c", "user.email=praetor@example.invalid", "commit", "--quiet", "-m", "534 files")
	registration := project.Registration{ProjectId: modelProjectId, RepositoryRoot: root}
	cache, err := modelcache.Open(t.TempDir(), registration)
	if err != nil {
		t.Fatal(err)
	}
	service, err := repositorymodel.NewService(modelProjectId, root, repository.InspectModelSource, []repositorymodel.Analyzer{repositoryanalysis.New()}, cache, repositorymodel.DefaultConfiguration(), time.Now)
	if err != nil {
		t.Fatal(err)
	}
	defer service.Close()
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	started := time.Now()
	full, err := service.Build()
	if err != nil {
		t.Fatal(err)
	}
	fullDuration := time.Since(started)
	runtime.ReadMemStats(&after)
	changed := filepath.Join(root, "pkg", "p0", "source.go")
	if err := os.WriteFile(changed, []byte("package p\n\nfunc Value() int { return 2 }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	started = time.Now()
	incremental, err := service.Build()
	if err != nil {
		t.Fatal(err)
	}
	incrementalDuration := time.Since(started)
	if incremental.Statistics.ReusedFiles != 533 || incremental.Statistics.AnalyzedFiles != 1 {
		t.Fatalf("incremental reuse=%d analyzed=%d, want 533/1", incremental.Statistics.ReusedFiles, incremental.Statistics.AnalyzedFiles)
	}
	if len(full.Model.Nodes()) == 0 || len(full.Model.Edges()) == 0 {
		t.Fatal("performance fixture produced an empty graph")
	}
	t.Logf("production 534-file evidence: full=%s incremental=%s nodes=%d edges=%d full_alloc_delta=%d reused=%d analyzed=%d", fullDuration, incrementalDuration, len(full.Model.Nodes()), len(full.Model.Edges()), after.TotalAlloc-before.TotalAlloc, incremental.Statistics.ReusedFiles, incremental.Statistics.AnalyzedFiles)
}

func assertEvidenceSemantics(t *testing.T, model repositorymodel.Model) {
	t.Helper()
	inferred := false
	for _, edge := range model.Edges() {
		if edge.Assertion.Class == repositorymodel.EvidenceInferred {
			inferred = true
			if edge.Assertion.Confidence == "" {
				t.Fatal("inferred edge lacks confidence")
			}
		} else if edge.Assertion.Confidence != "" {
			t.Fatal("deterministic edge has invented confidence")
		}
		if edge.Assertion.Provenance.AnalyzerId == "" || len(edge.Assertion.Provenance.Evidence) == 0 {
			t.Fatal("edge lacks provenance")
		}
	}
	if !inferred {
		t.Fatal("bounded history/test analyzer produced no inferred relationship")
	}
}
func assertGraphSignals(t *testing.T, model repositorymodel.Model) {
	t.Helper()
	nodeKinds := map[repositorymodel.NodeKind]bool{}
	edgeKinds := map[repositorymodel.EdgeKind]bool{}
	for _, node := range model.Nodes() {
		nodeKinds[node.Kind] = true
	}
	for _, edge := range model.Edges() {
		edgeKinds[edge.Kind] = true
	}
	for _, kind := range []repositorymodel.NodeKind{repositorymodel.NodeModule, repositorymodel.NodeSymbol, repositorymodel.NodeTest, repositorymodel.NodeAPI, repositorymodel.NodeExternalDependency, repositorymodel.NodeOwner, repositorymodel.NodeArchitectureElement} {
		if !nodeKinds[kind] {
			t.Fatalf("heterogeneous graph lacks node kind %s", kind)
		}
	}
	for _, kind := range []repositorymodel.EdgeKind{repositorymodel.EdgeContains, repositorymodel.EdgeDeclares, repositorymodel.EdgeDependsOn, repositorymodel.EdgeTests, repositorymodel.EdgeExposes, repositorymodel.EdgeOwnedBy, repositorymodel.EdgeAssociatedWithArchitecture, repositorymodel.EdgeCoChangesWith} {
		if !edgeKinds[kind] {
			t.Fatalf("heterogeneous graph lacks edge kind %s", kind)
		}
	}
}
func hasGap(gaps []repositorymodel.KnowledgeGap, category string) bool {
	for _, gap := range gaps {
		if gap.Category == category {
			return true
		}
	}
	return false
}

func heterogeneousRepository(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	git(t, root, "init", "--quiet")
	files := map[string]string{
		"go.mod":                         "module example.invalid/poly\n\ngo 1.25.1\n",
		"internal/core/core.go":          "package core\n\nfunc Value() int { return 1 }\n",
		"internal/core/core_test.go":     "package core\n\nimport \"testing\"\nfunc TestValue(t *testing.T) { _ = Value() }\n",
		"internal/service/service.go":    "package service\n\nimport \"example.invalid/poly/internal/core\"\nfunc Read() int { return core.Value() }\n",
		"src/main/java/example/App.java": "package example; import java.util.List; public class App {}\n",
		"web/api.ts":                     "export function api(): number { return 1 }\n",
		"web/api.test.ts":                "import { api } from './api'; api();\n",
		"package.json":                   `{"name":"poly","dependencies":{"left-pad":"1.0.0"}}`,
		"pom.xml":                        `<project><dependencies><dependency><groupId>org.example</groupId><artifactId>lib</artifactId></dependency></dependencies></project>`,
		"CODEOWNERS":                     "/internal/ @backend\n/web/ @frontend\n",
		"docs/system.puml":               "@startuml\nComponent(core, \"Core\", \"Go\")\n@enduml\n",
		"scripts/unknown.py":             "print('unsupported')\n",
		"vendor/generated.go":            "package vendor\n",
	}
	for name, content := range files {
		absolute := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(absolute, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	git(t, root, "add", ".")
	git(t, root, "-c", "user.name=Praetor Test", "-c", "user.email=praetor@example.invalid", "commit", "--quiet", "-m", "baseline")
	// A second co-change observation produces bounded inferred history edges.
	if err := os.WriteFile(filepath.Join(root, "internal/core/core.go"), []byte("package core\n\nfunc Value() int { return 3 }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(root, "internal/service/service.go"), []byte("package service\n\nimport \"example.invalid/poly/internal/core\"\nfunc Read() int { return core.Value()+1 }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	git(t, root, "add", ".")
	git(t, root, "-c", "user.name=Praetor Test", "-c", "user.email=praetor@example.invalid", "commit", "--quiet", "-m", "cochange")
	return root
}
func git(t *testing.T, root string, arguments ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, arguments...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", arguments, err, output)
	}
}
