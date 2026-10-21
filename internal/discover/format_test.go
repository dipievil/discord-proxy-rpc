package discover

import (
	"bytes"
	"encoding/json"
	"strings"
	"testing"
)

func sampleResults() []Result {
	return []Result{
		{
			Instance:  "proxy-1",
			Host:      "proxy-1.local.",
			Addresses: []string{"192.168.0.10"},
			Port:      8765,
			TXT:       map[string]string{"v": "1", "client_id": "123"},
		},
		{
			Instance:  "proxy-2",
			Host:      "proxy-2.local.",
			Addresses: []string{"192.168.0.11", "fe80::1"},
			Port:      8766,
			TXT:       map[string]string{},
		},
	}
}

func TestWriteTable(t *testing.T) {
	var buf bytes.Buffer
	if err := WriteTable(&buf, sampleResults()); err != nil {
		t.Fatalf("WriteTable: %v", err)
	}
	out := buf.String()

	for _, want := range []string{"INSTANCE", "HOST", "ADDRESS", "PORT", "TXT", "proxy-1", "proxy-1.local.", "192.168.0.10", "8765", "v=1", "client_id=123", "proxy-2", "192.168.0.11,fe80::1", "8766"} {
		if !strings.Contains(out, want) {
			t.Errorf("table output missing %q:\n%s", want, out)
		}
	}
}

func TestWriteResultsEmptyTable(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := WriteResults(&stdout, &stderr, nil, false); err != nil {
		t.Fatalf("WriteResults: %v", err)
	}
	if !strings.Contains(stdout.String(), "no proxies found") {
		t.Errorf("stdout = %q, want \"no proxies found\"", stdout.String())
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want empty", stderr.String())
	}
}

func TestWriteResultsJSON(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := WriteResults(&stdout, &stderr, sampleResults(), true); err != nil {
		t.Fatalf("WriteResults: %v", err)
	}

	var out Output
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, stdout.String())
	}
	if out.Count != 2 {
		t.Errorf("Count = %d, want 2", out.Count)
	}
	if len(out.Proxies) != 2 || out.Proxies[0].Instance != "proxy-1" {
		t.Errorf("Proxies = %+v, want two entries starting with proxy-1", out.Proxies)
	}
	if stderr.Len() != 0 {
		t.Errorf("stderr = %q, want empty", stderr.String())
	}
}

func TestWriteResultsJSONEmpty(t *testing.T) {
	var stdout, stderr bytes.Buffer
	if err := WriteResults(&stdout, &stderr, nil, true); err != nil {
		t.Fatalf("WriteResults: %v", err)
	}

	var out Output
	if err := json.Unmarshal(stdout.Bytes(), &out); err != nil {
		t.Fatalf("stdout is not valid JSON: %v\n%s", err, stdout.String())
	}
	if out.Count != 0 {
		t.Errorf("Count = %d, want 0", out.Count)
	}
	if out.Proxies == nil || len(out.Proxies) != 0 {
		t.Errorf("Proxies = %v, want empty non-nil slice", out.Proxies)
	}
	if !strings.Contains(stderr.String(), "no proxies found") {
		t.Errorf("stderr = %q, want \"no proxies found\" note", stderr.String())
	}
}

func TestFormatTXT(t *testing.T) {
	cases := []struct {
		name string
		in   map[string]string
		want string
	}{
		{"empty", map[string]string{}, "-"},
		{"single", map[string]string{"v": "1"}, "v=1"},
		{"sorted", map[string]string{"v": "1", "client_id": "123"}, "client_id=123 v=1"},
		{"bare key", map[string]string{"flag": ""}, "flag"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := formatTXT(tc.in); got != tc.want {
				t.Errorf("formatTXT(%v) = %q, want %q", tc.in, got, tc.want)
			}
		})
	}
}

func TestFormatAddresses(t *testing.T) {
	if got := formatAddresses(nil); got != "-" {
		t.Errorf("formatAddresses(nil) = %q, want -", got)
	}
	if got := formatAddresses([]string{"a", "b"}); got != "a,b" {
		t.Errorf("formatAddresses = %q, want a,b", got)
	}
}
