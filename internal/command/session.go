// Package command contains Praetor-owned interactive command metadata,
// parsing, dispatch and retained session context. It has no terminal-library
// dependency.
package command

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Eu-Pedro0ficial/praetor/internal/aiprovider"
	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"github.com/Eu-Pedro0ficial/praetor/internal/execution"
	"github.com/Eu-Pedro0ficial/praetor/internal/intelligence"
	"github.com/Eu-Pedro0ficial/praetor/internal/project"
	"github.com/Eu-Pedro0ficial/praetor/internal/proposal"
	"github.com/Eu-Pedro0ficial/praetor/internal/workflow"
)

// Session retains the active Project context and process-local milestone
// capabilities for one interactive shell session.
type Session struct {
	registration           project.Registration
	changeWorkflow         *workflow.Service
	repositoryIntelligence *intelligence.Service
	proposalLifecycle      *proposal.Service
	providerExecution      *execution.Service
	providerRegistry       *aiprovider.Registry
	providerSelection      aiprovider.Selection
	currentChange          change.Change
	hasCurrentChange       bool
	currentProposal        proposal.Proposal
	hasCurrentProposal     bool
	modeStack              []ModeContext
}

// NewSession constructs retained command context from explicitly composed
// application capabilities.
func NewSession(
	registration project.Registration,
	changeWorkflow *workflow.Service,
	repositoryIntelligence *intelligence.Service,
	proposalLifecycle *proposal.Service,
	providerExecution *execution.Service,
	providerRegistry *aiprovider.Registry,
	providerSelection aiprovider.Selection,
) (*Session, error) {
	if !registration.ProjectId.IsValid() {
		return nil, fmt.Errorf("valid active ProjectId is required")
	}
	if strings.TrimSpace(registration.RepositoryRoot) == "" {
		return nil, fmt.Errorf("active RepositoryRoot is required")
	}
	if changeWorkflow == nil {
		return nil, fmt.Errorf("Change workflow capability is not configured")
	}
	if repositoryIntelligence == nil {
		return nil, fmt.Errorf("repository intelligence capability is not configured")
	}
	if proposalLifecycle == nil {
		return nil, fmt.Errorf("proposal lifecycle capability is not configured")
	}
	if providerExecution == nil {
		return nil, fmt.Errorf("provider execution capability is not configured")
	}
	if providerRegistry == nil {
		return nil, fmt.Errorf("AI provider registry is not configured")
	}
	if _, err := providerRegistry.Resolve(providerSelection); err != nil {
		return nil, fmt.Errorf("initial AI provider selection: %w", err)
	}
	return &Session{
		registration:           registration,
		changeWorkflow:         changeWorkflow,
		repositoryIntelligence: repositoryIntelligence,
		proposalLifecycle:      proposalLifecycle,
		providerExecution:      providerExecution,
		providerRegistry:       providerRegistry,
		providerSelection:      providerSelection,
		modeStack:              []ModeContext{rootModeContext()},
	}, nil
}

// ProviderSelection returns the active session-scoped provider/model choice.
func (session *Session) ProviderSelection() aiprovider.Selection {
	if session == nil {
		return aiprovider.Selection{}
	}
	return session.providerSelection
}

// ProviderDescriptors returns the explicitly registered adapter metadata.
func (session *Session) ProviderDescriptors() []aiprovider.ProviderDescriptor {
	if session == nil || session.providerRegistry == nil {
		return nil
	}
	return session.providerRegistry.Descriptors()
}

// SelectedProviderDescriptor returns metadata for the exact active adapter.
func (session *Session) SelectedProviderDescriptor() (aiprovider.ProviderDescriptor, bool) {
	if session == nil || session.providerRegistry == nil {
		return aiprovider.ProviderDescriptor{}, false
	}
	provider, err := session.providerRegistry.Resolve(session.providerSelection)
	if err != nil {
		return aiprovider.ProviderDescriptor{}, false
	}
	return provider.Descriptor(), true
}

// SelectProvider changes only active session configuration and clears the old
// provider-scoped model. It performs exact lookup and never routes or falls
// back to another adapter.
func (session *Session) SelectProvider(providerValue string) error {
	if session == nil || session.providerRegistry == nil {
		return fmt.Errorf("AI provider registry is not configured")
	}
	selection, err := session.providerRegistry.Select(providerValue, "")
	if err != nil {
		return err
	}
	session.providerSelection = selection
	return nil
}

// SelectProviderModel changes the opaque model for future attempts on the
// active provider. It does not mutate Project, Change, or Proposal state.
func (session *Session) SelectProviderModel(modelValue string) error {
	if session == nil || session.providerRegistry == nil {
		return fmt.Errorf("AI provider registry is not configured")
	}
	selection, err := session.providerRegistry.Select(
		string(session.providerSelection.ProviderIdentifier()),
		modelValue,
	)
	if err != nil {
		return err
	}
	session.providerSelection = selection
	return nil
}

// CurrentProposal returns the current process-local isolated proposal, when
// one exists.
func (session *Session) CurrentProposal() (proposal.Proposal, bool) {
	if session == nil || !session.hasCurrentProposal {
		return proposal.Proposal{}, false
	}
	return session.currentProposal, true
}

// Registration returns the retained logical Project and repository
// association for this shell session.
func (session *Session) Registration() project.Registration {
	if session == nil {
		return project.Registration{}
	}
	return session.registration
}

// CurrentChange returns the latest Change handled in this process-local
// session, when one exists.
func (session *Session) CurrentChange() (change.Change, bool) {
	if session == nil || !session.hasCurrentChange {
		return change.Change{}, false
	}
	return session.currentChange, true
}

func (session *Session) setCurrentChange(currentChange change.Change) {
	session.currentChange = currentChange
	session.hasCurrentChange = true
}

func (session *Session) setCurrentProposal(currentProposal proposal.Proposal) {
	session.currentProposal = currentProposal
	session.hasCurrentProposal = true
}

func (session *Session) clearCurrentProposal() {
	session.currentProposal = proposal.Proposal{}
	session.hasCurrentProposal = false
}

// Close cleans any process-owned proposal workspace before the interactive
// session ends. Canonical developer source is never repaired or overwritten.
func (session *Session) Close() error {
	if session == nil || !session.hasCurrentProposal {
		return nil
	}
	transitionError := session.rejectCurrentChange("interactive session closed with proposal workspace")
	cleaned, discardError := session.proposalLifecycle.Discard(
		session.currentProposal,
		"interactive session closed",
	)
	if cleaned.Workspace().State() == proposal.WorkspaceCleaned {
		session.clearCurrentProposal()
	} else {
		session.setCurrentProposal(cleaned)
	}
	return errors.Join(transitionError, discardError)
}

func (session *Session) rejectAndDiscardProposal(reason string) error {
	if session == nil || !session.hasCurrentProposal {
		return fmt.Errorf("current proposal workspace is required")
	}
	transitionError := session.rejectCurrentChange(reason)
	cleaned, discardError := session.proposalLifecycle.Discard(session.currentProposal, reason)
	if cleaned.Workspace().State() == proposal.WorkspaceCleaned {
		session.clearCurrentProposal()
	} else {
		session.setCurrentProposal(cleaned)
	}
	return errors.Join(transitionError, discardError)
}

func (session *Session) rejectCurrentChange(context string) error {
	if !session.hasCurrentChange || session.currentChange.State() == change.StateRejected {
		return nil
	}
	if session.currentChange.State() != change.StatePlanned &&
		session.currentChange.State() != change.StateIsolated {
		return fmt.Errorf(
			"cannot reject Change %q from state %q while discarding proposal",
			session.currentChange.ChangeId(),
			session.currentChange.State(),
		)
	}
	rejected, err := session.changeWorkflow.Transition(
		session.currentChange.ChangeId(),
		change.StateRejected,
		context,
	)
	if err == nil {
		session.setCurrentChange(rejected)
	}
	return err
}
