package shell

import (
	"io"
	"strings"
	"testing"
	"unicode/utf8"
)

func TestConsoleWideNarrowResizeAndSidebarPreference(t *testing.T) {
	_, _, session, registry := prepareShellTest(t)
	dimensions := terminalDimensions{Width: 120, Height: 30}
	renderer := newConsoleRenderer(session, func() terminalDimensions { return dimensions }, false)

	wide := renderer.Render()
	for _, expected := range []string{
		"P R A E T O R", "GOVERNED AI ENGINEERING", "110101010101011", "CONTEXT", "PROVIDER", "STATUS",
	} {
		if !strings.Contains(wide, expected) {
			t.Fatalf("wide console lacks %q:\n%s", expected, wide)
		}
	}
	assertRenderedWidth(t, wide, 120)
	closed := renderer.ClosePrompt()
	for _, expected := range []string{"? Help", "Tab Complete", "Ctrl+C Cancel", "Ready"} {
		if !strings.Contains(closed, expected) {
			t.Fatalf("console footer lacks %q:\n%s", expected, closed)
		}
	}
	assertRenderedWidth(t, closed, 120)
	if prompt := renderer.Prompt(); !strings.HasPrefix(prompt, "│ {praetor-") {
		t.Fatalf("prompt is not visually inside console: %q", prompt)
	}
	if right := renderer.RightPrompt(); !strings.HasPrefix(right, "│") || !strings.HasSuffix(right, "│") || displayWidth(right) != sidebarWidthFor(120)+2 {
		t.Fatalf("active input row does not retain sidebar edge: %q", right)
	}
	if !strings.Contains(strings.SplitN(closed, "\n", 2)[0], "┴") {
		t.Fatalf("footer does not close below the split workspace: %q", closed)
	}

	dimensions.Width = minimumSidebarWidth - 5
	narrow := renderer.Render()
	if strings.Contains(narrow, "110101010101011") || strings.Contains(narrow, "CONTEXT") {
		t.Fatalf("narrow console retained sidebar:\n%s", narrow)
	}
	assertRenderedWidth(t, narrow, dimensions.Width)
	if !strings.Contains(renderer.ClosePrompt(), "Tab Complete") || !strings.Contains(renderer.ClosePrompt(), "Ready") {
		t.Fatalf("narrow footer was not adapted:\n%s", renderer.ClosePrompt())
	}
	if !session.LayoutPreferences().Sidebar.Visible {
		t.Fatal("adaptive suppression changed persisted preference")
	}

	dimensions.Width = 120
	if !strings.Contains(renderer.Render(), "110101010101011") {
		t.Fatal("widening did not restore configured sidebar")
	}
	if _, err := registry.Dispatch(session, "configure layout sidebar show off", io.Discard); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(renderer.Render(), "110101010101011") {
		t.Fatal("explicit sidebar off still rendered sidebar")
	}
}

func TestConsoleIndividualSectionsStatusSourceAndDeterministicIdentity(t *testing.T) {
	_, _, session, registry := prepareShellTest(t)
	layout := session.LayoutPreferences()
	snapshot := session.StatusSnapshot()
	rows := sidebarRows(layout, snapshot)
	var sidebarText []string
	for _, row := range rows {
		sidebarText = append(sidebarText, row.text+row.label+row.value)
	}
	plainSidebar := strings.Join(sidebarText, "\n")
	for _, value := range []string{
		snapshot.Project, snapshot.Repository, snapshot.Change, snapshot.Proposal,
		snapshot.ProviderAdapter, snapshot.ProviderVendor, snapshot.ProviderModel,
		snapshot.Verification, snapshot.HumanDecision, snapshot.Session, snapshot.GitRepository,
	} {
		if !strings.Contains(plainSidebar, value) {
			t.Fatalf("sidebar projection lacks status snapshot value %q", value)
		}
	}
	first := strings.Join(binaryShield, "\n")
	secondRows := sidebarRows(layout, snapshot)
	var secondIdentity []string
	for _, row := range secondRows[:len(binaryShield)] {
		secondIdentity = append(secondIdentity, row.text)
	}
	second := strings.Join(secondIdentity, "\n")
	if first != second || len(binaryShield) != 9 {
		t.Fatalf("binary identity is not deterministic: %q", first)
	}
	if strings.Contains(first, "1111110000") {
		t.Fatalf("legacy digit rows remain in binary shield: %q", first)
	}

	tests := []struct{ command, absent string }{
		{"configure layout sidebar identity off", "110101010101011"},
		{"configure layout sidebar context off", "CONTEXT"},
		{"configure layout sidebar provider off", "PROVIDER"},
		{"configure layout sidebar status off", "STATUS"},
	}
	for _, test := range tests {
		if _, err := registry.Dispatch(session, test.command, io.Discard); err != nil {
			t.Fatal(err)
		}
	}
	rendered := newConsoleRenderer(session, func() terminalDimensions {
		return terminalDimensions{Width: 120, Height: 30}
	}, false).Render()
	for _, test := range tests {
		if strings.Contains(rendered, test.absent) {
			t.Fatalf("disabled section %q still rendered:\n%s", test.absent, rendered)
		}
	}
}

func TestConsoleSanitizesAndTruncatesDynamicTerminalText(t *testing.T) {
	value := "repo\x1b[2J\n秘密-e\u0301-very-long"
	fitted := fitTerminalText(value, 12)
	if strings.ContainsAny(fitted, "\x1b\n\r") || !utf8.ValidString(fitted) || displayWidth(fitted) > 12 || !strings.HasSuffix(fitted, "…") {
		t.Fatalf("fitTerminalText() = %q width=%d", fitted, displayWidth(fitted))
	}
	if got := fitTerminalText("e\u0301", 1); got != "e\u0301" {
		t.Fatalf("grapheme-safe fit = %q", got)
	}
}

func TestConsoleBoundedColorAndReadableFallback(t *testing.T) {
	_, _, session, registry := prepareShellTest(t)
	for _, line := range []string{
		"configure layout color accent magenta",
		"configure layout color border cyan",
		"configure layout color background black",
		"configure layout color text white",
	} {
		if _, err := registry.Dispatch(session, line, io.Discard); err != nil {
			t.Fatal(err)
		}
	}
	dimensions := func() terminalDimensions { return terminalDimensions{Width: 100, Height: 30} }
	renderer := newConsoleRenderer(session, dimensions, true)
	colored := renderer.Render() + renderer.Prompt() + renderer.ClosePrompt()
	for _, sequence := range []string{"\x1b[35m", "\x1b[36m", "\x1b[40m", "\x1b[37m", "\x1b[0m"} {
		if !strings.Contains(colored, sequence) {
			t.Fatalf("colored console lacks %q", sequence)
		}
	}
	plainRenderer := newConsoleRenderer(session, dimensions, false)
	plain := plainRenderer.Render() + plainRenderer.Prompt() + plainRenderer.ClosePrompt()
	if strings.Contains(plain, "\x1b[") || !strings.Contains(plain, "P R A E T O R") || !strings.Contains(plain, "? Help") {
		t.Fatalf("color-disabled fallback = %q", plain)
	}
}

func assertRenderedWidth(t *testing.T, rendered string, width int) {
	t.Helper()
	for index, line := range strings.Split(strings.TrimSuffix(rendered, "\n"), "\n") {
		if got := displayWidth(line); got != width {
			t.Fatalf("line %d width = %d, want %d: %q", index, got, width, line)
		}
	}
}
