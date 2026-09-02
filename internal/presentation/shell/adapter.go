// Package shell adapts the Praetor-owned command registry to an interactive
// terminal. Terminal mechanics stay isolated from command and domain code.
package shell

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"

	"github.com/Eu-Pedro0ficial/praetor/internal/command"
)

var errInterrupted = errors.New("interactive input interrupted")

type lineEditor interface {
	Readline() (string, error)
}

// Adapter runs one retained-context interactive Praetor shell.
type Adapter struct {
	registry command.Registry
	session  *command.Session
	output   io.Writer
	editor   lineEditor
}

// New constructs the approved readline-backed presentation adapter.
func New(registry command.Registry, session *command.Session, output io.Writer) (*Adapter, error) {
	editor, err := newReadlineEditor(registry, session)
	if err != nil {
		return nil, err
	}
	return newWithEditor(registry, session, output, editor)
}

func newWithEditor(
	registry command.Registry,
	session *command.Session,
	output io.Writer,
	editor lineEditor,
) (*Adapter, error) {
	if len(registry.Commands()) == 0 {
		return nil, fmt.Errorf("command registry is empty")
	}
	if session == nil {
		return nil, fmt.Errorf("active command session is required")
	}
	if output == nil {
		return nil, fmt.Errorf("shell output is required")
	}
	if editor == nil {
		return nil, fmt.Errorf("line editor is required")
	}
	return &Adapter{
		registry: registry,
		session:  session,
		output:   output,
		editor:   editor,
	}, nil
}

// Run prints retained Project context and dispatches until root exit or EOF.
// Command failures are reported without terminating the session.
func (adapter *Adapter) Run() error {
	return adapter.RunContext(context.Background())
}

// RunContext propagates execution-scoped cancellation to commands such as
// M0.5 provider implementation. Readline remains responsible for terminal
// interrupt behavior while waiting for input.
func (adapter *Adapter) RunContext(ctx context.Context) error {
	if ctx == nil {
		return fmt.Errorf("shell execution context is required")
	}
	if err := adapter.writeBanner(); err != nil {
		return err
	}
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		line, err := adapter.editor.Readline()
		switch {
		case errors.Is(err, io.EOF):
			return nil
		case errors.Is(err, errInterrupted):
			continue
		case err != nil:
			return fmt.Errorf("read interactive command: %w", err)
		}
		if strings.TrimSpace(line) == "" {
			continue
		}
		result, dispatchError := adapter.registry.DispatchContext(ctx, adapter.session, line, adapter.output)
		if dispatchError != nil {
			if _, writeError := fmt.Fprintf(adapter.output, "praetor: %v\n", dispatchError); writeError != nil {
				return writeError
			}
			continue
		}
		if result.Exit {
			return nil
		}
	}
}

func (adapter *Adapter) writeBanner() error {
	registration := adapter.session.Registration()
	currentChange := "none"
	if activeChange, ok := adapter.session.CurrentChange(); ok {
		currentChange = fmt.Sprintf("%s (%s)", activeChange.ChangeId(), activeChange.State())
	}
	_, err := fmt.Fprintf(
		adapter.output,
		"Praetor\nProject: %s\nRepository: %s\nChange: %s\n\n",
		registration.ProjectId,
		registration.RepositoryRoot,
		currentChange,
	)
	return err
}
