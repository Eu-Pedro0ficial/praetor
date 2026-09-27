package shell

import (
	"strings"
	"sync"

	"github.com/Eu-Pedro0ficial/praetor/internal/presentation/preferences"
)

const maximumConsoleHistoryLines = 5000

// consoleHistory owns only presentation history for the active interactive
// session. It is not engineering memory, audit history, or domain state.
type consoleHistory struct {
	mutex        sync.Mutex
	lines        []consoleRow
	scrollOffset int
}

func newConsoleHistory() *consoleHistory {
	return &consoleHistory{
		lines: []consoleRow{
			{},
			{text: "  Welcome to Praetor", color: preferences.ColorCyan},
			{text: "  Governed engineering workspace", color: preferences.ColorGray},
			{},
			{text: "  AI proposes. System validates. Human governs.", color: preferences.ColorWhite},
			{},
		},
	}
}

func (history *consoleHistory) appendCommand(prompt, commandLine string) {
	if history == nil {
		return
	}

	history.mutex.Lock()
	defer history.mutex.Unlock()

	if len(history.lines) > 0 && history.lines[len(history.lines)-1].text != "" {
		history.lines = append(history.lines, consoleRow{})
	}

	history.lines = append(history.lines, consoleRow{
		text:  "  " + prompt + commandLine,
		color: preferences.ColorCyan,
	})

	history.scrollOffset = 0
	history.trimLocked()
}

func (history *consoleHistory) appendOutput(value string) {
	if history == nil {
		return
	}

	history.mutex.Lock()
	defer history.mutex.Unlock()

	normalized := strings.ReplaceAll(value, "\r\n", "\n")
	normalized = strings.ReplaceAll(normalized, "\r", "\n")

	for _, line := range strings.Split(normalized, "\n") {
		history.lines = append(history.lines, consoleRow{
			text:  "  " + line,
			color: preferences.ColorWhite,
		})
	}

	history.scrollOffset = 0
	history.trimLocked()
}

func (history *consoleHistory) visible(height, width int) []consoleRow {
	if history == nil || height <= 0 || width <= 0 {
		return nil
	}

	history.mutex.Lock()
	defer history.mutex.Unlock()

	rows := history.wrappedRowsLocked(width)
	total := len(rows)
	if total == 0 {
		return nil
	}
	if history.scrollOffset > total {
		history.scrollOffset = total
	}

	end := total - history.scrollOffset
	if end < 0 {
		end = 0
	}
	start := end - height
	if start < 0 {
		start = 0
	}

	result := make([]consoleRow, end-start)
	copy(result, rows[start:end])
	return result
}

func (history *consoleHistory) scrollOlder(viewportHeight, width int) {
	if history == nil || viewportHeight <= 0 || width <= 0 {
		return
	}

	history.mutex.Lock()
	defer history.mutex.Unlock()

	maximumOffset := len(history.wrappedRowsLocked(width)) - viewportHeight
	if maximumOffset <= 0 {
		history.scrollOffset = 0
		return
	}

	step := viewportHeight / 2
	if step < 1 {
		step = 1
	}

	history.scrollOffset += step
	if history.scrollOffset > maximumOffset {
		history.scrollOffset = maximumOffset
	}
}

func (history *consoleHistory) scrollNewer(viewportHeight, width int) {
	if history == nil || viewportHeight <= 0 {
		return
	}

	history.mutex.Lock()
	defer history.mutex.Unlock()

	step := viewportHeight / 2
	if step < 1 {
		step = 1
	}

	history.scrollOffset -= step
	if history.scrollOffset < 0 {
		history.scrollOffset = 0
	}
}

func (history *consoleHistory) scrollbarThumb(viewportHeight, width int) int {
	if history == nil || viewportHeight <= 0 || width <= 0 {
		return -1
	}

	history.mutex.Lock()
	defer history.mutex.Unlock()

	total := len(history.wrappedRowsLocked(width))
	if total <= viewportHeight {
		return -1
	}

	maximumStart := total - viewportHeight
	start := total - history.scrollOffset - viewportHeight

	if start < 0 {
		start = 0
	}
	if start > maximumStart {
		start = maximumStart
	}

	if viewportHeight == 1 || maximumStart == 0 {
		return 0
	}

	return start * (viewportHeight - 1) / maximumStart
}

func (history *consoleHistory) wrappedRowsLocked(width int) []consoleRow {
	var rows []consoleRow
	for _, line := range history.lines {
		for _, wrapped := range wrapTerminalText(line.text, width) {
			rows = append(rows, consoleRow{text: wrapped, color: line.color})
		}
	}
	return rows
}

func (history *consoleHistory) trimLocked() {
	if len(history.lines) <= maximumConsoleHistoryLines {
		return
	}

	remove := len(history.lines) - maximumConsoleHistoryLines
	copy(history.lines, history.lines[remove:])
	history.lines = history.lines[:maximumConsoleHistoryLines]

	if history.scrollOffset > len(history.lines) {
		history.scrollOffset = len(history.lines)
	}
}
