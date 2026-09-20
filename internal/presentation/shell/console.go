package shell

import (
	"io"
	"os"
	"strings"
	"unicode"

	"github.com/Eu-Pedro0ficial/praetor/internal/command"
	"github.com/Eu-Pedro0ficial/praetor/internal/presentation/preferences"
	"github.com/rivo/uniseg"
	"golang.org/x/sys/unix"
)

const (
	minimumConsoleWidth = 20
	minimumSidebarWidth = 84
	minimumSidebarSize  = 26
	maximumSidebarSize  = 34
)

var binaryShield = []string{
	"    101010101     ",
	"  1101010101011   ",
	" 110101010101011  ",
	" 101010101010101  ",
	"  1101010101011   ",
	"   10101010101    ",
	"    101010101     ",
	"      10101       ",
	"        1         ",
}

type terminalDimensions struct {
	Width  int
	Height int
}

type dimensionProvider func() terminalDimensions

type consoleRenderer struct {
	session    *command.Session
	dimensions dimensionProvider
	color      bool
	fullScreen bool
	history    *consoleHistory
}

type consoleRow struct {
	text  string
	color preferences.Color
}

type sidebarRow struct {
	text       string
	label      string
	value      string
	color      preferences.Color
	valueColor preferences.Color
	centered   bool
	separator  bool
}

type consoleSegment struct {
	value string
	color preferences.Color
}

func newConsoleRenderer(session *command.Session, dimensions dimensionProvider, color bool) *consoleRenderer {
	return &consoleRenderer{
		session:    session,
		dimensions: dimensions,
		color:      color,
		history:    newConsoleHistory(),
	}
}

func (renderer *consoleRenderer) setFullScreen(enabled bool) {
	renderer.fullScreen = enabled
}

func (renderer *consoleRenderer) viewportHeight() int {
	height := renderer.dimensions().Height - 8
	if height < 4 {
		return 4
	}
	return height
}

func (renderer *consoleRenderer) AppendCommand(commandLine string) {
	if renderer == nil || renderer.history == nil {
		return
	}
	renderer.history.appendCommand(renderer.session.Prompt(), commandLine)
}

func (renderer *consoleRenderer) AppendOutput(value string) {
	if renderer == nil || renderer.history == nil {
		return
	}
	renderer.history.appendOutput(value)
}

func (renderer *consoleRenderer) ScrollOlder() {
	if renderer == nil || renderer.history == nil {
		return
	}
	renderer.history.scrollOlder(renderer.viewportHeight())
}

func (renderer *consoleRenderer) ScrollNewer() {
	if renderer == nil || renderer.history == nil {
		return
	}
	renderer.history.scrollNewer(renderer.viewportHeight())
}

// terminalDefaultBackground returns an OSC 11 sequence that temporarily makes
// the configured Praetor presentation background the terminal's default
// background. This is required in fullscreen mode because readline legitimately
// emits SGR resets while editing; after a reset the terminal falls back to its
// default background rather than the previously active SGR background.
func terminalDefaultBackground(color preferences.Color) string {
	value := map[preferences.Color]string{
		preferences.ColorBlack:   "#171421",
		preferences.ColorWhite:   "#ffffff",
		preferences.ColorGray:    "#808080",
		preferences.ColorCyan:    "#00ffff",
		preferences.ColorBlue:    "#0000aa",
		preferences.ColorGreen:   "#00aa00",
		preferences.ColorYellow:  "#aaaa00",
		preferences.ColorRed:     "#aa0000",
		preferences.ColorMagenta: "#aa00aa",
	}[color]

	if value == "" {
		return ""
	}

	// OSC 11 changes the terminal's default background color.
	return "\x1b]11;" + value + "\x07"
}

// resetTerminalDefaultBackground resets an OSC 11 override back to the terminal
// profile's original/default background.
func resetTerminalDefaultBackground() string {
	// OSC 111 resets the default background color.
	return "\x1b]111\x07"
}

func (renderer *consoleRenderer) screenSurfaceStyle() string {
	if renderer == nil || !renderer.color {
		return ""
	}

	layout := renderer.session.LayoutPreferences()
	return backgroundCode(layout.Colors.Background) +
		foregroundCode(layout.Colors.Text)
}

func (renderer *consoleRenderer) EnterScreen() string {
	if renderer == nil || !renderer.fullScreen {
		return ""
	}

	layout := renderer.session.LayoutPreferences()

	// Enter the alternate screen first, then temporarily make Praetor's
	// configured background the terminal default. This also covers cells
	// exposed by readline SGR resets and any unused terminal rows.
	return "\x1b[?1049h" +
		terminalDefaultBackground(layout.Colors.Background) +
		renderer.screenSurfaceStyle() +
		"\x1b[H\x1b[2J"
}

func (renderer *consoleRenderer) LeaveScreen() string {
	if renderer == nil || !renderer.fullScreen {
		return ""
	}

	// Restore both SGR state and the terminal profile's default background
	// before leaving Praetor's alternate screen.
	return "\x1b[0m" +
		resetTerminalDefaultBackground() +
		"\x1b[?1049l"
}

func (renderer *consoleRenderer) Redraw() string {
	if renderer == nil {
		return ""
	}
	if !renderer.fullScreen {
		return renderer.Render()
	}

	// Re-apply the surface background before every clear because individual
	// rendered rows intentionally reset ANSI attributes.
	return renderer.screenSurfaceStyle() +
		"\x1b[H\x1b[2J" +
		renderer.Render()
}

func (renderer *consoleRenderer) RenderContextualHelp(value string) string {
	if renderer == nil {
		return ""
	}

	renderer.history.appendCommand(renderer.session.Prompt(), "?")
	renderer.history.appendOutput(strings.TrimSuffix(value, "\n"))
	return renderer.Redraw()
}

// Render draws the console through the workspace/input boundary. Readline
// supplies Prompt on the next row; ClosePrompt then finishes the footer/frame.
func (renderer *consoleRenderer) Render() string {
	if renderer.fullScreen {
		return renderer.renderViewport()
	}
	return renderer.renderLegacy()
}

func (renderer *consoleRenderer) renderViewport() string {
	dimensions := renderer.dimensions()

	width := dimensions.Width
	if width < minimumConsoleWidth {
		width = minimumConsoleWidth
	}

	layout := renderer.session.LayoutPreferences()
	status := renderer.session.StatusSnapshot()
	showSidebar := layout.Sidebar.Visible && dimensions.Width >= minimumSidebarWidth
	bodyHeight := renderer.viewportHeight()
	body := renderer.history.visible(bodyHeight)

	lines := []string{
		renderer.horizontal("┌", "┐", width, layout),
		renderer.fullLine(
			"◈  P R A E T O R   │   GOVERNED AI ENGINEERING",
			width,
			layout,
			layout.Colors.Accent,
		),
	}

	if showSidebar {
		sidebarWidth := sidebarWidthFor(width)
		leftWidth := width - sidebarWidth - 3
		sidebar := sidebarRows(layout, status)
		scrollbarThumb := renderer.history.scrollbarThumb(bodyHeight)

		lines = append(lines, renderer.splitRule(leftWidth, sidebarWidth, layout))

		for index := 0; index < bodyHeight; index++ {
			var left consoleRow
			var right sidebarRow

			if index < len(body) {
				left = body[index]
			}
			if index < len(sidebar) {
				right = sidebar[index]
			}

			divider := "│"
			if index == scrollbarThumb {
				divider = "┃"
			}

			lines = append(
				lines,
				renderer.splitLineWithDivider(
					left,
					right,
					leftWidth,
					sidebarWidth,
					divider,
					layout,
				),
			)
		}
	} else {
		lines = append(lines, renderer.horizontal("├", "┤", width, layout))

		for index := 0; index < bodyHeight; index++ {
			var row consoleRow
			if index < len(body) {
				row = body[index]
			}

			lines = append(
				lines,
				renderer.fullLine(
					row.text,
					width,
					layout,
					rowColor(row.color, layout.Colors.Text),
				),
			)
		}
	}

	lines = append(lines, renderer.horizontal("├", "┤", width, layout))
	return strings.Join(lines, "\n") + "\n"
}

func (renderer *consoleRenderer) renderLegacy() string {
	dimensions := renderer.dimensions()
	width := dimensions.Width
	if width < minimumConsoleWidth {
		width = minimumConsoleWidth
	}
	layout := renderer.session.LayoutPreferences()
	status := renderer.session.StatusSnapshot()
	showSidebar := layout.Sidebar.Visible && dimensions.Width >= minimumSidebarWidth

	lines := []string{
		renderer.horizontal("┌", "┐", width, layout),
		renderer.fullLine("◈  P R A E T O R   │   GOVERNED AI ENGINEERING", width, layout, layout.Colors.Accent),
	}
	main := []consoleRow{
		{},
		{text: "  Welcome to Praetor", color: layout.Colors.Accent},
		{text: "  Governed engineering workspace", color: preferences.ColorGray},
		{},
		{text: "  AI proposes. System validates. Human governs.", color: layout.Colors.Text},
	}
	if showSidebar {
		sidebarWidth := sidebarWidthFor(width)
		leftWidth := width - sidebarWidth - 3
		lines = append(lines, renderer.splitRule(leftWidth, sidebarWidth, layout))
		sidebar := sidebarRows(layout, status)
		rows := len(sidebar)
		if rows < len(main)+3 {
			rows = len(main) + 3
		}
		for index := 0; index < rows; index++ {
			var left consoleRow
			var right sidebarRow
			if index < len(main) {
				left = main[index]
			}
			if index < len(sidebar) {
				right = sidebar[index]
			}
			lines = append(lines, renderer.splitLine(left, right, leftWidth, sidebarWidth, layout))
		}
	} else {
		lines = append(lines, renderer.horizontal("├", "┤", width, layout))
		for _, row := range append(main, consoleRow{}, consoleRow{}) {
			lines = append(lines, renderer.fullLine(row.text, width, layout, rowColor(row.color, layout.Colors.Text)))
		}
	}
	lines = append(lines, renderer.horizontal("├", "┤", width, layout))
	return strings.Join(lines, "\n") + "\n"
}

// Prompt returns the styled readline-owned input prompt. Erasing the row with
// the active background keeps the terminal emulator color from showing through
// while leaving editing, redisplay, history, and completion to readline.
// PrepareInputRow paints the complete readline row with Praetor's configured
// presentation background before readline begins editing it. Readline may reset
// SGR attributes while redisplaying input, but already-painted terminal cells
// retain the intended surface background.
func (renderer *consoleRenderer) PrepareInputRow() string {
	if renderer == nil || !renderer.fullScreen || !renderer.color {
		return ""
	}

	width := renderer.dimensions().Width
	if width < minimumConsoleWidth {
		width = minimumConsoleWidth
	}

	layout := renderer.session.LayoutPreferences()

	return backgroundCode(layout.Colors.Background) +
		foregroundCode(layout.Colors.Text) +
		strings.Repeat(" ", width) +
		"\r"
}

func (renderer *consoleRenderer) Prompt() string {
	dimensions := renderer.dimensions()
	width := dimensions.Width
	if width < minimumConsoleWidth {
		width = minimumConsoleWidth
	}
	layout := renderer.session.LayoutPreferences()
	prompt := fitTerminalText(renderer.session.Prompt(), width-4)
	if !renderer.color {
		return "│ " + prompt
	}

	if renderer.fullScreen {
		return backgroundCode(layout.Colors.Background) +
			foregroundCode(layout.Colors.Border) + "│ " +
			foregroundCode(layout.Colors.Accent) + prompt
	}

	return backgroundCode(layout.Colors.Background) + "\x1b[2K\r" +
		foregroundCode(layout.Colors.Border) + "│ " +
		foregroundCode(layout.Colors.Accent) + prompt
}

// RightPrompt keeps the sidebar edge (or the outer edge in compact mode) on
// the active readline row. Readline hides it automatically if input reaches it.
func (renderer *consoleRenderer) RightPrompt() string {
	dimensions := renderer.dimensions()
	width := dimensions.Width
	if width < minimumConsoleWidth {
		width = minimumConsoleWidth
	}
	layout := renderer.session.LayoutPreferences()
	if !layout.Sidebar.Visible || dimensions.Width < minimumSidebarWidth {
		return renderer.lineStyle("│", layout, layout.Colors.Border)
	}
	sidebarWidth := sidebarWidthFor(width)
	return renderer.segmentedLine(layout,
		consoleSegment{value: "│", color: layout.Colors.Border},
		consoleSegment{value: strings.Repeat(" ", sidebarWidth), color: layout.Colors.Text},
		consoleSegment{value: "│", color: layout.Colors.Border},
	)
}

// ClosePrompt completes the console after readline has accepted or cancelled
// the input row, keeping command output in normal terminal scrollback.
func (renderer *consoleRenderer) ClosePrompt() string {
	dimensions := renderer.dimensions()
	width := dimensions.Width
	if width < minimumConsoleWidth {
		width = minimumConsoleWidth
	}
	layout := renderer.session.LayoutPreferences()
	separator := renderer.horizontal("├", "┤", width, layout)
	if layout.Sidebar.Visible && dimensions.Width >= minimumSidebarWidth {
		sidebarWidth := sidebarWidthFor(width)
		separator = renderer.splitBottom(width-sidebarWidth-3, sidebarWidth, layout)
	}
	result := separator + "\n" +
		renderer.footer(width, layout) + "\n" +
		renderer.horizontal("└", "┘", width, layout)

	if !renderer.fullScreen {
		result += "\n"
	}

	if renderer.color {
		if renderer.fullScreen {
			return renderer.screenSurfaceStyle() + result
		}
		return "\x1b[0m" + result
	}

	return result
}

func sidebarWidthFor(width int) int {
	sidebarWidth := width * 22 / 100
	if sidebarWidth < minimumSidebarSize {
		return minimumSidebarSize
	}
	if sidebarWidth > maximumSidebarSize {
		return maximumSidebarSize
	}
	return sidebarWidth
}

func sidebarRows(layout preferences.Layout, status command.StatusSnapshot) []sidebarRow {
	var rows []sidebarRow
	appendSeparator := func() {
		if len(rows) > 0 && !rows[len(rows)-1].separator {
			rows = append(rows, sidebarRow{separator: true})
		}
	}
	if layout.Sidebar.Identity {
		for _, line := range binaryShield {
			rows = append(rows, sidebarRow{text: line, color: layout.Colors.Accent, centered: true})
		}
		rows = append(rows,
			sidebarRow{text: "P R A E T O R", color: layout.Colors.Text, centered: true},
			sidebarRow{text: "GOVERNED AI ENGINEERING", color: preferences.ColorGray, centered: true},
		)
		appendSeparator()
	}
	if layout.Sidebar.Context {
		rows = append(rows,
			sidebarRow{text: "CONTEXT", color: layout.Colors.Accent},
			statusRow("Project", status.Project),
			statusRow("Repository", status.Repository),
			statusRow("Change", status.Change),
			statusRow("Proposal", status.Proposal),
		)
		appendSeparator()
	}
	if layout.Sidebar.Provider {
		rows = append(rows,
			sidebarRow{text: "PROVIDER", color: layout.Colors.Accent},
			statusRow("Adapter", status.ProviderAdapter),
			statusRow("Vendor", status.ProviderVendor),
			statusRow("Model", status.ProviderModel),
		)
		appendSeparator()
	}
	if layout.Sidebar.Status {
		rows = append(rows,
			sidebarRow{text: "STATUS", color: layout.Colors.Accent},
			semanticStatusRow("Verify", status.Verification),
			statusRow("Decision", status.HumanDecision),
			semanticStatusRow("Recovery", status.Recovery),
			semanticStatusRow("Session", status.Session),
			semanticStatusRow("Git", status.GitRepository),
		)
	}
	return rows
}

func statusRow(label, value string) sidebarRow {
	return sidebarRow{label: label, value: value, color: preferences.ColorGray, valueColor: preferences.ColorWhite}
}

func semanticStatusRow(label, value string) sidebarRow {
	row := statusRow(label, value)
	switch strings.ToUpper(value) {
	case "ACTIVE", "TRUE", "PASS", "READY":
		row.valueColor = preferences.ColorGreen
	}
	return row
}

func rowColor(color, fallback preferences.Color) preferences.Color {
	if color == "" {
		return fallback
	}
	return color
}

func (renderer *consoleRenderer) horizontal(left, right string, width int, layout preferences.Layout) string {
	return renderer.lineStyle(left+strings.Repeat("─", width-2)+right, layout, layout.Colors.Border)
}

func (renderer *consoleRenderer) splitRule(leftWidth, sidebarWidth int, layout preferences.Layout) string {
	return renderer.lineStyle("├"+strings.Repeat("─", leftWidth)+"┬"+strings.Repeat("─", sidebarWidth)+"┤", layout, layout.Colors.Border)
}

func (renderer *consoleRenderer) splitBottom(leftWidth, sidebarWidth int, layout preferences.Layout) string {
	return renderer.lineStyle("├"+strings.Repeat("─", leftWidth)+"┴"+strings.Repeat("─", sidebarWidth)+"┤", layout, layout.Colors.Border)
}

func (renderer *consoleRenderer) fullLine(value string, width int, layout preferences.Layout, foreground preferences.Color) string {
	content := fitTerminalText(value, width-4)
	middle := " " + content + strings.Repeat(" ", width-4-displayWidth(content)) + " "
	return renderer.segmentedLine(layout,
		consoleSegment{value: "│", color: layout.Colors.Border},
		consoleSegment{value: middle, color: foreground},
		consoleSegment{value: "│", color: layout.Colors.Border},
	)
}

func (renderer *consoleRenderer) splitLine(left consoleRow, right sidebarRow, leftWidth, sidebarWidth int, layout preferences.Layout) string {
	return renderer.splitLineWithDivider(
		left,
		right,
		leftWidth,
		sidebarWidth,
		"│",
		layout,
	)
}

func (renderer *consoleRenderer) splitLineWithDivider(
	left consoleRow,
	right sidebarRow,
	leftWidth int,
	sidebarWidth int,
	divider string,
	layout preferences.Layout,
) string {
	leftContent := fitTerminalText(left.text, leftWidth-2)
	leftMiddle := " " + leftContent + strings.Repeat(" ", leftWidth-2-displayWidth(leftContent)) + " "

	segments := []consoleSegment{
		{value: "│", color: layout.Colors.Border},
		{value: leftMiddle, color: rowColor(left.color, layout.Colors.Text)},
		{value: divider, color: layout.Colors.Accent},
	}

	segments = append(segments, sidebarSegments(right, sidebarWidth, layout)...)
	segments = append(segments, consoleSegment{value: "│", color: layout.Colors.Border})

	return renderer.segmentedLine(layout, segments...)
}

func sidebarSegments(row sidebarRow, width int, layout preferences.Layout) []consoleSegment {
	contentWidth := width - 2
	if row.separator {
		return []consoleSegment{{value: strings.Repeat("─", width), color: layout.Colors.Border}}
	}
	if row.label != "" {
		label := fitTerminalText(row.label, 10)
		valueWidth := contentWidth - displayWidth(label) - 1
		value := fitTerminalText(row.value, valueWidth)
		gap := contentWidth - displayWidth(label) - displayWidth(value)
		if gap < 1 {
			gap = 1
		}
		return []consoleSegment{
			{value: " " + label, color: rowColor(row.color, preferences.ColorGray)},
			{value: strings.Repeat(" ", gap), color: layout.Colors.Text},
			{value: value, color: rowColor(row.valueColor, layout.Colors.Text)},
			{value: " ", color: layout.Colors.Text},
		}
	}
	text := fitTerminalText(row.text, contentWidth)
	leftPadding := 0
	if row.centered {
		leftPadding = (contentWidth - displayWidth(text)) / 2
	}
	rightPadding := contentWidth - displayWidth(text) - leftPadding
	return []consoleSegment{{
		value: " " + strings.Repeat(" ", leftPadding) + text + strings.Repeat(" ", rightPadding) + " ",
		color: rowColor(row.color, layout.Colors.Text),
	}}
}

func (renderer *consoleRenderer) footer(width int, layout preferences.Layout) string {
	contentWidth := width - 4
	left := "? Help    ↑↓ History    Tab Complete    Ctrl+C Cancel"
	if renderer.fullScreen {
		left = "? Help    ↑↓ History    PgUp/PgDn Scroll    Tab Complete    Ctrl+C Cancel"
	}
	right := "• Ready"
	if width < 80 {
		left = "? Help    Tab Complete    Ctrl+C Cancel"
	}
	if width < 52 {
		left = "? Help    Tab    Ctrl+C"
		right = "Ready"
	}
	left = fitTerminalText(left, contentWidth)
	remaining := contentWidth - displayWidth(left)
	if remaining <= displayWidth(right) {
		right = ""
	}
	gap := contentWidth - displayWidth(left) - displayWidth(right)
	return renderer.segmentedLine(layout,
		consoleSegment{value: "│", color: layout.Colors.Border},
		consoleSegment{value: " " + left, color: preferences.ColorGray},
		consoleSegment{value: strings.Repeat(" ", gap), color: layout.Colors.Text},
		consoleSegment{value: right, color: preferences.ColorGreen},
		consoleSegment{value: " │", color: layout.Colors.Border},
	)
}

func (renderer *consoleRenderer) segmentedLine(layout preferences.Layout, segments ...consoleSegment) string {
	if !renderer.color {
		var plain strings.Builder
		for _, segment := range segments {
			plain.WriteString(segment.value)
		}
		return plain.String()
	}
	var styled strings.Builder
	styled.WriteString(backgroundCode(layout.Colors.Background))
	for _, segment := range segments {
		styled.WriteString(foregroundCode(segment.color))
		styled.WriteString(segment.value)
	}
	styled.WriteString("\x1b[0m")
	return styled.String()
}

func (renderer *consoleRenderer) lineStyle(value string, layout preferences.Layout, foreground preferences.Color) string {
	if !renderer.color {
		return value
	}
	return backgroundCode(layout.Colors.Background) + foregroundCode(foreground) + value + "\x1b[0m"
}

func fitTerminalText(value string, width int) string {
	if width <= 0 {
		return ""
	}
	value = strings.ToValidUTF8(value, "�")
	value = strings.Map(func(character rune) rune {
		if unicode.IsControl(character) {
			return ' '
		}
		return character
	}, value)
	if displayWidth(value) <= width {
		return value
	}
	if width == 1 {
		return "…"
	}
	limit := width - 1
	var result strings.Builder
	graphemes := uniseg.NewGraphemes(value)
	used := 0
	for graphemes.Next() {
		cluster := graphemes.Str()
		clusterWidth := uniseg.StringWidth(cluster)
		if clusterWidth < 0 || used+clusterWidth > limit {
			break
		}
		result.WriteString(cluster)
		used += clusterWidth
	}
	return result.String() + "…"
}

func displayWidth(value string) int { return uniseg.StringWidth(value) }

func foregroundCode(color preferences.Color) string {
	return map[preferences.Color]string{
		preferences.ColorDefault: "\x1b[39m", preferences.ColorBlack: "\x1b[30m",
		preferences.ColorRed: "\x1b[31m", preferences.ColorGreen: "\x1b[32m",
		preferences.ColorYellow: "\x1b[33m", preferences.ColorBlue: "\x1b[34m",
		preferences.ColorMagenta: "\x1b[35m", preferences.ColorCyan: "\x1b[36m",
		preferences.ColorWhite: "\x1b[37m", preferences.ColorGray: "\x1b[90m",
	}[color]
}

func backgroundCode(color preferences.Color) string {
	return map[preferences.Color]string{
		preferences.ColorTerminal: "", preferences.ColorBlack: "\x1b[48;2;23;20;33m",
		preferences.ColorRed: "\x1b[41m", preferences.ColorGreen: "\x1b[42m",
		preferences.ColorYellow: "\x1b[43m", preferences.ColorBlue: "\x1b[44m",
		preferences.ColorMagenta: "\x1b[45m", preferences.ColorCyan: "\x1b[46m",
		preferences.ColorWhite: "\x1b[47m", preferences.ColorGray: "\x1b[100m",
	}[color]
}

func terminalIsInteractive(output io.Writer) bool {
	file, ok := output.(interface{ Fd() uintptr })
	if !ok {
		return false
	}

	_, terminalError := unix.IoctlGetTermios(int(file.Fd()), unix.TCGETS)
	return terminalError == nil && os.Getenv("TERM") != "dumb"
}

func terminalCapabilities(output io.Writer) (dimensionProvider, bool) {
	file, ok := output.(interface{ Fd() uintptr })
	if !ok {
		return func() terminalDimensions { return terminalDimensions{Width: 80, Height: 24} }, false
	}
	provider := func() terminalDimensions {
		window, err := unix.IoctlGetWinsize(int(file.Fd()), unix.TIOCGWINSZ)
		if err != nil || window.Col == 0 {
			return terminalDimensions{Width: 80, Height: 24}
		}
		return terminalDimensions{Width: int(window.Col), Height: int(window.Row)}
	}
	_, terminalError := unix.IoctlGetTermios(int(file.Fd()), unix.TCGETS)
	_, noColor := os.LookupEnv("NO_COLOR")
	color := terminalError == nil && !noColor && os.Getenv("TERM") != "dumb"
	return provider, color
}
