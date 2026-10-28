// Command discover browses the LAN for running discord-proxy instances via
// mDNS (zeroconf) and prints the endpoints it finds: instance name, host,
// addresses, port and TXT fields. It is the client-side counterpart of the
// proxy's mDNS advertiser (see internal/mdns) and is useful for verifying
// that a proxy is visible on the network or for wiring up consumers
// manually when auto-discovery is not available.
//
// Usage:
//
//	go run ./cmd/discover [-timeout 5s] [-json]
//
// Exit codes: 0 on success (including "no proxies found"), 1 on failure.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"time"

	"github.com/discord-proxy-rpc/discord-proxy-rpc/internal/discover"
	"github.com/discord-proxy-rpc/discord-proxy-rpc/internal/mdns"
)

func main() {
	timeout := flag.Duration("timeout", 5*time.Second, "how long to browse mDNS before giving up")
	asJSON := flag.Bool("json", false, "emit machine-readable JSON instead of a table")
	flag.Parse()

	ctx, cancel := context.WithTimeout(context.Background(), *timeout)
	defer cancel()

	results, err := discover.Browse(ctx, mdns.DefaultServiceType)
	if err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}

	if err := discover.WriteResults(os.Stdout, os.Stderr, results, *asJSON); err != nil {
		fmt.Fprintf(os.Stderr, "error: %v\n", err)
		os.Exit(1)
	}
}
