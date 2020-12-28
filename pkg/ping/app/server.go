package app

import (
	"github.com/rosenlo/go-batchping/pkg/ping"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func Run(cmd *cobra.Command, args []string) {
	command := viper.GetString("command")
	ping.TCPSockAddr(command)
}
