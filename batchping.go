package batchping

import (
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

	addrs []string

	interval time.Duration

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
}

func New(addrs []string, privileged bool) (*BatchPing, error) {
	network := "ip"
	protocol := "udp"
	if privileged {
		protocol = "icmp"
	}
	batchping := &BatchPing{
		addrs:    addrs,
		interval: time.Second,
		id:       os.Getpid() & 0xffff,
		network:  network,
		protocol: protocol,
		done:     make(chan bool),
	}
	return batchping, nil
}

func (bp *BatchPing) Run() error {
	var err error
	bp.conn4, err = icmp.ListenPacket(ipv4Proto[bp.protocol], bp.source)
	if err != nil {
		return err
	}
	if err := bp.conn4.IPv4PacketConn().SetControlMessage(ipv4.FlagTTL, true); err != nil {
		return err
	}
	if bp.conn6, err = icmp.ListenPacket(ipv6Proto[bp.protocol], bp.source); err != nil {
		return err
	}
	if err := bp.conn6.IPv6PacketConn().SetControlMessage(ipv6.FlagHopLimit, true); err != nil {
		return err
	}
	defer bp.conn4.Close()
	for _, addr := range bp.addrs {
		pinger, err := NewPinger(addr, bp.network, bp.protocol, bp.id)
		if err != nil {
			return err
		}
		bp.pingers[pinger.IPAddr().String()] = pinger
	}

	recv := make(chan *packet, len(bp.addrs))
	defer close(recv)

	var wg sync.WaitGroup
	wg.Add(3)
	go bp.batchRecvIpv4ICMP(recv, &wg)
	go bp.batchRecvIpv6ICMP(recv, &wg)
	go bp.batchSendICMP(&wg)
	wg.Wait()
	return nil
}

func (bp *BatchPing) batchSendICMP(wg *sync.WaitGroup) {
	for _, pinger := range bp.pingers {
		pinger.SendICMP(bp.conn4, bp.sequence)
	}
	bp.sequence = (bp.sequence + 1) & 0xffff
}

func (bp *BatchPing) batchRecvICMP(proto string, recv chan *packet, wg *sync.WaitGroup) {
	defer wg.Done()
	for {
		select {
		case <-bp.done:
			return
		default:
			bytes := make([]byte, 512)
			if err := bp.conn4.SetReadDeadline(time.Now().Add(time.Millisecond * 100)); err != nil {
				log.Println(err)
			}
			var n, ttl int
			var err error
			var addr net.Addr
			if proto == protoIpv4 {
				var cm *ipv4.ControlMessage
				n, cm, addr, err = bp.conn4.IPv4PacketConn().ReadFrom(bytes)
				if cm != nil {
					ttl = cm.TTL
				}
			} else {
				var cm *ipv6.ControlMessage
				n, cm, addr, err = bp.conn6.IPv6PacketConn().ReadFrom(bytes)
				if cm != nil {
					ttl = cm.HopLimit
				}
			}
			if err != nil {
				if neterr, ok := err.(*net.OpError); ok {
					if neterr.Timeout() {
						// Read timeout
						continue
					} else {
						log.Printf("read err %s ", err)
					}
				}
			}

			recv <- &packet{bytes: bytes, nbytes: n, ttl: ttl, proto: proto, addr: addr}
		}
	}
}

func (bp *BatchPing) batchRecvIpv4ICMP(recv chan *packet, wg *sync.WaitGroup) {
	bp.batchRecvICMP(protoIpv4, recv, wg)
}

func (bp *BatchPing) batchRecvIpv6ICMP(recv chan *packet, wg *sync.WaitGroup) {
	bp.batchRecvICMP(protoIpv6, recv, wg)
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

	outPkt := &Packet{
		Nbytes: recv.nbytes,
		Addr:   recv.addr.String(),
		Ttl:    recv.ttl,
	}

	switch pkt := m.Body.(type) {
	case *icmp.Echo:
		// If we are priviledged, we can match icmp.ID
		if bp.protocol == "icmp" {
			// Check if reply from same ID
			if pkt.ID != bp.id {
				return nil
			}
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

		if pinger, ok := bp.pingers[ip]; ok {
			pinger.PacketsRecv++
			pinger.rtts = append(pinger.rtts, receivedAt.Sub(timestamp))
		}

		outPkt.Rtt = receivedAt.Sub(timestamp)
		outPkt.Seq = pkt.Seq
		bp.Lock()
		bp.PacketsRecv++
		bp.Unlock()
	default:
		// Very bad, not sure how this can happen
		return fmt.Errorf("invalid ICMP echo reply; type: '%T', '%v'", pkt, pkt)
	}

	return nil
}
