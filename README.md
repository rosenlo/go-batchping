# go-batchping

A simple but powerful ICMP echo (ping) library for Go, inspired by
[go-ping](https://github.com/go-ping/ping).

## Features

- Ping multiple IP Address at the same time
- Support use ipv4 and ipv6 at the same time


Here is a very simple example that sends and receives three packets:

```go
privileged := false
addrs := []string{"8.8.8.8", "8.8.4.4", "::1"} // ipv4 and ipv6

bp, err := batchping.New(privileged) // udp if privileged is false else tcp
bp.Count = 3 // send 3 packets
bp.Timeout = 10 // a timeout before ping exists, regardless of how many packets have been received
bp.OnFinish = func(mapStats map[string]*Statistics) {
    for _, stats := range mapStats {
        fmt.Printf("\n--- %s ping statistics ---\n", stats.Addr)
        fmt.Printf("%d packets transmitted, %d packets received, %v%% packet loss\n",
            stats.PacketsSent, stats.PacketsRecv, stats.PacketLoss)
        fmt.Printf("round-trip min/avg/max/stddev = %v/%v/%v/%v\n",
            stats.MinRtt, stats.AvgRtt, stats.MaxRtt, stats.StdDevRtt)
    }
}

err := bp.Run(addrs) // Blocks until finished.
if err != nil {
    panic(err)
}
```

Here is an example that work with UNIX shell command as daemon:

```go
func GetEstAddrs(command string) ([]string, error) {
    cmd := exec.Command("/bin/sh", "-ec", command)
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

command = "ss -atn4 state established | grep -v Address | awk '{print $4}'| awk -F':' '{print $1}'| sort -u"

bp, err := batchping.New(privileged)

bp.Count = 5

bp.OnFinish = func(mapStats map[string]*batchping.Statistics) {
    // do something
}

interval := time.NewTicker(time.Second * 30)
defer interval.Stop()

sigs := make(chan os.Signal, 1)
signal.Notify(sigs, syscall.SIGINT, syscall.SIGTERM, syscall.SIGQUIT)

for {
    addrs, err := GetEstAddrs(command)
    if err != nil {
        panic(err)
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
            os.Exit(0)
        }
    }
}

```

It sends ICMP Echo Request packet(s) every 30 seconds and waits for an Echo Reply in
response. If it receives a response, it calls the `OnRecv` callback.
When it's finished, it calls the `OnFinish` callback.

For a full batchping example, see
[examples/bping/server.go](https://github.com/rosenlo/go-batchping/blob/master/examples/bping/server.go).

## Installation

```
go get -u github.com/rosenlo/go-batchping
```

## Supported Operating Systems

### Linux
This library attempts to send an "unprivileged" ping via UDP. On Linux,
this must be enabled with the following sysctl command:

```
sudo sysctl -w net.ipv4.ping_group_range="0 2147483647"
```

If you do not wish to do this, you can call `batchping.New(true)`
in your code and then use setcap on your binary to allow it to bind to
raw sockets (or just run it as root):

```
setcap cap_net_raw=+ep /path/to/your/compiled/binary
```

See [this blog](https://sturmflut.github.io/linux/ubuntu/2015/01/17/unprivileged-icmp-sockets-on-linux/)
and the Go [x/net/icmp](https://godoc.org/golang.org/x/net/icmp) package
for more details.

### Windows

You must use `batchping.New(true)`, otherwise you will receive
the following error:

```
socket: The requested protocol has not been configured into the system, or no implementation for it exists.
```

Despite the method name, this should work without the need to elevate
privileges and has been tested on Windows 10. Please note that accessing
packet TTL values is not supported due to limitations in the Go
x/net/ipv4 and x/net/ipv6 packages.


## Getting Help:

For support and help, you can open an issue.
