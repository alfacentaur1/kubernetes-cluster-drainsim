package cli

import (
	"fmt"

	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/analyses/drainorder"
	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/engine"
	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/engine/loader"
	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/engine/psa"
	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/engine/scheduler"
	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/reporter"
	"github.com/spf13/cobra"
)

// drainOptions holds the flags of the drain command. Cobra fills the fields
// from the command line before runDrain is called.
type drainOptions struct {
	nodes       []string // --nodes a,b,c: nodes to drain, in this order
	selector    string   // --selector pool=blue: label selector of the nodes
	format      string   // --format text|json: format of the report
	strategy    string   // --strategy greedy|backtracking: how to search for an order
	maxAttempts int      // --max-attempts N: limit of the backtracking search, 0 for none
}

// newDrainCommand creates the "drain" command, which checks whether nodes can
// be drained and, if not in the given order, searches for another order
// (UC3, UC5, Q1). Like every command it only connects the components; the
// simulation itself happens in the engine (Command pattern, Section 6.2).
func newDrainCommand(deps Deps) *cobra.Command {
	opts := &drainOptions{}
	cmd := &cobra.Command{
		// SNAPSHOT in Use is shown in --help as the expected argument.
		Use:   "drain SNAPSHOT",
		Short: "Checks whether nodes can be drained, using a snapshot",
		// Exactly one positional argument: the path of the snapshot file.
		Args: cobra.ExactArgs(1),
		// RunE is called by cobra after the flags are parsed. The returned
		// error decides the exit code.
		RunE: func(cmd *cobra.Command, args []string) error {
			return runDrain(cmd, deps, args[0], opts)
		},
	}

	// Each flag is bound to a field of opts: cobra writes the value there.
	cmd.Flags().StringSliceVar(&opts.nodes, "nodes", nil,
		"nodes to drain, in this order (default: all nodes, by name)")
	cmd.Flags().StringVarP(&opts.selector, "selector", "l", "",
		"label selector of the nodes to drain, e.g. pool=blue")
	cmd.Flags().StringVar(&opts.format, "format", "text",
		"report format: text or json")
	cmd.Flags().StringVar(&opts.strategy, "strategy", "greedy",
		"how to search for an order if the given one fails: greedy (fast) or backtracking (finds an order whenever one exists)")
	cmd.Flags().IntVar(&opts.maxAttempts, "max-attempts", 0,
		"limit of simulated drains for the backtracking strategy (0: no limit)")

	// --nodes and --selector choose the nodes in two different ways, so
	// cobra refuses a command line that uses both.
	cmd.MarkFlagsMutuallyExclusive("nodes", "selector")
	return cmd
}

// runDrain does the work of the drain command, in the order of the main
// scenario of UC3: load the snapshot, choose the nodes, simulate, report.
//
// Its error decides the exit code (Section 5.7, FR35):
//   - nil: the nodes can be drained, exit code 0;
//   - a CheckFailedError: no order was found, exit code 1;
//   - any other error: the tool could not run (bad flag, invalid snapshot,
//     unknown node), exit code 2.
func runDrain(cmd *cobra.Command, deps Deps, path string, opts *drainOptions) error {
	// Check the flags first, so that a typo is reported at once and not
	// after a long simulation.
	if opts.format != "text" && opts.format != "json" {
		return fmt.Errorf("unknown format %q: use text or json", opts.format)
	}
	strategy, err := newStrategy(cmd, opts)
	if err != nil {
		return err
	}

	// 1. Load the snapshot and build the cluster state (FR3, FR4).
	st, err := loader.Load(path)
	if err != nil {
		return err
	}

	// 2. Choose the nodes and their order (FR5).
	nodes, err := selectNodes(st, opts.nodes, opts.selector)
	if err != nil {
		return err
	}

	// 3. Build the engine: scheduler adapter, PSA checker with the levels
	//    set on the namespaces. The drain order analysis attaches its own
	//    event log, so none is passed here.
	sched, err := scheduler.New(cmd.Context())
	if err != nil {
		return err
	}
	policy, err := psa.New(nil, "")
	if err != nil {
		return err
	}
	eng := engine.New(st, sched, policy)

	// 4. Check the given order and, if it fails, search for another one
	//    with the chosen strategy (UC3, UC5).
	res, err := drainorder.Run(cmd.Context(), eng, nodes, strategy)
	if err != nil {
		return err
	}

	// 5. Build the report from the result; its events come from the
	//    analysis (Section 5.7, FR18, FR19).
	rep := reporter.Drain(res, deps.now())

	// 6. Write the report as text or JSON (FR33, FR34).
	out := cmd.OutOrStdout()
	if opts.format == "json" {
		err = reporter.JSON(out, rep)
	} else {
		err = reporter.Text(out, rep)
	}
	if err != nil {
		return fmt.Errorf("writing the report: %w", err)
	}

	// 7. No order found: exit code 1 for the CI pipeline.
	if !rep.Passed {
		return &reporter.CheckFailedError{Reason: rep.Summary}
	}
	return nil
}

// newStrategy returns the search strategy chosen by --strategy and
// --max-attempts (Section 5.4). Greedy is the default, because it stays fast
// on large clusters (NFR9); backtracking finds an order whenever one exists,
// and its number of attempts can be limited.
func newStrategy(cmd *cobra.Command, opts *drainOptions) (drainorder.Strategy, error) {
	if opts.maxAttempts < 0 {
		return nil, fmt.Errorf("--max-attempts must not be negative")
	}
	switch opts.strategy {
	case "greedy":
		// Greedy needs at most n(n+1)/2 attempts, so a limit would be
		// silently ignored; refuse it instead (NFR17).
		if cmd.Flags().Changed("max-attempts") {
			return nil, fmt.Errorf("--max-attempts applies only to --strategy backtracking")
		}
		return drainorder.Greedy{}, nil
	case "backtracking":
		return drainorder.Backtracking{MaxAttempts: opts.maxAttempts}, nil
	default:
		return nil, fmt.Errorf("unknown strategy %q: use greedy or backtracking", opts.strategy)
	}
}
