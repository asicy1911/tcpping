package main

import (
	"errors"
	"flag"
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"syscall"
	"time"
)

func getenvFloat(name string, def float64) float64 {
	v := strings.TrimSpace(os.Getenv(name))
	if v == "" {
		return def
	}
	f, err := strconv.ParseFloat(v, 64)
	if err != nil || f <= 0 {
		return def
	}
	return f
}

func isRefusedOrReset(err error) bool {
	// Treat “connection refused” / “connection reset” as *reachable* (like TCP SYN RTT)
	// so that closed ports still yield a latency value (RST is a response).
	return errors.Is(err, syscall.ECONNREFUSED) || errors.Is(err, syscall.ECONNRESET)
}

func usage() {
	name := "tcpping-connect"
	if len(os.Args) > 0 {
		name = os.Args[0]
	}
	fmt.Fprintf(os.Stderr, "Usage: %s -C -x <count> [-w <timeout_sec>] <host> [port]\n\n", name)
	fmt.Fprintln(os.Stderr, "SmokePing calls: <binary> -C -x N <host> [port]")
	fmt.Fprintln(os.Stderr, "Outputs: <host> : <ms> <ms> ... (successful probes only)")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Options:")
	fmt.Fprintln(os.Stderr, "  -C              fping -C compatible output (always recommended)")
	fmt.Fprintln(os.Stderr, "  -x <count>      number of connection attempts")
	fmt.Fprintln(os.Stderr, "  -w <seconds>    per-attempt timeout (float supported); default 1.0")
	fmt.Fprintln(os.Stderr, "")
	fmt.Fprintln(os.Stderr, "Environment:")
	fmt.Fprintln(os.Stderr, "  TCPPING_TIMEOUT or TCPPING_TIMEOUT_SEC  default timeout seconds")
}

func main() {
	defaultTimeout := getenvFloat("TCPPING_TIMEOUT", 1.0)
	defaultTimeout = getenvFloat("TCPPING_TIMEOUT_SEC", defaultTimeout)

	C := flag.Bool("C", false, "fping -C compatible output")
	x := flag.Int("x", 1, "repeat count")
	w := flag.Float64("w", defaultTimeout, "per-try timeout seconds")

	flag.Usage = usage
	flag.Parse()

	args := flag.Args()
	if len(args) < 1 {
		usage()
		os.Exit(2)
	}

	host := args[0]
	port := 80
	if len(args) >= 2 {
		p, err := strconv.Atoi(args[1])
		if err != nil || p < 0 || p > 65535 {
			fmt.Fprintf(os.Stderr, "Invalid port: %q\n", args[1])
			os.Exit(2)
		}
		port = p
	}

	if *x < 1 {
		fmt.Fprintln(os.Stderr, "-x must be >= 1")
		os.Exit(2)
	}
	if *w <= 0 {
		fmt.Fprintln(os.Stderr, "-w must be > 0")
		os.Exit(2)
	}

	addr := net.JoinHostPort(host, strconv.Itoa(port))
	dialer := net.Dialer{Timeout: time.Duration(*w * float64(time.Second))}

	ms := make([]float64, 0, *x)
	for i := 0; i < *x; i++ {
		start := time.Now()
		conn, err := dialer.Dial("tcp", addr)
		dur := time.Since(start)
		if conn != nil {
			_ = conn.Close()
		}

		if err != nil {
			// Timeout / no route / DNS fail: count as loss
			if ne, ok := err.(net.Error); ok && ne.Timeout() {
				continue
			}
			// Connection refused/reset: reachable (RST), keep a latency sample
			if isRefusedOrReset(err) {
				ms = append(ms, dur.Seconds()*1000.0)
				continue
			}
			continue
		}

		ms = append(ms, dur.Seconds()*1000.0)
	}

	// SmokePing’s TCPPing probe expects -C style output.
	_ = *C

	if len(ms) == 0 {
		// Print a line (useful for manual runs). SmokePing will treat “no numeric samples” as full loss.
		fmt.Printf("%s :\n", host)
		os.Exit(1)
	}

	fmt.Printf("%s :", host)
	for _, v := range ms {
		fmt.Printf(" %.3f", v)
	}
	fmt.Printf("\n")
	os.Exit(0)
}
