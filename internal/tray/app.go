package tray

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

// App ties a state fetcher to a notifier: it polls the proxy and pushes every
// status transition to the tray backend.
type App struct {
	fetcher   Fetcher
	notifier  Notifier
	interval  time.Duration
	logger    *slog.Logger
	newTicker func(interval time.Duration) ticker
}

// NewApp validates its inputs and returns a tray application ready for Run.
func NewApp(fetcher Fetcher, notifier Notifier, interval time.Duration, logger *slog.Logger) (*App, error) {
	if fetcher == nil {
		return nil, errors.New("tray app needs a state fetcher")
	}
	if notifier == nil {
		return nil, errors.New("tray app needs a notifier")
	}
	if err := ValidateInterval(interval); err != nil {
		return nil, err
	}
	if logger == nil {
		logger = slog.Default()
	}
	return &App{
		fetcher:   fetcher,
		notifier:  notifier,
		interval:  interval,
		logger:    logger,
		newTicker: newRealTicker,
	}, nil
}

// Run polls until ctx is cancelled. Backend failures are logged and retried on
// the next tick instead of ending the tray.
func (a *App) Run(ctx context.Context) error {
	poller, err := newPoller(a.fetcher, a.interval, a.apply, a.newTicker)
	if err != nil {
		return err
	}
	poller.Run(ctx)
	return nil
}

func (a *App) apply(update Update) {
	if update.Err != nil {
		a.logger.Warn("proxy state unavailable", "err", update.Err)
	}
	if err := a.notifier.Set(update.Status); err != nil {
		a.logger.Error("updating tray indicator", "err", err)
	}
}
