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

// InteractionRecorder observes the shell's logical command/output boundary.
// Implementations remain presentation concerns and never receive raw terminal
// rendering or provider streams directly.
type InteractionRecorder interface {
	BeginCommand(commandLine string) (io.Writer, error)
	EndCommand() error
	Close(runError error) error
}

// Adapter runs one retained-context interactive Praetor shell.
type Adapter struct {
	registry          command.Registry
	session           *command.Session
	output            io.Writer
	editor            lineEditor
	renderer          *consoleRenderer
	interactiveScreen bool
	recorder          InteractionRecorder
}

// Option configures optional presentation behavior.
type Option func(*Adapter) error

// WithInteractionRecorder records logical shell interaction. The caller owns
// recorder construction; Adapter closes it when the interactive run ends.
func WithInteractionRecorder(recorder InteractionRecorder) Option {
	return func(adapter *Adapter) error {
		if recorder == nil {
			return fmt.Errorf("interaction recorder is required")
		}
		adapter.recorder = recorder
		return nil
	}
}

// New constructs the approved readline-backed presentation adapter.
func New(
	registry command.Registry,
	session *command.Session,
	output io.Writer,
	options ...Option,
) (*Adapter, error) {
	editor, err := newReadlineEditor(registry, session)
	if err != nil {
		return nil, err
	}
	adapter, err := newWithEditor(registry, session, output, editor)
	if err != nil {
		return nil, err
	}
	dimensions, color := terminalCapabilities(output)
	adapter.renderer = newConsoleRenderer(session, dimensions, color)
	adapter.interactiveScreen = terminalIsInteractive(output)
	adapter.renderer.setFullScreen(adapter.interactiveScreen)
	for _, option := range options {
		if option == nil {
			return nil, fmt.Errorf("shell option is required")
		}
		if err := option(adapter); err != nil {
			return nil, err
		}
	}

	editor.setPrompt(adapter.renderer.Prompt)
	editor.setRightPrompt(adapter.renderer.RightPrompt)
	editor.setFooter(adapter.renderer.ClosePrompt)

	if adapter.interactiveScreen {
		editor.setHelpRenderer(adapter.renderer.RenderContextualHelp)
		editor.setViewportHandlers(
			func() string {
				adapter.renderer.ScrollOlder()
				return adapter.renderer.Redraw()
			},
			func() string {
				adapter.renderer.ScrollNewer()
				return adapter.renderer.Redraw()
			},
		)
	}

	return adapter, nil
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
		renderer: newConsoleRenderer(session, func() terminalDimensions {
			return terminalDimensions{Width: 100, Height: 30}
		}, false),
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
func (adapter *Adapter) RunContext(ctx context.Context) (runError error) {
	if ctx == nil {
		return fmt.Errorf("shell execution context is required")
	}
	if adapter.recorder != nil {
		defer func() {
			runError = errors.Join(runError, adapter.recorder.Close(runError))
		}()
	}

	if adapter.interactiveScreen {
		return adapter.runFullScreenContext(ctx)
	}

	return adapter.runScrollbackContext(ctx)
}

func (adapter *Adapter) commandOutput(commandLine string, visible io.Writer) (io.Writer, error) {
	if adapter.recorder == nil {
		return visible, nil
	}
	recorded, err := adapter.recorder.BeginCommand(commandLine)
	if err != nil {
		return nil, fmt.Errorf("record interactive command: %w", err)
	}
	return io.MultiWriter(visible, recorded), nil
}

func (adapter *Adapter) finishCommand() error {
	if adapter.recorder == nil {
		return nil
	}
	if err := adapter.recorder.EndCommand(); err != nil {
		return fmt.Errorf("record interactive output: %w", err)
	}
	return nil
}

func (adapter *Adapter) runScrollbackContext(ctx context.Context) error {
	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		if _, err := fmt.Fprint(adapter.output, adapter.renderer.Render()); err != nil {
			return err
		}

		line, err := adapter.editor.Readline()

		if _, closeError := fmt.Fprint(adapter.output, adapter.renderer.ClosePrompt()); closeError != nil {
			return closeError
		}

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

		commandOutput, recorderError := adapter.commandOutput(line, adapter.output)
		if recorderError != nil {
			return recorderError
		}
		result, dispatchError := adapter.registry.DispatchContext(
			ctx,
			adapter.session,
			line,
			commandOutput,
		)
		if dispatchError != nil {
			if _, writeError := fmt.Fprintf(commandOutput, "praetor: %v\n", dispatchError); writeError != nil {
				return writeError
			}
		}
		if err := adapter.finishCommand(); err != nil {
			return err
		}
		if dispatchError != nil {
			continue
		}

		if result.Exit {
			return nil
		}
	}
}

func (adapter *Adapter) runFullScreenContext(ctx context.Context) (runError error) {
	if _, err := fmt.Fprint(adapter.output, adapter.renderer.EnterScreen()); err != nil {
		return err
	}

	defer func() {
		_, leaveError := fmt.Fprint(adapter.output, adapter.renderer.LeaveScreen())
		runError = errors.Join(runError, leaveError)
	}()

	for {
		if err := ctx.Err(); err != nil {
			return err
		}

		if _, err := fmt.Fprint(adapter.output, adapter.renderer.Redraw()); err != nil {
			return err
		}

		if _, err := fmt.Fprint(adapter.output, adapter.renderer.PrepareInputRow()); err != nil {
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

		adapter.renderer.AppendCommand(line)
		historyOutput := newConsoleHistoryWriter(adapter.renderer)
		commandOutput, recorderError := adapter.commandOutput(line, historyOutput)
		if recorderError != nil {
			return recorderError
		}

		result, dispatchError := adapter.registry.DispatchContext(
			ctx,
			adapter.session,
			line,
			commandOutput,
		)

		if dispatchError != nil {
			_, _ = fmt.Fprintf(commandOutput, "praetor: %v\n", dispatchError)
		}

		historyOutput.Flush()
		if err := adapter.finishCommand(); err != nil {
			return err
		}

		if dispatchError != nil {
			continue
		}

		if result.Exit {
			return nil
		}
	}
}
