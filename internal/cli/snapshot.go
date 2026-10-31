package cli

import (
	"fmt"

	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/collector"
	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/snapshot"
	"github.com/spf13/cobra"
)

// snapshotOptions holds the flags of the snapshot command.
type snapshotOptions struct {
	kubeconfig  string // path to the kubeconfig file; empty means the default location
	kubeContext string // kubeconfig context; empty means the current context
	output      string // path of the snapshot file to write
}

// newSnapshotCommand creates the "snapshot" command, which reads the cluster
// and saves its state to a file (UC1).
func newSnapshotCommand(deps Deps) *cobra.Command {
	opts := &snapshotOptions{}
	cmd := &cobra.Command{
		Use:   "snapshot",
		Short: "Reads the cluster and saves a snapshot",
		Args:  cobra.NoArgs, //No additional args.
		//Run error func.
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSnapshot(cmd, deps, opts)
		},
	}
	//Flags for cli tool.
	cmd.Flags().StringVar(&opts.kubeconfig, "kubeconfig", "", "path to the kubeconfig file")
	cmd.Flags().StringVar(&opts.kubeContext, "context", "", "kubeconfig context to use")
	cmd.Flags().StringVarP(&opts.output, "output", "o", "snapshot.json", "path of the snapshot file")
	return cmd
}

// runSnapshot creates the collector, reads the cluster, writes the snapshot
// to the output file and reports the result. Errors are returned unchanged,
// because the called functions already describe what failed.
func runSnapshot(cmd *cobra.Command, deps Deps, opts *snapshotOptions) error {
	c, err := deps.NewCollector(collector.Options{
		Kubeconfig:  opts.kubeconfig,
		KubeContext: opts.kubeContext,
	})
	if err != nil {
		return err
	}

	snap, err := c.Collect(cmd.Context())
	if err != nil {
		return err
	}

	if err := snapshot.Write(opts.output, snap); err != nil {
		return err
	}

	fmt.Fprintf(cmd.OutOrStdout(), "collected %d nodes, %d pods, %d PDBs\n",
		len(snap.Nodes), len(snap.Pods), len(snap.PDBs))
	return nil
}
