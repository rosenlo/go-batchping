package app

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rosenlo/go-batchping"
	"github.com/rosenlo/go-batchping/pkg/bping"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

func StartSignal(pid int, exitfunc ...func()) {
	sigs := make(chan os.Signal, 1)
	log.Printf("pid: %d register signal notify", pid)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	for {
		s := <-sigs
		log.Printf("recv %s", s)

		switch s {
		case syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT:
			if len(exitfunc) != 0 {
				for _, f := range exitfunc {
					f()
				}
			}
			log.Printf("graceful shut down")
			log.Printf("main pid: %d exit", pid)
			os.Exit(0)
		}
	}
}

func Run(cmd *cobra.Command, args []string) {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	command := viper.GetString("command")
	addrs, err := ping.TCPSockAddr(command)
	log.Printf("[debug] addrs: %v", addrs)
	if err != nil {
		quit(err)
	}
	if len(addrs) == 0 {
		quit(errors.New("no such addrs"))
	}
	privileged := viper.GetBool("privileged")
	bp, err := batchping.New(addrs, privileged)
	if err != nil {
		quit(err)
	}
	bp.Interval = time.Second * time.Duration(viper.GetInt("interval"))
	bp.Timeout = time.Second * time.Duration(viper.GetInt("timeout"))
	bp.OnFinish = func(pingerStat map[string]*batchping.Statistics) {
		for ip, stat := range pingerStat {
			log.Printf("[debug] %s %v", ip, stat.Rtts)
		}
	}
	log.Printf("[debug] packetsSent: %d", bp.PacketsSent)
	log.Printf("[debug] packetsRecv: %d", bp.PacketsRecv)

	ctx, cancel := context.WithCancel(context.Background())
	bp.Run(ctx)
	StartSignal(os.Getpid(), cancel)
}

func quit(err error) {
	log.Printf("[error] quit with error: %v", err)
	os.Exit(1)
}
