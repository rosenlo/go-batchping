package batchping

import (
	"errors"
	"fmt"
	"log"
	"math"
	"math/rand"
	"net"
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

	// Number of packets received
	PacketsRecv int

	// Tracker: Used to uniquely identify packet when non-priviledged
	Tracker int64

	// network is one of "ip", "ip4", or "ip6".
	network string
	// protocol is "icmp" or "udp".
	protocol string

	// stop chan bool
	done chan bool

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
	r := rand.New(rand.NewSource(time.Now().UnixNano()))
	bp := &BatchPing{
		Interval: time.Second,
		Timeout:  time.Second * 10,
		id:       r.Intn(0xffff),
		Count:    3,
		network:  network,
		protocol: protocol,
		done:     make(chan bool),
		pingers:  make(map[string]*Pinger),
		Tracker:  r.Int63n(math.MaxInt64),
	}

	var err error

	// ipv4
	if bp.conn4, err = icmp.ListenPacket(ipv4Proto[protocol], bp.source); err != nil {
		log.Printf("[error] %s %v", protocol, err)
		return nil, err
	} else {
		err := bp.conn4.IPv4PacketConn().SetControlMessage(ipv4.FlagTTL, true)
		if err != nil {
			log.Printf("[error] %v", err)
			return nil, err
		}
	}

	// ipv6
	if bp.conn6, err = icmp.ListenPacket(ipv6Proto[bp.protocol], bp.source); err != nil {
		log.Printf("[error] %s %v", bp.protocol, err)
	} else {
		err := bp.conn6.IPv6PacketConn().SetControlMessage(ipv6.FlagHopLimit, true)
		if err != nil {
			log.Printf("[error] %v", err)
		}
	}

	return bp, nil
}

func (bp *BatchPing) PreRun() {
	bp.PacketsRecv = 0
	bp.done = make(chan bool)
	bp.pingers = make(map[string]*Pinger)
}

func (bp *BatchPing) Close() {
	if bp.conn4 != nil {
		bp.conn4.Close()
	}
	if bp.conn6 != nil {
		bp.conn6.Close()
	}
}

func (bp *BatchPing) Run(addrs []string) error {
	bp.PreRun()
	if len(addrs) == 0 {
		return errors.New("missing address")
	}

	for _, addr := range addrs {
		pinger, err := NewPinger(addr, bp.network, bp.protocol, bp.id)
		pinger.Tracker = bp.Tracker
		if err != nil {
			log.Printf("[error] %v, addr: %s network: %s, protocol: %s", err, addr, bp.network, bp.protocol)
			continue
		}
		pinger.SetConns(bp.conn4, bp.conn6)
		bp.pingers[pinger.IPAddr().String()] = pinger
	}

	var wg sync.WaitGroup
	wg.Add(2)
	go bp.batchRecvIpv4ICMP(&wg)
	go bp.batchRecvIpv6ICMP(&wg)

	timeout := time.NewTimer(bp.Timeout)
	defer timeout.Stop()

	interval := time.NewTicker(bp.Interval)
	defer interval.Stop()

	defer bp.Finish()

	sequence := 0
	for {

		if sequence < bp.Count {
			bp.batchSendICMP(sequence)
			sequence++
		}

		if bp.Count > 0 && bp.PacketsRecv/len(addrs) >= bp.Count {
			log.Printf("[debug] close")
			close(bp.done)
			wg.Wait()
			return nil
		}

		select {
		case <-bp.done:
			log.Printf("receivce close")
			return nil
		case <-interval.C:
			continue
		case <-timeout.C:
			log.Printf("[debug] timeout close")
			close(bp.done)
			wg.Wait()
			return nil
		}
	}
}

func (bp *BatchPing) batchSendICMP(seq int) {

	log.Printf("[debug] send seq=%d", seq)
	for _, pinger := range bp.pingers {
		pinger.SendICMP(seq)
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
			var src net.Addr
			if proto == protoIpv4 {
				if err := bp.conn4.SetReadDeadline(time.Now().Add(time.Millisecond * 100)); err != nil {
					log.Printf("[error] %v", err)
				}
				var cm *ipv4.ControlMessage
				n, cm, src, err = bp.conn4.IPv4PacketConn().ReadFrom(bytes)
				if cm != nil {
					ttl = cm.TTL
				}
			} else {
				if err := bp.conn6.SetReadDeadline(time.Now().Add(time.Millisecond * 100)); err != nil {
					log.Printf("[error] %v", err)
				}
				var cm *ipv6.ControlMessage
				n, cm, src, err = bp.conn6.IPv6PacketConn().ReadFrom(bytes)
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
						return
					}
				}
			}

			recv := &packet{bytes: bytes, nbytes: n, ttl: ttl, proto: proto, src: src}
			go bp.processPacket(recv)
		}
	}
}

func (bp *BatchPing) batchRecvIpv4ICMP(wg *sync.WaitGroup) {
	defer wg.Done()
	if bp.conn4 == nil {
		return
	}
	log.Printf("[debug] %s: start recv", protoIpv4)
	bp.batchRecvICMP(protoIpv4)
}

func (bp *BatchPing) batchRecvIpv6ICMP(wg *sync.WaitGroup) {
	defer wg.Done()
	if bp.conn6 == nil {
		return
	}
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

		tracker := bytesToInt(pkt.Data[timeSliceLength:])
		timestamp := bytesToTime(pkt.Data[:timeSliceLength])

		if tracker != bp.Tracker {
			return nil
		}

		var ip string
		if bp.protocol == "udp" {
			if ip, _, err = net.SplitHostPort(recv.src.String()); err != nil {
				return fmt.Errorf("err ip : %v, err %v", recv.src, err)
			}
		} else {
			ip = recv.src.String()
		}

		rtt := receivedAt.Sub(timestamp)
		log.Printf("[debug] %s: recv pkt from %s, took %s", recv.proto, ip, rtt)

		if pinger, ok := bp.pingers[ip]; ok {
			pinger.PacketsRecv++
			pinger.rtts = append(pinger.rtts, rtt)
		}

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
