package command_test

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Eu-Pedro0ficial/praetor/internal/adapters/gitproposal"
	"github.com/Eu-Pedro0ficial/praetor/internal/aiprovider"
	"github.com/Eu-Pedro0ficial/praetor/internal/command"
	"github.com/Eu-Pedro0ficial/praetor/internal/composition"
	"github.com/Eu-Pedro0ficial/praetor/internal/proposal"
)

type cleanupFailingWorkspaceAdapter struct {
	delegate   *gitproposal.Adapter
	failRemove bool
}

func (adapter *cleanupFailingWorkspaceAdapter) Create(request proposal.WorkspaceRequest) (proposal.ProposalWorkspace, error) {
	return adapter.delegate.Create(request)
}

func (adapter *cleanupFailingWorkspaceAdapter) Extract(workspace proposal.ProposalWorkspace) (proposal.ExtractedPatch, error) {
	return adapter.delegate.Extract(workspace)
}

func (adapter *cleanupFailingWorkspaceAdapter) Remove(workspace proposal.ProposalWorkspace) error {
	if adapter.failRemove {
		return errors.New("injected workspace cleanup failure")
	}
	return adapter.delegate.Remove(workspace)
}

func (adapter *cleanupFailingWorkspaceAdapter) Reattach(workspace proposal.ProposalWorkspace) error {
	return adapter.delegate.Reattach(workspace)
}

func (adapter *cleanupFailingWorkspaceAdapter) ReservedWorkspaceRoot(workspaceId proposal.WorkspaceId) (string, error) {
	return adapter.delegate.ReservedWorkspaceRoot(workspaceId)
}

func (adapter *cleanupFailingWorkspaceAdapter) VerifyReservedWorkspace(workspaceId proposal.WorkspaceId, canonicalRoot, baseRevision string) error {
	return adapter.delegate.VerifyReservedWorkspace(workspaceId, canonicalRoot, baseRevision)
}

func (adapter *cleanupFailingWorkspaceAdapter) ClassifyReservedWorkspace(workspaceId proposal.WorkspaceId, canonicalRoot string) (proposal.WorkspaceReservationCondition, error) {
	return adapter.delegate.ClassifyReservedWorkspace(workspaceId, canonicalRoot)
}

func (adapter *cleanupFailingWorkspaceAdapter) RemoveReservedWorkspace(workspaceId proposal.WorkspaceId, canonicalRoot string) error {
	if adapter.failRemove {
		return errors.New("injected workspace cleanup failure")
	}
	return adapter.delegate.RemoveReservedWorkspace(workspaceId, canonicalRoot)
}

func TestWave3CleanupFailureBlocksImplementationRetryAndStatusIsTruthful(t *testing.T) {
	providerCalls := 0
	provider := newCommandFakeProvider(t, func(_ context.Context, request aiprovider.ExecutionRequest) (aiprovider.ProviderResponse, error) {
		providerCalls++
		if err := os.WriteFile(
			filepath.Join(request.Workspace().Root(), "internal/service/service.go"),
			[]byte("package service\n\nconst Partial = true\n"),
			0o600,
		); err != nil {
			return aiprovider.ProviderResponse{}, err
		}
		return aiprovider.ProviderResponse{}, aiprovider.NewExecutionError(
			aiprovider.FailureProcess,
			"codex-cli",
			"thread-cleanup",
			errors.New("injected provider failure"),
		)
	})
	delegate, err := gitproposal.New("")
	if err != nil {
		t.Fatalf("gitproposal.New: %v", err)
	}
	adapter := &cleanupFailingWorkspaceAdapter{delegate: delegate}
	repositoryRoot, _, session, registry := prepareProviderCommandTestWithContainer(t, provider, nil, func(container *composition.Container) {
		container.ProposalWorkspaces = adapter
		container.PatchExtraction = adapter
	})
	if _, err := registry.Dispatch(session, "change isolate change-cleanup \"Exercise cleanup failure.\" --expected internal/service/service.go", io.Discard); err != nil {
		t.Fatalf("isolate: %v", err)
	}
	if _, err := registry.Dispatch(session, "change implement", io.Discard); err == nil {
		t.Fatal("first implementation unexpectedly succeeded")
	}
	adapter.failRemove = true
	if _, err := registry.Dispatch(session, "change implement", io.Discard); err == nil ||
		!strings.Contains(err.Error(), "cleanup is not proven") {
		t.Fatalf("retry cleanup error = %v", err)
	}
	if providerCalls != 1 {
		t.Fatalf("provider ran before failed workspace cleanup: calls=%d", providerCalls)
	}
	current, ok := session.CurrentProposal()
	if !ok || current.Workspace().State() != proposal.WorkspaceCleanupFailed {
		t.Fatalf("cleanup failure proposal = %#v/%t", current, ok)
	}
	var status bytes.Buffer
	if _, err := registry.Dispatch(session, "status", &status); err != nil {
		t.Fatalf("status: %v", err)
	}
	for _, expected := range []string{
		"Recovery: action required",
		"Outcome: cleanup failed",
		"Proposal workspace: retained; cleanup not proven",
	} {
		if !strings.Contains(status.String(), expected) {
			t.Fatalf("status %q lacks %q", status.String(), expected)
		}
	}
	assertCommandCanonicalSourceUnchanged(t, repositoryRoot)
	adapter.failRemove = false
}

func TestWave3RestartReconstructsCleanedRetryableChange(t *testing.T) {
	provider := newCommandFakeProvider(t, func(_ context.Context, request aiprovider.ExecutionRequest) (aiprovider.ProviderResponse, error) {
		return aiprovider.ProviderResponse{}, aiprovider.NewExecutionError(
			aiprovider.FailureProcess,
			"codex-cli",
			"thread-restart",
			errors.New("injected provider failure"),
		)
	})
	repositoryRoot, _, first, registry := prepareM05CommandTest(t, provider)
	const changeID = "change-restart-retry"
	if _, err := registry.Dispatch(first, "change isolate "+changeID+" \"Recover after restart.\" --expected internal/service/service.go", io.Discard); err != nil {
		t.Fatalf("isolate: %v", err)
	}
	if _, err := registry.Dispatch(first, "change implement", io.Discard); err == nil {
		t.Fatal("implementation unexpectedly succeeded")
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
		t.Fatalf("registry: %v", err)
	}
	if _, err := secondRegistry.Dispatch(second, "change select "+changeID, io.Discard); err != nil {
		t.Fatalf("select recoverable Change: %v", err)
	}
	if _, ok := second.CurrentProposal(); ok {
		t.Fatal("restart reconstructed a cleaned workspace as live")
	}
	var status bytes.Buffer
	if _, err := secondRegistry.Dispatch(second, "status", &status); err != nil {
		t.Fatalf("status after restart: %v", err)
	}
	for _, expected := range []string{
		"Current change: " + changeID + " (isolated)",
		"Outcome: proposal workspace cleaned",
		"Proposal workspace: cleaned",
		"Recovery: retryable",
		"Safe next actions: change implement; change diagnose",
	} {
		if !strings.Contains(status.String(), expected) {
			t.Fatalf("restart status %q lacks %q", status.String(), expected)
		}
	}
	var diagnosis bytes.Buffer
	if _, err := secondRegistry.Dispatch(second, "change diagnose "+changeID, &diagnosis); err != nil {
		t.Fatalf("diagnose after restart: %v", err)
	}
	for _, expected := range []string{
		"lifecycle-recovery: retryable",
		"outcome: proposal workspace cleaned",
		"proposal-workspace: cleaned",
		"safe-next-actions: change implement; change diagnose",
	} {
		if !strings.Contains(diagnosis.String(), expected) {
			t.Fatalf("restart diagnosis %q lacks %q", diagnosis.String(), expected)
		}
	}
	assertCommandCanonicalSourceUnchanged(t, repositoryRoot)
}

func TestWave3RetryPreservesStaleCanonicalSourceGuard(t *testing.T) {
	providerCalls := 0
	provider := newCommandFakeProvider(t, func(_ context.Context, request aiprovider.ExecutionRequest) (aiprovider.ProviderResponse, error) {
		providerCalls++
		return aiprovider.ProviderResponse{}, aiprovider.NewExecutionError(
			aiprovider.FailureProcess,
			"codex-cli",
			"thread-stale",
			errors.New("injected provider failure"),
		)
	})
	repositoryRoot, _, session, registry := prepareM05CommandTest(t, provider)
	if _, err := registry.Dispatch(session, "change isolate change-stale-retry \"Exercise stale retry.\" --expected internal/service/service.go", io.Discard); err != nil {
		t.Fatalf("isolate: %v", err)
	}
	if _, err := registry.Dispatch(session, "change implement", io.Discard); err == nil {
		t.Fatal("first implementation unexpectedly succeeded")
	}
	writeCommandFile(t, repositoryRoot, "README.md", "# External developer drift\n")
	if _, err := registry.Dispatch(session, "change implement", io.Discard); err == nil ||
		!strings.Contains(err.Error(), "canonical source drift") {
		t.Fatalf("stale retry error = %v", err)
	}
	if providerCalls != 1 {
		t.Fatalf("stale retry invoked provider: calls=%d", providerCalls)
	}
	current, ok := session.CurrentChange()
	if !ok || current.State() != "isolated" {
		t.Fatalf("stale retry Change = %#v/%t", current, ok)
	}
	if contents, err := os.ReadFile(filepath.Join(repositoryRoot, "README.md")); err != nil ||
		string(contents) != "# External developer drift\n" {
		t.Fatalf("canonical drift changed: %q/%v", contents, err)
	}
}
