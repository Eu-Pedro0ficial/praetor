package main

import (
	"errors"
	"fmt"
	"os"

	"github.com/Eu-Pedro0ficial/praetor/internal/command"
	"github.com/Eu-Pedro0ficial/praetor/internal/composition"
	"github.com/Eu-Pedro0ficial/praetor/internal/presentation/shell"
	"github.com/Eu-Pedro0ficial/praetor/internal/repository"
)

type startupOptions struct {
	debug bool
	help  bool
}

type debugConfiguration struct {
	option   shell.Option
	recorder shell.InteractionRecorder
	path     string
}

func run(arguments []string) (runError error) {
	options, err := parseStartupOptions(arguments)
	if err != nil {
		return err
	}
	if options.help {
		_, err = fmt.Fprint(os.Stdout, startupUsage())
		return err
	}

	container := composition.New()
	session, err := container.NewInteractiveSession(".")
	if err != nil {
		return err
	}
	defer func() {
		runError = errors.Join(runError, session.Close())
	}()

	debugConfig, err := configureDebug(options, session)
	if err != nil {
		return err
	}
	if debugConfig.path != "" {
		if _, err := fmt.Fprintf(os.Stdout, "Development debug transcript: %s\n", debugConfig.path); err != nil {
			_ = debugConfig.recorder.Close(err)
			return err
		}
	}

	registry, err := command.DefaultRegistry()
	if err != nil {
		if debugConfig.recorder != nil {
			_ = debugConfig.recorder.Close(err)
		}
		return err
	}
	var shellOptions []shell.Option
	if debugConfig.option != nil {
		shellOptions = append(shellOptions, debugConfig.option)
	}
	interactiveShell, err := shell.New(registry, session, os.Stdout, shellOptions...)
	if err != nil {
		if debugConfig.recorder != nil {
			_ = debugConfig.recorder.Close(err)
		}
		return err
	}
	return interactiveShell.Run()
}

func formatStartupError(err error) string {
	if errors.Is(err, repository.ErrNotGitRepository) {
		return `no Git repository found from the current directory.

Praetor must be started inside the Git repository you want it to govern.

Example:
  cd /path/to/repository
  praetor`
	}
	return err.Error()
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		_, _ = fmt.Fprintf(os.Stderr, "praetor: %s\n", formatStartupError(err))
		os.Exit(1)
	}
}
