package types

// Defines the types for server health check

import (
	"context"

	"buf.build/gen/go/cedana/cedana/protocolbuffers/go/daemon"
)

// A list of checks
type Checklist []Checks

// Represents a named list of checks
type Checks struct {
	Name string
	List []Check
}

// A health check function that returns a list of components
// A health check if never supposed to return an error. If it wants to,
// it should simply add the failure information to the components.
type Check = func(context.Context) []*daemon.HealthCheckComponent

func (c *Checks) String() string {
	return c.Name
}

// Run runs every check in the list, and returns the result
func (c *Checks) Run(ctx context.Context) *daemon.HealthCheckResult {
	components := make([]*daemon.HealthCheckComponent, 0, len(c.List))
	for _, check := range c.List {
		result := check(ctx)
		if result == nil {
			continue
		}
		components = append(components, result...)
	}
	return &daemon.HealthCheckResult{
		Name:       c.Name,
		Components: components,
	}
}

// Run runs all checks in the list, and returns the result
func (c Checklist) Run(ctx context.Context) []*daemon.HealthCheckResult {
	results := make([]*daemon.HealthCheckResult, 0, len(c))
	for _, checks := range c {
		results = append(results, checks.Run(ctx))
	}
	return results
}
