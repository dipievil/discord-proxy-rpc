package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"net"
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

// Set at build time with -ldflags "-X main.version=... -X main.commit=... -X main.date=...".
// goreleaser and the Makefile stamp all three; an unstamped build reports "dev".
var (
	version = "dev"
	commit  = "unknown"
	date    = "unknown"
)

// versionString is the single line printed by -version and logged at startup.
func versionString() string {
	return fmt.Sprintf("discord-proxy %s (commit %s, built %s)", version, commit, date)
}

func main() {
	showVersion := flag.Bool("version", false, "print the proxy version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println(versionString())
		return
	}

	cfg, err := config.LoadConfig()
	if err != nil {
		log.Fatalf("loading config: %v", err)
	}

	logger, err := config.SetupLogger(cfg.Logging.Level, cfg.Logging.Format)
	if err != nil {
		log.Fatalf("setting up logger: %v", err)
	}
	defer logger.Sync()

	logger.Info("discord-proxy-rpc starting",
		zap.String("version", version),
		zap.String("commit", commit),
		zap.String("built", date),
	)

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGTERM, syscall.SIGINT)
	defer stop()

	manager := ipc.NewManager(cfg.Discord.ClientID, cfg.Discord.MaxAccounts, logger)
	manager.Connect()
	defer manager.Close()

	presences := state.NewRegistry(cfg.Discord.CoalesceInterval, logger)
	presences.Start(ctx)
	defer presences.Stop()

	accounts := manager.Accounts()
	primary := manager.Primary()
	multiAccount := len(accounts) > 1

	hub := server.NewHub(logger)
	hub.ClientID = cfg.Discord.ClientID
	if multiAccount && primary != nil {
		hub.PrimaryAccount = primary.Name
	}
	go hub.Run(ctx)

	rateLimiter := server.NewRateLimiter(cfg.Server.RateLimitPerIP, cfg.Server.RateLimitWindow)

	ipcState := func() string {
		if primary := manager.Primary(); primary != nil {
			return primary.Client.State().String()
		}
		return ipc.StateDisconnected.String()
	}

	var metrics *server.Metrics
	if cfg.Metrics.Enabled {
		metrics = server.NewMetrics(hub, ipcState, rateLimiter)
	}

	for _, account := range accounts {
		presence := presences.For(account.Name)

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
			if multiAccount {
				msg.Account = account.Name
				msg.ClientID = cfg.Discord.ClientID
			}
			hub.Broadcast(msg)
		})
	}

	primarySnapshot := func() types.PresenceSnapshot {
		if primary == nil {
			return types.PresenceSnapshot{Source: types.SourceNone}
		}
		snapshot, ok := presences.Snapshot(primary.Name)
		if !ok {
			return types.PresenceSnapshot{Source: types.SourceNone}
		}
		return snapshot
	}

	listAccounts := func() []server.AccountStatus {
		tracked := manager.Accounts()
		out := make([]server.AccountStatus, 0, len(tracked))
		for _, account := range tracked {
			out = append(out, server.AccountStatus{
				Account: account.Name,
				State:   account.Client.State().String(),
			})
		}
		return out
	}

	opts := []server.ServerOption{
		server.WithGetCurrentPresence(primarySnapshot),
		server.WithGetAccountPresence(presences.Snapshot),
		server.WithListAccounts(listAccounts),
		server.WithGetIPCState(ipcState),
		server.WithWSPath(cfg.Server.WsPath),
		server.WithRateLimiter(rateLimiter),
		server.WithTrustedProxies(cfg.Server.TrustedProxies),
		server.WithConnectionTracker(server.NewConnectionTracker(cfg.Server.MaxWSConnections)),
	}
	if cfg.Auth.Enabled {
		opts = append(opts, server.WithAuth(server.BearerAuthFunc(cfg.Auth, logger)))
	}
	if cfg.Server.TLS.MTLSEnabled() {
		opts = append(opts, server.WithRequireClientCert())
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

	// Each account owns its own reconnect loop and event loop, so one account
	// dropping or exhausting its retries never stops another. ipcDone is closed
	// once that account will not open another session; its event loop then
	// emits one terminal disconnected for that account.
	for _, account := range accounts {
		presence := presences.For(account.Name)
		ipcDone := make(chan struct{})

		go runAccountEventLoop(ctx, account.Name, account.Client, presence, hub, ipcDone, multiAccount, logger)
		go func() {
			defer close(ipcDone)
			if err := account.Client.RunWithReconnect(ctx, cfg.Discord); err != nil && ctx.Err() == nil {
				logger.Warn("IPC client stopped", zap.String("account", account.Name), zap.Error(err))
			}
		}()
	}

	tlsCfg, err := server.NewTLSConfig(cfg.Server.TLS)
	if err != nil {
		logger.Fatal("configuring TLS", zap.Error(err))
	}

	if cfg.Mdns.Enabled {
		adv, err := mdns.NewAdvertiser(cfg.Mdns, cfg.Server.Port, logger)
		if err != nil {
			logger.Fatal("creating mDNS advertiser", zap.Error(err))
		}
		if err := adv.WithTLSMode(cfg.Server.TLS.Mode()).Advertise(); err != nil {
			logger.Fatal("advertising mDNS service", zap.Error(err))
		}
		defer adv.Shutdown()
	}

	ln, err := net.Listen("tcp", srv.Addr)
	if err != nil {
		logger.Fatal("creating listener", zap.String("addr", srv.Addr), zap.Error(err))
	}

	go func() {
		logger.Info("HTTP server starting",
			zap.String("addr", srv.Addr),
			zap.String("tls", cfg.Server.TLS.Mode()),
		)
		if err := server.Serve(srv, ln, tlsCfg); err != nil && err != http.ErrServerClosed {
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
	runAccountEventLoop(ctx, "", source, presence, hub, ipcDone, false, logger)
}

// runAccountEventLoop is runIPCEventLoop scoped to one account: presence updates
// flow into that account's state machine and state frames carry its identity
// when more than one account is tracked.
func runAccountEventLoop(ctx context.Context, account string, source ipcEventSource, presence *state.Presence, hub *server.Hub, ipcDone <-chan struct{}, multiAccount bool, logger *zap.Logger) {
	accountField := ""
	if multiAccount {
		accountField = account
	}
	stateMessage := func(status server.ConnectionState) server.ServerMessage {
		msg := server.NewStateMessage(status)
		msg.Account = accountField
		return msg
	}

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
				logger.Info("IPC session ended and will not reconnect", zap.String("account", account))
				if !hub.BroadcastWait(ctx, stateMessage(server.StateDisconnected)) {
					logger.Error("terminal disconnected state was not delivered", zap.String("account", account))
				}
				return
			case <-time.After(500 * time.Millisecond):
			}
			continue
		}

		hub.Broadcast(stateMessage(server.StateConnected))

		for event := range events {
			if event.Err != nil {
				logger.Debug("IPC event error", zap.Error(event.Err), zap.String("account", account))
				break
			}
			if event.Opcode == ipc.OpFrame && event.Frame != nil {
				activity, err := ipc.ParseActivityFrame(event.Frame)
				if err != nil {
					logger.Debug("failed to parse activity", zap.Error(err), zap.String("account", account))
					continue
				}
				presence.Update(activity)
			}
		}

		hub.Broadcast(stateMessage(server.StateReconnecting))
	}
}
