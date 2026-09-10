package mdns

import (
	"context"
	"os"
	"os/signal"
	"syscall"

	"go.uber.org/zap"
)

// DeregisterOnSignal listens for SIGTERM and SIGINT and calls
// Advertiser.Shutdown to deregister the mDNS service. It also deregisters
// when the provided context is cancelled. This function blocks until a
// signal is received or the context is done.
func DeregisterOnSignal(ctx context.Context, adv *Advertiser, logger *zap.Logger) {
	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGTERM, syscall.SIGINT)
	defer signal.Stop(sigCh)

	deregisterOnSignal(ctx, adv, logger, sigCh)
}

func deregisterOnSignal(ctx context.Context, adv *Advertiser, logger *zap.Logger, sigCh <-chan os.Signal) {
	select {
	case sig := <-sigCh:
		logger.Info("received signal, shutting down mDNS", zap.String("signal", sig.String()))
		if err := adv.Shutdown(); err != nil {
			logger.Error("failed to deregister mDNS service", zap.Error(err))
		}
	case <-ctx.Done():
		logger.Info("context cancelled, shutting down mDNS")
		if err := adv.Shutdown(); err != nil {
			logger.Error("failed to deregister mDNS service", zap.Error(err))
		}
	}
}
