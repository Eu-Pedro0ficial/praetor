package impact_test

import (
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/Eu-Pedro0ficial/praetor/internal/adapters/persistence/modelcache"
	sqliteadapter "github.com/Eu-Pedro0ficial/praetor/internal/adapters/persistence/sqlite"
	"github.com/Eu-Pedro0ficial/praetor/internal/adapters/repositoryanalysis"
	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"github.com/Eu-Pedro0ficial/praetor/internal/impact"
	"github.com/Eu-Pedro0ficial/praetor/internal/project"
	"github.com/Eu-Pedro0ficial/praetor/internal/repository"
	"github.com/Eu-Pedro0ficial/praetor/internal/repositorymodel"
	"github.com/Eu-Pedro0ficial/praetor/internal/workflow"
)

const serviceProjectId project.ProjectId = "01890c29-7a78-7abc-8def-0123456789ab"

func TestDurableImpactReportSurvivesRestartAndCacheLoss(t *testing.T) {
	root := impactRepository(t)
	registration := project.Registration{ProjectId: serviceProjectId, RepositoryRoot: root}
	dataDirectory, cacheDirectory := t.TempDir(), t.TempDir()
	store, modelService, impactService, workflowService := openServices(t, dataDirectory, cacheDirectory, registration)
	current, err := workflowService.Create("durable-impact", "change core", "test")
	if err != nil {
		t.Fatal(err)
	}
	result, err := impactService.AnalyzeAndPersist(current.ChangeId(), impact.Request{Expected: []string{"core/core.go"}, Protected: []string{"go.mod"}})
	if err != nil {
		t.Fatal(err)
	}
	if result.Artifact.Kind() != "impact-report" || result.Report.RepositoryModelDigest == "" || len(result.Report.Items) == 0 {
		t.Fatalf("result=%#v", result)
	}
	for _, pathValue := range []string{"consumer/consumer.go", "core/core_test.go"} {
		found := false
		for _, item := range result.Report.Items {
			if item.Element.Path == pathValue {
				found = len(item.Explanation) > 0
			}
		}
		if !found {
			t.Fatalf("production graph traversal did not explain downstream path %q: %#v", pathValue, result.Report.Items)
		}
	}
	persistedChange, _, err := store.GetChange(current.ChangeId())
	if err != nil {
		t.Fatal(err)
	}
	if persistedChange.State() != change.StateCreated || persistedChange.Revision() != current.Revision() || !result.Report.Risk.Advisory {
		t.Fatal("impact or risk changed workflow authority")
	}
	history, err := store.AuditHistory(current.ChangeId())
	if err != nil {
		t.Fatal(err)
	}
	audited := false
	for _, event := range history {
		if event.EventType == impact.EventImpactReportCommitted {
			audited = true
		}
	}
	if !audited {
		t.Fatal("durable ImpactReport lacks audit evidence")
	}
	if _, err := impactService.AnalyzeAndPersist(current.ChangeId(), impact.Request{Expected: []string{"core/core.go"}}); !errors.Is(err, impact.ErrReportAlreadyBound) {
		t.Fatalf("same-revision replacement error=%v", err)
	}
	cachePath := filepath.Join(cacheDirectory, "projects", string(registration.ProjectId), "repository-model-cache.sqlite3")
	if err := modelService.Close(); err != nil {
		t.Fatal(err)
	}
	if err := store.Close(); err != nil {
		t.Fatal(err)
	}
	for _, suffix := range []string{"", "-wal", "-shm"} {
		if err := os.Remove(cachePath + suffix); err != nil && !os.IsNotExist(err) {
			t.Fatal(err)
		}
	}

	restartedStore, restartedModels, restartedImpact, restartedWorkflow := openServices(t, dataDirectory, cacheDirectory, registration)
	defer restartedModels.Close()
	defer restartedStore.Close()
	report, inspectedId, freshness, err := restartedImpact.Inspect(current.ChangeId(), result.Artifact.Id())
	if err != nil {
		t.Fatal(err)
	}
	if inspectedId != result.Artifact.Id() || freshness != repositorymodel.FreshnessCurrent || report.ReportDigest != result.Report.ReportDigest || len(report.Items) != len(result.Report.Items) {
		t.Fatalf("restart report freshness=%s report=%#v", freshness, report)
	}
	if err := os.WriteFile(filepath.Join(root, "core", "core.go"), []byte("package core\nfunc Value() int { return 2 }\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	_, _, freshness, err = restartedImpact.Inspect(current.ChangeId(), result.Artifact.Id())
	if err != nil || freshness != repositorymodel.FreshnessStale {
		t.Fatalf("stale report freshness=%s err=%v", freshness, err)
	}
	advanced, err := restartedWorkflow.Transition(current.ChangeId(), change.StatePlanned, "new report revision")
	if err != nil {
		t.Fatal(err)
	}
	replacement, err := restartedImpact.AnalyzeAndPersist(advanced.ChangeId(), impact.Request{Expected: []string{"core/core.go"}})
	if err != nil {
		t.Fatal(err)
	}
	if replacement.Artifact.Id() == result.Artifact.Id() {
		t.Fatal("ImpactReport was mutated instead of appended")
	}
	bindings, err := restartedStore.ListBindings(current.ChangeId())
	if err != nil {
		t.Fatal(err)
	}
	found := false
	for _, binding := range bindings {
		if binding.Role == impact.BindingRole {
			found = binding.ArtifactId == replacement.Artifact.Id() && binding.Revision == advanced.Revision()
		}
	}
	if !found {
		t.Fatalf("current ImpactReport binding=%#v", bindings)
	}
	artifacts, err := restartedStore.ListArtifacts(current.ChangeId())
	if err != nil {
		t.Fatal(err)
	}
	count := 0
	for _, item := range artifacts {
		if item.Kind == "impact-report" {
			count++
		}
	}
	if count != 2 {
		t.Fatalf("durable ImpactReport count=%d", count)
	}
}

func openServices(t *testing.T, dataDirectory, cacheDirectory string, registration project.Registration) (*sqliteadapter.Store, *repositorymodel.Service, *impact.Service, *workflow.Service) {
	t.Helper()
	store, err := sqliteadapter.Open(dataDirectory, registration)
	if err != nil {
		t.Fatal(err)
	}
	cache, err := modelcache.Open(cacheDirectory, registration)
	if err != nil {
		store.Close()
		t.Fatal(err)
	}
	models, err := repositorymodel.NewService(registration.ProjectId, registration.RepositoryRoot, repository.InspectModelSource, []repositorymodel.Analyzer{repositoryanalysis.New()}, cache, repositorymodel.DefaultConfiguration(), time.Now)
	if err != nil {
		cache.Close()
		store.Close()
		t.Fatal(err)
	}
	impacts, err := impact.NewService(registration.ProjectId, registration.RepositoryRoot, models, store, impact.DefaultConfiguration(), time.Now)
	if err != nil {
		models.Close()
		store.Close()
		t.Fatal(err)
	}
	workflows, err := workflow.NewDurable(store, registration.ProjectId, time.Now)
	if err != nil {
		models.Close()
		store.Close()
		t.Fatal(err)
	}
	return store, models, impacts, workflows
}

func impactRepository(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	runGit(t, root, "init", "--quiet")
	files := map[string]string{"go.mod": "module example.invalid/impact\n\ngo 1.25.1\n", "core/core.go": "package core\nfunc Value() int { return 1 }\n", "consumer/consumer.go": "package consumer\nimport \"example.invalid/impact/core\"\nfunc Read() int { return core.Value() }\n", "core/core_test.go": "package core\nimport \"testing\"\nfunc TestValue(t *testing.T) { _ = Value() }\n"}
	for name, content := range files {
		absolute := filepath.Join(root, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(absolute), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(absolute, []byte(content), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	runGit(t, root, "add", ".")
	runGit(t, root, "-c", "user.name=Praetor Test", "-c", "user.email=praetor@example.invalid", "commit", "--quiet", "-m", "baseline")
	return root
}
func runGit(t *testing.T, root string, args ...string) {
	t.Helper()
	command := exec.Command("git", append([]string{"-C", root}, args...)...)
	if output, err := command.CombinedOutput(); err != nil {
		t.Fatalf("git %v: %v: %s", args, err, output)
	}
}
