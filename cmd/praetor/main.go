package main

import (
	"fmt"
	"os"

	"github.com/Eu-Pedro0ficial/praetor/internal/composition"
)

func run(args []string) error {
	if len(args) != 1 || args[0] != "status" {
		return fmt.Errorf("usage: praetor status")
	}

	container := composition.New()
	registration, err := container.EnsureProjectRegistration(".")
	if err != nil {
		return err
	}

	if _, err := container.RecordInitialization(registration, map[string]any{
		"command": "status",
		"phase":   "startup",
	}); err != nil {
		return err
	}
	if _, err := container.RecordProjectAttach(registration, map[string]any{
		"command":  "status",
		"attached": true,
	}); err != nil {
		return err
	}
	if _, err := container.RecordConfiguration(registration, map[string]any{
		"command":   "status",
		"runtime":   "local",
		"directory": "data",
	}); err != nil {
		return err
	}

	fmt.Printf("Project ID: %s\n", registration.ProjectId)
	fmt.Printf("Repository root: %s\n", registration.RepositoryRoot)
	fmt.Println("Git repository: true")
	return nil
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "praetor: %v\n", err)
		os.Exit(1)
	}
}
