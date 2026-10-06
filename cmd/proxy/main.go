package main

import (
	"context"
	"crypto/subtle"
	"fmt"
	"log"
	"net/http"
	"os/signal"
	"strings"
	"syscall"
	"time"

	"go.uber.org/zap"

	"github.com/discord-proxy-rpc/discord-proxy-rpc/internal/config"
	"github.com/discord-proxy-rpc/discord-proxy-rpc/internal/ipc"
	"github.com/discord-proxy-rpc/discord-proxy-rpc/internal/mdns"
	"github.com/discord-proxy-rpc/discord-proxy-rpc/internal/server"
	"github.com/discord-proxy-rpc/discord-proxy-rpc/internal/state"
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
	go hub.Run(ctx)

	presence.Subscribe(func(update state.PresenceUpdate) {
		msg, err := server.NewPresenceMessage(update.Activity)
		if err != nil {
			logger.Error("failed to create presence message", zap.Error(err))
			return
		}
		hub.Broadcast(msg)
	})

	opts := []server.ServerOption{
		server.WithGetCurrentPresence(presence.Current),
		server.WithGetIPCState(func() string { return client.State().String() }),
		server.WithWSPath(cfg.Server.WsPath),
	}
	if cfg.Auth.Enabled {
		opts = append(opts, server.WithAuth(newAuthFunc(cfg.Auth.Token)))
	}

	httpServer := server.NewServer(hub, logger, opts...)

	srv := &http.Server{
		Addr:         fmt.Sprintf("%s:%d", cfg.Server.Host, cfg.Server.Port),
		Handler:      httpServer,
		ReadTimeout:  cfg.Server.ReadTimeout,
		WriteTimeout: cfg.Server.WriteTimeout,
	}

	go runIPCEventLoop(ctx, client, presence, hub, logger)
	go client.RunWithReconnect(ctx, cfg.Discord)

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

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := srv.Shutdown(shutdownCtx); err != nil {
		logger.Error("HTTP server shutdown error", zap.Error(err))
	}

	client.Close()
}

func runIPCEventLoop(ctx context.Context, client *ipc.Client, presence *state.Presence, hub *server.Hub, logger *zap.Logger) {
	for {
		events := client.Events()
		if events == nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(500 * time.Millisecond):
				continue
			}
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

func newAuthFunc(token string) func(*http.Request) bool {
	return func(r *http.Request) bool {
		auth := r.Header.Get("Authorization")
		if auth == "" {
			return false
		}
		parts := strings.SplitN(auth, " ", 2)
		if len(parts) != 2 || !strings.EqualFold(parts[0], "bearer") {
			return false
		}
		t := strings.TrimSpace(parts[1])
		if t == "" {
			return false
		}
		return subtle.ConstantTimeCompare([]byte(t), []byte(token)) == 1
	}
}
