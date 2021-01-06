package bping

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/rosenlo/go-batchping"
	"github.com/spf13/cobra"
	"github.com/spf13/viper"
)

const (
	MetricType  = "GAUGE"
	DefaultName = "aggregator"
)

func init() {
	log.SetFlags(log.Lmicroseconds | log.Ltime | log.Lshortfile)
}

func TransferFalcon(mapStat map[string]*batchping.Statistics) {
	timestamp := time.Now().Unix()
	args := make([]*MetricValue, 0)
	metricResp := viper.GetString("metric_resp")
	metricLoss := viper.GetString("metric_loss")
	transferAddr := viper.GetString("transfer_addr")
	step := viper.GetInt("interval")
	for _, stat := range mapStat {
		log.Printf("[debug] %s", stat)
		tag := fmt.Sprintf("dst=%s", stat.Addr)

		hostname, err := os.Hostname()
		if err != nil {
			hostname = DefaultName
			tag = fmt.Sprintf("src=%s,%s", hostname, tag)
		}

		args = append(args, &MetricValue{
			Endpoint:  hostname,
			Metric:    metricResp,
			Value:     fmt.Sprintf("%.2f", float64(stat.MaxRtt)/1e6),
			Type:      MetricType,
			Tags:      tag,
			Timestamp: timestamp,
			Step:      step,
		})
		args = append(args, &MetricValue{
			Endpoint:  hostname,
			Metric:    metricLoss,
			Value:     fmt.Sprintf("%.2f", stat.PacketLoss),
			Type:      MetricType,
			Tags:      tag,
			Timestamp: timestamp,
			Step:      step,
		})
	}

	transfer, err := NewTransfer(transferAddr, time.Second)
	if err != nil {
		log.Printf("[error] %v", err)
		return
	}
	reply := new(TransferResponse)
	err = transfer.Send(args, reply)
	if err != nil {
		log.Printf("[error] %v", err)
	}
}

func Run(cmd *cobra.Command, args []string) {
	command := viper.GetString("command")
	count := viper.GetInt("count")
	privileged := viper.GetBool("privileged")
	bp, err := batchping.New(privileged)
	if err != nil {
		quit(err)
	}
	defer bp.Close()

	interval := time.NewTicker(time.Second * time.Duration(viper.GetInt("interval")))
	defer interval.Stop()

	bp.Timeout = time.Second * time.Duration(viper.GetInt("timeout"))
	bp.OnFinish = func(pingerStat map[string]*batchping.Statistics) {
		TransferFalcon(pingerStat)
	}

	if count > 0 {
		bp.Count = count
	}

	sigs := make(chan os.Signal, 1)
	signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

	for {
		addrs, err := TCPSockAddr(command)
		log.Printf("[debug] addrs: %v", addrs)
		if err != nil {
			quit(err)
		}

		if len(addrs) != 0 {
			go func() {
				err := bp.Run(addrs)
				if err != nil {
					log.Printf("[error] %v", err)
				}
			}()
		}

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
