// Package discover browses the LAN for discord-proxy instances announced
// over mDNS (see internal/mdns) and turns raw zeroconf browse results into
// printable, machine-consumable results.
package discover

import (
	"context"
	"fmt"
	"net"
	"sort"
	"strings"

	"github.com/grandcat/zeroconf"
)

// Result is one discovered proxy instance. Addresses holds the dialable
// addresses; LinkLocal holds link-local IPv6 addresses, which are kept apart
// because they are not usable as endpoints without a zone.
type Result struct {
	Instance  string            `json:"instance"`
	Host      string            `json:"host"`
	Addresses []string          `json:"addresses"`
	LinkLocal []string          `json:"link_local,omitempty"`
	Port      int               `json:"port"`
	TXT       map[string]string `json:"txt"`
}

// FromEntry converts a zeroconf browse entry into a Result. Addresses and
// link-local addresses are sorted so the output is deterministic for the
// same instance.
func FromEntry(entry *zeroconf.ServiceEntry) Result {
	routable := make([]string, 0, len(entry.AddrIPv4)+len(entry.AddrIPv6))
	linkLocal := make([]string, 0, len(entry.AddrIPv6))

	collect := func(ip net.IP) {
		if ip == nil {
			return
		}
		if ip.IsLinkLocalUnicast() {
			linkLocal = append(linkLocal, ip.String())
			return
		}
		routable = append(routable, ip.String())
	}
	for _, ip := range entry.AddrIPv4 {
		collect(ip)
	}
	for _, ip := range entry.AddrIPv6 {
		collect(ip)
	}
	sort.Strings(routable)
	sort.Strings(linkLocal)

	return Result{
		Instance:  entry.Instance,
		Host:      entry.HostName,
		Addresses: routable,
		LinkLocal: linkLocal,
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

// browseFn starts a browse and returns the channel entries arrive on. It is a
// package variable so tests can substitute the network with a fake source.
var browseFn = func(ctx context.Context, serviceType, domain string) (<-chan *zeroconf.ServiceEntry, error) {
	resolver, err := zeroconf.NewResolver()
	if err != nil {
		return nil, fmt.Errorf("discover: new resolver: %w", err)
	}
	entries := make(chan *zeroconf.ServiceEntry, 16)
	if err := resolver.Browse(ctx, serviceType, domain, entries); err != nil {
		return nil, fmt.Errorf("discover: browse %s: %w", serviceType, err)
	}
	return entries, nil
}

// Browse queries the mDNS for serviceType in domain until ctx is done and
// returns the instances discovered, deduplicated and sorted for stable
// output. Entries announcing the same instance, host and port again (e.g.
// one record per address family) have their addresses and TXT records
// merged.
//
// ctx must carry a deadline (or be cancelled by the caller): browsing runs
// until cancellation, so a context with neither blocks the caller until the
// process exits.
func Browse(ctx context.Context, serviceType, domain string) ([]Result, error) {
	entries, err := browseFn(ctx, serviceType, domain)
	if err != nil {
		return nil, err
	}

	results := make([]Result, 0, 16)
	for entry := range entries {
		if entry != nil {
			results = append(results, FromEntry(entry))
		}
	}
	return dedup(results), nil
}

// entryKey identifies one announced instance. It is a struct rather than a
// concatenated string so that separators inside untrusted mDNS names cannot
// collide two different proxies.
type entryKey struct {
	instance string
	host     string
	port     int
}

// dedup merges results sharing instance, host and port, then sorts by
// instance name so output order does not depend on network timing.
func dedup(results []Result) []Result {
	index := make(map[entryKey]int, len(results))
	merged := make([]Result, 0, len(results))
	for _, r := range results {
		key := entryKey{instance: r.Instance, host: r.Host, port: r.Port}
		if i, ok := index[key]; ok {
			merged[i].Addresses = union(merged[i].Addresses, r.Addresses)
			merged[i].LinkLocal = union(merged[i].LinkLocal, r.LinkLocal)
			merged[i].TXT = mergeTXT(merged[i].TXT, r.TXT)
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

// mergeTXT adds the keys of b missing from a. a always comes from ParseTXT,
// so it is safe to mutate.
func mergeTXT(a, b map[string]string) map[string]string {
	if a == nil {
		a = make(map[string]string, len(b))
	}
	for k, v := range b {
		if _, ok := a[k]; !ok {
			a[k] = v
		}
	}
	return a
}
