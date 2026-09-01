package command

import (
	"errors"
	"fmt"
	"io"
	"strings"
	"unicode"
)

var errInvalidArguments = errors.New("invalid command arguments")

// Result reports presentation control requested by a command handler.
type Result struct {
	Exit bool
}

// Invocation is registry-derived command identity plus parsed arguments.
type Invocation struct {
	SlashPath string
	Arguments []string
}

// Handler delegates one parsed command to the retained application session.
type Handler func(*Session, Invocation, io.Writer) (Result, error)

// Definition is explicit compile-time command registration input.
type Definition struct {
	Name        string
	Description string
	Usage       string
	Handler     Handler
	Subcommands []Definition
}

// Metadata is immutable-by-copy command information used by help and terminal
// adapters.
type Metadata struct {
	Name        string
	SlashPath   string
	Description string
	Usage       string
	Subcommands []Metadata
}

// Suggestion is one registry-owned completion/discovery candidate.
type Suggestion struct {
	Text        string
	Description string
}

// Registry owns the deterministic command tree. It is constructed explicitly
// and is never global or mutated after construction.
type Registry struct {
	definitions []Definition
}

// NewRegistry validates and defensively copies explicit command definitions.
func NewRegistry(definitions []Definition) (Registry, error) {
	if len(definitions) == 0 {
		return Registry{}, fmt.Errorf("at least one slash command is required")
	}
	cloned := cloneDefinitions(definitions)
	if err := validateDefinitions(cloned, ""); err != nil {
		return Registry{}, err
	}
	return Registry{definitions: cloned}, nil
}

// DefaultRegistry constructs the current M0.1-M0.3 shell command surface.
func DefaultRegistry() (Registry, error) {
	var registry Registry
	definitions := []Definition{
		{
			Name:        "status",
			Description: "Show current project and runtime status",
			Usage:       "/status",
			Handler:     handleStatus,
		},
		{
			Name:        "change",
			Description: "Govern software Changes",
			Usage:       "/change <subcommand>",
			Subcommands: []Definition{
				{
					Name:        "new",
					Description: "Create a Change and optionally apply lifecycle transitions",
					Usage:       "/change new <change-id> <intent> [<state> ...]",
					Handler:     handleChangeNew,
				},
			},
		},
		{
			Name:        "analysis",
			Description: "Analyze source and Change impact",
			Usage:       "/analysis <subcommand>",
			Subcommands: []Definition{
				{
					Name:        "impact",
					Description: "Establish and validate a bounded file-level Change Surface",
					Usage:       "/analysis impact <change-id> <intent> --expected <path>... [--possible <path>...] [--protected <path>...] --actual <path>...",
					Handler:     handleAnalysisImpact,
				},
			},
		},
		{
			Name:        "help",
			Description: "Show slash-command help",
			Usage:       "/help [<command> [<subcommand>]]",
			Handler: func(_ *Session, invocation Invocation, output io.Writer) (Result, error) {
				return Result{}, registry.writeHelp(invocation.Arguments, output)
			},
		},
		{
			Name:        "exit",
			Description: "Exit Praetor",
			Usage:       "/exit",
			Handler: func(_ *Session, invocation Invocation, _ io.Writer) (Result, error) {
				if len(invocation.Arguments) != 0 {
					return Result{}, errInvalidArguments
				}
				return Result{Exit: true}, nil
			},
		},
	}

	var err error
	registry, err = NewRegistry(definitions)
	return registry, err
}

// Commands returns a defensive metadata tree in registration order.
func (registry Registry) Commands() []Metadata {
	return metadataForDefinitions(registry.definitions, "")
}

// Dispatch parses and executes one slash-command line.
func (registry Registry) Dispatch(session *Session, line string, output io.Writer) (Result, error) {
	if session == nil {
		return Result{}, fmt.Errorf("active command session is required")
	}
	if output == nil {
		return Result{}, fmt.Errorf("command output is required")
	}
	tokens, err := parseCommandLine(line)
	if err != nil {
		return Result{}, err
	}
	if len(tokens) == 0 {
		return Result{}, nil
	}
	if !strings.HasPrefix(tokens[0], "/") || strings.Count(tokens[0], "/") != 1 {
		return Result{}, fmt.Errorf("commands must use slash syntax; try /help")
	}

	name := strings.TrimPrefix(tokens[0], "/")
	definition, found := findDefinition(registry.definitions, name)
	if !found {
		return Result{}, fmt.Errorf("unknown command %q; try /help", tokens[0])
	}
	consumed := 1
	path := "/" + definition.Name
	for len(definition.Subcommands) > 0 {
		if len(tokens) <= consumed {
			return Result{}, fmt.Errorf("%s requires a subcommand; usage: %s", path, definition.Usage)
		}
		next, exists := findDefinition(definition.Subcommands, tokens[consumed])
		if !exists {
			return Result{}, fmt.Errorf("unknown subcommand %q for %s; usage: %s", tokens[consumed], path, definition.Usage)
		}
		definition = next
		path += " " + definition.Name
		consumed++
	}
	if definition.Handler == nil {
		return Result{}, fmt.Errorf("command %s has no handler", path)
	}
	result, err := definition.Handler(session, Invocation{
		SlashPath: path,
		Arguments: append([]string(nil), tokens[consumed:]...),
	}, output)
	if errors.Is(err, errInvalidArguments) {
		return Result{}, fmt.Errorf("usage: %s", definition.Usage)
	}
	return result, err
}

// Complete returns descriptions for top-level or subcommand candidates at the
// current input prefix. Argument completion is intentionally outside M0.3.
func (registry Registry) Complete(input string) []Suggestion {
	trimmed := strings.TrimLeftFunc(input, unicode.IsSpace)
	if !strings.HasPrefix(trimmed, "/") {
		return nil
	}
	trimmedRunes := []rune(trimmed)
	trailingSpace := len(trimmedRunes) > 0 && unicode.IsSpace(trimmedRunes[len(trimmedRunes)-1])
	fields := strings.Fields(trimmed)
	if len(fields) == 0 {
		return nil
	}

	if len(fields) == 1 && !trailingSpace {
		prefix := fields[0]
		var suggestions []Suggestion
		for _, definition := range registry.definitions {
			candidate := "/" + definition.Name
			if strings.HasPrefix(candidate, prefix) {
				suggestions = append(suggestions, Suggestion{Text: candidate, Description: definition.Description})
			}
		}
		return suggestions
	}

	topLevelName := strings.TrimPrefix(fields[0], "/")
	topLevel, found := findDefinition(registry.definitions, topLevelName)
	if !found || len(topLevel.Subcommands) == 0 || len(fields) > 2 || (len(fields) == 2 && trailingSpace) {
		return nil
	}
	prefix := ""
	if len(fields) == 2 {
		prefix = fields[1]
	}
	var suggestions []Suggestion
	for _, subcommand := range topLevel.Subcommands {
		if strings.HasPrefix(subcommand.Name, prefix) {
			suggestions = append(suggestions, Suggestion{Text: subcommand.Name, Description: subcommand.Description})
		}
	}
	return suggestions
}

func (registry Registry) writeHelp(arguments []string, output io.Writer) error {
	if len(arguments) == 0 {
		if _, err := fmt.Fprintln(output, "Praetor slash commands:"); err != nil {
			return err
		}
		for _, metadata := range registry.Commands() {
			if _, err := fmt.Fprintf(output, "  %-12s %s\n", metadata.SlashPath, metadata.Description); err != nil {
				return err
			}
		}
		return nil
	}

	definitions := registry.definitions
	path := ""
	var selected Definition
	for index, argument := range arguments {
		name := strings.TrimPrefix(argument, "/")
		definition, found := findDefinition(definitions, name)
		if !found {
			return fmt.Errorf("unknown help topic %q", strings.Join(arguments[:index+1], " "))
		}
		selected = definition
		if path == "" {
			path = "/" + definition.Name
		} else {
			path += " " + definition.Name
		}
		definitions = definition.Subcommands
	}
	if _, err := fmt.Fprintf(output, "%s — %s\nUsage: %s\n", path, selected.Description, selected.Usage); err != nil {
		return err
	}
	for _, subcommand := range selected.Subcommands {
		if _, err := fmt.Fprintf(output, "  %-12s %s\n", subcommand.Name, subcommand.Description); err != nil {
			return err
		}
	}
	return nil
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

func validateDefinitions(definitions []Definition, parentPath string) error {
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
		if len(definition.Subcommands) == 0 && definition.Handler == nil {
			return fmt.Errorf("command %q requires a handler", path)
		}
		if len(definition.Subcommands) > 0 && definition.Handler != nil {
			return fmt.Errorf("command %q cannot have both a handler and subcommands", path)
		}
		if err := validateDefinitions(definition.Subcommands, path); err != nil {
			return err
		}
	}
	return nil
}

func cloneDefinitions(definitions []Definition) []Definition {
	cloned := make([]Definition, len(definitions))
	for index, definition := range definitions {
		cloned[index] = definition
		cloned[index].Subcommands = cloneDefinitions(definition.Subcommands)
	}
	return cloned
}

func metadataForDefinitions(definitions []Definition, parentPath string) []Metadata {
	metadata := make([]Metadata, len(definitions))
	for index, definition := range definitions {
		path := "/" + definition.Name
		if parentPath != "" {
			path = parentPath + " " + definition.Name
		}
		metadata[index] = Metadata{
			Name:        definition.Name,
			SlashPath:   path,
			Description: definition.Description,
			Usage:       definition.Usage,
			Subcommands: metadataForDefinitions(definition.Subcommands, path),
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
