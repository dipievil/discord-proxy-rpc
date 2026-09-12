package main

import (
	"context"
	"log"

	"go.uber.org/zap"

	"github.com/discord-proxy-rpc/discord-proxy-rpc/internal/config"
	"github.com/discord-proxy-rpc/discord-proxy-rpc/internal/mdns"
)

func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("loading config: %v", err)
	}

	logger, err := config.SetupLogger(cfg.Logging.Level, cfg.Logging.Format)
	if err != nil {
		log.Fatalf("setting up logger: %v", err)
	}

	if !cfg.Mdns.Enabled {
		logger.Info("mDNS advertisement disabled, exiting")
		return
	}

	adv, err := mdns.NewAdvertiser(cfg.Mdns, cfg.Server.Port, logger)
	if err != nil {
		logger.Fatal("creating mDNS advertiser", zap.Error(err))
	}

	if err := adv.Advertise(); err != nil {
		logger.Fatal("advertising mDNS service", zap.Error(err))
	}

	// DeregisterOnSignal blocks until SIGTERM/SIGINT or context
	// cancellation, then deregisters the mDNS service so the proxy
	// disappears from the LAN on shutdown.
	mdns.DeregisterOnSignal(context.Background(), adv, logger)
}