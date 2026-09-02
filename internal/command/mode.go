package command

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode"
)

// ModeIdentity names shell interaction context only. It is unrelated to the
// Change lifecycle state machine.
type ModeIdentity string

const (
	ModeRoot             ModeIdentity = "root"
	ModeAnalysis         ModeIdentity = "analysis"
	ModeChange           ModeIdentity = "change"
	ModeProvider         ModeIdentity = "provider"
	ModeConfigure        ModeIdentity = "configure"
	ModeConfigureProject ModeIdentity = "configure-project"
)

// ModeContext is immutable-by-value contextual shell navigation metadata.
type ModeContext struct {
	Identity           ModeIdentity
	Parent             ModeIdentity
	CommandPath        string
	PromptContribution string
}

func rootModeContext() ModeContext {
	return ModeContext{Identity: ModeRoot, CommandPath: "root"}
}

// CurrentMode returns the active contextual shell mode.
func (session *Session) CurrentMode() ModeContext {
	if session == nil || len(session.modeStack) == 0 {
		return rootModeContext()
	}
	return session.modeStack[len(session.modeStack)-1]
}

// ModeStack returns a defensive copy from root through the active mode.
func (session *Session) ModeStack() []ModeContext {
	if session == nil || len(session.modeStack) == 0 {
		return []ModeContext{rootModeContext()}
	}
	return append([]ModeContext(nil), session.modeStack...)
}

func (session *Session) enterMode(mode ModeContext) error {
	if session == nil {
		return fmt.Errorf("active command session is required")
	}
	if len(session.modeStack) == 0 {
		session.modeStack = []ModeContext{rootModeContext()}
	}
	current := session.CurrentMode()
	if mode.Identity == "" || mode.Identity == ModeRoot {
		return fmt.Errorf("valid non-root mode is required")
	}
	if mode.Parent != current.Identity {
		return fmt.Errorf(
			"mode %q requires parent %q, current mode is %q",
			mode.Identity,
			mode.Parent,
			current.Identity,
		)
	}
	session.modeStack = append(session.modeStack, mode)
	return nil
}

func (session *Session) leaveMode() error {
	if session == nil {
		return fmt.Errorf("active command session is required")
	}
	if len(session.modeStack) <= 1 {
		return fmt.Errorf("shell is already at the root mode")
	}
	session.modeStack = session.modeStack[:len(session.modeStack)-1]
	return nil
}

// ProjectDisplayName returns presentation-only repository metadata for the
// prompt. It is not ProjectId or durable project configuration.
func (session *Session) ProjectDisplayName() string {
	if session == nil {
		return "project"
	}
	displayName := promptSegment(filepath.Base(filepath.Clean(session.registration.RepositoryRoot)))
	if displayName == "" {
		return "project"
	}
	return displayName
}

// Prompt renders the current retained Project and contextual mode stack.
func (session *Session) Prompt() string {
	segments := []string{"praetor", session.ProjectDisplayName()}
	for _, mode := range session.ModeStack()[1:] {
		if contribution := promptSegment(mode.PromptContribution); contribution != "" {
			segments = append(segments, contribution)
		}
	}
	return "{" + strings.Join(segments, "-") + "}$ "
}

func promptSegment(value string) string {
	var normalized strings.Builder
	previousSeparator := false
	for _, character := range strings.TrimSpace(value) {
		switch {
		case unicode.IsLetter(character) || unicode.IsDigit(character):
			normalized.WriteRune(unicode.ToLower(character))
			previousSeparator = false
		case !previousSeparator:
			normalized.WriteRune('-')
			previousSeparator = true
		}
	}
	return strings.Trim(normalized.String(), "-")
}
