// Command drainsim simulates node drains on a snapshot of a Kubernetes cluster.
package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"

	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/cli"
	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/collector"
	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/reporter"
)

func main() {
	os.Exit(run())
}

// run executes the CLI and returns the process exit code. It is separate from
// main so that deferred calls run before os.Exit.
func run() int {
	// Cancel the context when the user presses Ctrl+C.
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
	defer stop()

	deps := cli.Deps{
		NewCollector: func(opts collector.Options) (cli.Collector, error) {
			return collector.New(opts)
		},
	}

	err := cli.NewRootCommand(deps).ExecuteContext(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "Error:", err)
	}
	return reporter.ExitCode(err)
}
