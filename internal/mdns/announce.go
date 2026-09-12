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

var hostnameFunc = os.Hostname

type Advertiser struct {
	server *zeroconf.Server
	cfg    config.MdnsConfig
	port   int
	logger *zap.Logger
}

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

func (a *Advertiser) IsEnabled() bool {
	return a.cfg.Enabled
}

func (a *Advertiser) Advertise() error {
	if !a.cfg.Enabled {
		return nil
	}
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
	if h, err := hostnameFunc(); err == nil && h != "" {
		return h
	}
	return fallbackName
}
