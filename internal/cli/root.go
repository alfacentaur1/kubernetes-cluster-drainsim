package cli

import "github.com/spf13/cobra"

// NewRootCommand creates the root command "drainsim" with all its subcommands.
func NewRootCommand(deps Deps) *cobra.Command {
	root := &cobra.Command{
		Use:           "drainsim",                                                    // Name of the command.
		Short:         "Simulates node drains on a snapshot of a Kubernetes cluster", // Shown by drainsim --help.
		SilenceUsage:  true,                                                          // On error, do not print usage.
		SilenceErrors: true,                                                          // Errors are printed by main.
	}
	root.AddCommand(newSnapshotCommand(deps)) // drainsim snapshot
	root.AddCommand(newDrainCommand(deps))    // drainsim drain
	root.AddCommand(newPolicyCommand(deps))   // drainsim policy
	return root
}
