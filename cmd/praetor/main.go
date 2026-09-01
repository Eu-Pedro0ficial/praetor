package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/Eu-Pedro0ficial/praetor/internal/command"
	"github.com/Eu-Pedro0ficial/praetor/internal/composition"
	"github.com/Eu-Pedro0ficial/praetor/internal/presentation/shell"
)

func run(arguments []string) error {
	if len(arguments) != 0 {
		return errors.New("praetor starts an interactive shell; use slash commands inside the session")
	}
	container := composition.New()
	session, err := container.NewInteractiveSession(".")
	if err != nil {
		return err
	}
	registry, err := command.DefaultRegistry()
	if err != nil {
		return err
	}
	interactiveShell, err := shell.New(registry, session, os.Stdout)
	if err != nil {
		return err
	}
	return interactiveShell.Run()
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "praetor: %v\n", err)
		os.Exit(1)
	}
}
