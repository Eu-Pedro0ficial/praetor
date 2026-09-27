package command

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/Eu-Pedro0ficial/praetor/internal/artifact"
	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"github.com/Eu-Pedro0ficial/praetor/internal/project"
	"github.com/Eu-Pedro0ficial/praetor/internal/source"
)

func TestDecodeApprovedScopeRequestPreservesHistoricalExplicitAuthority(t *testing.T) {
	historical := newApprovedScopeTestArtifact(t, 1, []byte(`{
		"authorization_mode": "repository-wide",
		"expected": ["internal/service/service.go"],
		"possible": ["internal/service/service_test.go"],
		"protected": ["go.mod"]
	}`))
	request, err := decodeApprovedScopeRequest(historical)
	if err != nil {
		t.Fatalf("decode historical ApprovedScope: %v", err)
	}
	if request.AuthorizationMode != source.AuthorizationExplicitPaths {
		t.Fatalf("historical authorization mode = %q, want explicit-paths", request.AuthorizationMode)
	}
	if !slices.Equal(request.Expected, []string{"internal/service/service.go"}) ||
		!slices.Equal(request.Possible, []string{"internal/service/service_test.go"}) ||
		!slices.Equal(request.Protected, []string{"go.mod"}) {
		t.Fatalf("historical scope request = %#v", request)
	}
	surface, err := source.NewChangeSurface(request)
	if err != nil {
		t.Fatalf("rehydrate historical ChangeSurface: %v", err)
	}
	result, err := source.RehydrateApprovedScope(
		change.ChangeId("change-historical-scope"),
		project.ProjectId("01890c29-7a78-7abc-8def-0123456789ab"),
		source.SourceStateDigest("sha256:"+strings.Repeat("a", 64)),
		request,
	)
	if err != nil {
		t.Fatalf("rehydrate historical ApprovedScope: %v", err)
	}
	if surface.AuthorizationMode() != source.AuthorizationExplicitPaths ||
		result.Surface().AuthorizationMode() != source.AuthorizationExplicitPaths {
		t.Fatal("historical v1 scope was silently widened")
	}
	validation, err := result.ValidateActualSurface([]string{"README.md"})
	if err == nil || validation.Allowed() ||
		len(validation.Violations()) != 1 ||
		validation.Violations()[0].Kind() != source.ViolationUnexpected {
		t.Fatalf("historical strict validation = %#v, error = %v", validation, err)
	}
}

func TestDecodeApprovedScopeRequestRequiresModeInSchemaV2(t *testing.T) {
	current := newApprovedScopeTestArtifact(t, 2, []byte(`{
		"authorization_mode": "repository-wide",
		"protected": ["go.mod"]
	}`))
	request, err := decodeApprovedScopeRequest(current)
	if err != nil || request.AuthorizationMode != source.AuthorizationRepositoryWide {
		t.Fatalf("decode v2 ApprovedScope = %#v, error = %v", request, err)
	}

	missingMode := newApprovedScopeTestArtifact(t, 2, []byte(`{"expected":["service.go"]}`))
	if _, err := decodeApprovedScopeRequest(missingMode); err == nil ||
		!strings.Contains(err.Error(), "requires authorization_mode") {
		t.Fatalf("missing v2 authorization mode error = %v", err)
	}
}

func newApprovedScopeTestArtifact(t *testing.T, kindSchemaVersion uint32, payload []byte) artifact.Artifact {
	t.Helper()
	id, err := artifact.GenerateId()
	if err != nil {
		t.Fatal(err)
	}
	item, err := artifact.New(
		id,
		project.ProjectId("01890c29-7a78-7abc-8def-0123456789ab"),
		change.ChangeId("change-historical-scope"),
		artifact.KindApprovedScope,
		1,
		kindSchemaVersion,
		"application/json",
		time.Date(2026, time.September, 27, 12, 0, 0, 0, time.UTC),
		artifact.Producer{Component: "hydration-compatibility-test"},
		payload,
		false,
	)
	if err != nil {
		t.Fatalf("create ApprovedScope artifact: %v", err)
	}
	return item
}
