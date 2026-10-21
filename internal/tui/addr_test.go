package tui

import "testing"

func TestResolveAddr(t *testing.T) {
	t.Run("flag wins", func(t *testing.T) {
		t.Setenv("PROXY_HOST", "10.0.0.9")
		t.Setenv("PROXY_PORT", "9999")
		if got := ResolveAddr("proxy.example:1234"); got != "proxy.example:1234" {
			t.Fatalf("ResolveAddr(flag) = %q, want flag value", got)
		}
	})

	t.Run("host and port env", func(t *testing.T) {
		t.Setenv("PROXY_HOST", "192.168.0.103")
		t.Setenv("PROXY_PORT", "9000")
		if got := ResolveAddr(""); got != "192.168.0.103:9000" {
			t.Fatalf("ResolveAddr() = %q, want env-derived address", got)
		}
	})

	t.Run("falls back to server port env", func(t *testing.T) {
		t.Setenv("PROXY_HOST", "")
		t.Setenv("PROXY_PORT", "")
		t.Setenv("PROXY_SERVER_PORT", "8766")
		if got := ResolveAddr(""); got != "127.0.0.1:8766" {
			t.Fatalf("ResolveAddr() = %q, want 127.0.0.1:8766", got)
		}
	})

	t.Run("default", func(t *testing.T) {
		t.Setenv("PROXY_HOST", "")
		t.Setenv("PROXY_PORT", "")
		t.Setenv("PROXY_SERVER_PORT", "")
		if got := ResolveAddr(""); got != "127.0.0.1:8765" {
			t.Fatalf("ResolveAddr() = %q, want 127.0.0.1:8765", got)
		}
	})
}

func TestResolveToken(t *testing.T) {
	t.Run("flag wins", func(t *testing.T) {
		t.Setenv("PROXY_AUTH_TOKEN", "env-token")
		if got := ResolveToken("flag-token"); got != "flag-token" {
			t.Fatalf("ResolveToken(flag) = %q, want flag value", got)
		}
	})

	t.Run("legacy env wins over auth env", func(t *testing.T) {
		t.Setenv("PROXY_AUTH_TOKEN", "env-token")
		t.Setenv("PROXY_TOKEN", "legacy")
		if got := ResolveToken(""); got != "legacy" {
			t.Fatalf("ResolveToken() = %q, want legacy token to win", got)
		}
	})

	t.Run("auth env when no legacy", func(t *testing.T) {
		t.Setenv("PROXY_AUTH_TOKEN", "env-token")
		t.Setenv("PROXY_TOKEN", "")
		if got := ResolveToken(""); got != "env-token" {
			t.Fatalf("ResolveToken() = %q, want env token", got)
		}
	})
}

func TestResolveWSPath(t *testing.T) {
	t.Setenv("PROXY_SERVER_WS_PATH", "")
	if got := ResolveWSPath(""); got != "/ws" {
		t.Fatalf("ResolveWSPath() = %q, want /ws", got)
	}
	t.Setenv("PROXY_SERVER_WS_PATH", "/custom")
	if got := ResolveWSPath(""); got != "/custom" {
		t.Fatalf("ResolveWSPath() = %q, want /custom", got)
	}
	if got := ResolveWSPath("/flag"); got != "/flag" {
		t.Fatalf("ResolveWSPath(flag) = %q, want flag value", got)
	}
}

func TestWSURL(t *testing.T) {
	cases := []struct {
		addr, path, want string
	}{
		{"127.0.0.1:8765", "/ws", "ws://127.0.0.1:8765/ws"},
		{"127.0.0.1:8765", "ws", "ws://127.0.0.1:8765/ws"},
		{"192.168.0.103:9000", "/custom", "ws://192.168.0.103:9000/custom"},
		{"wss://proxy.example.com/ws", "/ignored", "wss://proxy.example.com/ws"},
		{"ws://proxy.example.com:1/ws", "/ignored", "ws://proxy.example.com:1/ws"},
	}
	for _, tc := range cases {
		if got := WSURL(tc.addr, tc.path); got != tc.want {
			t.Errorf("WSURL(%q, %q) = %q, want %q", tc.addr, tc.path, got, tc.want)
		}
	}
}
