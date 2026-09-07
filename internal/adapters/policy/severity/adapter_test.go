package severity

import (
	"testing"

	"github.com/Eu-Pedro0ficial/praetor/internal/policy"
)

func TestNormalizeKeepsToolVocabularySeparateFromPraetorSeverity(t *testing.T) {
	tests := map[string]policy.Severity{"note": policy.SeverityInfo, "minor": policy.SeverityLow, "warning": policy.SeverityMedium, "error": policy.SeverityHigh, "fatal": policy.SeverityCritical}
	for native, want := range tests {
		got, err := New().Normalize(native)
		if err != nil || got != want {
			t.Fatalf("Normalize(%q) = %q, %v", native, got, err)
		}
	}
	if _, err := New().Normalize("vendor-unknown"); err == nil {
		t.Fatal("unsupported severity accepted")
	}
}
