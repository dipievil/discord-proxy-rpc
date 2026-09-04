package mdns

import (
	"os"
	"testing"

	"github.com/discord-proxy-rpc/discord-proxy-rpc/internal/config"
	"go.uber.org/zap"
)

func TestNewAdvertiser(t *testing.T) {
	cfg := config.MdnsConfig{
		Enabled:      true,
		InstanceName: "my-pc",
		ServiceType:  "_discord-proxy._tcp",
	}
	a, err := NewAdvertiser(cfg, 8765, zap.NewNop())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.port != 8765 {
		t.Errorf("port = %d, want 8765", a.port)
	}
	if a.cfg.InstanceName != "my-pc" {
		t.Errorf("instance = %q, want %q", a.cfg.InstanceName, "my-pc")
	}
}

func TestNewAdvertiserEmptyServiceType(t *testing.T) {
	cfg := config.MdnsConfig{ServiceType: ""}
	a, err := NewAdvertiser(cfg, 8765, zap.NewNop())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if a.cfg.ServiceType != defaultServiceType {
		t.Errorf("service type = %q, want %q", a.cfg.ServiceType, defaultServiceType)
	}
}

func TestNewAdvertiserInvalidPort(t *testing.T) {
	cases := []struct {
		name string
		port int
	}{
		{"zero", 0},
		{"negative", -1},
		{"too high", 70000},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, err := NewAdvertiser(config.MdnsConfig{}, tc.port, zap.NewNop())
			if err == nil {
				t.Errorf("expected error for port %d, got nil", tc.port)
			}
		})
	}
}

func TestInstanceDefaultHostname(t *testing.T) {
	cfg := config.MdnsConfig{InstanceName: ""}
	a, err := NewAdvertiser(cfg, 8765, zap.NewNop())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := a.resolveInstanceName()
	want, _ := os.Hostname()
	if want == "" {
		want = fallbackName
	}
	if got != want {
		t.Errorf("instance name = %q, want %q", got, want)
	}
}

func TestInstanceCustomName(t *testing.T) {
	cfg := config.MdnsConfig{InstanceName: "my-custom-name"}
	a, err := NewAdvertiser(cfg, 8765, zap.NewNop())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	got := a.resolveInstanceName()
	if got != "my-custom-name" {
		t.Errorf("instance name = %q, want %q", got, "my-custom-name")
	}
}

func TestShutdownNilServer(t *testing.T) {
	a, err := NewAdvertiser(config.MdnsConfig{}, 8765, zap.NewNop())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if err := a.Shutdown(); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
}

func TestAdvertiseWithoutNetwork(t *testing.T) {
	a, err := NewAdvertiser(config.MdnsConfig{
		ServiceType: "_test._tcp",
	}, 0, zap.NewNop())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	a.port = 19999

	err = a.Advertise()
	if err != nil {
		t.Fatalf("advertise failed: %v", err)
	}
	defer a.Shutdown()

	if a.server == nil {
		t.Error("server should not be nil after advertise")
	}
}

func TestDoubleAdvertise(t *testing.T) {
	a, err := NewAdvertiser(config.MdnsConfig{
		ServiceType: "_test._tcp",
	}, 0, zap.NewNop())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	a.port = 19998

	if err := a.Advertise(); err != nil {
		t.Fatalf("first advertise: %v", err)
	}
	defer a.Shutdown()

	err = a.Advertise()
	if err == nil {
		t.Error("expected error on double advertise, got nil")
	}
}
