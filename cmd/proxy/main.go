package main

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"syscall"

	"github.com/discord-proxy-rpc/discord-proxy-rpc/internal/config"
	"go.uber.org/zap"
)

func main() {
	cfg, err := config.LoadConfig()
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to load config: %v\n", err)
		os.Exit(1)
	}

	logger, err := config.SetupLogger(cfg.Logging.Level, cfg.Logging.Format)
	if err != nil {
		fmt.Fprintf(os.Stderr, "failed to setup logger: %v\n", err)
		os.Exit(1)
	}
	defer logger.Sync()

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger.Info("starting discord-proxy-rpc",
		zap.String("host", cfg.Server.Host),
		zap.Int("port", cfg.Server.Port),
		zap.Bool("mdns", cfg.Mdns.Enabled),
		zap.Bool("auth", cfg.Auth.Enabled),
	)

	// TODO: wire up IPC client (gopresence)
	// TODO: wire up activity state machine
	// TODO: wire up WebSocket server
	// TODO: wire up mDNS advertisement

	<-ctx.Done()
	logger.Info("shutting down")
}
