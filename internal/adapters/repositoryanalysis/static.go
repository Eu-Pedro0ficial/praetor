// Package repositoryanalysis provides bounded, in-process, read-only initial
// analyzers for the M1.2 RepositoryModel port.
package repositoryanalysis

import (
	"bufio"
	"encoding/json"
	"fmt"
	"go/ast"
	"go/parser"
	"go/token"
	"path"
	"regexp"
	"slices"
	"strings"
	"unicode"

	"github.com/Eu-Pedro0ficial/praetor/internal/repositorymodel"
)

type Static struct{}

func New() *Static { return &Static{} }

func (*Static) Descriptor() repositorymodel.AnalyzerDescriptor {
	return repositorymodel.AnalyzerDescriptor{Id: "static-repository-analyzer", Version: "1", Capabilities: []string{"api", "architecture", "dependencies", "go", "jvm", "manifests", "ownership", "tests", "typescript"}}
}

func (*Static) Supports(file repositorymodel.FileInput) bool {
	base := path.Base(file.Path)
	extension := strings.ToLower(path.Ext(file.Path))
	if base == "go.mod" || base == "package.json" || base == "pom.xml" || base == "build.gradle" || base == "build.gradle.kts" || base == "CODEOWNERS" {
		return true
	}
	switch extension {
	case ".go", ".java", ".kt", ".kts", ".js", ".jsx", ".ts", ".tsx", ".puml":
		return true
	}
	return false
}

func (analyzer *Static) Analyze(file repositorymodel.FileInput) (repositorymodel.FileAnalysis, error) {
	result := repositorymodel.FileAnalysis{Path: file.Path}
	base := path.Base(file.Path)
	extension := strings.ToLower(path.Ext(file.Path))
	var err error
	switch {
	case base == "go.mod":
		err = analyzer.analyzeGoMod(file, &result)
	case base == "package.json":
		err = analyzer.analyzePackageJSON(file, &result)
	case base == "pom.xml":
		err = analyzer.analyzePom(file, &result)
	case base == "build.gradle" || base == "build.gradle.kts":
		analyzer.analyzeGradle(file, &result)
	case base == "CODEOWNERS":
		err = analyzer.analyzeCodeOwners(file, &result)
	case extension == ".go":
		err = analyzer.analyzeGo(file, &result)
	case extension == ".java" || extension == ".kt" || extension == ".kts":
		err = analyzer.analyzeJVM(file, &result)
	case extension == ".js" || extension == ".jsx" || extension == ".ts" || extension == ".tsx":
		err = analyzer.analyzeJavaScript(file, &result)
	case extension == ".puml":
		analyzer.analyzeArchitecture(file, &result)
	}
	return result, err
}

func (*Static) analyzeGo(file repositorymodel.FileInput, result *repositorymodel.FileAnalysis) error {
	parsed, err := parser.ParseFile(token.NewFileSet(), file.Path, file.Content, parser.SkipObjectResolution)
	if err != nil {
		return fmt.Errorf("parse Go source: %w", err)
	}
	fileId := repositorymodel.NodeId(repositorymodel.NodeFile, file.Path)
	directory := path.Dir(file.Path)
	if directory == "." {
		directory = "root"
	}
	moduleId := repositorymodel.NodeId(repositorymodel.NodeModule, "go:"+directory)
	assertion := derived(file, "go-ast", file.Path)
	result.Nodes = append(result.Nodes, repositorymodel.Node{Id: moduleId, Kind: repositorymodel.NodeModule, Name: directory, Path: path.Dir(file.Path), Language: "Go", Assertion: assertion})
	result.Edges = append(result.Edges, repositorymodel.Edge{Id: repositorymodel.EdgeId(repositorymodel.EdgeContains, moduleId, fileId), Kind: repositorymodel.EdgeContains, From: moduleId, To: fileId, Assertion: assertion})
	for _, declaration := range parsed.Decls {
		var names []string
		switch typed := declaration.(type) {
		case *ast.FuncDecl:
			names = []string{typed.Name.Name}
		case *ast.GenDecl:
			for _, spec := range typed.Specs {
				switch value := spec.(type) {
				case *ast.TypeSpec:
					names = append(names, value.Name.Name)
				case *ast.ValueSpec:
					for _, name := range value.Names {
						names = append(names, name.Name)
					}
				}
			}
		}
		for _, name := range names {
			symbolId := repositorymodel.NodeId(repositorymodel.NodeSymbol, file.Path+":"+name)
			result.Nodes = append(result.Nodes, repositorymodel.Node{Id: symbolId, Kind: repositorymodel.NodeSymbol, Name: name, Path: file.Path, Language: "Go", Assertion: assertion})
			result.Edges = append(result.Edges, repositorymodel.Edge{Id: repositorymodel.EdgeId(repositorymodel.EdgeDeclares, fileId, symbolId), Kind: repositorymodel.EdgeDeclares, From: fileId, To: symbolId, Assertion: assertion})
			if ast.IsExported(name) {
				apiId := repositorymodel.NodeId(repositorymodel.NodeAPI, "go:"+file.Path+":"+name)
				result.Nodes = append(result.Nodes, repositorymodel.Node{Id: apiId, Kind: repositorymodel.NodeAPI, Name: name, Path: file.Path, Language: "Go", Assertion: assertion})
				result.Edges = append(result.Edges, repositorymodel.Edge{Id: repositorymodel.EdgeId(repositorymodel.EdgeExposes, symbolId, apiId), Kind: repositorymodel.EdgeExposes, From: symbolId, To: apiId, Assertion: assertion})
			}
		}
	}
	for _, imported := range parsed.Imports {
		value := strings.Trim(imported.Path.Value, `"`)
		result.PendingEdges = append(result.PendingEdges, repositorymodel.PendingEdge{Kind: repositorymodel.EdgeDependsOn, From: fileId, TargetKind: repositorymodel.NodeModule, Target: value, Assertion: derived(file, "go-import", value)})
	}
	if strings.HasSuffix(file.Path, "_test.go") {
		testName := strings.TrimSuffix(path.Base(file.Path), "_test.go")
		testId := repositorymodel.NodeId(repositorymodel.NodeTest, file.Path)
		result.Nodes = append(result.Nodes, repositorymodel.Node{Id: testId, Kind: repositorymodel.NodeTest, Name: testName, Path: file.Path, Language: "Go", Assertion: assertion})
		result.Edges = append(result.Edges, repositorymodel.Edge{Id: repositorymodel.EdgeId(repositorymodel.EdgeDeclares, fileId, testId), Kind: repositorymodel.EdgeDeclares, From: fileId, To: testId, Assertion: assertion})
		target := path.Join(path.Dir(file.Path), testName+".go")
		result.PendingEdges = append(result.PendingEdges, repositorymodel.PendingEdge{Kind: repositorymodel.EdgeTests, From: testId, TargetKind: repositorymodel.NodeFile, Target: target, Assertion: inferred(file, "go-test-name", target, repositorymodel.ConfidenceMedium)})
	}
	return nil
}

func (*Static) analyzeGoMod(file repositorymodel.FileInput, result *repositorymodel.FileAnalysis) error {
	fileId := repositorymodel.NodeId(repositorymodel.NodeFile, file.Path)
	scanner := bufio.NewScanner(strings.NewReader(string(file.Content)))
	scanner.Buffer(make([]byte, 4096), 1<<20)
	inRequire := false
	hasModule := false
	for scanner.Scan() {
		line := strings.TrimSpace(strings.Split(scanner.Text(), "//")[0])
		if line == "" {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) >= 2 && fields[0] == "module" {
			hasModule = true
			id := repositorymodel.NodeId(repositorymodel.NodeModule, fields[1])
			assertion := derived(file, "go-module", fields[1])
			result.Nodes = append(result.Nodes, repositorymodel.Node{Id: id, Kind: repositorymodel.NodeModule, Name: fields[1], Path: path.Dir(file.Path), Language: "Go", Assertion: assertion})
			result.Edges = append(result.Edges, repositorymodel.Edge{Id: repositorymodel.EdgeId(repositorymodel.EdgeDeclares, fileId, id), Kind: repositorymodel.EdgeDeclares, From: fileId, To: id, Assertion: assertion})
			continue
		}
		if fields[0] == "require" {
			if len(fields) == 1 || fields[1] == "(" {
				inRequire = true
				continue
			}
			addDependency(file, result, fileId, fields[1], "go-require")
			continue
		}
		if line == ")" {
			inRequire = false
			continue
		}
		if inRequire && len(fields) >= 2 {
			addDependency(file, result, fileId, fields[0], "go-require")
		}
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan go.mod: %w", err)
	}
	if !hasModule || inRequire {
		return fmt.Errorf("malformed go.mod: module declaration or require block is incomplete")
	}
	return nil
}

var (
	jvmPackage          = regexp.MustCompile(`(?m)^\s*package\s+([A-Za-z_][\w.]*)`)
	jvmImport           = regexp.MustCompile(`(?m)^\s*import\s+(?:static\s+)?([A-Za-z_][\w.*]*)`)
	jvmType             = regexp.MustCompile(`(?m)\b(?:class|interface|enum|object|record)\s+([A-Za-z_][\w]*)`)
	jsImport            = regexp.MustCompile(`(?m)(?:from\s+|require\s*\(\s*)['"]([^'"]+)['"]`)
	jsExport            = regexp.MustCompile(`(?m)\bexport\s+(?:default\s+)?(?:class|function|const|let|var|interface|type)\s+([A-Za-z_$][\w$]*)`)
	pomDependency       = regexp.MustCompile(`(?s)<dependency>.*?<groupId>\s*([^<]+)\s*</groupId>.*?<artifactId>\s*([^<]+)\s*</artifactId>.*?</dependency>`)
	gradleDependency    = regexp.MustCompile(`(?m)\b(?:implementation|api|compileOnly|runtimeOnly|testImplementation)\s*\(?\s*['"]([^:'"]+):([^:'"]+)`)
	architectureElement = regexp.MustCompile(`(?m)\b(?:Component|Container|System|Database|Queue)\s*\(\s*([A-Za-z_][\w-]*)\s*,\s*"([^"]+)"`)
)

func (*Static) analyzeJVM(file repositorymodel.FileInput, result *repositorymodel.FileAnalysis) error {
	if !strings.Contains(string(file.Content), "class") && !strings.Contains(string(file.Content), "interface") && !strings.Contains(string(file.Content), "package") {
		return fmt.Errorf("unsupported or malformed JVM source")
	}
	fileId := repositorymodel.NodeId(repositorymodel.NodeFile, file.Path)
	language := "Java"
	if path.Ext(file.Path) != ".java" {
		language = "Kotlin"
	}
	packageName := path.Dir(file.Path)
	if match := jvmPackage.FindSubmatch(file.Content); len(match) == 2 {
		packageName = string(match[1])
	}
	moduleId := repositorymodel.NodeId(repositorymodel.NodeModule, "jvm:"+packageName)
	assertion := derived(file, "jvm-package", packageName)
	result.Nodes = append(result.Nodes, repositorymodel.Node{Id: moduleId, Kind: repositorymodel.NodeModule, Name: packageName, Path: path.Dir(file.Path), Language: language, Assertion: assertion})
	result.Edges = append(result.Edges, repositorymodel.Edge{Id: repositorymodel.EdgeId(repositorymodel.EdgeContains, moduleId, fileId), Kind: repositorymodel.EdgeContains, From: moduleId, To: fileId, Assertion: assertion})
	for _, match := range jvmType.FindAllSubmatch(file.Content, 128) {
		name := string(match[1])
		symbolId := repositorymodel.NodeId(repositorymodel.NodeSymbol, file.Path+":"+name)
		result.Nodes = append(result.Nodes, repositorymodel.Node{Id: symbolId, Kind: repositorymodel.NodeSymbol, Name: name, Path: file.Path, Language: language, Assertion: assertion})
		result.Edges = append(result.Edges, repositorymodel.Edge{Id: repositorymodel.EdgeId(repositorymodel.EdgeDeclares, fileId, symbolId), Kind: repositorymodel.EdgeDeclares, From: fileId, To: symbolId, Assertion: assertion})
		if unicode.IsUpper(rune(name[0])) {
			apiId := repositorymodel.NodeId(repositorymodel.NodeAPI, "jvm:"+packageName+"."+name)
			result.Nodes = append(result.Nodes, repositorymodel.Node{Id: apiId, Kind: repositorymodel.NodeAPI, Name: name, Path: file.Path, Language: language, Assertion: assertion})
			result.Edges = append(result.Edges, repositorymodel.Edge{Id: repositorymodel.EdgeId(repositorymodel.EdgeExposes, symbolId, apiId), Kind: repositorymodel.EdgeExposes, From: symbolId, To: apiId, Assertion: assertion})
		}
	}
	for _, match := range jvmImport.FindAllSubmatch(file.Content, 256) {
		value := strings.TrimSuffix(string(match[1]), ".*")
		result.PendingEdges = append(result.PendingEdges, repositorymodel.PendingEdge{Kind: repositorymodel.EdgeDependsOn, From: fileId, TargetKind: repositorymodel.NodeModule, Target: value, Assertion: derived(file, "jvm-import", value)})
	}
	if strings.HasSuffix(file.Path, "Test.java") || strings.HasSuffix(file.Path, "Test.kt") {
		testId := repositorymodel.NodeId(repositorymodel.NodeTest, file.Path)
		result.Nodes = append(result.Nodes, repositorymodel.Node{Id: testId, Kind: repositorymodel.NodeTest, Name: path.Base(file.Path), Path: file.Path, Language: language, Assertion: assertion})
		result.Edges = append(result.Edges, repositorymodel.Edge{Id: repositorymodel.EdgeId(repositorymodel.EdgeDeclares, fileId, testId), Kind: repositorymodel.EdgeDeclares, From: fileId, To: testId, Assertion: assertion})
		base := strings.TrimSuffix(strings.TrimSuffix(path.Base(file.Path), "Test.java"), "Test.kt")
		result.PendingEdges = append(result.PendingEdges, repositorymodel.PendingEdge{Kind: repositorymodel.EdgeTests, From: testId, TargetKind: repositorymodel.NodeSymbol, Target: base, Assertion: inferred(file, "jvm-test-name", base, repositorymodel.ConfidenceLow)})
	}
	return nil
}

func (*Static) analyzeJavaScript(file repositorymodel.FileInput, result *repositorymodel.FileAnalysis) error {
	fileId := repositorymodel.NodeId(repositorymodel.NodeFile, file.Path)
	language := "JavaScript"
	if strings.Contains(path.Ext(file.Path), "ts") {
		language = "TypeScript"
	}
	moduleName := strings.TrimSuffix(file.Path, path.Ext(file.Path))
	moduleId := repositorymodel.NodeId(repositorymodel.NodeModule, "js:"+moduleName)
	assertion := derived(file, "javascript-module", moduleName)
	result.Nodes = append(result.Nodes, repositorymodel.Node{Id: moduleId, Kind: repositorymodel.NodeModule, Name: moduleName, Path: path.Dir(file.Path), Language: language, Assertion: assertion})
	result.Edges = append(result.Edges, repositorymodel.Edge{Id: repositorymodel.EdgeId(repositorymodel.EdgeContains, moduleId, fileId), Kind: repositorymodel.EdgeContains, From: moduleId, To: fileId, Assertion: assertion})
	for _, match := range jsImport.FindAllSubmatch(file.Content, 256) {
		value := string(match[1])
		if !strings.HasPrefix(value, ".") {
			addDependency(file, result, fileId, value, "javascript-import")
			continue
		}
		target := path.Clean(path.Join(path.Dir(file.Path), value))
		if target == ".." || strings.HasPrefix(target, "../") || strings.HasPrefix(target, "/") {
			return fmt.Errorf("relative import escapes repository scope: %q", value)
		}
		result.PendingEdges = append(result.PendingEdges, repositorymodel.PendingEdge{Kind: repositorymodel.EdgeDependsOn, From: fileId, TargetKind: repositorymodel.NodeFile, Target: target, Assertion: derived(file, "javascript-import", value)})
	}
	for _, match := range jsExport.FindAllSubmatch(file.Content, 128) {
		name := string(match[1])
		apiId := repositorymodel.NodeId(repositorymodel.NodeAPI, "js:"+file.Path+":"+name)
		result.Nodes = append(result.Nodes, repositorymodel.Node{Id: apiId, Kind: repositorymodel.NodeAPI, Name: name, Path: file.Path, Language: language, Assertion: assertion})
		result.Edges = append(result.Edges, repositorymodel.Edge{Id: repositorymodel.EdgeId(repositorymodel.EdgeExposes, fileId, apiId), Kind: repositorymodel.EdgeExposes, From: fileId, To: apiId, Assertion: assertion})
	}
	if strings.Contains(file.Path, ".test.") || strings.Contains(file.Path, ".spec.") {
		testId := repositorymodel.NodeId(repositorymodel.NodeTest, file.Path)
		result.Nodes = append(result.Nodes, repositorymodel.Node{Id: testId, Kind: repositorymodel.NodeTest, Name: path.Base(file.Path), Path: file.Path, Language: language, Assertion: assertion})
		result.Edges = append(result.Edges, repositorymodel.Edge{Id: repositorymodel.EdgeId(repositorymodel.EdgeDeclares, fileId, testId), Kind: repositorymodel.EdgeDeclares, From: fileId, To: testId, Assertion: assertion})
	}
	return nil
}

func (*Static) analyzePackageJSON(file repositorymodel.FileInput, result *repositorymodel.FileAnalysis) error {
	var document struct {
		Name            string            `json:"name"`
		Dependencies    map[string]string `json:"dependencies"`
		DevDependencies map[string]string `json:"devDependencies"`
	}
	if err := json.Unmarshal(file.Content, &document); err != nil {
		return fmt.Errorf("parse package.json: %w", err)
	}
	fileId := repositorymodel.NodeId(repositorymodel.NodeFile, file.Path)
	if strings.TrimSpace(document.Name) != "" {
		moduleId := repositorymodel.NodeId(repositorymodel.NodeModule, "npm:"+document.Name)
		assertion := derived(file, "package-json-name", document.Name)
		result.Nodes = append(result.Nodes, repositorymodel.Node{Id: moduleId, Kind: repositorymodel.NodeModule, Name: document.Name, Path: path.Dir(file.Path), Language: "JavaScript", Assertion: assertion})
		result.Edges = append(result.Edges, repositorymodel.Edge{Id: repositorymodel.EdgeId(repositorymodel.EdgeDeclares, fileId, moduleId), Kind: repositorymodel.EdgeDeclares, From: fileId, To: moduleId, Assertion: assertion})
	}
	names := make([]string, 0, len(document.Dependencies)+len(document.DevDependencies))
	for name := range document.Dependencies {
		names = append(names, name)
	}
	for name := range document.DevDependencies {
		names = append(names, name)
	}
	slices.Sort(names)
	names = slices.Compact(names)
	for _, name := range names {
		addDependency(file, result, fileId, name, "package-json-dependency")
	}
	return nil
}

func (*Static) analyzePom(file repositorymodel.FileInput, result *repositorymodel.FileAnalysis) error {
	contents := string(file.Content)
	if !strings.Contains(contents, "<project") || !strings.Contains(contents, "</project>") || strings.Count(contents, "<dependency>") != strings.Count(contents, "</dependency>") {
		return fmt.Errorf("malformed pom.xml structure")
	}
	fileId := repositorymodel.NodeId(repositorymodel.NodeFile, file.Path)
	for _, match := range pomDependency.FindAllSubmatch(file.Content, 256) {
		addDependency(file, result, fileId, strings.TrimSpace(string(match[1]))+":"+strings.TrimSpace(string(match[2])), "maven-dependency")
	}
	return nil
}
func (*Static) analyzeGradle(file repositorymodel.FileInput, result *repositorymodel.FileAnalysis) {
	fileId := repositorymodel.NodeId(repositorymodel.NodeFile, file.Path)
	for _, match := range gradleDependency.FindAllSubmatch(file.Content, 256) {
		addDependency(file, result, fileId, string(match[1])+":"+string(match[2]), "gradle-dependency")
	}
}

func (*Static) analyzeCodeOwners(file repositorymodel.FileInput, result *repositorymodel.FileAnalysis) error {
	scanner := bufio.NewScanner(strings.NewReader(string(file.Content)))
	scanner.Buffer(make([]byte, 4096), 1<<20)
	for scanner.Scan() {
		line := strings.TrimSpace(scanner.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return fmt.Errorf("malformed CODEOWNERS rule")
		}
		cleanedPattern := path.Clean(strings.TrimPrefix(fields[0], "/"))
		if cleanedPattern == ".." || strings.HasPrefix(cleanedPattern, "../") {
			return fmt.Errorf("unsafe CODEOWNERS pattern %q", fields[0])
		}
		owners := append([]string(nil), fields[1:]...)
		slices.Sort(owners)
		assertion := derived(file, "codeowners-rule", fields[0])
		for _, owner := range owners {
			id := repositorymodel.NodeId(repositorymodel.NodeOwner, owner)
			result.Nodes = append(result.Nodes, repositorymodel.Node{Id: id, Kind: repositorymodel.NodeOwner, Name: owner, Assertion: assertion})
		}
		result.OwnerRules = append(result.OwnerRules, repositorymodel.OwnerRule{Pattern: fields[0], Owners: owners, Assertion: assertion})
	}
	if err := scanner.Err(); err != nil {
		return fmt.Errorf("scan CODEOWNERS: %w", err)
	}
	return nil
}

func (*Static) analyzeArchitecture(file repositorymodel.FileInput, result *repositorymodel.FileAnalysis) {
	fileId := repositorymodel.NodeId(repositorymodel.NodeFile, file.Path)
	for _, match := range architectureElement.FindAllSubmatch(file.Content, 256) {
		identity, name := string(match[1]), string(match[2])
		assertion := derived(file, "plantuml-element", identity)
		id := repositorymodel.NodeId(repositorymodel.NodeArchitectureElement, identity)
		result.Nodes = append(result.Nodes, repositorymodel.Node{Id: id, Kind: repositorymodel.NodeArchitectureElement, Name: name, Path: file.Path, Assertion: assertion})
		result.Edges = append(result.Edges, repositorymodel.Edge{Id: repositorymodel.EdgeId(repositorymodel.EdgeAssociatedWithArchitecture, fileId, id), Kind: repositorymodel.EdgeAssociatedWithArchitecture, From: fileId, To: id, Assertion: assertion})
	}
}

func addDependency(file repositorymodel.FileInput, result *repositorymodel.FileAnalysis, from, name, evidenceKind string) {
	id := repositorymodel.NodeId(repositorymodel.NodeExternalDependency, name)
	assertion := derived(file, evidenceKind, name)
	result.Nodes = append(result.Nodes, repositorymodel.Node{Id: id, Kind: repositorymodel.NodeExternalDependency, Name: name, Assertion: assertion})
	result.Edges = append(result.Edges, repositorymodel.Edge{Id: repositorymodel.EdgeId(repositorymodel.EdgeDependsOn, from, id), Kind: repositorymodel.EdgeDependsOn, From: from, To: id, Assertion: assertion})
}

func derived(file repositorymodel.FileInput, kind, detail string) repositorymodel.Assertion {
	return repositorymodel.Assertion{Class: repositorymodel.EvidenceDerived, Provenance: repositorymodel.Provenance{AnalyzerId: "static-repository-analyzer", AnalyzerVersion: "1", Inputs: []string{file.Path}, Evidence: []repositorymodel.Evidence{{Kind: kind, Reference: file.Path, Digest: file.ContentDigest, Detail: detail}}}}
}
func inferred(file repositorymodel.FileInput, kind, detail string, confidence repositorymodel.Confidence) repositorymodel.Assertion {
	value := derived(file, kind, detail)
	value.Class, value.Confidence = repositorymodel.EvidenceInferred, confidence
	return value
}
