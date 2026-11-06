package cli

import (
	"fmt"

	"github.com/alfacentaur1/kubernetes-cluster-drainsim/internal/reporter"
	"github.com/spf13/cobra"
)

// checkFormat checks the value of --format before any work is done.
func checkFormat(format string) error {
	if format != "text" && format != "json" {
		return fmt.Errorf("unknown format %q: use text or json", format)
	}
	return nil
}

// writeReport writes the report as text or JSON (FR33, FR34) and turns a
// failed check into a CheckFailedError, so that main returns exit code 1
// (FR35). Every analysis command ends with it.
func writeReport(cmd *cobra.Command, format string, rep reporter.Report) error {
	out := cmd.OutOrStdout()
	var err error
	if format == "json" {
		err = reporter.JSON(out, rep)
	} else {
		err = reporter.Text(out, rep)
	}
	if err != nil {
		return fmt.Errorf("writing the report: %w", err)
	}
	if !rep.Passed {
		return &reporter.CheckFailedError{Reason: rep.Summary}
	}
	return nil
}
