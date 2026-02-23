// Package icmp provides utilities for constructing, sending, and receiving
// ICMP Echo Request/Reply packets using raw sockets.
//
// ICMP Packet Structure (RFC 792):
//
//	 0                   1                   2                   3
//	 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	|     Type      |     Code      |          Checksum             |
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	|           Identifier          |        Sequence Number        |
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//	|                             Payload                           |
//	+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
//
// Type 8 = Echo Request, Type 0 = Echo Reply (IPv4)
// Type 128 = Echo Request, Type 129 = Echo Reply (IPv6)
package icmp

import (
	"encoding/binary"
	"fmt"
	"net"
	"os"
	"time"

	"golang.org/x/net/icmp"
	"golang.org/x/net/ipv4"
	"golang.org/x/net/ipv6"
)

const (
	// ProtocolICMP is the IP protocol number for ICMPv4.
	ProtocolICMP = 1
	// ProtocolICMPv6 is the IP protocol number for ICMPv6.
	ProtocolICMPv6 = 58

	// DefaultPayloadSize is the default ICMP payload size in bytes.
	DefaultPayloadSize = 56

	// DefaultTimeout is how long to wait for a reply before marking as lost.
	DefaultTimeout = 3 * time.Second
)

// Pinger holds state for a single ping session.
type Pinger struct {
	Host        string        // Target hostname or IP
	Count       int           // Number of pings to send (-1 = infinite)
	Interval    time.Duration // Time between pings
	Timeout     time.Duration // Per-packet reply timeout
	PayloadSize int           // Bytes of payload to send
	Verbose     bool          // Print timestamps

	id      int    // ICMP identifier (our PID)
	isIPv6  bool   // Whether we're pinging an IPv6 address
	addr    net.IP // Resolved target IP
	network string // "ip4:icmp" or "ip6:ipv6-icmp"

	// Statistics
	sent     int
	received int
	rtts     []time.Duration
}

// Result holds the outcome of a single ping.
type Result struct {
	Seq      int
	RTT      time.Duration
	Received bool
	Error    error
}

// NewPinger creates a Pinger with sensible defaults.
func NewPinger(host string) *Pinger {
	return &Pinger{
		Host:        host,
		Count:       4,
		Interval:    time.Second,
		Timeout:     DefaultTimeout,
		PayloadSize: DefaultPayloadSize,
		id:          os.Getpid() & 0xffff, // Identifier: lower 16 bits of PID
	}
}

// Resolve looks up the target host and sets addr/network/isIPv6.
func (p *Pinger) Resolve() error {
	ips, err := net.LookupIP(p.Host)
	if err != nil {
		return fmt.Errorf("lookup %q: %w", p.Host, err)
	}

	// Prefer IPv4 unless only IPv6 is available.
	for _, ip := range ips {
		if ip4 := ip.To4(); ip4 != nil {
			p.addr = ip4
			p.network = "ip4:icmp"
			p.isIPv6 = false
			return nil
		}
	}
	for _, ip := range ips {
		if ip.To16() != nil {
			p.addr = ip
			p.network = "ip6:ipv6-icmp"
			p.isIPv6 = true
			return nil
		}
	}
	return fmt.Errorf("no usable IP address for %q", p.Host)
}

// buildRequest constructs a raw ICMP Echo Request packet.
//
// The packet layout (manually constructed):
//
//	Byte 0   : Type  (8 for IPv4, 128 for IPv6)
//	Byte 1   : Code  (always 0 for echo)
//	Bytes 2-3: Checksum (one's complement of the entire header+payload)
//	Bytes 4-5: Identifier
//	Bytes 6-7: Sequence Number
//	Bytes 8+ : Payload (timestamp + padding)
func (p *Pinger) buildRequest(seq int) ([]byte, error) {
	// Payload = 8-byte timestamp + padding to reach PayloadSize.
	payload := make([]byte, p.PayloadSize)
	now := time.Now().UnixNano()
	binary.BigEndian.PutUint64(payload[:8], uint64(now)) // embed send time

	var msgType icmp.Type
	msgType = ipv4.ICMPTypeEcho
	if p.isIPv6 {
		msgType = ipv6.ICMPTypeEchoRequest
	}

	msg := icmp.Message{
		Type: msgType,
		Code: 0,
		Body: &icmp.Echo{
			ID:   p.id,
			Seq:  seq,
			Data: payload,
		},
	}

	proto := ProtocolICMP
	if p.isIPv6 {
		proto = ProtocolICMPv6
	}

	_ = proto // referenced above; x/net uses message type to determine checksum approach
	return msg.Marshal(nil)
}

// Addr returns the resolved IP address as a string.
func (p *Pinger) Addr() string {
	if p.addr == nil {
		return p.Host
	}
	return p.addr.String()
}

// SendPing sends one ICMP Echo Request and waits for the reply.
// RTT is measured from just before write to just after a matching reply is read.
func (p *Pinger) SendPing(seq int) Result {
	conn, err := icmp.ListenPacket(p.network, "")
	if err != nil {
		return Result{Seq: seq, Error: fmt.Errorf("listen: %w (try running as root)", err)}
	}
	defer conn.Close()

	data, err := p.buildRequest(seq)
	if err != nil {
		return Result{Seq: seq, Error: fmt.Errorf("build packet: %w", err)}
	}

	dst := &net.IPAddr{IP: p.addr}

	// --- Send ---
	sendTime := time.Now()
	if _, err := conn.WriteTo(data, dst); err != nil {
		return Result{Seq: seq, Error: fmt.Errorf("write: %w", err)}
	}
	p.sent++

	// --- Receive ---
	_ = conn.SetReadDeadline(time.Now().Add(p.Timeout))

	buf := make([]byte, 1500)
	for {
		n, _, err := conn.ReadFrom(buf)
		if err != nil {
			// Timeout or other error => packet lost.
			return Result{Seq: seq, Received: false}
		}
		rtt := time.Since(sendTime)

		// Parse the reply to verify it's ours.
		proto := ProtocolICMP
		if p.isIPv6 {
			proto = ProtocolICMPv6
		}
		msg, err := icmp.ParseMessage(proto, buf[:n])
		if err != nil {
			continue
		}

		// Check for Echo Reply type.
		var replyType icmp.Type
		replyType = ipv4.ICMPTypeEchoReply
		if p.isIPv6 {
			replyType = ipv6.ICMPTypeEchoReply
		}
		if msg.Type != replyType {
			continue
		}

		echo, ok := msg.Body.(*icmp.Echo)
		if !ok {
			continue
		}

		// Match on our identifier and sequence number.
		if echo.ID != p.id || echo.Seq != seq {
			continue
		}

		p.received++
		p.rtts = append(p.rtts, rtt)
		return Result{Seq: seq, RTT: rtt, Received: true}
	}
}

// Stats holds the final summary statistics.
type Stats struct {
	Sent     int
	Received int
	Lost     int
	Loss     float64
	Min      time.Duration
	Max      time.Duration
	Avg      time.Duration
}

// Statistics computes summary stats from the ping session.
func (p *Pinger) Statistics() Stats {
	s := Stats{
		Sent:     p.sent,
		Received: p.received,
		Lost:     p.sent - p.received,
	}
	if p.sent > 0 {
		s.Loss = float64(s.Lost) / float64(p.sent) * 100
	}
	if len(p.rtts) == 0 {
		return s
	}

	s.Min = p.rtts[0]
	s.Max = p.rtts[0]
	var total time.Duration
	for _, r := range p.rtts {
		total += r
		if r < s.Min {
			s.Min = r
		}
		if r > s.Max {
			s.Max = r
		}
	}
	s.Avg = total / time.Duration(len(p.rtts))
	return s
}

// Checksum computes the ICMP checksum (one's complement sum) over b.
//
// Algorithm:
//  1. Sum all 16-bit words.
//  2. Fold carry bits back into the lower 16 bits.
//  3. Take the one's complement (bitwise NOT).
//
// This is exported for educational purposes; golang.org/x/net/icmp uses it
// internally when marshalling, but understanding it is essential for writing
// a raw ICMP implementation from scratch.
func Checksum(b []byte) uint16 {
	var sum uint32
	for i := 0; i < len(b)-1; i += 2 {
		sum += uint32(b[i])<<8 | uint32(b[i+1])
	}
	// If odd length, pad last byte.
	if len(b)%2 != 0 {
		sum += uint32(b[len(b)-1]) << 8
	}
	// Fold 32-bit sum into 16 bits.
	for sum>>16 != 0 {
		sum = (sum & 0xffff) + (sum >> 16)
	}
	return ^uint16(sum)
}
