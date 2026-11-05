package tray

import (
	"errors"
	"fmt"
	"net/url"
	"os"
	"strings"
	"time"
)

const (
	// DefaultServer is the proxy the tray polls when nothing else is given.
	DefaultServer = "http://127.0.0.1:8765"
	// DefaultInterval is how often the tray polls /api/state by default.
	DefaultInterval = 5 * time.Second
	// MinInterval keeps the tray from hammering the proxy.
	MinInterval = time.Second
	// MaxInterval keeps a stale icon from looking like a live one.
	MaxInterval = 10 * time.Minute

	redactedToken = "<redacted>"
	noToken       = "<none>"
)

// Options is the resolved tray configuration.
type Options struct {
	Server   string
	Token    string
	Interval time.Duration
}

// ResolveServer picks the proxy base URL with flag > $PROXY_URL > default
// precedence.
func ResolveServer(flagValue string) string {
	if v := strings.TrimSpace(flagValue); v != "" {
		return v
	}
	if v := strings.TrimSpace(os.Getenv("PROXY_URL")); v != "" {
		return v
	}
	return DefaultServer
}

// ResolveToken picks the bearer token with the same precedence as the other
// proxy clients: the -token flag, else $PROXY_TOKEN (legacy, wins when set),
// else $PROXY_AUTH_TOKEN.
func ResolveToken(flagValue string) string {
	if flagValue != "" {
		return flagValue
	}
	if legacy := os.Getenv("PROXY_TOKEN"); legacy != "" {
		return legacy
	}
	return os.Getenv("PROXY_AUTH_TOKEN")
}

// NormalizeServer validates a proxy base URL and returns it without a trailing
// slash. A bare host:port is assumed to be plain HTTP.
func NormalizeServer(raw string) (string, error) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return "", errors.New("server URL is empty")
	}
	if !strings.Contains(raw, "://") {
		raw = "http://" + raw
	}

	parsed, err := url.Parse(raw)
	if err != nil {
		return "", fmt.Errorf("invalid server URL %q: %w", raw, err)
	}
	switch parsed.Scheme {
	case "http", "https":
	default:
		return "", fmt.Errorf("unsupported server URL scheme %q: want http or https", parsed.Scheme)
	}
	if parsed.Host == "" {
		return "", fmt.Errorf("server URL %q has no host", raw)
	}
	if parsed.User != nil {
		return "", errors.New("server URL must not embed credentials: pass the token with -token or $PROXY_TOKEN")
	}
	if parsed.RawQuery != "" || parsed.Fragment != "" {
		return "", fmt.Errorf("server URL %q must not carry a query or fragment", raw)
	}

	parsed.Path = strings.TrimSuffix(parsed.Path, "/")
	parsed.RawPath = ""
	return parsed.String(), nil
}

// ValidateInterval enforces the polling interval bounds.
func ValidateInterval(interval time.Duration) error {
	if interval < MinInterval {
		return fmt.Errorf("interval %s is too short: minimum is %s", interval, MinInterval)
	}
	if interval > MaxInterval {
		return fmt.Errorf("interval %s is too long: maximum is %s", interval, MaxInterval)
	}
	return nil
}

// ResolveOptions resolves, defaults and validates the CLI configuration.
func ResolveOptions(serverFlag, tokenFlag string, interval time.Duration) (Options, error) {
	server, err := NormalizeServer(ResolveServer(serverFlag))
	if err != nil {
		return Options{}, err
	}
	if interval == 0 {
		interval = DefaultInterval
	}
	if err := ValidateInterval(interval); err != nil {
		return Options{}, err
	}
	return Options{
		Server:   server,
		Token:    ResolveToken(tokenFlag),
		Interval: interval,
	}, nil
}

// Redacted renders the configuration for logs without ever revealing the
// token.
func (o Options) Redacted() string {
	token := noToken
	if o.Token != "" {
		token = redactedToken
	}
	return fmt.Sprintf("server=%s token=%s interval=%s", o.Server, token, o.Interval)
}
