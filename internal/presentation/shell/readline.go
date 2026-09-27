package shell

import (
	"errors"
	"fmt"
	"strings"

	"github.com/Eu-Pedro0ficial/praetor/internal/command"
	"github.com/reeflective/readline"
)

type readlineEditor struct {
	shell       *readline.Shell
	registry    command.Registry
	session     *command.Session
	renderHelp  func(string)
	scrollOlder func() string
	scrollNewer func() string
}

func newReadlineEditor(registry command.Registry, session *command.Session) (*readlineEditor, error) {
	if session == nil {
		return nil, fmt.Errorf("active command session is required for readline")
	}
	terminalShell := readline.NewShell()
	terminalShell.Prompt.Primary(session.Prompt)
	if err := terminalShell.Config.Set("autocomplete", true); err != nil {
		return nil, fmt.Errorf("enable readline autocomplete: %w", err)
	}
	terminalShell.Completer = func(line []rune, cursor int) readline.Completions {
		if cursor < 0 {
			cursor = 0
		}
		if cursor > len(line) {
			cursor = len(line)
		}
		suggestions := registry.Complete(session, string(line[:cursor]))
		values := make([]string, 0, len(suggestions)*2)
		for _, suggestion := range suggestions {
			values = append(values, suggestion.Text, suggestion.Description)
		}
		return readline.CompleteValuesDescribed(values...).DisplayList()
	}
	editor := &readlineEditor{
		shell:    terminalShell,
		registry: registry,
		session:  session,
	}
	editor.renderHelp = func(helpText string) {
		_, _ = terminalShell.Printf("%s", strings.TrimSuffix(helpText, "\n"))
	}
	terminalShell.Keymap.Register(map[string]func(){
		"praetor-contextual-help": editor.showContextualHelp,
		"praetor-scroll-older":    editor.scrollViewportOlder,
		"praetor-scroll-newer":    editor.scrollViewportNewer,
	})
	for _, keymap := range []string{"emacs", "vi-insert"} {
		if err := terminalShell.Config.Bind(keymap, "[A", "previous-history", false); err != nil {
			return nil, fmt.Errorf("bind history Up in %s mode: %w", keymap, err)
		}
		if err := terminalShell.Config.Bind(keymap, "[B", "next-history", false); err != nil {
			return nil, fmt.Errorf("bind history Down in %s mode: %w", keymap, err)
		}
		if err := terminalShell.Config.Bind(keymap, "?", "praetor-contextual-help", false); err != nil {
			return nil, fmt.Errorf("bind contextual help in %s mode: %w", keymap, err)
		}
		if err := terminalShell.Config.Bind(keymap, "\x1b[5~", "praetor-scroll-older", false); err != nil {
			return nil, fmt.Errorf("bind viewport PageUp in %s mode: %w", keymap, err)
		}
		if err := terminalShell.Config.Bind(keymap, "\x1b[6~", "praetor-scroll-newer", false); err != nil {
			return nil, fmt.Errorf("bind viewport PageDown in %s mode: %w", keymap, err)
		}
	}
	return editor, nil
}

func (editor *readlineEditor) showContextualHelp() {
	if editor == nil || editor.shell == nil || editor.renderHelp == nil {
		return
	}
	line := append([]rune(nil), []rune(*editor.shell.Line())...)
	cursor := editor.shell.Cursor().Pos()
	if cursor < 0 {
		cursor = 0
	}
	if cursor > len(line) {
		cursor = len(line)
	}
	suggestions := editor.registry.ContextualHelp(editor.session, string(line[:cursor]))
	editor.renderHelp(formatContextualSuggestions(suggestions))
}

func (editor *readlineEditor) scrollViewportOlder() {
	if editor == nil || editor.shell == nil || editor.scrollOlder == nil {
		return
	}
	_, _ = editor.shell.Printf("%s", editor.scrollOlder())
}

func (editor *readlineEditor) scrollViewportNewer() {
	if editor == nil || editor.shell == nil || editor.scrollNewer == nil {
		return
	}
	_, _ = editor.shell.Printf("%s", editor.scrollNewer())
}

func formatContextualSuggestions(suggestions []command.Suggestion) string {
	if len(suggestions) == 0 {
		return "No contextual commands or options match the current input."
	}
	width := 0
	for _, suggestion := range suggestions {
		if displayWidth(suggestion.Text) > width {
			width = displayWidth(suggestion.Text)
		}
	}
	var output strings.Builder
	for _, suggestion := range suggestions {
		padding := width - displayWidth(suggestion.Text)
		fmt.Fprintf(&output, "%s%s  %s\n", suggestion.Text, strings.Repeat(" ", padding), suggestion.Description)
	}
	return strings.TrimSuffix(output.String(), "\n")
}

func (editor *readlineEditor) Readline() (string, error) {
	line, err := editor.shell.Readline()
	if errors.Is(err, readline.ErrInterrupt) {
		return line, errInterrupted
	}
	return line, err
}

func (editor *readlineEditor) setHelpRenderer(render func(string) string) {
	if editor == nil || editor.shell == nil || render == nil {
		return
	}

	editor.renderHelp = func(helpText string) {
		_, _ = editor.shell.Printf("%s", render(helpText))
	}
}

func (editor *readlineEditor) setViewportHandlers(
	scrollOlder func() string,
	scrollNewer func() string,
) {
	if editor == nil {
		return
	}

	editor.scrollOlder = scrollOlder
	editor.scrollNewer = scrollNewer
}

func (editor *readlineEditor) setPrompt(prompt func() string) {
	if editor == nil || editor.shell == nil || prompt == nil {
		return
	}
	editor.shell.Prompt.Primary(prompt)
}

func (editor *readlineEditor) setRightPrompt(prompt func() string) {
	if editor == nil || editor.shell == nil || prompt == nil {
		return
	}
	editor.shell.Prompt.Right(prompt)
}

func (editor *readlineEditor) setFooter(footer func() string) {
	if editor == nil || editor.shell == nil || footer == nil {
		return
	}
	editor.shell.Hint.SetProvider(func(_ []rune, _ int) []rune {
		return []rune(strings.TrimSuffix(footer(), "\n"))
	})
}
