package utils

// Utility functions for the cobra CLI

import (
	"strings"

	"github.com/spf13/cobra"
)

// AliasOf creates an alias of the provided command, That includes the same flags and hooks.
// Even the parent command's PersistentPreRunE and PersistentPostRunE hooks are invoked.
// Provide a name only if it's different from provided command's name
func AliasOf(cmd *cobra.Command, name ...string) *cobra.Command {
	if cmd == nil {
		return nil
	}
	return &cobra.Command{
		Use:   AliasCommandUse(cmd, name...),
		Short: "(alias) " + cmd.Short,
		Long:  "(alias) " + cmd.Long,
		Args:  cmd.Args,
		RunE:  AliasCommandRunE(cmd),
	}
}

// Use this for RunE to make a command an alias to another command's RunE.
// Invokes all PersistentPreRunE and PersistentPostRunE hooks for immediate parents as well.
func AliasCommandRunE(aliasOf *cobra.Command) func(cmd *cobra.Command, args []string) error {
	if aliasOf == nil {
		return nil
	}

	return func(cmd *cobra.Command, args []string) error {
		// Run all PersistentPreRunE hooks for all parents
		if p := aliasOf.Parent(); p != nil {
			if p.PersistentPreRunE != nil {
				if err := p.PersistentPreRunE(cmd, args); err != nil {
					return err
				}
			}
		}

		// Run the alias command
		if err := aliasOf.RunE(cmd, args); err != nil {
			return err
		}

		// Run all PersistentPostRunE hooks for all parents
		if p := aliasOf.Parent(); p != nil {
			if p.PersistentPostRunE != nil {
				if err := p.PersistentPostRunE(cmd, args); err != nil {
					return err
				}
			}
		}

		return nil
	}
}

func AliasCommandUse(aliasOf *cobra.Command, name ...string) string {
	if len(name) > 0 {
		if aliasOf == nil {
			return name[0]
		}

		// Append the rest of the aliasOf.Use to the name
		return name[0] + " " + strings.Join(strings.Split(aliasOf.Use, " ")[1:], " ")
	}
	return aliasOf.Use
}
