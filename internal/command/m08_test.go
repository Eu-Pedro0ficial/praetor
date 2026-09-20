package command_test

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/Eu-Pedro0ficial/praetor/internal/audit"
	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"github.com/Eu-Pedro0ficial/praetor/internal/composition"
	"github.com/Eu-Pedro0ficial/praetor/internal/repository"
)

func TestM08ApprovedChangeAppliesExactPatchAndClosesAuditLocked(t *testing.T) {
	repositoryRoot, dataDirectory, session, registry, providerCalls := prepareValidatedDecisionCommandTest(t, nil)
	before, err := repository.Inspect(session.Registration().ProjectId, repositoryRoot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Dispatch(session, "change approve", io.Discard); err != nil {
		t.Fatal(err)
	}
	afterApproval, err := repository.Inspect(session.Registration().ProjectId, repositoryRoot)
	if err != nil {
		t.Fatal(err)
	}
	if afterApproval.SourceStateDigest() != before.SourceStateDigest() ||
		afterApproval.WorkingTreeState() != before.WorkingTreeState() {
		t.Fatal("approval mutated canonical source before explicit apply")
	}
	callsBeforeApply := *providerCalls
	var output bytes.Buffer
	if _, err := registry.Dispatch(session, "change apply", &output); err != nil {
		t.Fatalf("change apply: %v\n%s", err, output.String())
	}
	for _, expected := range []string{
		"Canonical application: explicit working-tree-only operation",
		"Canonical application: completed and deterministically proven",
		"Canonical HEAD: " + before.HeadRevision() + " (unchanged)",
		"Git index: unchanged=true",
		"Git commit: not created",
		"Git push: not performed",
		"Change state: audit-locked",
	} {
		if !strings.Contains(output.String(), expected) {
			t.Fatalf("apply output %q lacks %q", output.String(), expected)
		}
	}
	if *providerCalls != callsBeforeApply {
		t.Fatal("canonical application invoked the AI provider")
	}
	currentChange, _ := session.CurrentChange()
	if currentChange.State() != change.StateAuditLocked {
		t.Fatalf("applied Change state = %q", currentChange.State())
	}
	if _, retained := session.CurrentProposal(); retained {
		t.Fatal("terminal approved path retained ProposalWorkspace")
	}
	contents, err := os.ReadFile(filepath.Join(repositoryRoot, "internal/service/service.go"))
	if err != nil || string(contents) != "package service\n\nfunc Greeting() string { return \"approved-candidate\" }\n" {
		t.Fatalf("canonical contents = %q/%v", contents, err)
	}
	after, err := repository.Inspect(session.Registration().ProjectId, repositoryRoot)
	if err != nil {
		t.Fatal(err)
	}
	if after.HeadRevision() != before.HeadRevision() ||
		after.WorkingTreeState() != "dirty" ||
		after.SourceStateDigest() == before.SourceStateDigest() {
		t.Fatalf("post-apply snapshot = %#v", after)
	}
	if cached := runCommandGitOutput(t, repositoryRoot, "diff", "--cached", "--name-only"); len(cached) != 0 {
		t.Fatalf("canonical index changed: %q", cached)
	}
	if count := strings.TrimSpace(string(runCommandGitOutput(t, repositoryRoot, "rev-list", "--count", "HEAD"))); count != "1" {
		t.Fatalf("canonical commit count = %q", count)
	}
	assertM08ApprovedAudit(t, readCommandAudit(t, dataDirectory))
	assertNoCanonicalMetadata(t, repositoryRoot)
}

func TestM08RejectedChangeClosesWithoutCanonicalApplication(t *testing.T) {
	repositoryRoot, dataDirectory, session, registry, providerCalls := prepareValidatedDecisionCommandTest(t, nil)
	before, err := repository.Inspect(session.Registration().ProjectId, repositoryRoot)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Dispatch(session, `change reject "not wanted"`, io.Discard); err != nil {
		t.Fatal(err)
	}
	callsBeforeClose := *providerCalls
	var output bytes.Buffer
	if _, err := registry.Dispatch(session, "change close", &output); err != nil {
		t.Fatalf("change close: %v", err)
	}
	if !strings.Contains(output.String(), "canonical source proven unchanged") ||
		!strings.Contains(output.String(), "Canonical application: not performed") ||
		!strings.Contains(output.String(), "Change state: audit-locked") {
		t.Fatalf("close output = %q", output.String())
	}
	if *providerCalls != callsBeforeClose {
		t.Fatal("rejection closure invoked provider")
	}
	currentChange, _ := session.CurrentChange()
	if currentChange.State() != change.StateAuditLocked {
		t.Fatalf("rejected closure state = %q", currentChange.State())
	}
	if _, retained := session.CurrentProposal(); retained {
		t.Fatal("terminal rejected path retained ProposalWorkspace")
	}
	after, err := repository.Inspect(session.Registration().ProjectId, repositoryRoot)
	if err != nil {
		t.Fatal(err)
	}
	if before.SourceStateDigest() != after.SourceStateDigest() || after.WorkingTreeState() != "clean" {
		t.Fatalf("rejection closure changed canonical source: %#v -> %#v", before, after)
	}
	if _, err := registry.Dispatch(session, "change apply", io.Discard); err == nil {
		t.Fatal("audit-locked rejected Change could apply")
	}
	events := readCommandAudit(t, dataDirectory)
	closure := eventIndex(events, audit.EventChangeClosureRecorded)
	terminal := transitionIndex(events, change.StateRejected, change.StateAuditLocked)
	if closure < 0 || terminal != closure+1 {
		t.Fatalf("rejection closure ordering = closure %d terminal %d", closure, terminal)
	}
	for _, event := range events {
		if event.EventType == audit.EventCanonicalApplicationStarted ||
			event.EventType == audit.EventCanonicalApplicationCompleted {
			t.Fatalf("rejection invoked canonical application: %#v", event)
		}
	}
	assertNoCanonicalMetadata(t, repositoryRoot)
}

func TestM08ApplyRequiresSeparateExplicitApprovedOperation(t *testing.T) {
	repositoryRoot, _, session, registry, _ := prepareValidatedDecisionCommandTest(t, nil)
	before, err := repository.Inspect(session.Registration().ProjectId, repositoryRoot)
	if err != nil {
		t.Fatal(err)
	}
	for _, line := range []string{"change apply", "change apply ?", "help change apply", "", "?"} {
		_, dispatchError := registry.Dispatch(session, line, io.Discard)
		if line == "change apply" && dispatchError == nil {
			t.Fatal("validated Change applied without approval")
		}
	}
	_ = registry.Complete(session, "change app")
	after, err := repository.Inspect(session.Registration().ProjectId, repositoryRoot)
	if err != nil {
		t.Fatal(err)
	}
	if before.SourceStateDigest() != after.SourceStateDigest() {
		t.Fatal("help/completion/invalid apply mutated canonical source")
	}
	if _, err := registry.Dispatch(session, "change reject", io.Discard); err != nil {
		t.Fatal(err)
	}
	if _, err := registry.Dispatch(session, "change apply", io.Discard); err == nil ||
		!strings.Contains(err.Error(), "must be approved") {
		t.Fatalf("rejected apply error = %v", err)
	}
}

func TestM08ApplicationAuditFailuresAreTruthfulAndReplaySafe(t *testing.T) {
	auditFailure := errors.New("M0.8 audit unavailable")
	tests := []struct {
		name          string
		failedEvent   string
		wantMutation  bool
		wantCompleted bool
	}{
		{name: "start append", failedEvent: audit.EventCanonicalApplicationStarted},
		{name: "completion append", failedEvent: audit.EventCanonicalApplicationCompleted, wantMutation: true},
		{name: "terminal transition", failedEvent: audit.EventChangeTransition, wantMutation: true, wantCompleted: true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			failureEnabled := false
			configure := func(container *composition.Container) {
				base := container.ChangeAuditLogger
				container.ChangeAuditLogger = func(dataDirectory, eventType, projectID, changeID, repositoryRoot string, metadata map[string]any) (audit.Event, error) {
					fail := failureEnabled && eventType == test.failedEvent
					if fail && eventType == audit.EventChangeTransition {
						fail = metadata["previous_state"] == string(change.StateApproved) &&
							metadata["resulting_state"] == string(change.StateAuditLocked)
					}
					if fail {
						return audit.Event{}, auditFailure
					}
					return base(dataDirectory, eventType, projectID, changeID, repositoryRoot, metadata)
				}
			}
			repositoryRoot, dataDirectory, session, registry, _ := prepareValidatedDecisionCommandTest(t, configure)
			if _, err := registry.Dispatch(session, "change approve", io.Discard); err != nil {
				t.Fatal(err)
			}
			failureEnabled = true
			var output bytes.Buffer
			if _, err := registry.Dispatch(session, "change apply", &output); !errors.Is(err, auditFailure) {
				t.Fatalf("apply error = %v", err)
			}
			currentChange, _ := session.CurrentChange()
			if currentChange.State() != change.StateApproved {
				t.Fatalf("failed application state = %q", currentChange.State())
			}
			status := runCommandGitOutput(t, repositoryRoot, "status", "--porcelain=v1", "--untracked-files=all")
			if (len(status) != 0) != test.wantMutation {
				t.Fatalf("canonical mutation status = %q, want mutation=%t", status, test.wantMutation)
			}
			if test.wantMutation {
				if !strings.Contains(output.String(), "mutation occurred; lifecycle closure is incomplete") {
					t.Fatalf("late failure output = %q", output.String())
				}
				contentsBeforeReplay, err := os.ReadFile(filepath.Join(repositoryRoot, "internal/service/service.go"))
				if err != nil {
					t.Fatal(err)
				}
				if _, err := registry.Dispatch(session, "change apply", io.Discard); err == nil {
					t.Fatal("late failure allowed replay application")
				}
				contentsAfterReplay, _ := os.ReadFile(filepath.Join(repositoryRoot, "internal/service/service.go"))
				if !bytes.Equal(contentsBeforeReplay, contentsAfterReplay) {
					t.Fatal("replay changed already-applied canonical source")
				}
			}
			events := readCommandAudit(t, dataDirectory)
			if (eventIndex(events, audit.EventCanonicalApplicationCompleted) >= 0) != test.wantCompleted {
				t.Fatalf("completion event presence mismatch: %#v", events)
			}
			if test.failedEvent == audit.EventCanonicalApplicationCompleted &&
				eventIndex(events, audit.EventCanonicalApplicationFailed) < 0 {
				t.Fatal("completion audit failure lacked truthful failed-event attempt")
			}
		})
	}
}

func TestM08RejectedClosureFailsClosedOnCanonicalDrift(t *testing.T) {
	repositoryRoot, dataDirectory, session, registry, _ := prepareValidatedDecisionCommandTest(t, nil)
	if _, err := registry.Dispatch(session, "change reject", io.Discard); err != nil {
		t.Fatal(err)
	}
	beforeEvents := readCommandAudit(t, dataDirectory)
	writeCommandFile(t, repositoryRoot, "README.md", "# drift\n")
	if _, err := registry.Dispatch(session, "change close", io.Discard); err == nil ||
		!strings.Contains(err.Error(), "canonical source drift") {
		t.Fatalf("drifted rejection closure error = %v", err)
	}
	currentChange, _ := session.CurrentChange()
	if currentChange.State() != change.StateRejected {
		t.Fatalf("drifted rejection closure state = %q", currentChange.State())
	}
	afterEvents := readCommandAudit(t, dataDirectory)
	if !reflect.DeepEqual(beforeEvents, afterEvents) {
		t.Fatal("preflight rejection closure failure appended audit")
	}
	writeCommandFile(t, repositoryRoot, "README.md", "# Fixture\n")
}

func assertM08ApprovedAudit(t *testing.T, events []audit.Event) {
	t.Helper()
	start := eventIndex(events, audit.EventCanonicalApplicationStarted)
	completed := eventIndex(events, audit.EventCanonicalApplicationCompleted)
	terminal := transitionIndex(events, change.StateApproved, change.StateAuditLocked)
	artifact := eventIndexAfter(events, audit.EventArtifactCommitted, terminal)
	cleanup := eventIndex(events, audit.EventProposalWorkspaceDiscarded)
	if start < 0 || completed != start+1 || terminal != completed+1 || artifact != terminal+1 || cleanup != artifact+1 {
		t.Fatalf("approved application audit ordering start=%d completed=%d terminal=%d cleanup=%d", start, completed, terminal, cleanup)
	}
	metadata := events[completed].Metadata
	for _, key := range []string{
		"workspace_id", "base_revision", "source_state_digest", "patch_digest",
		"changed_paths", "verification_attempt_id", "evidence_set_id",
		"human_decision", "resulting_source_state_digest", "canonical_head",
		"index_unchanged", "canonical_result_digest",
	} {
		if metadata[key] == nil || metadata[key] == "" {
			t.Fatalf("completion audit lacks %s: %#v", key, metadata)
		}
	}
	text := fmt.Sprintf("%v", metadata)
	for _, forbidden := range []string{"package service", "approved-candidate", "praetor-proposal-", "implementation-thread"} {
		if strings.Contains(text, forbidden) {
			t.Fatalf("completion audit leaked %q: %#v", forbidden, metadata)
		}
	}
}

func eventIndexAfter(events []audit.Event, eventType string, after int) int {
	for index := after + 1; index < len(events); index++ {
		if events[index].EventType == eventType {
			return index
		}
	}
	return -1
}

func eventIndex(events []audit.Event, eventType string) int {
	for index, event := range events {
		if event.EventType == eventType {
			return index
		}
	}
	return -1
}

func transitionIndex(events []audit.Event, from, to change.ChangeState) int {
	for index, event := range events {
		if event.EventType == audit.EventChangeTransition &&
			event.Metadata["previous_state"] == string(from) &&
			event.Metadata["resulting_state"] == string(to) {
			return index
		}
	}
	return -1
}

func assertNoCanonicalMetadata(t *testing.T, repositoryRoot string) {
	t.Helper()
	if _, err := os.Stat(filepath.Join(repositoryRoot, ".praetor")); !os.IsNotExist(err) {
		t.Fatalf("runtime metadata appeared in governed repository: %v", err)
	}
	if matches, err := filepath.Glob(filepath.Join(repositoryRoot, "*.patch")); err != nil || len(matches) != 0 {
		t.Fatalf("temporary patch persisted in governed repository: %v/%v", matches, err)
	}
}
