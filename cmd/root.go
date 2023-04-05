package cmd

import (
	"github.com/spf13/cobra"
)

var (
	// Used for flags.
	rootCmd = &cobra.Command{
		Use:   "cedana",
		Short: "simple criu dump/restore client",
		Long: `________  _______   ________  ________  ________   ________     
|\   ____\|\  ___ \ |\   ___ \|\   __  \|\   ___  \|\   __  \    
\ \  \___|\ \   __/|\ \  \_|\ \ \  \|\  \ \  \\ \  \ \  \|\  \   
 \ \  \    \ \  \_|/_\ \  \ \\ \ \   __  \ \  \\ \  \ \   __  \  
  \ \  \____\ \  \_|\ \ \  \_\\ \ \  \ \  \ \  \\ \  \ \  \ \  \ 
   \ \_______\ \_______\ \_______\ \__\ \__\ \__\\ \__\ \__\ \__\
    \|_______|\|_______|\|_______|\|__|\|__|\|__| \|__|\|__|\|__|
                                                                 
                                                                 
                                                                 ` + "\n Instance Brokerage, Orchestration and Migration System." +
			"\n Property of Cedana, Inc.",
	}
)

func Execute() error {
	return rootCmd.Execute()
}

func init() {
	cobra.OnInitialize()
}
