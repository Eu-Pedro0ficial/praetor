//go:build !praetor_debug

package main

import (
	"fmt"

	"github.com/Eu-Pedro0ficial/praetor/internal/command"
)

func parseStartupOptions(arguments []string) (startupOptions, error) {
	if len(arguments) == 0 {
		return startupOptions{}, nil
	}
	if len(arguments) == 1 && (arguments[0] == "--help" || arguments[0] == "-h") {
		return startupOptions{help: true}, nil
	}
	return startupOptions{}, fmt.Errorf(
		"unknown startup option %q; praetor starts an interactive shell and accepts plain hierarchical commands inside the session",
		arguments[0],
	)
}

func startupUsage() string {
	return "Usage: praetor\n\nStarts the retained-context interactive Praetor shell.\n"
}

func configureDebug(startupOptions, *command.Session) (debugConfiguration, error) {
	return debugConfiguration{}, nil
}
