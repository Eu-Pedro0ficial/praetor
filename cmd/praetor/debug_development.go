//go:build praetor_debug

package main

import (
	"fmt"

	"github.com/Eu-Pedro0ficial/praetor/internal/command"
	"github.com/Eu-Pedro0ficial/praetor/internal/presentation/debugtranscript"
	"github.com/Eu-Pedro0ficial/praetor/internal/presentation/shell"
)

func parseStartupOptions(arguments []string) (startupOptions, error) {
	var options startupOptions
	for _, argument := range arguments {
		switch argument {
		case "--debug":
			if options.debug {
				return startupOptions{}, fmt.Errorf("--debug may be specified only once")
			}
			options.debug = true
		case "--help", "-h":
			options.help = true
		default:
			return startupOptions{}, fmt.Errorf(
				"unknown startup option %q; praetor starts an interactive shell and accepts plain hierarchical commands inside the session",
				argument,
			)
		}
	}
	return options, nil
}

func startupUsage() string {
	return "Usage: praetor [--debug]\n\n" +
		"Starts the retained-context interactive Praetor shell.\n" +
		"  --debug  write a bounded, sanitized development transcript under XDG STATE\n"
}

func configureDebug(options startupOptions, session *command.Session) (debugConfiguration, error) {
	if !options.debug {
		return debugConfiguration{}, nil
	}
	recorder, err := debugtranscript.New(debugtranscript.Config{
		ProjectID: string(session.Registration().ProjectId),
	})
	if err != nil {
		return debugConfiguration{}, fmt.Errorf("start development debug transcript: %w", err)
	}
	return debugConfiguration{
		option:   shell.WithInteractionRecorder(recorder),
		recorder: recorder,
		path:     recorder.Path(),
	}, nil
}
