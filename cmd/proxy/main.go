package main

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"time"

	"go.uber.org/zap"

	"github.com/discord-proxy-rpc/discord-proxy-rpc/internal/config"
	"github.com/discord-proxy-rpc/discord-proxy-rpc/internal/ipc"
	"github.com/discord-proxy-rpc/discord-proxy-rpc/internal/mdns"
	"github.com/discord-proxy-rpc/discord-proxy-rpc/internal/server"
	"github.com/discord-proxy-rpc/discord-proxy-rpc/internal/state"
	"github.com/discord-proxy-rpc/discord-proxy-rpc/pkg/types"
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
	defer logger.Sync()

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	client := ipc.New(cfg.Discord.ClientID, logger)
	if err := client.Connect(); err != nil {
		logger.Warn("initial IPC connect failed, will reconnect", zap.Error(err))
	}

	presence := state.NewPresence(cfg.Discord.CoalesceInterval, logger)
	presence.Start(ctx)
	defer presence.Stop()

	hub := server.NewHub(logger)
	hub.ClientID = cfg.Discord.ClientID
	go hub.Run(ctx)

	rateLimiter := server.NewRateLimiter(cfg.Server.RateLimitPerIP, cfg.Server.RateLimitWindow)

	var metrics *server.Metrics
	if cfg.Metrics.Enabled {
		metrics = server.NewMetrics(hub, func() string { return client.State().String() }, rateLimiter)
	}

	presence.Subscribe(func(update state.PresenceUpdate) {
		if metrics != nil {
			metrics.RecordPresenceSend()
		}
		activity := update.Activity
		msg, err := server.NewPresenceMessage(types.PresenceSnapshot{
			Source:   types.SourceIPC,
			Activity: &activity,
		})
		if err != nil {
			logger.Error("failed to create presence message", zap.Error(err))
			return
		}
		hub.Broadcast(msg)
	})

	opts := []server.ServerOption{
		server.WithGetCurrentPresence(presence.Snapshot),
		server.WithGetIPCState(func() string { return client.State().String() }),
		server.WithWSPath(cfg.Server.WsPath),
		server.WithRateLimiter(rateLimiter),
		server.WithTrustedProxies(cfg.Server.TrustedProxies),
		server.WithConnectionTracker(server.NewConnectionTracker(cfg.Server.MaxWSConnections)),
	}
	if cfg.Auth.Enabled {
		opts = append(opts, server.WithAuth(server.BearerAuthFunc(cfg.Auth, logger)))
	}
	if metrics != nil {
		opts = append(opts, server.WithMetrics(metrics))
		logger.Info("Prometheus metrics enabled", zap.String("path", "/metrics"))
	}

	httpServer := server.NewServer(hub, logger, opts...)
	defer httpServer.Close()

	srv := &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
		Handler:      httpServer,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
	}

	// ipcDone is closed once the IPC client gives up, i.e. when no further
	// session will be attempted; the event loop turns that into one terminal
	// disconnected state for WebSocket clients.
	ipcDone := make(chan struct{})
	go runIPCEventLoop(ctx, client, presence, hub, ipcDone, logger)
	go func() {
		defer close(ipcDone)
		if err := client.RunWithReconnect(ctx, cfg.Discord); err != nil && ctx.Err() == nil {
			logger.Warn("IPC client stopped", zap.Error(err))
		}
	}()

	if cfg.Mdns.Enabled {
		adv, err := mdns.NewAdvertiser(cfg.Mdns, cfg.Server.Port, logger)
		if err != nil {
			logger.Fatal("creating mDNS advertiser", zap.Error(err))
		}
		if err := adv.Advertise(); err != nil {
			logger.Fatal("advertising mDNS service", zap.Error(err))
		}
		defer adv.Shutdown()
	}

	go func() {
		logger.Info("HTTP server starting", zap.String("addr", srv.Addr))
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			logger.Fatal("HTTP server failed", zap.Error(err))
		}
	}()

	<-ctx.Done()
	logger.Info("shutting down")

	hub.Close()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("HTTP server shutdown error", zap.Error(err))
	}

	client.Close()
}

// ipcEventSource is the part of the IPC client the event loop depends on, so
// the loop can be driven by a test without a live Discord socket.
type ipcEventSource interface {
	Events() <-chan ipc.Event
}

// runIPCEventLoop mirrors IPC session events into the presence state machine and
// the WebSocket hub. A session end is announced as reconnecting; when ipcDone
// reports that no further session will be attempted, the loop broadcasts one
// terminal disconnected instead and returns.
func runIPCEventLoop(ctx context.Context, source ipcEventSource, presence *state.Presence, hub *server.Hub, ipcDone <-chan struct{}, logger *zap.Logger) {
	for {
		events := source.Events()
		if events == nil {
			select {
			case <-ctx.Done():
				return
			case <-ipcDone:
				if ctx.Err() != nil {
					return
				}
				logger.Info("IPC session ended and will not reconnect")
				if !hub.BroadcastWait(ctx, server.NewStateMessage(server.StateDisconnected)) {
					logger.Error("terminal disconnected state was not delivered")
				}
				return
			case <-time.After(500 * time.Millisecond):
			}
			continue
		}

		hub.Broadcast(server.NewStateMessage(server.StateConnected))

		for event := range events {
			if event.Err != nil {
				logger.Debug("IPC event error", zap.Error(event.Err))
				break
			}
			if event.Opcode == ipc.OpFrame && event.Frame != nil {
				activity, err := ipc.ParseActivityFrame(event.Frame)
				if err != nil {
					logger.Debug("failed to parse activity", zap.Error(err))
					continue
				}
				presence.Update(activity)
			}
		}

		hub.Broadcast(server.NewStateMessage(server.StateReconnecting))
	}
}
