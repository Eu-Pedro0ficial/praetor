package main

import (
	"fmt"
	"io"
	"os"

	"github.com/Eu-Pedro0ficial/praetor/internal/change"
	"github.com/Eu-Pedro0ficial/praetor/internal/composition"
	"github.com/Eu-Pedro0ficial/praetor/internal/project"
)

func run(args []string) error {
	return runWithOutput(args, os.Stdout)
}

func runWithOutput(args []string, output io.Writer) error {
	container := composition.New()
	if len(args) == 1 && args[0] == "status" {
		return runStatus(container, output)
	}
	if len(args) >= 4 && args[0] == "change" && args[1] == "new" {
		return runChangeNew(container, args[2], args[3], args[4:], output)
	}
	return fmt.Errorf("usage: praetor status | praetor change new <change-id> <intent> [<state> ...]")
}

func registerRuntime(container composition.Container, command string) (project.Registration, error) {
	registration, err := container.EnsureProjectRegistration(".")
	if err != nil {
		return project.Registration{}, err
	}

	if _, err := container.RecordInitialization(registration, map[string]any{
		"command": command,
		"phase":   "startup",
	}); err != nil {
		return project.Registration{}, err
	}
	if _, err := container.RecordProjectAttach(registration, map[string]any{
		"command":  command,
		"attached": true,
	}); err != nil {
		return project.Registration{}, err
	}
	if _, err := container.RecordConfiguration(registration, map[string]any{
		"command":   command,
		"runtime":   "local",
		"directory": "data",
	}); err != nil {
		return project.Registration{}, err
	}
	return registration, nil
}

func runStatus(container composition.Container, output io.Writer) error {
	registration, err := registerRuntime(container, "status")
	if err != nil {
		return err
	}
	fmt.Fprintf(output, "Project ID: %s\n", registration.ProjectId)
	fmt.Fprintf(output, "Repository root: %s\n", registration.RepositoryRoot)
	fmt.Fprintln(output, "Git repository: true")
	return nil
}

func runChangeNew(
	container composition.Container,
	changeIdValue string,
	intentValue string,
	stateValues []string,
	output io.Writer,
) error {
	changeId, err := change.NewChangeId(changeIdValue)
	if err != nil {
		return err
	}
	intent, err := change.NewChangeIntent(intentValue)
	if err != nil {
		return err
	}
	states := make([]change.ChangeState, 0, len(stateValues))
	for _, stateValue := range stateValues {
		state, parseError := change.ParseState(stateValue)
		if parseError != nil {
			return parseError
		}
		states = append(states, state)
	}

	registration, err := registerRuntime(container, "change new")
	if err != nil {
		return err
	}
	changeWorkflow, err := container.NewChangeWorkflow(registration)
	if err != nil {
		return err
	}
	createdChange, err := changeWorkflow.Create(
		changeId,
		intent,
		"praetor change new",
	)
	if err != nil {
		return err
	}
	currentChange := createdChange
	for _, state := range states {
		currentChange, err = changeWorkflow.Transition(
			changeId,
			state,
			fmt.Sprintf("praetor change new transition to %s", state),
		)
		if err != nil {
			return err
		}
	}

	fmt.Fprintf(output, "Change ID: %s\n", currentChange.ChangeId())
	fmt.Fprintf(output, "Project ID: %s\n", currentChange.ProjectId())
	fmt.Fprintf(output, "Intent: %s\n", currentChange.Intent())
	fmt.Fprintf(output, "State: %s\n", currentChange.State())
	return nil
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "praetor: %v\n", err)
		os.Exit(1)
	}
}
