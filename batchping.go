package batchping

import (
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"sync"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

var (
	protoIpv4 = "ipv4"
	protoIpv6 = "ipv6"
)

type BatchPing struct {
	sync.RWMutex

	// Interval is the wait time between each packet send. Default is 1s.
	Interval time.Duration

	// Timeout specifies a timeout before ping exits, regardless of how many
	// packets have been received. Default is 10s.
	Timeout time.Duration

	Count int

	id int

	sequence int

	source string

	// network is one of "ip", "ip4", or "ip6".
	network string
	// protocol is "icmp" or "udp".
	protocol string

	// stop chan bool
	done chan bool

	// Number of packets sent
	PacketsSent int

	// Number of packets received
	PacketsRecv int

	pingers map[string]*Pinger

	//conn4 is ipv4 icmp PacketConn
	conn4 *icmp.PacketConn

	//conn6 is ipv6 icmp PacketConn
	conn6 *icmp.PacketConn

	// OnFinish is called when Pinger exits
	OnFinish func(map[string]*Statistics)
}

func New(privileged bool) (*BatchPing, error) {
	network := "ip"
	protocol := "udp"
	if privileged {
		protocol = "icmp"
	}
	batchping := &BatchPing{
		Interval: time.Second,
		Timeout:  time.Second * 10,
		id:       os.Getpid() & 0xffff,
		Count:    3,
		network:  network,
		protocol: protocol,
		done:     make(chan bool),
		pingers:  make(map[string]*Pinger),
	}
	return batchping, nil
}

func (bp *BatchPing) PreRun() {
	bp.PacketsSent = 0
	bp.PacketsRecv = 0
	bp.done = make(chan bool)
}

func (bp *BatchPing) Run(addrs []string) error {
	bp.PreRun()
	if len(addrs) == 0 {
		return errors.New("no such hosts")
	}
	var err error
	bp.conn4, err = icmp.ListenPacket(ipv4Proto[bp.protocol], bp.source)
	if err != nil {
		log.Printf("[error] %v", err)
		return err
	}
	if err := bp.conn4.IPv4PacketConn().SetControlMessage(ipv4.FlagTTL, true); err != nil {
		log.Printf("[error] %v", err)
		return err
	}
	if bp.conn6, err = icmp.ListenPacket(ipv6Proto[bp.protocol], bp.source); err != nil {
		log.Printf("[error] %v", err)
		return err
	}
	if err := bp.conn6.IPv6PacketConn().SetControlMessage(ipv6.FlagHopLimit, true); err != nil {
		log.Printf("[error] %v", err)
		return err
	}
	defer bp.conn4.Close()
	defer bp.conn6.Close()

	for _, addr := range addrs {
		pinger, err := NewPinger(addr, bp.network, bp.protocol, bp.id)
		if err != nil {
			log.Printf("[error] %v, addr: %s network: %s, protocol: %s", err, addr, bp.network, bp.protocol)
			return err
		}
		pinger.SetConns(bp.conn4, bp.conn6)
		bp.pingers[pinger.IPAddr().String()] = pinger
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go bp.batchRecvIpv4ICMP(&wg)
	go bp.batchRecvIpv6ICMP(&wg)

	timeout := time.NewTimer(bp.Timeout)
	defer log.Println(4)
	defer timeout.Stop()

	interval := time.NewTimer(bp.Interval)
	defer interval.Stop()
	defer log.Println(3)

	defer bp.Finish()

	sequence := 0
	for {

		if sequence < bp.Count {
			go bp.batchSendICMP(sequence)
			sequence++
		}

		if bp.Count > 0 && bp.PacketsRecv/len(addrs) >= bp.Count {
			log.Printf("[debug] packetsSent: %d", bp.PacketsSent)
			log.Printf("[debug] packetsRecv: %d", bp.PacketsRecv)
			log.Printf("[debug] close")
			close(bp.done)
			wg.Wait()
			return nil
		}

		select {
		case <-bp.done:
			return nil
		case <-interval.C:
			log.Printf("interval.C")
			continue
		case <-timeout.C:
			log.Printf("[debug] timeout close")
			close(bp.done)
			wg.Wait()
			return nil
		default:
			time.Sleep(time.Millisecond * 100)
			log.Println("default")
		}
	}
}

func (bp *BatchPing) batchSendICMP(seq int) {

	log.Printf("[debug] send seq=%d", seq)
	for _, pinger := range bp.pingers {
		pinger.SendICMP(seq)
		bp.PacketsSent++
	}
}

func (bp *BatchPing) batchRecvICMP(proto string) {
	for {
		select {
		case <-bp.done:
			log.Printf("[debug] %s: recv closed", proto)
			return
		default:
			bytes := make([]byte, 512)
			var n, ttl int
			var err error
			var addr net.Addr
			if proto == protoIpv4 {
				if err := bp.conn4.SetReadDeadline(time.Now().Add(time.Millisecond * 100)); err != nil {
					log.Printf("[error] %v", err)
				}
				var cm *ipv4.ControlMessage
				n, cm, addr, err = bp.conn4.IPv4PacketConn().ReadFrom(bytes)
				if cm != nil {
					ttl = cm.TTL
				}
			} else {
				if err := bp.conn6.SetReadDeadline(time.Now().Add(time.Millisecond * 100)); err != nil {
					log.Printf("[error] %v", err)
				}
				var cm *ipv6.ControlMessage
				n, cm, addr, err = bp.conn6.IPv6PacketConn().ReadFrom(bytes)
				if cm != nil {
					ttl = cm.HopLimit
					log.Printf("[debug] ipv6, %v", cm.Src.String())
				}
			}
			if err != nil {
				if neterr, ok := err.(*net.OpError); ok {
					if neterr.Timeout() {
						// Read timeout
						continue
					} else {
						log.Printf("read err %s ", err)
						return
					}
				}
			}

			recv := &packet{bytes: bytes, nbytes: n, ttl: ttl, proto: proto, addr: addr}
			go bp.processPacket(recv)
		}
	}
}

func (bp *BatchPing) batchRecvIpv4ICMP(wg *sync.WaitGroup) {
	defer wg.Done()
	log.Printf("[debug] %s: start recv", protoIpv4)
	bp.batchRecvICMP(protoIpv4)
}

func (bp *BatchPing) batchRecvIpv6ICMP(wg *sync.WaitGroup) {
	defer wg.Done()
	log.Printf("[debug] %s: start recv", protoIpv6)
	bp.batchRecvICMP(protoIpv6)
}

func (bp *BatchPing) processPacket(recv *packet) error {
	receivedAt := time.Now()
	var proto int
	if recv.proto == protoIpv4 {
		proto = protocolICMP
	} else {
		proto = protocolIPv6ICMP
	}

	var m *icmp.Message
	var err error
	if m, err = icmp.ParseMessage(proto, recv.bytes); err != nil {
		return fmt.Errorf("error parsing icmp message: %s", err.Error())
	}

	if m.Type != ipv4.ICMPTypeEchoReply && m.Type != ipv6.ICMPTypeEchoReply {
		// Not an echo reply, ignore it
		return nil
	}

	switch pkt := m.Body.(type) {
	case *icmp.Echo:
		// Check if reply from same ID
		if pkt.ID != bp.id {
			return nil
		}

		if len(pkt.Data) < timeSliceLength+trackerLength {
			return fmt.Errorf("insufficient data received; got: %d %v",
				len(pkt.Data), pkt.Data)
		}

		timestamp := bytesToTime(pkt.Data[:timeSliceLength])

		var ip string
		if bp.protocol == "udp" {
			if ip, _, err = net.SplitHostPort(recv.addr.String()); err != nil {
				return fmt.Errorf("err ip : %v, err %v", recv.addr, err)
			}
		} else {
			ip = recv.addr.String()
		}

		rtt := receivedAt.Sub(timestamp)

		if pinger, ok := bp.pingers[ip]; ok {
			pinger.PacketsRecv++
			pinger.rtts = append(pinger.rtts, rtt)
		}

		log.Printf("[debug] %s: recv pkt", recv.proto)

		bp.Lock()
		bp.PacketsRecv++
		bp.Unlock()
	default:
		// Very bad, not sure how this can happen
		return fmt.Errorf("invalid ICMP echo reply; type: '%T', '%v'", pkt, pkt)
	}

	return nil
}

func (bp *BatchPing) Statistics() map[string]*Statistics {
	pingerStat := map[string]*Statistics{}
	for ip, pinger := range bp.pingers {
		pingerStat[ip] = pinger.Statistics()
	}
	return pingerStat
}

func (bp *BatchPing) Finish() {
	log.Printf("[debug] onFinish start")
	handler := bp.OnFinish
	if bp.OnFinish != nil {
		handler(bp.Statistics())
	}
	log.Printf("[debug] onFinish done")
}
