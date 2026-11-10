package cli

import (
	"errors"
	"fmt"
	"net/url"
	"time"

	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/collector"
	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/snapshot"
	"github.com/spf13/cobra"
)

// snapshotOptions holds the flags of the snapshot command.
type snapshotOptions struct {
	kubeconfig  string // path to the kubeconfig file; empty means the default location
	kubeContext string // kubeconfig context; empty means the current context
	output      string // path of the snapshot file to write
	prometheus  string // base URL of the Prometheus API; empty means no usage profiles
	usageWindow string // length of the usage history, e.g. "7d"
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
	cmd.Flags().StringVar(&opts.prometheus, "prometheus", "",
		"base URL of the Prometheus HTTP API to read the usage of the workloads from, e.g. http://localhost:9090")
	cmd.Flags().StringVar(&opts.usageWindow, "usage-window", "7d",
		"length of the usage history read from Prometheus, e.g. 7d or 24h (requires --prometheus)")
	return cmd
}

// runSnapshot creates the collector, reads the cluster, writes the snapshot
// to the output file and reports the result. Errors are returned unchanged,
// because the called functions already describe what failed.
func runSnapshot(cmd *cobra.Command, deps Deps, opts *snapshotOptions) error {
	window, err := validateUsageFlags(cmd, opts)
	if err != nil {
		return err
	}

	c, err := deps.NewCollector(collector.Options{
		Kubeconfig:    opts.kubeconfig,
		KubeContext:   opts.kubeContext,
		PrometheusURL: opts.prometheus,
		UsageWindow:   window,
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

	fmt.Fprintf(cmd.OutOrStdout(), "wrote %s: %d nodes, %d pods, %d PDBs",
		opts.output, len(snap.Nodes), len(snap.Pods), len(snap.PDBs))
	if opts.prometheus != "" {
		fmt.Fprintf(cmd.OutOrStdout(), ", %d usage profiles", len(snap.UsageProfiles))
	}
	fmt.Fprintln(cmd.OutOrStdout())
	return nil
}

// validateUsageFlags checks --prometheus and --usage-window and returns the
// parsed window (FR20). The URL must be absolute with the scheme http or
// https. --usage-window without --prometheus is an error, because it would
// silently have no effect.
func validateUsageFlags(cmd *cobra.Command, opts *snapshotOptions) (time.Duration, error) {
	if opts.prometheus == "" {
		if cmd.Flags().Changed("usage-window") {
			return 0, errors.New("--usage-window requires --prometheus")
		}
		return 0, nil
	}
	u, err := url.Parse(opts.prometheus)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return 0, fmt.Errorf("invalid --prometheus %q: want an http or https URL such as http://localhost:9090", opts.prometheus)
	}
	window, err := collector.ParseWindow(opts.usageWindow)
	if err != nil {
		return 0, fmt.Errorf("invalid --usage-window: %w", err)
	}
	return window, nil
}
