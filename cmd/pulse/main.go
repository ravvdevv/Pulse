// pulse — a minimal, educational ICMP ping tool.
//
// Usage:
//
//	sudo pulse [flags] <host>
//
// Flags:
//
//	-c int     Number of pings to send (default 4, -1 = infinite)
//	-i float   Interval between pings in seconds (default 1.0)
//	-t float   Per-packet timeout in seconds (default 3.0)
//	-s int     Payload size in bytes (default 56)
//	-v         Verbose mode (show send timestamps)
//
// Example:
//
//	sudo pulse -c 5 -i 0.5 google.com
package main

import (
	"flag"
	"fmt"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	ping "github.com/ravvdevv/pulse/internal/icmp"
)

func main() {
	count := flag.Int("c", 4, "number of pings to send (-1 = infinite)")
	interval := flag.Float64("i", 1.0, "interval between pings (seconds)")
	timeout := flag.Float64("timeout", 3.0, "per-packet timeout (seconds)")
	infinite := flag.Bool("t", false, "infinite ping mode (same as -c -1)")
	size := flag.Int("s", ping.DefaultPayloadSize, "payload size (bytes)")
	verbose := flag.Bool("v", false, "verbose: show packet timestamps")
	flag.Usage = func() {
		fmt.Fprintf(os.Stderr, "Usage: pulse [flags] <host> [flags]\n\n")
		flag.PrintDefaults()
	}

	var host string
	var remainingArgs []string
	args := os.Args[1:]

	for i, arg := range args {
		if !strings.HasPrefix(arg, "-") {
			host = arg
			remainingArgs = append(args[:i], args[i+1:]...)
			break
		}
	}

	if host == "" {
		flag.Usage()
		os.Exit(1)
	}

	os.Args = append([]string{os.Args[0]}, remainingArgs...)
	flag.Parse()

	if *infinite {
		*count = -1
	}

	p := ping.NewPinger(host)
	p.Count = *count
	p.Interval = time.Duration(*interval * float64(time.Second))
	p.Timeout = time.Duration(*timeout * float64(time.Second))
	p.PayloadSize = *size
	p.Verbose = *verbose

	if err := p.Resolve(); err != nil {
		fmt.Fprintf(os.Stderr, "pulse: %v\n", err)
		os.Exit(1)
	}

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)

	packetSize := p.PayloadSize + 8 // payload + 8-byte ICMP header
	fmt.Printf("🔍 PULSE scanning %s (%s) with %d-byte packets\n", host, p.Addr(), packetSize)

	done := make(chan struct{})

	go func() {
		defer close(done)

		seq := 1
		for {
			if p.Count >= 0 && seq > p.Count {
				return
			}

			result := p.SendPing(seq)

			if result.Error != nil {
				fmt.Fprintf(os.Stderr, "❌ PULSE seq=%d failed: %v\n", seq, result.Error)
			} else if result.Received {
				ts := ""
				if p.Verbose {
					ts = fmt.Sprintf(" [%s]", time.Now().Format("15:04:05.000"))
				}
				fmt.Printf("✅ %s: seq=%d latency=%v%s\n",
					p.Addr(), seq, fmtRTT(result.RTT), ts)
			} else {
				fmt.Printf("⏰ Timeout for seq=%d\n", seq)
			}

			seq++

			if p.Count < 0 || seq <= p.Count {
				select {
				case <-stop:
					return
				case <-time.After(p.Interval):
				}
			}
		}
	}()

	// Wait for completion or signal.
	select {
	case <-done:
	case <-stop:
		fmt.Println() // newline after ^C
	}

	printStats(host, p)
}

func printStats(host string, p *ping.Pinger) {
	s := p.Statistics()
	fmt.Printf("\n📊 PULSE scan complete for %s\n", host)
	fmt.Printf("📤 Sent: %d | 📥 Received: %d | 💔 Loss: %.1f%%\n",
		s.Sent, s.Received, s.Loss)
	if s.Received > 0 {
		fmt.Printf("⚡ Latency: min=%v | avg=%v | max=%v\n",
			fmtRTT(s.Min), fmtRTT(s.Avg), fmtRTT(s.Max))
	}
}

func fmtRTT(d time.Duration) string {
	ms := float64(d) / float64(time.Millisecond)
	return fmt.Sprintf("%.3f ms", ms)
}
