package composition

import (
	"fmt"

	"github.com/V1n1v131r4/praetor/internal/repository"
)

// RepositoryDiscoveryFunc is the concrete dependency used by the C01 composition boundary.
type RepositoryDiscoveryFunc func(string) (repository.Context, error)

// Container assembles only the dependencies required by the current C01 scope.
type Container struct {
	RepositoryDiscovery RepositoryDiscoveryFunc
}

// New creates the explicit composition root for the current runtime boundary.
func New() Container {
	return Container{
		RepositoryDiscovery: repository.Discover,
	}
}

// DiscoverRepository resolves the current repository context through the explicitly assembled dependency.
func (c Container) DiscoverRepository(path string) (repository.Context, error) {
	if c.RepositoryDiscovery == nil {
		return repository.Context{}, fmt.Errorf("repository discovery dependency is not configured")
	}
	return c.RepositoryDiscovery(path)
}
