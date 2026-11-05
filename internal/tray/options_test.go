package tray

import (
	"strings"
	"testing"
	"time"
)

func TestResolveServer(t *testing.T) {
	t.Run("flag wins", func(t *testing.T) {
		t.Setenv("PROXY_URL", "http://env:1234")
		if got := ResolveServer("http://flag:9999"); got != "http://flag:9999" {
			t.Errorf("ResolveServer = %q, want the flag value", got)
		}
	})

	t.Run("env fallback", func(t *testing.T) {
		t.Setenv("PROXY_URL", "http://env:1234")
		if got := ResolveServer("  "); got != "http://env:1234" {
			t.Errorf("ResolveServer = %q, want $PROXY_URL", got)
		}
	})

	t.Run("default", func(t *testing.T) {
		t.Setenv("PROXY_URL", "")
		if got := ResolveServer(""); got != DefaultServer {
			t.Errorf("ResolveServer = %q, want %q", got, DefaultServer)
		}
	})
}

func TestResolveToken(t *testing.T) {
	t.Run("flag wins", func(t *testing.T) {
		t.Setenv("PROXY_TOKEN", "legacy")
		t.Setenv("PROXY_AUTH_TOKEN", "auth")
		if got := ResolveToken("flag"); got != "flag" {
			t.Errorf("ResolveToken = %q, want flag", got)
		}
	})

	t.Run("legacy PROXY_TOKEN wins over auth token", func(t *testing.T) {
		t.Setenv("PROXY_TOKEN", "legacy")
		t.Setenv("PROXY_AUTH_TOKEN", "auth")
		if got := ResolveToken(""); got != "legacy" {
			t.Errorf("ResolveToken = %q, want legacy", got)
		}
	})

	t.Run("PROXY_AUTH_TOKEN fallback", func(t *testing.T) {
		t.Setenv("PROXY_TOKEN", "")
		t.Setenv("PROXY_AUTH_TOKEN", "auth")
		if got := ResolveToken(""); got != "auth" {
			t.Errorf("ResolveToken = %q, want auth", got)
		}
	})

	t.Run("none", func(t *testing.T) {
		t.Setenv("PROXY_TOKEN", "")
		t.Setenv("PROXY_AUTH_TOKEN", "")
		if got := ResolveToken(""); got != "" {
			t.Errorf("ResolveToken = %q, want empty", got)
		}
	})
}

func TestNormalizeServer(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		want    string
		wantErr bool
	}{
		{name: "bare host and port", raw: "127.0.0.1:8765", want: "http://127.0.0.1:8765"},
		{name: "trailing slash removed", raw: "http://127.0.0.1:8765/", want: "http://127.0.0.1:8765"},
		{name: "path preserved", raw: "https://proxy.example/base/", want: "https://proxy.example/base"},
		{name: "spaces trimmed", raw: "  http://127.0.0.1:8765  ", want: "http://127.0.0.1:8765"},
		{name: "empty", raw: "", wantErr: true},
		{name: "bad scheme", raw: "ftp://127.0.0.1:8765", wantErr: true},
		{name: "no host", raw: "http://", wantErr: true},
		{name: "credentials", raw: "http://user:pass@127.0.0.1:8765", wantErr: true},
		{name: "query", raw: "http://127.0.0.1:8765/?token=x", wantErr: true},
		{name: "fragment", raw: "http://127.0.0.1:8765/#x", wantErr: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := NormalizeServer(test.raw)
			if test.wantErr {
				if err == nil {
					t.Fatalf("NormalizeServer(%q) = %q, want an error", test.raw, got)
				}
				return
			}
			if err != nil {
				t.Fatalf("NormalizeServer(%q): %v", test.raw, err)
			}
			if got != test.want {
				t.Errorf("NormalizeServer(%q) = %q, want %q", test.raw, got, test.want)
			}
		})
	}
}

func TestValidateInterval(t *testing.T) {
	tests := []struct {
		interval time.Duration
		wantErr  bool
	}{
		{DefaultInterval, false},
		{MinInterval, false},
		{MaxInterval, false},
		{MinInterval - time.Millisecond, true},
		{MaxInterval + time.Second, true},
		{0, true},
		{-time.Second, true},
	}
	for _, test := range tests {
		err := ValidateInterval(test.interval)
		if test.wantErr && err == nil {
			t.Errorf("ValidateInterval(%s) = nil, want an error", test.interval)
		}
		if !test.wantErr && err != nil {
			t.Errorf("ValidateInterval(%s) = %v, want nil", test.interval, err)
		}
	}
}

func TestResolveOptions(t *testing.T) {
	t.Setenv("PROXY_URL", "")
	t.Setenv("PROXY_TOKEN", "")
	t.Setenv("PROXY_AUTH_TOKEN", "")

	options, err := ResolveOptions("proxy.local:8765", "", 0)
	if err != nil {
		t.Fatalf("ResolveOptions: %v", err)
	}
	if options.Server != "http://proxy.local:8765" {
		t.Errorf("Server = %q, want http://proxy.local:8765", options.Server)
	}
	if options.Interval != DefaultInterval {
		t.Errorf("Interval = %s, want %s", options.Interval, DefaultInterval)
	}
	if options.Token != "" {
		t.Errorf("Token = %q, want empty", options.Token)
	}

	if _, err := ResolveOptions("proxy.local:8765", "", time.Millisecond); err == nil {
		t.Error("ResolveOptions accepted an interval below the minimum")
	}
	if _, err := ResolveOptions("ftp://proxy.local", "", DefaultInterval); err == nil {
		t.Error("ResolveOptions accepted an unsupported scheme")
	}
}

func TestResolveOptionsUsesEnvToken(t *testing.T) {
	t.Setenv("PROXY_URL", "")
	t.Setenv("PROXY_TOKEN", "")
	t.Setenv("PROXY_AUTH_TOKEN", "from-env")

	options, err := ResolveOptions("", "", DefaultInterval)
	if err != nil {
		t.Fatalf("ResolveOptions: %v", err)
	}
	if options.Token != "from-env" {
		t.Errorf("Token = %q, want from-env", options.Token)
	}
}

func TestOptionsRedacted(t *testing.T) {
	withToken := Options{Server: DefaultServer, Token: "super-secret", Interval: DefaultInterval}
	rendered := withToken.Redacted()
	if strings.Contains(rendered, "super-secret") {
		t.Errorf("Redacted leaked the token: %q", rendered)
	}
	if !strings.Contains(rendered, redactedToken) {
		t.Errorf("Redacted = %q, want %q", rendered, redactedToken)
	}

	withoutToken := Options{Server: DefaultServer, Interval: DefaultInterval}
	if !strings.Contains(withoutToken.Redacted(), noToken) {
		t.Errorf("Redacted = %q, want %q", withoutToken.Redacted(), noToken)
	}
}
