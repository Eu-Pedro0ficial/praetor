package command_test

import (
	"bytes"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"github.com/Eu-Pedro0ficial/praetor/internal/command"
	"github.com/Eu-Pedro0ficial/praetor/internal/composition"
)

func TestM12CommandsBuildPersistAndInspectExplainableImpact(t *testing.T) {
	repositoryRoot, _, session, registry := prepareCommittedCommandTest(t)
	if _, err := registry.Dispatch(session, `change new change-m12 "repository intelligence"`, &bytes.Buffer{}); err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	if _, err := registry.Dispatch(session, "analysis model", &output); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"RepositoryModel ID:", "Source fingerprint:", "freshness=CURRENT", "Graph:"} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("model output %q lacks %q", output.String(), expected)
		}
	}
	output.Reset()
	if _, err := registry.Dispatch(session, "analysis report change-m12 --expected internal/service/service.go --protected internal/service/service.go", &output); err != nil {
		t.Fatal(err)
	}
	for _, expected := range []string{"ImpactReport artifact:", "Risk:", "risk breadth", "freshness=CURRENT", "PROTECTED", "protected exposure", "why "} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("report output %q lacks %q", output.String(), expected)
		}
	}
	output.Reset()
	if _, err := registry.Dispatch(session, "change artifacts change-m12", &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "impact-report") || !strings.Contains(output.String(), "binding role=impact-report") {
		t.Fatalf("durable artifact output=%q", output.String())
	}
	if err := os.WriteFile(filepath.Join(repositoryRoot, "internal", "service", "service.go"), []byte("package service\nfunc Changed() {}\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	output.Reset()
	if _, err := registry.Dispatch(session, "analysis inspect change-m12", &output); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(output.String(), "ImpactReport artifact:") || !strings.Contains(output.String(), "freshness=STALE") || !strings.Contains(output.String(), "Report digest:") {
		t.Fatalf("stale inspection output=%q", output.String())
	}
}

func TestM12IsolateResumesCreatedDurableChangeAfterSessionRestart(t *testing.T) {
	repositoryRoot, _, first, registry := prepareCommittedCommandTest(t)

	const changeID = "change-m12-resume"
	const intent = "resume durable repository intelligence change"

	if _, err := registry.Dispatch(
		first,
		`change new `+changeID+` "`+intent+`"`,
		io.Discard,
	); err != nil {
		t.Fatalf("create durable Change: %v", err)
	}

	created, ok := first.CurrentChange()
	if !ok {
		t.Fatal("created Change was not selected")
	}
	if created.State() != change.StateCreated {
		t.Fatalf("created state=%s, want %s", created.State(), change.StateCreated)
	}
	if created.Revision() != 1 {
		t.Fatalf("created revision=%d, want 1", created.Revision())
	}

	if err := first.Close(); err != nil {
		t.Fatalf("close first session: %v", err)
	}

	second, err := composition.New().NewInteractiveSession(repositoryRoot)
	if err != nil {
		t.Fatalf("restart session: %v", err)
	}
	t.Cleanup(func() { _ = second.Close() })

	secondRegistry, err := command.DefaultRegistry()
	if err != nil {
		t.Fatalf("DefaultRegistry after restart: %v", err)
	}

	var output bytes.Buffer
	if _, err := secondRegistry.Dispatch(
		second,
		`change isolate `+changeID+` "`+intent+`" --expected internal/service/service.go --protected go.mod`,
		&output,
	); err != nil {
		t.Fatalf("resume durable Change with isolate after restart: %v\noutput=%s", err, output.String())
	}

	for _, want := range []string{
		"Change ID: " + changeID,
		"Change state: isolated",
		"Workspace state:",
		"Base revision:",
		"Source state digest:",
	} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("isolate output lacks %q: %q", want, output.String())
		}
	}

	current, ok := second.CurrentChange()
	if !ok {
		t.Fatal("resumed Change was not selected")
	}
	if current.ChangeId() != change.ChangeId(changeID) {
		t.Fatalf("ChangeId=%q, want %q", current.ChangeId(), changeID)
	}
	if current.State() != change.StateIsolated {
		t.Fatalf("state=%s, want %s", current.State(), change.StateIsolated)
	}
	if current.Revision() != 3 {
		t.Fatalf("revision=%d, want 3", current.Revision())
	}

	output.Reset()
	if _, err := secondRegistry.Dispatch(
		second,
		"change show "+changeID,
		&output,
	); err != nil {
		t.Fatalf("inspect resumed durable Change: %v", err)
	}

	for _, want := range []string{
		"Change ID: " + changeID,
		"Intent: " + intent,
		"State: isolated",
		"Revision: 3",
	} {
		if !strings.Contains(output.String(), want) {
			t.Fatalf("show output lacks %q: %q", want, output.String())
		}
	}
}

func TestM12IsolateExistingDurableChangeFailsClosedWhenResumeIsIncompatible(t *testing.T) {
	tests := []struct {
		name           string
		createCommand  string
		isolateCommand string
		wantError      string
	}{
		{
			name:           "intent mismatch",
			createCommand:  `change new change-m12-intent "original intent"`,
			isolateCommand: `change isolate change-m12-intent "different intent" --expected internal/service/service.go`,
			wantError:      "intent does not match isolate request",
		},
		{
			name:           "state is not created",
			createCommand:  `change new change-m12-state "already planned" planned`,
			isolateCommand: `change isolate change-m12-state "already planned" --expected internal/service/service.go`,
			wantError:      "must be in CREATED state to isolate",
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repositoryRoot, _, first, registry := prepareCommittedCommandTest(t)

			if _, err := registry.Dispatch(first, test.createCommand, io.Discard); err != nil {
				t.Fatalf("prepare durable Change: %v", err)
			}

			before, ok := first.CurrentChange()
			if !ok {
				t.Fatal("prepared Change was not selected")
			}

			if err := first.Close(); err != nil {
				t.Fatalf("close first session: %v", err)
			}

			second, err := composition.New().NewInteractiveSession(repositoryRoot)
			if err != nil {
				t.Fatalf("restart session: %v", err)
			}
			t.Cleanup(func() { _ = second.Close() })

			secondRegistry, err := command.DefaultRegistry()
			if err != nil {
				t.Fatalf("DefaultRegistry after restart: %v", err)
			}

			_, isolateErr := secondRegistry.Dispatch(
				second,
				test.isolateCommand,
				io.Discard,
			)
			if isolateErr == nil || !strings.Contains(isolateErr.Error(), test.wantError) {
				t.Fatalf("isolate error=%v, want containing %q", isolateErr, test.wantError)
			}

			var output bytes.Buffer
			if _, err := secondRegistry.Dispatch(
				second,
				"change show "+string(before.ChangeId()),
				&output,
			); err != nil {
				t.Fatalf("inspect rejected resume: %v", err)
			}

			wantState := "State: " + string(before.State())
			wantRevision := fmt.Sprintf("Revision: %d", before.Revision())

			if !strings.Contains(output.String(), wantState) ||
				!strings.Contains(output.String(), wantRevision) {
				t.Fatalf(
					"failed resume mutated durable Change\nwant %q and %q\noutput=%q",
					wantState,
					wantRevision,
					output.String(),
				)
			}
		})
	}
}
