package mdns

import (
	"context"
	"os"
	"syscall"
	"testing"
	"time"

	"github.com/discord-proxy-rpc/discord-proxy-rpc/internal/config"
	"go.uber.org/zap"
)

func TestDeregisterOnSignalReception(t *testing.T) {
	a, err := NewAdvertiser(config.MdnsConfig{ServiceType: "_test._tcp"}, 19999, zap.NewNop())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sigCh := make(chan os.Signal, 1)
	done := make(chan struct{})

	go func() {
		deregisterOnSignal(context.Background(), a, zap.NewNop(), sigCh)
		close(done)
	}()

	sigCh <- syscall.SIGUSR1

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("deregisterOnSignal did not complete after signal")
	}
}

func TestDeregisterOnSignalContextCancellation(t *testing.T) {
	a, err := NewAdvertiser(config.MdnsConfig{ServiceType: "_test._tcp"}, 19999, zap.NewNop())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sigCh := make(chan os.Signal, 1)
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})

	go func() {
		deregisterOnSignal(ctx, a, zap.NewNop(), sigCh)
		close(done)
	}()

	cancel()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("deregisterOnSignal did not complete after context cancellation")
	}
}

func TestDeregisterOnSignalMultipleSignals(t *testing.T) {
	a, err := NewAdvertiser(config.MdnsConfig{ServiceType: "_test._tcp"}, 19999, zap.NewNop())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sigCh := make(chan os.Signal, 1)
	done := make(chan struct{})

	go func() {
		deregisterOnSignal(context.Background(), a, zap.NewNop(), sigCh)
		close(done)
	}()

	sigCh <- syscall.SIGUSR1
	sigCh <- syscall.SIGUSR1

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("deregisterOnSignal did not complete after multiple signals")
	}
}

func TestDeregisterOnSignalShutdownIdempotent(t *testing.T) {
	a, err := NewAdvertiser(config.MdnsConfig{ServiceType: "_test._tcp"}, 19999, zap.NewNop())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sigCh := make(chan os.Signal, 1)
	done := make(chan struct{})

	go func() {
		deregisterOnSignal(context.Background(), a, zap.NewNop(), sigCh)
		close(done)
	}()

	sigCh <- syscall.SIGUSR1

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("deregisterOnSignal did not complete")
	}

	if err := a.Shutdown(); err != nil {
		t.Fatalf("second Shutdown should be safe, got: %v", err)
	}
}

func TestDeregisterOnSignalNilServer(t *testing.T) {
	a, err := NewAdvertiser(config.MdnsConfig{}, 8765, zap.NewNop())
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	sigCh := make(chan os.Signal, 1)
	done := make(chan struct{})

	go func() {
		deregisterOnSignal(context.Background(), a, zap.NewNop(), sigCh)
		close(done)
	}()

	sigCh <- syscall.SIGUSR1

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("deregisterOnSignal did not complete with nil server")
	}
}
