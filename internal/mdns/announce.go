package mdns

import (
	"fmt"
	"os"

	"github.com/grandcat/zeroconf"
	"go.uber.org/zap"

	"github.com/discord-proxy-rpc/discord-proxy-rpc/internal/config"
)

const (
	// DefaultServiceType is the mDNS service type the proxy advertises
	// itself under. Discovery tools (cmd/discover) browse for it.
	DefaultServiceType = "_discord-proxy._tcp"
	// Domain is the mDNS domain the service is registered in.
	Domain = "local."
	// TXTAPIVersion is the TXT record (a single key=value string) that
	// carries the advertised API version.
	TXTAPIVersion = "v=1"
	// TXTTLS is the TXT key announcing the transport authentication mode:
	// "server" for server-side TLS, "mtls" for mutual TLS. Its presence tells
	// clients to connect with https/wss. DNS-SD has no scheme field, so this
	// record is the only scheme signal.
	TXTTLS = "tls"

	fallbackName = "discord-proxy"
)

var hostnameFunc = os.Hostname

type Advertiser struct {
	server  *zeroconf.Server
	cfg     config.MdnsConfig
	port    int
	tlsMode string
	logger  *zap.Logger
}

func NewAdvertiser(cfg config.MdnsConfig, port int, logger *zap.Logger) (*Advertiser, error) {
	if port <= 0 || port > 65535 {
		return nil, fmt.Errorf("mdns: invalid port %d", port)
	}
	if cfg.ServiceType == "" {
		cfg.ServiceType = DefaultServiceType
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

// WithTLSMode announces the transport scheme alongside the service. Call it
// before Advertise; config.TLSModeOff or an empty mode is not advertised, so
// discovery results keep meaning plain http unless the record says otherwise.
func (a *Advertiser) WithTLSMode(mode string) *Advertiser {
	a.tlsMode = mode
	return a
}

func (a *Advertiser) txtRecords() []string {
	txt := []string{TXTAPIVersion}
	if a.tlsMode != "" && a.tlsMode != config.TLSModeOff {
		txt = append(txt, TXTTLS+"="+a.tlsMode)
	}
	return txt
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
		Domain,
		a.port,
		a.txtRecords(),
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
		zap.String("tls", a.tlsMode),
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
