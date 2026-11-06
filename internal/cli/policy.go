package cli

import (
	"fmt"

	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/analyses/policyimpact"
	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/config"
	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/engine"
	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/engine/loader"
	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/engine/psa"
	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/engine/scheduler"
	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/reporter"
	"github.com/spf13/cobra"
)

// policyOptions holds the flags of the policy command.
type policyOptions struct {
	config   string   // --config FILE: configuration with the target levels
	nodes    []string // --nodes a,b,c: nodes to drain, in this order
	selector string   // --selector pool=blue: label selector of the nodes
	format   string   // --format text|json: format of the report
}

// newPolicyCommand creates the "policy" command, which checks whether the
// target Pod Security levels from the configuration would break a drain
// (UC13, UC14, Q3).
func newPolicyCommand(deps Deps) *cobra.Command {
	opts := &policyOptions{}
	cmd := &cobra.Command{
		Use:   "policy SNAPSHOT",
		Short: "Checks whether stricter Pod Security levels would break a drain, using a snapshot",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runPolicy(cmd, deps, args[0], opts)
		},
	}
	cmd.Flags().StringVar(&opts.config, "config", "",
		"configuration file with the target Pod Security levels (policyTargets)")
	cmd.Flags().StringSliceVar(&opts.nodes, "nodes", nil,
		"nodes to drain, in this order (default: all nodes, by name)")
	cmd.Flags().StringVarP(&opts.selector, "selector", "l", "",
		"label selector of the nodes to drain, e.g. pool=blue")
	cmd.Flags().StringVar(&opts.format, "format", "text",
		"report format: text or json")
	cmd.MarkFlagsMutuallyExclusive("nodes", "selector")
	_ = cmd.MarkFlagRequired("config")
	return cmd
}

// runPolicy follows Algorithm 5.4: it reads the target levels, simulates the
// drain without and with them, and reports the difference. The check fails
// (exit code 1) when the target levels make a drain step fail, so a pipeline
// can stop a policy change that would break the next maintenance (UC15).
func runPolicy(cmd *cobra.Command, deps Deps, path string, opts *policyOptions) error {
	if err := checkFormat(opts.format); err != nil {
		return err
	}

	// Target levels from the configuration (FR29). Without any, the two
	// runs would be the same and the analysis would say nothing.
	cfg, err := config.Load(opts.config)
	if err != nil {
		return err
	}
	if cfg.PolicyTargets.Empty() {
		return fmt.Errorf("configuration %s sets no target level (policyTargets)", opts.config)
	}

	// Two PSA checkers: the levels set on the namespaces for the first run,
	// the target levels for the second (Section 5.6, Strategy pattern).
	current, err := psa.New(nil, "")
	if err != nil {
		return err
	}
	target, err := psa.New(cfg.PolicyTargets.Namespaces, cfg.PolicyTargets.Cluster)
	if err != nil {
		return fmt.Errorf("configuration %s: %w", opts.config, err)
	}

	st, err := loader.Load(path)
	if err != nil {
		return err
	}
	nodes, err := selectNodes(st, opts.nodes, opts.selector)
	if err != nil {
		return err
	}
	sched, err := scheduler.New(cmd.Context())
	if err != nil {
		return err
	}
	eng := engine.New(st, sched, current)

	res, err := policyimpact.Run(cmd.Context(), eng, nodes, current, target)
	if err != nil {
		return err
	}
	return writeReport(cmd, opts.format, reporter.Policy(res, deps.now()))
}
