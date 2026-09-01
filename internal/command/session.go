// Package command contains Praetor-owned interactive command metadata,
// parsing, dispatch and retained session context. It has no terminal-library
// dependency.
package command

import (
	"fmt"
	"strings"

	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"github.com/Eu-Pedro0ficial/praetor/internal/intelligence"
	"github.com/Eu-Pedro0ficial/praetor/internal/project"
	"github.com/Eu-Pedro0ficial/praetor/internal/workflow"
)

// Session retains the active Project context and process-local milestone
// capabilities for one interactive shell session.
type Session struct {
	registration           project.Registration
	changeWorkflow         *workflow.Service
	repositoryIntelligence *intelligence.Service
	currentChange          change.Change
	hasCurrentChange       bool
}

// NewSession constructs retained command context from explicitly composed
// application capabilities.
func NewSession(
	registration project.Registration,
	changeWorkflow *workflow.Service,
	repositoryIntelligence *intelligence.Service,
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
	return &Session{
		registration:           registration,
		changeWorkflow:         changeWorkflow,
		repositoryIntelligence: repositoryIntelligence,
	}, nil
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
