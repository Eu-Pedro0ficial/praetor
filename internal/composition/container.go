package composition

import (
	"fmt"

	"github.com/V1n1v131r4/praetor/internal/project"
	"github.com/V1n1v131r4/praetor/internal/repository"
)

// RepositoryDiscoveryFunc is the concrete dependency used by the C01 composition boundary.
type RepositoryDiscoveryFunc func(string) (repository.Context, error)

// ProjectRegistrationFunc is the concrete dependency used to ensure a local registration exists.
type ProjectRegistrationFunc func(string) (project.Registration, error)

// Container assembles only the dependencies required by the current C01 + C02 scope.
type Container struct {
	RepositoryDiscovery RepositoryDiscoveryFunc
	ProjectRegistration ProjectRegistrationFunc
}

// New creates the explicit composition root for the current runtime boundary.
func New() Container {
	return Container{
		RepositoryDiscovery: repository.Discover,
		ProjectRegistration: project.EnsureRegistration,
	}
}

// DiscoverRepository resolves the current repository context through the explicitly assembled dependency.
func (c Container) DiscoverRepository(path string) (repository.Context, error) {
	if c.RepositoryDiscovery == nil {
		return repository.Context{}, fmt.Errorf("repository discovery dependency is not configured")
	}
	return c.RepositoryDiscovery(path)
}

// EnsureProjectRegistration resolves the repository context and ensures a local project registration exists.
func (c Container) EnsureProjectRegistration(path string) (project.Registration, error) {
	if c.RepositoryDiscovery == nil {
		return project.Registration{}, fmt.Errorf("repository discovery dependency is not configured")
	}
	if c.ProjectRegistration == nil {
		return project.Registration{}, fmt.Errorf("project registration dependency is not configured")
	}

	ctx, err := c.RepositoryDiscovery(path)
	if err != nil {
		return project.Registration{}, err
	}
	return c.ProjectRegistration(ctx.Root)
}
