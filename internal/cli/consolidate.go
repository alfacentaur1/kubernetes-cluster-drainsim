package cli

import (
	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/analyses/consolidation"
	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/config"
	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/engine"
	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/engine/loader"
	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/engine/psa"
	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/engine/scheduler"
	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/estimator"
	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/reporter"
	"github.com/spf13/cobra"
)

// consolidateOptions holds the flags of the consolidate command.
type consolidateOptions struct {
	config   string   // --config FILE: configuration with the estimate section (optional)
	nodes    []string // --nodes a,b,c: candidate nodes
	selector string   // --selector pool=blue: label selector of the candidate nodes
	format   string   // --format text|json: format of the report
}

// newConsolidateCommand creates the "consolidate" command, which finds the
// nodes that can be removed while all pods still run and estimates what that
// saves (UC10–UC12, Q2).
func newConsolidateCommand(deps Deps) *cobra.Command {
	opts := &consolidateOptions{}
	cmd := &cobra.Command{
		Use:   "consolidate SNAPSHOT",
		Short: "Finds nodes that can be removed and estimates the money and CO₂ saved, using a snapshot",
		Args:  cobra.ExactArgs(1),
		RunE: func(cmd *cobra.Command, args []string) error {
			return runConsolidate(cmd, deps, args[0], opts)
		},
	}
	cmd.Flags().StringVar(&opts.config, "config", "",
		"configuration file with prices and emission factors (estimate); without it the savings are unknown")
	cmd.Flags().StringSliceVar(&opts.nodes, "nodes", nil,
		"candidate nodes (default: all nodes)")
	cmd.Flags().StringVarP(&opts.selector, "selector", "l", "",
		"label selector of the candidate nodes, e.g. pool=blue")
	cmd.Flags().StringVar(&opts.format, "format", "text",
		"report format: text or json")
	cmd.MarkFlagsMutuallyExclusive("nodes", "selector")
	return cmd
}

// runConsolidate follows Algorithm 5.3 and reports the result. The exit code
// is 0 for every run that completes, because the consolidation is an
// estimate, not a check; invalid input gives 2 (Section 5.7).
func runConsolidate(cmd *cobra.Command, deps Deps, path string, opts *consolidateOptions) error {
	if err := checkFormat(opts.format); err != nil {
		return err
	}

	// The cost model is optional (Section 5.5). Without a configuration, or
	// without an estimate section, it stays nil: the removable nodes are
	// still found, their savings are unknown.
	var (
		cost     consolidation.CostModel
		currency string
		hours    float64
	)
	if opts.config != "" {
		cfg, err := config.Load(opts.config)
		if err != nil {
			return err
		}
		if !cfg.Estimate.Empty() {
			cost = estimator.New(cfg.Estimate)
			currency, hours = cfg.Estimate.Currency, cfg.Estimate.Period()
		}
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
	// The levels set on the namespaces apply, as in a real drain (FR25).
	policy, err := psa.New(nil, "")
	if err != nil {
		return err
	}
	eng := engine.New(st, sched, policy)

	res, err := consolidation.Run(cmd.Context(), eng, nodes, cost)
	if err != nil {
		return err
	}
	return writeReport(cmd, opts.format, reporter.Consolidate(res, currency, hours, deps.now()))
}
