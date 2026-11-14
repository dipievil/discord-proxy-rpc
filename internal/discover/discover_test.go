package discover

import (
	"context"
	"net"
	"reflect"
	"runtime"
	"testing"
	"time"

	"github.com/grandcat/zeroconf"
)

func fakeEntry() *zeroconf.ServiceEntry {
	e := zeroconf.NewServiceEntry("proxy-1", "_discord-proxy._tcp", "local.")
	e.HostName = "proxy-1.local."
	e.Port = 8765
	e.Text = []string{"v=1"}
	e.AddrIPv4 = []net.IP{net.ParseIP("192.168.0.10")}
	return e
}

func TestFromEntry(t *testing.T) {
	r := FromEntry(fakeEntry())

	if r.Instance != "proxy-1" {
		t.Errorf("Instance = %q, want proxy-1", r.Instance)
	}
	if r.Host != "proxy-1.local." {
		t.Errorf("Host = %q, want proxy-1.local.", r.Host)
	}
	if r.Port != 8765 {
		t.Errorf("Port = %d, want 8765", r.Port)
	}
	if len(r.Addresses) != 1 || r.Addresses[0] != "192.168.0.10" {
		t.Errorf("Addresses = %v, want [192.168.0.10]", r.Addresses)
	}
	if len(r.LinkLocal) != 0 {
		t.Errorf("LinkLocal = %v, want empty", r.LinkLocal)
	}
	if r.TXT["v"] != "1" {
		t.Errorf("TXT[v] = %q, want 1", r.TXT["v"])
	}
}

func TestFromEntrySortsAddresses(t *testing.T) {
	e := fakeEntry()
	e.AddrIPv4 = []net.IP{
		net.ParseIP("192.168.0.20"),
		net.ParseIP("192.168.0.10"),
	}
	r := FromEntry(e)
	want := []string{"192.168.0.10", "192.168.0.20"}
	if !reflect.DeepEqual(r.Addresses, want) {
		t.Errorf("Addresses = %v, want %v", r.Addresses, want)
	}
}

func TestFromEntrySeparatesLinkLocal(t *testing.T) {
	cases := []struct {
		name      string
		ipv4      []net.IP
		ipv6      []net.IP
		addresses []string
		linkLocal []string
	}{
		{
			name:      "ipv4 only",
			ipv4:      []net.IP{net.ParseIP("192.168.0.10")},
			addresses: []string{"192.168.0.10"},
			linkLocal: []string{},
		},
		{
			name:      "global ipv6",
			ipv6:      []net.IP{net.ParseIP("2001:db8::1")},
			addresses: []string{"2001:db8::1"},
			linkLocal: []string{},
		},
		{
			name:      "link-local ipv6 only",
			ipv6:      []net.IP{net.ParseIP("fe80::1")},
			addresses: []string{},
			linkLocal: []string{"fe80::1"},
		},
		{
			name:      "mixed",
			ipv4:      []net.IP{net.ParseIP("192.168.0.10")},
			ipv6:      []net.IP{net.ParseIP("fe80::1"), net.ParseIP("2001:db8::1")},
			addresses: []string{"192.168.0.10", "2001:db8::1"},
			linkLocal: []string{"fe80::1"},
		},
		{
			name:      "empty",
			addresses: []string{},
			linkLocal: []string{},
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			e := zeroconf.NewServiceEntry("proxy", "_discord-proxy._tcp", "local.")
			e.HostName = "proxy.local."
			e.Port = 8765
			e.AddrIPv4 = tc.ipv4
			e.AddrIPv6 = tc.ipv6

			r := FromEntry(e)
			if !reflect.DeepEqual(r.Addresses, tc.addresses) {
				t.Errorf("Addresses = %v, want %v", r.Addresses, tc.addresses)
			}
			if !reflect.DeepEqual(r.LinkLocal, tc.linkLocal) {
				t.Errorf("LinkLocal = %v, want %v", r.LinkLocal, tc.linkLocal)
			}
		})
	}
}

func TestFromEntryNoAddresses(t *testing.T) {
	e := zeroconf.NewServiceEntry("proxy-2", "_discord-proxy._tcp", "local.")
	e.HostName = "proxy-2.local."
	e.Port = 8765
	r := FromEntry(e)
	if len(r.Addresses) != 0 {
		t.Errorf("Addresses = %v, want empty", r.Addresses)
	}
	if r.TXT == nil || len(r.TXT) != 0 {
		t.Errorf("TXT = %v, want empty map", r.TXT)
	}
}

func TestParseTXT(t *testing.T) {
	cases := []struct {
		name string
		in   []string
		want map[string]string
	}{
		{"empty", nil, map[string]string{}},
		{"version", []string{"v=1"}, map[string]string{"v": "1"}},
		{"multiple", []string{"v=1", "client_id=123"}, map[string]string{"v": "1", "client_id": "123"}},
		{"bare key", []string{"flag"}, map[string]string{"flag": ""}},
		{"value with equals", []string{"k=a=b"}, map[string]string{"k": "a=b"}},
		{"skips empty", []string{"", "v=1"}, map[string]string{"v": "1"}},
		{"duplicate key last wins", []string{"v=1", "v=2"}, map[string]string{"v": "2"}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := ParseTXT(tc.in)
			if !reflect.DeepEqual(got, tc.want) {
				t.Errorf("ParseTXT(%v) = %v, want %v", tc.in, got, tc.want)
			}
		})
	}
}

func TestDedupMergesAddressesAndSorts(t *testing.T) {
	a := Result{Instance: "b-proxy", Host: "b.local.", Port: 8765, Addresses: []string{"10.0.0.2"}}
	b := Result{Instance: "a-proxy", Host: "a.local.", Port: 8765, Addresses: []string{"10.0.0.1"}}
	c := Result{Instance: "b-proxy", Host: "b.local.", Port: 8765, Addresses: []string{"10.0.0.3"}}

	got := dedup([]Result{a, b, c})

	if len(got) != 2 {
		t.Fatalf("dedup() len = %d, want 2", len(got))
	}
	if got[0].Instance != "a-proxy" {
		t.Errorf("got[0].Instance = %q, want a-proxy (sorted)", got[0].Instance)
	}
	if got[1].Instance != "b-proxy" {
		t.Errorf("got[1].Instance = %q, want b-proxy", got[1].Instance)
	}
	wantAddrs := []string{"10.0.0.2", "10.0.0.3"}
	if !reflect.DeepEqual(got[1].Addresses, wantAddrs) {
		t.Errorf("merged addresses = %v, want %v", got[1].Addresses, wantAddrs)
	}
}

func TestDedupMergesTXT(t *testing.T) {
	a := Result{
		Instance: "proxy", Host: "p.local.", Port: 8765,
		TXT: map[string]string{"v": "1"},
	}
	b := Result{
		Instance: "proxy", Host: "p.local.", Port: 8765,
		TXT: map[string]string{"v": "1", "client_id": "123"},
	}

	got := dedup([]Result{a, b})

	if len(got) != 1 {
		t.Fatalf("dedup() len = %d, want 1", len(got))
	}
	want := map[string]string{"v": "1", "client_id": "123"}
	if !reflect.DeepEqual(got[0].TXT, want) {
		t.Errorf("merged TXT = %v, want %v", got[0].TXT, want)
	}
}

func TestDedupMergesLinkLocal(t *testing.T) {
	a := Result{Instance: "proxy", Host: "p.local.", Port: 8765, LinkLocal: []string{"fe80::1"}}
	b := Result{Instance: "proxy", Host: "p.local.", Port: 8765, LinkLocal: []string{"fe80::2"}}

	got := dedup([]Result{a, b})

	if len(got) != 1 {
		t.Fatalf("dedup() len = %d, want 1", len(got))
	}
	want := []string{"fe80::1", "fe80::2"}
	if !reflect.DeepEqual(got[0].LinkLocal, want) {
		t.Errorf("merged LinkLocal = %v, want %v", got[0].LinkLocal, want)
	}
}

func TestDedupKeepsDifferentPorts(t *testing.T) {
	a := Result{Instance: "proxy", Host: "p.local.", Port: 8765}
	b := Result{Instance: "proxy", Host: "p.local.", Port: 9000}
	got := dedup([]Result{a, b})
	if len(got) != 2 {
		t.Errorf("dedup() len = %d, want 2 (different ports are distinct)", len(got))
	}
}

func TestDedupDoesNotCollideOnSeparatorInNames(t *testing.T) {
	a := Result{Instance: "a|b.local.", Host: "p.local.", Port: 8765}
	b := Result{Instance: "a", Host: "b.local.|p.local.", Port: 8765}

	got := dedup([]Result{a, b})

	if len(got) != 2 {
		t.Errorf("dedup() len = %d, want 2 (crafted names must not collide)", len(got))
	}
}

func TestDedupEmpty(t *testing.T) {
	got := dedup(nil)
	if len(got) != 0 {
		t.Errorf("dedup(nil) len = %d, want 0", len(got))
	}
}

func TestUnion(t *testing.T) {
	got := union([]string{"a", "b"}, []string{"b", "c", "a"})
	want := []string{"a", "b", "c"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("union() = %v, want %v", got, want)
	}
}

// stubBrowse replaces the network with a channel that the given send function
// owns and closes when done.
func stubBrowse(t *testing.T, entries func(ctx context.Context, ch chan<- *zeroconf.ServiceEntry)) {
	t.Helper()
	orig := browseFn
	t.Cleanup(func() { browseFn = orig })
	browseFn = func(ctx context.Context, _, _ string) (<-chan *zeroconf.ServiceEntry, error) {
		ch := make(chan *zeroconf.ServiceEntry, 16)
		go entries(ctx, ch)
		return ch, nil
	}
}

func TestBrowseReturnsPromptlyOnCancellation(t *testing.T) {
	// The fake source only closes its channel once the context is cancelled,
	// which is how zeroconf behaves: Browse must then return an empty,
	// non-nil result instead of hanging.
	stubBrowse(t, func(ctx context.Context, ch chan<- *zeroconf.ServiceEntry) {
		<-ctx.Done()
		close(ch)
	})

	before := runtime.NumGoroutine()

	ctx, cancel := context.WithTimeout(context.Background(), 50*time.Millisecond)
	defer cancel()

	type result struct {
		results []Result
		err     error
	}
	done := make(chan result, 1)
	go func() {
		results, err := Browse(ctx, "_discord-proxy._tcp", "local.")
		done <- result{results, err}
	}()

	select {
	case got := <-done:
		if got.err != nil {
			t.Fatalf("Browse: %v", got.err)
		}
		if got.results == nil {
			t.Error("results = nil, want empty non-nil slice")
		}
		if len(got.results) != 0 {
			t.Errorf("results = %v, want empty", got.results)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Browse did not return after context cancellation")
	}

	if after := waitGoroutines(before); after > before {
		t.Errorf("goroutines grew from %d to %d after Browse", before, after)
	}
}

// waitGoroutines waits briefly for goroutines started by Browse to finish, so
// the count is not read while the fake source is still unwinding.
func waitGoroutines(before int) int {
	deadline := time.Now().Add(time.Second)
	for {
		after := runtime.NumGoroutine()
		if after <= before || time.Now().After(deadline) {
			return after
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestBrowseConsumesUntilChannelClosed(t *testing.T) {
	stubBrowse(t, func(ctx context.Context, ch chan<- *zeroconf.ServiceEntry) {
		e := zeroconf.NewServiceEntry("proxy", "_discord-proxy._tcp", "local.")
		e.HostName = "proxy.local."
		e.Port = 8765
		e.AddrIPv4 = []net.IP{net.ParseIP("192.168.0.10")}
		ch <- e
		ch <- nil
		close(ch)
	})

	results, err := Browse(context.Background(), "_discord-proxy._tcp", "local.")
	if err != nil {
		t.Fatalf("Browse: %v", err)
	}
	if len(results) != 1 || results[0].Instance != "proxy" {
		t.Errorf("results = %+v, want one proxy entry", results)
	}
}

func TestBrowsePropagatesSourceError(t *testing.T) {
	orig := browseFn
	t.Cleanup(func() { browseFn = orig })
	browseFn = func(context.Context, string, string) (<-chan *zeroconf.ServiceEntry, error) {
		return nil, context.DeadlineExceeded
	}

	results, err := Browse(context.Background(), "_discord-proxy._tcp", "local.")
	if err == nil {
		t.Fatal("Browse error = nil, want the source error")
	}
	if results != nil {
		t.Errorf("results = %v, want nil on error", results)
	}
}
