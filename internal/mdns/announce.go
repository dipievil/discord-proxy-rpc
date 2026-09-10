// Package mdns implements mDNS/DNS-SD service advertisement for LAN
// discovery using github.com/grandcat/zeroconf.
package mdns

import (
	"fmt"
	"os"

	"github.com/grandcat/zeroconf"
	"go.uber.org/zap"

	"github.com/discord-proxy-rpc/discord-proxy-rpc/internal/config"
)

const (
	defaultServiceType = "_discord-proxy._tcp"
	fallbackName       = "discord-proxy"
	domain             = "local."
	txtAPIVersion      = "v=1"
)

// Advertiser registers the proxy service on the LAN via mDNS/DNS-SD so
// that clients can discover it automatically.
type Advertiser struct {
	server *zeroconf.Server
	cfg    config.MdnsConfig
	port   int
	logger *zap.Logger
}

// NewAdvertiser creates an Advertiser. It validates the port and applies
// defaults for missing fields in cfg.
func NewAdvertiser(cfg config.MdnsConfig, port int, logger *zap.Logger) (*Advertiser, error) {
	if port <= 0 || port > 65535 {
		return nil, fmt.Errorf("mdns: invalid port %d", port)
	}
	if cfg.ServiceType == "" {
		cfg.ServiceType = defaultServiceType
	}
	return &Advertiser{
		cfg:    cfg,
		port:   port,
		logger: logger,
	}, nil
}

// Advertise registers the service with mDNS. It blocks until the
// registration is sent.
func (a *Advertiser) Advertise() error {
	if a.server != nil {
		return fmt.Errorf("mdns: already advertising")
	}

	instance := a.resolveInstanceName()

	server, err := zeroconf.Register(
		instance,
		a.cfg.ServiceType,
		domain,
		a.port,
		[]string{txtAPIVersion},
		nil,
	)
	if err != nil {
		return fmt.Errorf("mdns: register: %w", err)
	}

	a.server = server
	a.logger.Info("mDNS service registered",
		zap.String("instance", instance),
		zap.String("type", a.cfg.ServiceType),
		zap.Int("port", a.port),
	)
	return nil
}

// Shutdown deregisters the mDNS service and releases resources. It is safe
// to call multiple times or before Advertise.
func (a *Advertiser) Shutdown() error {
	if a.server == nil {
		return nil
	}
	a.logger.Info("mDNS service shutting down")
	a.server.Shutdown()
	a.server = nil
	return nil
}

func (a *Advertiser) resolveInstanceName() string {
	if a.cfg.InstanceName != "" {
		return a.cfg.InstanceName
	}
	if h, err := os.Hostname(); err == nil && h != "" {
		return h
	}
	return fallbackName
}
