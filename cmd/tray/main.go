// Command tray runs a desktop system-tray indicator for the proxy's Discord
// IPC state on Linux (StatusNotifierItem over D-Bus). Other platforms get a
// stub that compiles and exits with a clear "not supported" message.
//
// Usage:
//
//	go run ./cmd/tray [-server http://127.0.0.1:8765] [-token <token>] [-interval 5s]
//
// Exit codes: 0 on a clean shutdown, 1 when the proxy cannot be watched or no
// tray backend is available, 2 on a usage or configuration error.
package main

import (
	"context"
	"errors"
	"flag"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/discord-proxy-rpc/discord-proxy-rpc/internal/tray"
)

// version is set at release time with -ldflags "-X main.version=...". It is
// deliberately self-contained: the tray does not depend on the proxy's version
// wiring.
var version = "dev"

func main() {
	server := flag.String("server", "", "proxy base URL (default $PROXY_URL or "+tray.DefaultServer+")")
	token := flag.String("token", "", "bearer token (default $PROXY_TOKEN or $PROXY_AUTH_TOKEN)")
	interval := flag.Duration("interval", tray.DefaultInterval, "how often to poll the proxy's state")
	showVersion := flag.Bool("version", false, "print the tray version and exit")
	flag.Parse()

	if *showVersion {
		fmt.Println("discord-proxy-tray " + version)
		return
	}

	opts, err := tray.ResolveOptions(*server, *token, *interval)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tray: %v\n", err)
		flag.Usage()
		os.Exit(2)
	}

	client, err := tray.NewClient(opts.Server, opts.Token, nil)
	if err != nil {
		fmt.Fprintf(os.Stderr, "tray: %v\n", err)
		os.Exit(2)
	}

	notifier, err := tray.NewNotifier()
	if err != nil {
		if errors.Is(err, tray.ErrUnsupportedPlatform) {
			fmt.Fprintf(os.Stderr, "tray: %v: the status icon needs a Linux session bus with StatusNotifierItem\n", err)
		} else {
			fmt.Fprintf(os.Stderr, "tray: %v\n", err)
		}
		os.Exit(1)
	}

	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	app, err := tray.NewApp(client, notifier, opts.Interval, logger)
	if err != nil {
		_ = notifier.Close()
		fmt.Fprintf(os.Stderr, "tray: %v\n", err)
		os.Exit(2)
	}

	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()

	logger.Info("tray started", "server", opts.Server, "interval", opts.Interval)
	runErr := app.Run(ctx)
	if closeErr := notifier.Close(); runErr == nil {
		runErr = closeErr
	}
	if runErr != nil {
		fmt.Fprintf(os.Stderr, "tray: %v\n", runErr)
		os.Exit(1)
	}
}
