package cli

import (
	"context"

	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/snapshot"
)

// collector corresponds to ICollector
type Collector interface {
	Collect(ctx context.Context) (*snapshot.Snapshot, error)
}

// structure for dependencies
type Deps struct {
	NewCollector func(kubeconfig, kubeContext string) (Collector, error)
}
