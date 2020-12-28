package app

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

// root represents the base command when called without any subcommands
var root = &cobra.Command{
	Use:                "pinger",
	Short:              "A simple but valid Ping tool",
	Long:               `A simple but valid Ping tool`,
	DisableFlagParsing: true,
	Run:                Run,
}

// Execute adds all child commands to the root command and sets flags appropriately.
// This is called by main(). It only needs to happen once to the rootCmd.
func Execute() error {
	return root.Execute()
}

func init() {
	cobra.OnInitialize(initConfig)
}

// initConfig reads in config file and ENV variables if set.
func initConfig() {
	viper.SetEnvPrefix("PINGER")
	// basic
	viper.SetDefault("command", "ss -atn4 state established '( sport = :8080 )' |awk '{print $4}'|awk -F':' '{print $1}'|sort -n|uniq")
	viper.SetDefault("executor", "/bin/sh")
	viper.SetDefault("executor_arg", "-ec")

	viper.AutomaticEnv() // read in environment variables that match

	// If a config file is found, read it in.
	if err := viper.ReadInConfig(); err != nil {
		fmt.Println(err)
		os.Exit(1)
	}
}
