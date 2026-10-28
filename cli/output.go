package cli

import (
	"fmt"

	"github.com/spf13/cobra"
)

// printf writes formatted command output to stdout. A failed write (closed
// pipe, full disk) fails the command instead of being silently dropped.
func printf(cmd *cobra.Command, format string, args ...any) error {
	if _, err := fmt.Fprintf(cmd.OutOrStdout(), format, args...); err != nil {
		return fmt.Errorf("write output: %w", err)
	}
	return nil
}
