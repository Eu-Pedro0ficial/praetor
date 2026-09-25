package repositoryanalysis

import (
	"strings"
	"testing"

	"github.com/Eu-Pedro0ficial/praetor/internal/repositorymodel"
)

func TestStaticAnalyzerRejectsRepositoryEscapingImport(t *testing.T) {
	file := repositorymodel.FileInput{Path: "web/api.ts", Kind: "regular", Available: true, ContentDigest: repositorymodel.DigestJSON("escape"), Content: []byte(`import value from '../../outside'; export const api = value`)}
	_, err := New().Analyze(file)
	if err == nil || !strings.Contains(err.Error(), "escapes repository scope") {
		t.Fatalf("unsafe import error=%v", err)
	}
}

func TestStaticAnalyzerRejectsMalformedSupportedMetadata(t *testing.T) {
	tests := []repositorymodel.FileInput{
		{Path: "go.mod", Content: []byte("go 1.25.1\n")},
		{Path: "pom.xml", Content: []byte("<project><dependency>")},
		{Path: "CODEOWNERS", Content: []byte("../outside @owner\n")},
	}
	for _, file := range tests {
		t.Run(file.Path, func(t *testing.T) {
			file.Kind, file.Available, file.ContentDigest = "regular", true, repositorymodel.DigestJSON(file.Path)
			if _, err := New().Analyze(file); err == nil {
				t.Fatalf("malformed %s was accepted", file.Path)
			}
		})
	}
}
