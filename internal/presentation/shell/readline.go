package shell

import (
	"errors"

	"github.com/Eu-Pedro0ficial/praetor/internal/command"
	"github.com/reeflective/readline"
)

type readlineEditor struct {
	shell *readline.Shell
}

func newReadlineEditor(registry command.Registry) *readlineEditor {
	terminalShell := readline.NewShell()
	terminalShell.Prompt.Primary(func() string { return "praetor> " })
	_ = terminalShell.Config.Set("autocomplete", true)
	terminalShell.Completer = func(line []rune, cursor int) readline.Completions {
		if cursor < 0 {
			cursor = 0
		}
		if cursor > len(line) {
			cursor = len(line)
		}
		suggestions := registry.Complete(string(line[:cursor]))
		values := make([]string, 0, len(suggestions)*2)
		for _, suggestion := range suggestions {
			values = append(values, suggestion.Text, suggestion.Description)
		}
		return readline.CompleteValuesDescribed(values...).DisplayList()
	}
	return &readlineEditor{shell: terminalShell}
}

func (editor *readlineEditor) Readline() (string, error) {
	line, err := editor.shell.Readline()
	if errors.Is(err, readline.ErrInterrupt) {
		return line, errInterrupted
	}
	return line, err
}
