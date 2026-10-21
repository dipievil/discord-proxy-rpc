package discover

import (
	"net"
	"reflect"
	"testing"

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

func TestDedupKeepsDifferentPorts(t *testing.T) {
	a := Result{Instance: "proxy", Host: "p.local.", Port: 8765}
	b := Result{Instance: "proxy", Host: "p.local.", Port: 9000}
	got := dedup([]Result{a, b})
	if len(got) != 2 {
		t.Errorf("dedup() len = %d, want 2 (different ports are distinct)", len(got))
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
