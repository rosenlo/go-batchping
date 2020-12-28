package ping

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"os/exec"
	"syscall"
	"time"

	"github.com/spf13/viper"
)

type SockAddr struct {
	IP net.IP
}

func (s *SockAddr) String() string {
	return fmt.Sprintf("%s", s.IP.String())
}

func NewCommandContext(ctx context.Context, path string, arg ...string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, path, arg...)
	cmd.Stdout = os.Stdout
	cmd.Stderr = os.Stderr
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return cmd
}

func TCPSockAddr(command string) ([]*SockAddr, error) {
	executor := viper.GetString("executor")
	executorArg := viper.GetString("executor_arg")
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Second)
	defer cancel()

	cmd := NewCommandContext(ctx, executor, executorArg, command)
	output, err := cmd.Output()
	if err != nil {
		log.Println("run cmd with error:", err)
		return nil, err
	}

	lines := bytes.Split(output, []byte("\n"))
	socksAddr := make([]*SockAddr, len(lines))
	for idx, line := range lines {
		ip := make(net.IP, net.IPv4len)
		err := ip.UnmarshalText(line)
		if err != nil {
			log.Println("unmarshal ip with error:", err)
			continue
		}
		socksAddr[idx] = &SockAddr{IP: ip}
	}
	return socksAddr, nil
}
