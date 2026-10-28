// Package discover browses the LAN for discord-proxy instances announced
// over mDNS (see internal/mdns) and turns raw zeroconf browse results into
// printable, machine-consumable results.
package discover

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"

	"github.com/grandcat/zeroconf"

	"github.com/discord-proxy-rpc/discord-proxy-rpc/internal/mdns"
)

// Result is one discovered proxy instance.
type Result struct {
	Instance  string            `json:"instance"`
	Host      string            `json:"host"`
	Addresses []string          `json:"addresses"`
	Port      int               `json:"port"`
	TXT       map[string]string `json:"txt"`
}

// FromEntry converts a zeroconf browse entry into a Result. Addresses are
// sorted so the output is deterministic for the same instance.
func FromEntry(entry *zeroconf.ServiceEntry) Result {
	addresses := make([]string, 0, len(entry.AddrIPv4)+len(entry.AddrIPv6))
	for _, ip := range entry.AddrIPv4 {
		addresses = append(addresses, ip.String())
	}
	for _, ip := range entry.AddrIPv6 {
		addresses = append(addresses, ip.String())
	}
	sort.Strings(addresses)

	return Result{
		Instance:  entry.Instance,
		Host:      entry.HostName,
		Addresses: addresses,
		Port:      entry.Port,
		TXT:       ParseTXT(entry.Text),
	}
}

// ParseTXT parses DNS-SD TXT strings ("key=value") into a map. A string
// without "=" keeps the whole string as its key with an empty value; empty
// strings are skipped.
func ParseTXT(txt []string) map[string]string {
	parsed := make(map[string]string, len(txt))
	for _, kv := range txt {
		if kv == "" {
			continue
		}
		key, value, found := strings.Cut(kv, "=")
		if !found {
			parsed[kv] = ""
			continue
		}
		parsed[key] = value
	}
	return parsed
}

// Browse queries the mDNS for serviceType until the context is done and
// returns the instances discovered, deduplicated and sorted for stable
// output. Entries announcing the same instance, host and port again (e.g.
// one record per address family) have their addresses merged.
func Browse(ctx context.Context, serviceType string) ([]Result, error) {
	resolver, err := zeroconf.NewResolver()
	if err != nil {
		return nil, fmt.Errorf("discover: new resolver: %w", err)
	}

	entries := make(chan *zeroconf.ServiceEntry, 16)
	if err := resolver.Browse(ctx, serviceType, mdns.Domain, entries); err != nil {
		return nil, fmt.Errorf("discover: browse %s: %w", serviceType, err)
	}

	var results []Result
	for entry := range entries {
		if entry != nil {
			results = append(results, FromEntry(entry))
		}
	}
	return dedup(results), nil
}

// dedup merges results sharing instance, host and port, then sorts by
// instance name so output order does not depend on network timing.
func dedup(results []Result) []Result {
	index := make(map[string]int, len(results))
	merged := make([]Result, 0, len(results))
	for _, r := range results {
		key := r.Instance + "|" + r.Host + "|" + strconv.Itoa(r.Port)
		if i, ok := index[key]; ok {
			merged[i].Addresses = union(merged[i].Addresses, r.Addresses)
			continue
		}
		index[key] = len(merged)
		merged = append(merged, r)
	}

	sort.Slice(merged, func(i, j int) bool {
		if merged[i].Instance != merged[j].Instance {
			return merged[i].Instance < merged[j].Instance
		}
		return merged[i].Host < merged[j].Host
	})
	return merged
}

// union appends the addresses of b not already present in a.
func union(a, b []string) []string {
	seen := make(map[string]struct{}, len(a))
	for _, addr := range a {
		seen[addr] = struct{}{}
	}
	for _, addr := range b {
		if _, ok := seen[addr]; ok {
			continue
		}
		seen[addr] = struct{}{}
		a = append(a, addr)
	}
	sort.Strings(a)
	return a
}
