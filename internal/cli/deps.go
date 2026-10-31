// Each subcommand is a separate cobra.Command (Command pattern). Commands only
// orchestrate the components behind the interfaces defined in this file and
// contain no simulation logic themselves.
package cli

import (
	"context"

	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/collector"
	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/snapshot"
)

// Collector reads the state of a cluster and returns it as a snapshot.
// It corresponds to the interface ICollector.
type Collector interface {
	// Collect reads the cluster. The context allows the reading to be
	// cancelled, for example when the user presses Ctrl+C.
	Collect(ctx context.Context) (*snapshot.Snapshot, error)
}

// Deps holds the components that the CLI depends on. They are passed in from
// main, so that tests can replace them with fakes.
type Deps struct {
	// NewCollector creates a collector for the given options.
	NewCollector func(opts collector.Options) (Collector, error)
}
