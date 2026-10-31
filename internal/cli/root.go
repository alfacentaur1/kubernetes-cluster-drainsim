package cli

import "github.com/spf13/cobra"

// NewRootCommand creates the root command "drainsim" with all its subcommands.
func NewRootCommand(deps Deps) *cobra.Command {
	root := &cobra.Command{
		Use:           "drainsim",                                                    //Name of command.
		Short:         "Simulates node drains on a snapshot of a Kubernetes cluster", //Hint for help (drainsim --help).
		SilenceUsage:  true,                                                          //On error, don't print usage.
		SilenceErrors: true,                                                          //Errors are printed by main.
	}
	root.AddCommand(newSnapshotCommand(deps)) //Adding the command
	return root
}
