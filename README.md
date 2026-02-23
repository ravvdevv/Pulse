# Pulse

An ICMP ping tool written in Go.

Pulse is designed to be **readable first** — every step of the ICMP process
is implemented clearly, from packet construction to checksum calculation to RTT
measurement. It's a great starting point for understanding how `ping` works
under the hood.

---

## Features

- IPv4 and IPv6 support
- Accurate RTT measurements with min/avg/max statistics
- Flexible flag positioning (before or after host)
- Real-time packet loss tracking
- Configurable timeouts and intervals
- Custom payload sizes
- Infinite ping mode

---

## Project Structure

```
pulse/
├── cmd/
│   └── pulse/
│       └── main.go          # CLI entry point, flag parsing, output formatting
├── internal/
│   └── icmp/
│       └── icmp.go          # ICMP packet construction, sending, receiving, stats
├── go.mod
├── go.sum
└── README.md
```

---

## Installation

```bash
git clone https://github.com/ravvdevv/pulse
cd pulse
go build ./cmd/pulse
```

> **Note:** Raw ICMP sockets require elevated privileges on most systems. Use sudo on Unix/Linux systems or run as Administrator on Windows.

---

## Usage

```bash
# Basic usage (requires elevated privileges)
./pulse google.com

# Send 10 pings with 500ms interval
./pulse -c 10 -i 0.5 google.com

# Verbose mode with timestamps
./pulse -v -c 5 cloudflare.com

# Large payload (1400 bytes) with 5s timeout
./pulse -s 1400 -timeout 5 8.8.8.8

# Ping IPv6 address
./pulse -c 4 2606:4700:4700::1111

# Infinite pings (stop with Ctrl+C)
./pulse -t example.com

# Flags can come after host too!
./pulse google.com -t -c 10
```

### Example Output

```bash
PULSE scanning google.com (142.250.80.46) with 64-byte packets
142.250.80.46: seq=1 latency=12.453 ms
142.250.80.46: seq=2 latency=11.821 ms
142.250.80.46: seq=3 latency=13.102 ms
142.250.80.46: seq=4 latency=12.087 ms

PULSE scan complete for google.com
Sent: 4 | Received: 4 | Loss: 0.0%
Latency: min=11.821 ms | avg=12.366 ms | max=13.102 ms
```

```bash
PULSE scanning 10.0.0.99 (10.0.0.99) with 64-byte packets
Timeout for seq=1
Timeout for seq=2
^C
PULSE scan complete for 10.0.0.99
Sent: 2 | Received: 0 | Loss: 100.0%
```

---

## Flags

| Flag | Default | Description                         |
|------|---------|-------------------------------------|
| `-c` | 4       | Packet count (-1 = infinite)        |
| `-i` | 1.0     | Interval between pings (seconds)    |
| `-timeout` | 3.0     | Per-packet timeout (seconds)        |
| `-t` | false   | Infinite ping mode                  |
| `-s` | 56      | Payload size in bytes               |
| `-v` | false   | Verbose: show packet timestamps     |

---

## How It Works

### ICMP Packet Structure

```
 0                   1                   2                   3
 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1 2 3 4 5 6 7 8 9 0 1
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|     Type      |     Code      |          Checksum             |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|           Identifier          |        Sequence Number        |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
|                      Payload (variable)                       |
+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+-+
```

| Field          | IPv4 Echo Request | IPv6 Echo Request |
|----------------|:-----------------:|:-----------------:|
| Type           | 8                 | 128               |
| Type (Reply)   | 0                 | 129               |
| Code           | 0                 | 0                 |

### Checksum Calculation

The ICMP checksum is a **16-bit one's complement** of the one's complement
sum of the entire ICMP message:

1. Sum all 16-bit words of the message (with any odd byte padded to 16 bits)
2. Fold any carry bits back into the sum until it fits in 16 bits
3. Take the bitwise NOT of the result

### RTT Measurement

RTT (Round-Trip Time) is measured by:
1. Recording `time.Now()` immediately before writing the packet to the socket
2. Recording `time.Now()` immediately after reading a matching reply
3. RTT = `receiveTime - sendTime`

The send timestamp is embedded in the first 8 bytes of the payload for
alternative measurement approaches.

---

## Dependencies

- [`golang.org/x/net/icmp`](https://pkg.go.dev/golang.org/x/net/icmp) — ICMP message marshaling
- [`golang.org/x/net/ipv4`](https://pkg.go.dev/golang.org/x/net/ipv4) — IPv4 ICMP type constants
- [`golang.org/x/net/ipv6`](https://pkg.go.dev/golang.org/x/net/ipv6) — IPv6 ICMP type constants

The checksum algorithm and packet structure are implemented manually in
`internal/icmp/icmp.go` for educational clarity.

---

## License

MIT