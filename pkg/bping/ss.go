package bping

import (
	"bytes"
	"context"
	"fmt"
	"log"
	"net"
	"os/exec"
	"strings"
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
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	return cmd
}

func TCPSockAddr(command string) ([]string, error) {
	executor := viper.GetString("executor")
	executorArg := viper.GetString("executor_arg")
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	log.Printf("[debug] run command: %s", command)
	cmd := NewCommandContext(ctx, executor, executorArg, command)
	output, err := cmd.Output()
	if err != nil {
		return nil, err
	}

	lines := bytes.Split(output, []byte("\n"))
	addrs := make([]string, 0)
	for _, line := range lines {
		addr := strings.Trim(string(line), "\n")
		if addr != "" {
			addrs = append(addrs, addr)
		}
	}
	return addrs, nil
}
