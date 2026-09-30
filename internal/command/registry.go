package command

import (
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode"

	"github.com/Eu-Pedro0ficial/praetor/internal/presentation/preferences"
)

var errInvalidArguments = errors.New("invalid command arguments")

// Result reports presentation control requested by a command handler.
type Result struct {
	Exit bool
}

// Invocation is registry-derived command identity plus parsed arguments.
type Invocation struct {
	CommandPath string
	Arguments   []string
	Context     context.Context
}

// Handler delegates one parsed command to the retained application session.
type Handler func(*Session, Invocation, io.Writer) (Result, error)

// Option is command-owned discovery metadata. It does not perform argument
// parsing or introduce a generic command framework.
type Option struct {
	Name        string
	Description string
}

// Definition is explicit compile-time command registration input. A node is
// either executable through Handler or enters Mode and optionally owns child
// commands.
type Definition struct {
	Name                string
	Description         string
	Usage               string
	Handler             Handler
	Children            []Definition
	Options             []Option
	ArgumentSuggestions func(*Session, string) []Suggestion
	Mode                ModeIdentity
	Global              bool
}

// Metadata is immutable-by-copy command information used by help and terminal
// adapters.
type Metadata struct {
	Name        string
	Path        string
	Description string
	Usage       string
	Children    []Metadata
	Options     []Option
	Mode        ModeIdentity
}

// Suggestion is one registry-owned completion or contextual-help candidate.
type Suggestion struct {
	Text        string
	Description string
}

type registeredMode struct {
	context    ModeContext
	definition Definition
}

// Registry owns the deterministic hierarchical command tree. It is
// constructed explicitly and is never global or mutated after construction.
type Registry struct {
	definitions []Definition
	end         Definition
	modes       map[ModeIdentity]registeredMode
}

// NewRegistry validates and defensively copies explicit root definitions.
// It is primarily useful for command-tree validation tests and small adapters.
func NewRegistry(definitions []Definition) (Registry, error) {
	return newRegistry(definitions, Definition{})
}

func newRegistry(definitions []Definition, end Definition) (Registry, error) {
	if len(definitions) == 0 {
		return Registry{}, fmt.Errorf("at least one root command is required")
	}
	cloned := cloneDefinitions(definitions)
	if err := validateDefinitions(cloned, "", true); err != nil {
		return Registry{}, err
	}
	clonedEnd := cloneDefinition(end)
	if clonedEnd.Name != "" {
		if err := validateDefinitions([]Definition{clonedEnd}, "navigation", false); err != nil {
			return Registry{}, err
		}
	}

	registry := Registry{
		definitions: cloned,
		end:         clonedEnd,
		modes:       make(map[ModeIdentity]registeredMode),
	}
	if err := registry.indexModes(cloned, ModeRoot, ""); err != nil {
		return Registry{}, err
	}
	return registry, nil
}

// DefaultRegistry constructs the current M0.1-M1.2 hierarchical shell
// command surface.
func DefaultRegistry() (Registry, error) {
	var registry Registry
	definitions := []Definition{
		{
			Name:        "status",
			Description: "Show current project and runtime status",
			Usage:       "status",
			Handler:     handleStatus,
		},
		{
			Name:        "analysis",
			Description: "Enter source and Change analysis mode",
			Usage:       "analysis [model|impact|report|inspect ...]",
			Mode:        ModeAnalysis,
			Children: []Definition{
				{
					Name:        "model",
					Description: "Build or reuse the exact current RepositoryModel",
					Usage:       "model",
					Handler:     handleAnalysisModel,
				},
				{
					Name:        "impact",
					Description: "Analyze the bounded impact and Change Surface",
					Usage:       "impact <change-id> <intent> [--expected <path>...] [--possible <path>...] [--protected <path>...] --actual <path>... (expected or possible is required; . means repository-wide)",
					Handler:     handleAnalysisImpact,
					Options:     surfaceOptions(true, false),
				},
				{
					Name:        "report",
					Description: "Persist an explainable Change-owned ImpactReport",
					Usage:       "report <change-id> [--expected <path>...] [--possible <path>...] [--protected <path>...] (expected or possible is required; . means repository-wide)",
					Handler:     handleAnalysisReport,
					Options:     surfaceOptions(false, false),
				},
				{
					Name:        "inspect",
					Description: "Inspect a durable ImpactReport and its current freshness",
					Usage:       "inspect <change-id> [<artifact-id>]",
					Handler:     handleAnalysisInspect,
				},
			},
		},
		{
			Name:        "change",
			Description: "Enter software Change governance mode",
			Usage:       "change [command]",
			Mode:        ModeChange,
			Children: []Definition{
				{Name: "list", Description: "List durable Changes for the active Project", Usage: "list", Handler: handleChangeList},
				{Name: "show", Description: "Inspect durable Change and workflow authority", Usage: "show [<change-id>]", Handler: handleChangeShow},
				{Name: "select", Description: "Select a durable Change for this session", Usage: "select <change-id>", Handler: handleChangeSelect},
				{Name: "artifacts", Description: "List durable artifact metadata and current bindings", Usage: "artifacts [<change-id>]", Handler: handleChangeArtifacts},
				{Name: "history", Description: "Show durable append-oriented Change audit history", Usage: "history [<change-id>]", Handler: handleChangeHistory},
				{Name: "diagnose", Description: "Explain recovery authority and safe next actions without mutation", Usage: "diagnose [<change-id>]", Handler: handleChangeDiagnose},
				{Name: "recover", Description: "Explicitly reconcile one exact owned durable operation", Usage: "recover <operation-id>", Handler: handleChangeRecover},
				{Name: "content", Description: "Inspect explicitly requested bounded artifact content", Usage: "content <artifact-id> [<change-id>]", Handler: handleChangeContent},
				{
					Name:        "new",
					Description: "Create a Change and optionally apply lifecycle transitions",
					Usage:       "new <change-id> <intent> [<state> ...]",
					Handler:     handleChangeNew,
				},
				{
					Name:        "isolate",
					Description: "Create an isolated proposal; scope is repository-wide unless paths are provided",
					Usage:       "isolate <change-id> <intent>",
					Handler:     handleChangeIsolate,
					Options:     surfaceOptions(false, true),
				},
				{
					Name:        "implement",
					Description: "Execute the selected AI provider in the active ProposalWorkspace",
					Usage:       "implement",
					Handler:     handleChangeImplement,
				},
				{
					Name:        "patch",
					Description: "Extract and surface-check the current isolated proposal",
					Usage:       "patch",
					Handler:     handleChangePatch,
				},
				{
					Name:        "verify",
					Description: "Run the deterministic verification gate for the retained proposal",
					Usage:       "verify",
					Handler:     handleChangeVerify,
				},
				{
					Name:        "approve",
					Description: "Explicitly authorize the validated proposal for later canonical application",
					Usage:       "approve [<rationale>]",
					Handler:     handleChangeApprove,
				},
				{
					Name:        "reject",
					Description: "Explicitly reject the validated proposal",
					Usage:       "reject [<rationale>]",
					Handler:     handleChangeReject,
				},
				{
					Name:        "apply",
					Description: "Apply the explicitly approved PatchArtifact to the canonical working tree",
					Usage:       "apply",
					Handler:     handleChangeApply,
				},
				{
					Name:        "close",
					Description: "Close a rejected Change with canonical source unchanged",
					Usage:       "close",
					Handler:     handleChangeClose,
				},
				{
					Name:        "discard",
					Description: "Reject and clean the current proposal workspace",
					Usage:       "discard",
					Handler:     handleChangeDiscard,
				},
			},
		},
		{
			Name: "policy", Description: "Enter Project Policy inspection and evaluation mode", Usage: "policy [show|list|evaluate|exception ...]", Mode: ModePolicy,
			Children: []Definition{
				{Name: "show", Description: "Show the Project Policy Manifest and bundle identity", Usage: "show", Handler: handlePolicyShow},
				{Name: "list", Description: "List policies in the effective project bundle", Usage: "list", Handler: handlePolicyList},
				{Name: "evaluate", Description: "Evaluate the retained deterministic EvidenceSet", Usage: "evaluate", Handler: handlePolicyEvaluate},
				{Name: "exception", Description: "Create an auditable exception candidate without granting it", Usage: "exception <policy-id> <scope> <authority> <reason>", Handler: handlePolicyException},
			},
		},
		{
			Name:        "provider",
			Description: "Enter explicit AI provider and model selection mode",
			Usage:       "provider [list|show|diagnose|select|model ...]",
			Mode:        ModeProvider,
			Children: []Definition{
				{
					Name:        "list",
					Description: "List explicitly registered AI provider adapters",
					Usage:       "list",
					Handler:     handleProviderList,
				},
				{
					Name:        "show",
					Description: "Show the active session provider and model selection",
					Usage:       "show",
					Handler:     handleProviderShow,
				},
				{
					Name:        "diagnose",
					Description: "Inspect local provider readiness without provider execution",
					Usage:       "diagnose",
					Handler:     handleProviderDiagnose,
				},
				{
					Name:                "select",
					Description:         "Select one registered AI provider adapter",
					Usage:               "select <provider>",
					Handler:             handleProviderSelect,
					ArgumentSuggestions: providerIdentifierSuggestions,
				},
				{
					Name:        "model",
					Description: "Select the active provider-scoped model",
					Usage:       "model <model>",
					Handler:     handleProviderModel,
				},
			},
		},
		{
			Name:        "configure",
			Description: "Enter runtime and presentation configuration contexts",
			Usage:       "configure [project|layout ...]",
			Mode:        ModeConfigure,
			Children: []Definition{
				{
					Name:        "project",
					Description: "Enter Project configuration context; no mutating settings exist in M0.5",
					Usage:       "project",
					Mode:        ModeConfigureProject,
				},
				{
					Name:        "layout",
					Description: "Configure user-local terminal presentation preferences",
					Usage:       "layout [show|sidebar|color|reset ...]",
					Mode:        ModeConfigureLayout,
					Children: []Definition{
						{Name: "show", Description: "Show effective layout preferences", Usage: "show", Handler: handleLayoutShow},
						{
							Name: "sidebar", Description: "Configure sidebar visibility and sections", Usage: "sidebar [show|identity|context|provider|status ...]", Mode: ModeLayoutSidebar,
							Children: []Definition{
								{Name: "show", Description: "Show or hide the complete sidebar", Usage: "show <on|off>", Handler: handleSidebarVisible, ArgumentSuggestions: onOffSuggestions},
								{Name: "identity", Description: "Show or hide Praetor identity", Usage: "identity <on|off>", Handler: handleSidebarSection("identity"), ArgumentSuggestions: onOffSuggestions},
								{Name: "context", Description: "Show or hide Project and Change context", Usage: "context <on|off>", Handler: handleSidebarSection("context"), ArgumentSuggestions: onOffSuggestions},
								{Name: "provider", Description: "Show or hide provider selection", Usage: "provider <on|off>", Handler: handleSidebarSection("provider"), ArgumentSuggestions: onOffSuggestions},
								{Name: "status", Description: "Show or hide runtime status", Usage: "status <on|off>", Handler: handleSidebarSection("status"), ArgumentSuggestions: onOffSuggestions},
							},
						},
						{
							Name: "color", Description: "Configure the bounded terminal color palette", Usage: "color [accent|border|background|text ...]", Mode: ModeLayoutColor,
							Children: []Definition{
								{Name: "accent", Description: "Set the restrained accent color", Usage: "accent <color>", Handler: handlePresentationColor("accent"), ArgumentSuggestions: foregroundColorSuggestions},
								{Name: "border", Description: "Set the console border color", Usage: "border <color>", Handler: handlePresentationColor("border"), ArgumentSuggestions: foregroundColorSuggestions},
								{Name: "background", Description: "Set Praetor-rendered background regions", Usage: "background <color>", Handler: handlePresentationColor("background"), ArgumentSuggestions: backgroundColorSuggestions},
								{Name: "text", Description: "Set console text color", Usage: "text <color>", Handler: handlePresentationColor("text"), ArgumentSuggestions: foregroundColorSuggestions},
							},
						},
						{Name: "reset", Description: "Persist Praetor layout defaults", Usage: "reset", Handler: handleLayoutReset},
					},
				},
			},
		},
		{
			Name:        "help",
			Description: "Show help for the current mode or a command",
			Usage:       "help [<command> [<child> ...]]",
			Global:      true,
			Handler: func(session *Session, invocation Invocation, output io.Writer) (Result, error) {
				return Result{}, registry.writeHelp(session, invocation.Arguments, output)
			},
		},
		{
			Name:        "?",
			Description: "Show contextual commands without executing input",
			Usage:       "?",
			Global:      true,
			Handler: func(session *Session, invocation Invocation, output io.Writer) (Result, error) {
				if len(invocation.Arguments) != 0 {
					return Result{}, errInvalidArguments
				}
				return Result{}, registry.writeContextualHelp(session, "", output)
			},
		},
		{
			Name:        "exit",
			Description: "Exit Praetor from root mode",
			Usage:       "exit",
			Handler: func(_ *Session, invocation Invocation, _ io.Writer) (Result, error) {
				if len(invocation.Arguments) != 0 {
					return Result{}, errInvalidArguments
				}
				return Result{Exit: true}, nil
			},
		},
	}
	end := Definition{
		Name:        "end",
		Description: "Return exactly one contextual mode toward root",
		Usage:       "end",
		Handler: func(session *Session, invocation Invocation, _ io.Writer) (Result, error) {
			if len(invocation.Arguments) != 0 {
				return Result{}, errInvalidArguments
			}
			return Result{}, session.leaveMode()
		},
	}

	var err error
	registry, err = newRegistry(definitions, end)
	return registry, err
}

func onOffSuggestions(_ *Session, prefix string) []Suggestion {
	return valueSuggestions([]string{"on", "off"}, prefix, "Set presentation visibility")
}

func foregroundColorSuggestions(_ *Session, prefix string) []Suggestion {
	return valueSuggestions(preferences.ColorNames(false), prefix, "Use bounded terminal color")
}

func backgroundColorSuggestions(_ *Session, prefix string) []Suggestion {
	return valueSuggestions(preferences.ColorNames(true), prefix, "Use bounded rendered background color")
}

func valueSuggestions(values []string, prefix, description string) []Suggestion {
	var suggestions []Suggestion
	for _, value := range values {
		if strings.HasPrefix(value, prefix) {
			suggestions = append(suggestions, Suggestion{Text: value, Description: description})
		}
	}
	return suggestions
}

func surfaceOptions(includeActual, defaultRepositoryWide bool) []Option {
	expectedDescription := "Strongly expected tracked paths; . selects repository-wide authorization"
	possibleDescription := "Additionally allowed tracked paths; . selects repository-wide authorization"
	if defaultRepositoryWide {
		expectedDescription = "Expected tracked paths; providing paths selects explicit scope"
		possibleDescription = "Additional allowed tracked paths; providing paths selects explicit scope"
	}
	options := []Option{
		{Name: "--expected", Description: expectedDescription},
		{Name: "--possible", Description: possibleDescription},
		{Name: "--protected", Description: "Forbidden tracked paths or subtrees"},
	}
	if includeActual {
		options = append(options, Option{Name: "--actual", Description: "Complete actual path set to classify"})
	}
	return options
}

// Commands returns a defensive metadata tree in registration order.
func (registry Registry) Commands() []Metadata {
	return metadataForDefinitions(registry.definitions, "")
}

// ContextCommands returns the commands valid in the session's active mode.
func (registry Registry) ContextCommands(session *Session) []Metadata {
	return metadataForDefinitions(registry.contextDefinitions(session), "")
}

// Dispatch parses and resolves either canonical plain syntax or the small
// leading-slash compatibility alias. Mode entry and direct child invocation
// resolve through the same immutable command definitions.
func (registry Registry) Dispatch(session *Session, line string, output io.Writer) (Result, error) {
	return registry.DispatchContext(context.Background(), session, line, output)
}

// DispatchContext dispatches one command with execution-scoped cancellation.
// Non-provider commands retain their existing deterministic behavior.
func (registry Registry) DispatchContext(
	ctx context.Context,
	session *Session,
	line string,
	output io.Writer,
) (Result, error) {
	if ctx == nil {
		return Result{}, fmt.Errorf("command execution context is required")
	}
	if session == nil {
		return Result{}, fmt.Errorf("active command session is required")
	}
	if output == nil {
		return Result{}, fmt.Errorf("command output is required")
	}
	trimmed := strings.TrimSpace(line)
	if strings.HasSuffix(trimmed, "?") && trimmed != "?" {
		query := strings.TrimSuffix(trimmed, "?")
		return Result{}, registry.writeContextualHelp(session, query, output)
	}
	tokens, err := parseCommandLine(line)
	if err != nil {
		return Result{}, err
	}
	if len(tokens) == 0 {
		return Result{}, nil
	}
	if strings.HasPrefix(tokens[0], "/") {
		if strings.Count(tokens[0], "/") != 1 {
			return Result{}, fmt.Errorf("invalid compatibility command %q", tokens[0])
		}
		tokens[0] = strings.TrimPrefix(tokens[0], "/")
	}

	definitions := registry.contextDefinitions(session)
	definition, found := findDefinition(definitions, tokens[0])
	if !found {
		return Result{}, registry.unknownCommandError(tokens[0], session.CurrentMode())
	}
	consumed := 1
	commandPath := registry.commandPath(session, definition.Name)
	pendingModes := make([]ModeContext, 0, 2)
	parentMode := session.CurrentMode().Identity

	for definition.Mode != "" {
		registered := registry.modes[definition.Mode]
		if registered.context.Parent != parentMode {
			return Result{}, fmt.Errorf("command mode %q is not available from %q", definition.Mode, parentMode)
		}
		pendingModes = append(pendingModes, registered.context)
		parentMode = definition.Mode
		if consumed >= len(tokens) {
			for _, mode := range pendingModes {
				if err := session.enterMode(mode); err != nil {
					return Result{}, err
				}
			}
			return Result{}, nil
		}
		next, exists := findDefinition(definition.Children, tokens[consumed])
		if !exists {
			return Result{}, fmt.Errorf(
				"unknown command %q in %s mode; use ? for contextual help",
				tokens[consumed],
				definition.Mode,
			)
		}
		definition = next
		commandPath += " " + definition.Name
		consumed++
	}
	if definition.Handler == nil {
		return Result{}, fmt.Errorf("command %q has no handler", commandPath)
	}
	result, err := definition.Handler(session, Invocation{
		CommandPath: commandPath,
		Arguments:   append([]string(nil), tokens[consumed:]...),
		Context:     ctx,
	}, output)
	if errors.Is(err, errInvalidArguments) {
		return Result{}, fmt.Errorf("usage: %s", definition.Usage)
	}
	return result, err
}

// Complete returns deterministic candidates for the current mode and input.
func (registry Registry) Complete(session *Session, input string) []Suggestion {
	return registry.ContextualHelp(session, input)
}

// ContextualHelp resolves semantic `?` help without executing a command or
// mutating session mode/domain state.
func (registry Registry) ContextualHelp(session *Session, input string) []Suggestion {
	if session == nil {
		return nil
	}
	trimmedLeft := strings.TrimLeftFunc(input, unicode.IsSpace)
	if strings.HasPrefix(trimmedLeft, "/") {
		trimmedLeft = strings.TrimPrefix(trimmedLeft, "/")
	}
	runes := []rune(trimmedLeft)
	trailingSpace := len(runes) > 0 && unicode.IsSpace(runes[len(runes)-1])
	fields := strings.Fields(trimmedLeft)
	definitions := registry.contextDefinitions(session)
	if len(fields) == 0 {
		return suggestionsForDefinitions(definitions, "")
	}

	consumed := 0
	for consumed < len(fields) {
		token := fields[consumed]
		last := consumed == len(fields)-1
		if last && !trailingSpace {
			if definition, exact := findDefinition(definitions, token); exact && definition.Mode == "" {
				return []Suggestion{{Text: definition.Name, Description: definition.Description}}
			}
			return suggestionsForDefinitions(definitions, token)
		}

		definition, exact := findDefinition(definitions, token)
		if !exact {
			return nil
		}
		consumed++
		if definition.Mode != "" {
			definitions = definition.Children
			if consumed == len(fields) {
				return suggestionsForDefinitions(definitions, "")
			}
			continue
		}

		if consumed == len(fields) {
			if trailingSpace {
				return append(
					suggestionsForArguments(definition, session, ""),
					suggestionsForOptions(definition.Options, "")...,
				)
			}
			return []Suggestion{{Text: definition.Name, Description: definition.Description}}
		}
		optionPrefix := fields[len(fields)-1]
		if !trailingSpace && strings.HasPrefix(optionPrefix, "--") {
			return suggestionsForOptions(definition.Options, optionPrefix)
		}
		if !trailingSpace {
			return suggestionsForArguments(definition, session, optionPrefix)
		}
		return nil
	}
	return nil
}

func (registry Registry) writeContextualHelp(session *Session, input string, output io.Writer) error {
	suggestions := registry.ContextualHelp(session, input)
	if len(suggestions) == 0 {
		_, err := fmt.Fprintln(output, "No contextual commands or options match the current input.")
		return err
	}
	return writeSuggestions(output, suggestions)
}

func (registry Registry) writeHelp(session *Session, arguments []string, output io.Writer) error {
	if len(arguments) == 0 {
		if _, err := fmt.Fprintf(output, "Commands in %s mode:\n", session.CurrentMode().Identity); err != nil {
			return err
		}
		return writeSuggestions(output, suggestionsForDefinitions(registry.contextDefinitions(session), ""))
	}

	definitions := registry.contextDefinitions(session)
	path := ""
	var selected Definition
	for index, argument := range arguments {
		name := strings.TrimPrefix(argument, "/")
		definition, found := findDefinition(definitions, name)
		if !found {
			return fmt.Errorf("unknown help topic %q in %s mode", strings.Join(arguments[:index+1], " "), session.CurrentMode().Identity)
		}
		selected = definition
		path = strings.TrimSpace(path + " " + definition.Name)
		definitions = definition.Children
	}
	if _, err := fmt.Fprintf(output, "%s — %s\nUsage: %s\n", path, selected.Description, selected.Usage); err != nil {
		return err
	}
	if len(selected.Children) > 0 {
		if err := writeSuggestions(output, suggestionsForDefinitions(selected.Children, "")); err != nil {
			return err
		}
	}
	return writeSuggestions(output, suggestionsForOptions(selected.Options, ""))
}

func writeSuggestions(output io.Writer, suggestions []Suggestion) error {
	width := 0
	for _, suggestion := range suggestions {
		if len(suggestion.Text) > width {
			width = len(suggestion.Text)
		}
	}
	for _, suggestion := range suggestions {
		if _, err := fmt.Fprintf(output, "  %-*s  %s\n", width, suggestion.Text, suggestion.Description); err != nil {
			return err
		}
	}
	return nil
}

func (registry Registry) contextDefinitions(session *Session) []Definition {
	if session == nil || session.CurrentMode().Identity == ModeRoot {
		return append([]Definition(nil), registry.definitions...)
	}
	registered, exists := registry.modes[session.CurrentMode().Identity]
	if !exists {
		return nil
	}
	definitions := append([]Definition(nil), registered.definition.Children...)
	for _, definition := range registry.definitions {
		if definition.Global {
			definitions = append(definitions, definition)
		}
	}
	if registry.end.Name != "" {
		definitions = append(definitions, registry.end)
	}
	return definitions
}

func (registry Registry) commandPath(session *Session, localName string) string {
	if session == nil || session.CurrentMode().Identity == ModeRoot {
		return localName
	}
	for _, definition := range registry.definitions {
		if definition.Global && definition.Name == localName {
			return localName
		}
	}
	if localName == registry.end.Name {
		return localName
	}
	return session.CurrentMode().CommandPath + " " + localName
}

func (registry Registry) unknownCommandError(name string, mode ModeContext) error {
	return fmt.Errorf("unknown command %q in %s mode; use ? for contextual help", name, mode.Identity)
}

func (registry *Registry) indexModes(definitions []Definition, parent ModeIdentity, parentPath string) error {
	for _, definition := range definitions {
		path := strings.TrimSpace(parentPath + " " + definition.Name)
		nextParent := parent
		if definition.Mode != "" {
			if _, duplicate := registry.modes[definition.Mode]; duplicate {
				return fmt.Errorf("duplicate command mode %q", definition.Mode)
			}
			context := ModeContext{
				Identity:           definition.Mode,
				Parent:             parent,
				CommandPath:        path,
				PromptContribution: definition.Name,
			}
			registry.modes[definition.Mode] = registeredMode{context: context, definition: definition}
			nextParent = definition.Mode
		}
		if err := registry.indexModes(definition.Children, nextParent, path); err != nil {
			return err
		}
	}
	return nil
}

func suggestionsForDefinitions(definitions []Definition, prefix string) []Suggestion {
	var suggestions []Suggestion
	for _, definition := range definitions {
		if strings.HasPrefix(definition.Name, prefix) {
			suggestions = append(suggestions, Suggestion{Text: definition.Name, Description: definition.Description})
		}
	}
	return suggestions
}

func suggestionsForOptions(options []Option, prefix string) []Suggestion {
	var suggestions []Suggestion
	for _, option := range options {
		if strings.HasPrefix(option.Name, prefix) {
			suggestions = append(suggestions, Suggestion{Text: option.Name, Description: option.Description})
		}
	}
	return suggestions
}

func suggestionsForArguments(definition Definition, session *Session, prefix string) []Suggestion {
	if definition.ArgumentSuggestions == nil {
		return nil
	}
	return definition.ArgumentSuggestions(session, prefix)
}

func providerIdentifierSuggestions(session *Session, prefix string) []Suggestion {
	var suggestions []Suggestion
	for _, descriptor := range session.ProviderDescriptors() {
		identifier := string(descriptor.Identifier())
		if strings.HasPrefix(identifier, prefix) {
			suggestions = append(suggestions, Suggestion{
				Text:        identifier,
				Description: descriptor.Vendor() + " — " + descriptor.DisplayName(),
			})
		}
	}
	return suggestions
}

func parseCommandLine(line string) ([]string, error) {
	var tokens []string
	var token strings.Builder
	var quote rune
	escaped := false
	tokenStarted := false
	flush := func() {
		if tokenStarted {
			tokens = append(tokens, token.String())
			token.Reset()
			tokenStarted = false
		}
	}

	for _, character := range line {
		if escaped {
			token.WriteRune(character)
			tokenStarted = true
			escaped = false
			continue
		}
		if quote != '\'' && character == '\\' {
			escaped = true
			tokenStarted = true
			continue
		}
		if quote != 0 {
			if character == quote {
				quote = 0
				tokenStarted = true
			} else {
				token.WriteRune(character)
				tokenStarted = true
			}
			continue
		}
		if character == '\'' || character == '"' {
			quote = character
			tokenStarted = true
			continue
		}
		if unicode.IsSpace(character) {
			flush()
			continue
		}
		token.WriteRune(character)
		tokenStarted = true
	}
	if escaped {
		return nil, fmt.Errorf("malformed command: trailing escape")
	}
	if quote != 0 {
		return nil, fmt.Errorf("malformed command: unclosed quote")
	}
	flush()
	return tokens, nil
}

func validateDefinitions(definitions []Definition, parentPath string, root bool) error {
	seen := make(map[string]struct{}, len(definitions))
	for _, definition := range definitions {
		name := strings.TrimSpace(definition.Name)
		if name == "" || name != definition.Name || strings.ContainsAny(name, "/ \t\r\n") {
			return fmt.Errorf("invalid command name %q", definition.Name)
		}
		if _, duplicate := seen[name]; duplicate {
			return fmt.Errorf("duplicate command %q under %q", name, parentPath)
		}
		seen[name] = struct{}{}
		path := strings.TrimSpace(parentPath + " " + name)
		if strings.TrimSpace(definition.Description) == "" {
			return fmt.Errorf("command %q requires a description", path)
		}
		if strings.TrimSpace(definition.Usage) == "" {
			return fmt.Errorf("command %q requires usage metadata", path)
		}
		if definition.Global && !root {
			return fmt.Errorf("global command %q must be registered at root", path)
		}
		if definition.Handler == nil && definition.Mode == "" {
			return fmt.Errorf("command %q requires a handler or contextual mode", path)
		}
		if definition.Handler != nil && definition.Mode != "" {
			return fmt.Errorf("command %q cannot execute and enter a mode", path)
		}
		if definition.Handler != nil && len(definition.Children) > 0 {
			return fmt.Errorf("executable command %q cannot own child commands", path)
		}
		optionNames := make(map[string]struct{}, len(definition.Options))
		for _, option := range definition.Options {
			if !strings.HasPrefix(option.Name, "--") || strings.TrimSpace(option.Description) == "" {
				return fmt.Errorf("command %q has invalid option metadata %q", path, option.Name)
			}
			if _, duplicate := optionNames[option.Name]; duplicate {
				return fmt.Errorf("command %q has duplicate option %q", path, option.Name)
			}
			optionNames[option.Name] = struct{}{}
		}
		if err := validateDefinitions(definition.Children, path, false); err != nil {
			return err
		}
	}
	return nil
}

func cloneDefinition(definition Definition) Definition {
	cloned := definition
	cloned.Children = cloneDefinitions(definition.Children)
	cloned.Options = append([]Option(nil), definition.Options...)
	return cloned
}

func cloneDefinitions(definitions []Definition) []Definition {
	cloned := make([]Definition, len(definitions))
	for index, definition := range definitions {
		cloned[index] = cloneDefinition(definition)
	}
	return cloned
}

func metadataForDefinitions(definitions []Definition, parentPath string) []Metadata {
	metadata := make([]Metadata, len(definitions))
	for index, definition := range definitions {
		path := definition.Name
		if parentPath != "" {
			path = parentPath + " " + definition.Name
		}
		metadata[index] = Metadata{
			Name:        definition.Name,
			Path:        path,
			Description: definition.Description,
			Usage:       definition.Usage,
			Children:    metadataForDefinitions(definition.Children, path),
			Options:     append([]Option(nil), definition.Options...),
			Mode:        definition.Mode,
		}
	}
	return metadata
}

func findDefinition(definitions []Definition, name string) (Definition, bool) {
	for _, definition := range definitions {
		if definition.Name == name {
			return definition, true
		}
	}
	return Definition{}, false
}
