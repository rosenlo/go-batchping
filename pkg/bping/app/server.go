package app

import (
	"errors"
	"fmt"
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

const (
	MetricType  = "GAUGE"
	DefaultName = "aggregator"
)

func ConvertTransfer(mapStat map[string]*batchping.Statistics) {
	timestamp := time.Now().Unix()
	args := make([]bping.MetricValue, 0)
	metricResp := viper.GetString("metric_resp")
	metricLoss := viper.GetString("metric_resp")
	for _, stat := range mapStat {
		log.Println(stat)
		hostname, err := os.Hostname()
		if err != nil {
			hostname = DefaultName
		}
		args = append(args, bping.MetricValue{
			Endpoint:  hostname,
			Metric:    metricResp,
			Value:     stat.AvgRtt.Milliseconds(),
			Type:      MetricType,
			Tags:      fmt.Sprintf("src=%s,dest=%s", hostname, stat.Addr),
			Timestamp: timestamp,
		})
		args = append(args, bping.MetricValue{
			Endpoint:  hostname,
			Metric:    metricLoss,
			Value:     stat.PacketLoss,
			Type:      MetricType,
			Tags:      fmt.Sprintf("src=%s,dest=%s", hostname, stat.Addr),
			Timestamp: timestamp,
		})
	}
	log.Println(args)
}

func Run(cmd *cobra.Command, args []string) {
	log.SetFlags(log.Ldate | log.Ltime | log.Lshortfile)
	command := viper.GetString("command")
	privileged := viper.GetBool("privileged")
	bp, err := batchping.New(privileged)
	if err != nil {
		quit(err)
	}

	bp.Timeout = time.Second * time.Duration(viper.GetInt("timeout"))
	bp.OnFinish = func(pingerStat map[string]*batchping.Statistics) {
		ConvertTransfer(pingerStat)
	}

	interval := time.NewTimer(time.Second * time.Duration(viper.GetInt("interval")))
	defer interval.Stop()

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	for {

		addrs, err := bping.TCPSockAddr(command)
		log.Printf("[debug] addrs: %v", addrs)
		if err != nil {
			quit(err)
		}
		if len(addrs) == 0 {
			quit(errors.New("no such addrs"))
		}

		go func() {
			err := bp.Run(addrs)
			if err != nil {
				log.Printf("[error] %v", err)
			}
		}()

		select {
		case <-interval.C:
			continue
		case signal := <-sigs:
			log.Printf("[debug] Receive %s signal", signal)
			switch signal {

			case syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT:
				time.Sleep(time.Second)
				os.Exit(0)
			}
		}
	}
}

func quit(err error) {
	log.Printf("[error] quit with error: %v", err)
	os.Exit(1)
}
