package utils

// Utility functions for the cobra CLI

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"

	"github.com/spf13/cobra"
)

// AliasOf creates an alias of the provided command, That includes the same flags and hooks.
// Even the parent command's PersistentPreRunE and PersistentPostRunE hooks are invoked.
// Provide a name only if it's different from provided command's name
func AliasOf(src *cobra.Command, name ...string) *cobra.Command {
	if src == nil {
		return nil
	}
	cmd := *src
	cmd.Use = AliasCommandUse(src, name...)

	cmd.Flags().AddFlagSet(src.LocalFlags())
	cmd.Flags().AddFlagSet(src.InheritedFlags())

	if src.HasSubCommands() {
		for _, c := range src.Commands() {
			cmd.AddCommand(AliasOf(c))
		}
	} else {
		cmd.Run = AliasCommandRun(src)
		cmd.RunE = AliasCommandRunE(src)
	}

	return &cmd
}

// Use this for Run to make a command an alias to another command's Run.
// Invokes all persistent hooks for all parents as well.
func AliasCommandRun(aliasOf *cobra.Command) func(cmd *cobra.Command, args []string) {
	if aliasOf == nil {
		return nil
	}

	parents := []*cobra.Command{}
	aliasOf.VisitParents(func(p *cobra.Command) {
		parents = append(parents, p)
	})

	return func(cmd *cobra.Command, args []string) {
		// Run all PersistentPreRunE hooks for immediate parents, reverse order
		for i := len(parents) - 1; i >= 0; i-- {
			p := parents[i]
			if p.PersistentPreRun != nil {
				p.PersistentPreRun(cmd, args)
			}
		}

		// Run the alias command
		if aliasOf.Run != nil {
			aliasOf.Run(cmd, args)
		}

		// Run all PersistentPostRunE hooks for immediate parents
		for _, p := range parents {
			if p.PersistentPostRun != nil {
				p.PersistentPostRun(cmd, args)
			}
		}
	}
}

// Use this for RunE to make a command an alias to another command's RunE.
// Invokes all persistent hooks for all parents as well.
func AliasCommandRunE(aliasOf *cobra.Command) func(cmd *cobra.Command, args []string) error {
	if aliasOf == nil {
		return nil
	}

	parents := []*cobra.Command{}
	aliasOf.VisitParents(func(p *cobra.Command) {
		parents = append(parents, p)
	})

	return func(cmd *cobra.Command, args []string) error {
		// Run all PersistentPreRunE hooks for immediate parents, reverse order
		for i := len(parents) - 1; i >= 0; i-- {
			p := parents[i]
			if p.PersistentPreRunE != nil {
				if err := p.PersistentPreRunE(cmd, args); err != nil {
					return err
				}
			}
		}

		// Run the alias command
		if aliasOf.RunE != nil {
			if err := aliasOf.RunE(cmd, args); err != nil {
				return err
			}
		}

		// Run all PersistentPostRunE hooks for immediate parents
		for _, p := range parents {
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

func FullUse(cmd *cobra.Command) string {
	if cmd == nil {
		return ""
	}

	parents := []*cobra.Command{}
	cmd.VisitParents(func(p *cobra.Command) {
		parents = append(parents, p)
	})
	if len(parents) > 1 {
		parents = parents[:len(parents)-1] // Remove the root command
	}

	use := ""
	for i := len(parents) - 1; i >= 0; i-- {
		p := parents[i]
		if p.Use != "" {
			use += p.Use + " "
		}
	}

	use += cmd.Use
	strings.TrimSpace(use)

	return use
}

func Confirm(ctx context.Context, msg string) bool {
	reader := bufio.NewReader(os.Stdin)
	answer := make(chan bool)

	go func() {
		for {
			fmt.Print(msg + " [y/n]: ")
			response, err := reader.ReadString('\n')
			if err != nil {
				answer <- false
				return
			}

			response = strings.ToLower(strings.TrimSpace(response))
			if response == "y" || response == "yes" {
				answer <- true
				return
			} else if response == "n" || response == "no" || response == "" {
				answer <- false
				return
			}
		}
	}()

	select {
	case <-ctx.Done():
		return false
	case res := <-answer:
		return res
	}
}
