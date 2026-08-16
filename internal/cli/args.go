package cli

import (
	"errors"
	"fmt"
	"strings"

	"github.com/spf13/cobra"
)

// runSubcommandGroup is the RunE for a command that exists only to group
// subcommands: bare invocation prints help, a stray argument is an error.
//
// Setting Args alone is not enough. Cobra returns flag.ErrHelp for a command that
// is not Runnable *before* it validates arguments, and ExecuteC turns that into
// "print help, exit 0". That is why `dorgu remediation frobnicate` printed the
// manual and reported success, which is indistinguishable from a command that
// worked (F-12). Giving the group a RunE makes it Runnable, so its Args validator
// is reached and its error propagates to a non-zero exit.
func runSubcommandGroup(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return cmd.Help()
	}
	return noUnknownSubcommand(cmd, args)
}

// noUnknownSubcommand rejects a stray argument on a subcommand group. Cobra's
// default (legacyArgs) only produces "unknown command" for the root command.
func noUnknownSubcommand(cmd *cobra.Command, args []string) error {
	if len(args) == 0 {
		return nil
	}

	var msg strings.Builder
	fmt.Fprintf(&msg, "unknown command %q for %q", args[0], cmd.CommandPath())

	// Cobra only defaults this on the root command, so a subcommand group would
	// otherwise suggest by prefix alone and stay silent on an ordinary typo.
	if cmd.SuggestionsMinimumDistance <= 0 {
		cmd.SuggestionsMinimumDistance = 2
	}
	if suggestions := cmd.SuggestionsFor(args[0]); len(suggestions) > 0 {
		msg.WriteString("\n\nDid you mean this?\n")
		for _, s := range suggestions {
			fmt.Fprintf(&msg, "\t%s\n", s)
		}
	}
	fmt.Fprintf(&msg, "\nRun '%s --help' for usage.", cmd.CommandPath())

	// Point at help rather than dumping the whole manual over the one line that
	// says what went wrong. This mirrors cobra's own root-level message.
	cmd.SilenceUsage = true

	return errors.New(msg.String())
}
