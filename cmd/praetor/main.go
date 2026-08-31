package main

import (
	"fmt"
	"os"

	"github.com/V1n1v131r4/praetor/internal/composition"
)

func run(args []string) error {
	if len(args) != 1 || args[0] != "status" {
		return fmt.Errorf("usage: praetor status")
	}

	container := composition.New()
	reg, err := container.EnsureProjectRegistration(".")
	if err != nil {
		return err
	}

	fmt.Printf("Project ID: %s\n", reg.ProjectId)
	fmt.Printf("Repository root: %s\n", reg.RepositoryRoot)
	fmt.Println("Git repository: true")
	return nil
}

func main() {
	if err := run(os.Args[1:]); err != nil {
		fmt.Fprintf(os.Stderr, "praetor: %v\n", err)
		os.Exit(1)
	}
}
