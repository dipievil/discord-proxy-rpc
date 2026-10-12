package ipc

import (
	"context"
	"testing"
	"time"

	"github.com/discord-proxy-rpc/discord-proxy-rpc/internal/config"
	"go.uber.org/zap"
)

func TestHealthCheckSendsPing(t *testing.T) {
	m := newMockDiscord(t)
	c := newTestClient(t, m)

	hc := NewHealthCheckManager(c, config.DiscordConfig{
		HealthCheckInterval: 50 * time.Millisecond,
		HealthCheckTimeout:  5 * time.Second,
	}, zap.NewNop())
	hc.Start(context.Background())
	defer hc.Stop()

	select {
	case f := <-m.recv:
		if f.opcode != OpPing {
			t.Errorf("opcode = %d, want %d", f.opcode, OpPing)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("health check never sent ping")
	}
}

func TestHealthCheckPongResetsTimer(t *testing.T) {
	m := newMockDiscord(t)
	c := newTestClient(t, m)

	hc := NewHealthCheckManager(c, config.DiscordConfig{
		HealthCheckInterval: 30 * time.Millisecond,
		HealthCheckTimeout:  2 * time.Second,
	}, zap.NewNop())
	hc.Start(context.Background())
	defer hc.Stop()

	for i := 0; i < 3; i++ {
		select {
		case f := <-m.recv:
			if f.opcode != OpPing {
				t.Fatalf("iteration %d: opcode = %d, want %d", i, f.opcode, OpPing)
			}
		case <-time.After(2 * time.Second):
			t.Fatalf("iteration %d: health check never sent ping", i)
		}

		hc.HandlePong()
	}
}

func TestHealthCheckDetectsStaleConnection(t *testing.T) {
	m := newMockDiscord(t)
	c := newTestClient(t, m)

	hc := NewHealthCheckManager(c, config.DiscordConfig{
		HealthCheckInterval: 50 * time.Millisecond,
		HealthCheckTimeout:  100 * time.Millisecond,
	}, zap.NewNop())
	hc.Start(context.Background())

	select {
	case <-m.recv:
	case <-time.After(2 * time.Second):
		t.Fatal("health check never sent ping")
	}

	time.Sleep(250 * time.Millisecond)

	if c.State() != StateDisconnected {
		t.Errorf("state = %v, want disconnected (stale connection detected)", c.State())
	}
}

func TestHealthCheckStop(t *testing.T) {
	m := newMockDiscord(t)
	c := newTestClient(t, m)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	hc := NewHealthCheckManager(c, config.DiscordConfig{
		HealthCheckInterval: 50 * time.Millisecond,
		HealthCheckTimeout:  5 * time.Second,
	}, zap.NewNop())
	hc.Start(ctx)

	select {
	case <-m.recv:
	case <-time.After(2 * time.Second):
		t.Fatal("health check never sent ping")
	}

	done := make(chan struct{})
	go func() {
		hc.Stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("Stop did not return within timeout")
	}
}

func TestHealthCheckPongBeforePing(t *testing.T) {
	m := newMockDiscord(t)
	c := newTestClient(t, m)

	hc := NewHealthCheckManager(c, config.DiscordConfig{
		HealthCheckInterval: 100 * time.Millisecond,
		HealthCheckTimeout:  5 * time.Second,
	}, zap.NewNop())
	hc.Start(context.Background())
	defer hc.Stop()

	hc.HandlePong()
	hc.HandlePong()

	select {
	case f := <-m.recv:
		if f.opcode != OpPing {
			t.Errorf("opcode = %d, want %d", f.opcode, OpPing)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("health check never sent ping")
	}

	if c.State() != StateConnected {
		t.Errorf("state = %v, want connected (pre-mature PONGs should not cause issues)", c.State())
	}
}

func TestHealthCheckContextCancel(t *testing.T) {
	m := newMockDiscord(t)
	c := newTestClient(t, m)

	ctx, cancel := context.WithCancel(context.Background())

	hc := NewHealthCheckManager(c, config.DiscordConfig{
		HealthCheckInterval: 50 * time.Millisecond,
		HealthCheckTimeout:  5 * time.Second,
	}, zap.NewNop())
	hc.Start(ctx)

	select {
	case <-m.recv:
	case <-time.After(2 * time.Second):
		t.Fatal("health check never sent ping")
	}

	cancel()

	select {
	case <-hc.Done():
	case <-time.After(2 * time.Second):
		t.Fatal("context cancel did not stop health check")
	}
}

func TestHealthCheckDefaultsApplied(t *testing.T) {
	c := newTestClient(t, newMockDiscord(t))

	hc := NewHealthCheckManager(c, config.DiscordConfig{}, zap.NewNop())

	if hc.interval != 30*time.Second {
		t.Errorf("interval = %v, want 30s (default)", hc.interval)
	}
	if hc.timeout != 10*time.Second {
		t.Errorf("timeout = %v, want 10s (default)", hc.timeout)
	}
}

func TestHandlePongBeforeStart(t *testing.T) {
	c := newTestClient(t, newMockDiscord(t))

	hc := NewHealthCheckManager(c, config.DiscordConfig{
		HealthCheckInterval: 30 * time.Second,
		HealthCheckTimeout:  10 * time.Second,
	}, zap.NewNop())

	hc.HandlePong()
}

func TestRunWithReconnectCreatesHealthCheck(t *testing.T) {
	m := newMockDiscord(t)
	c := newTestClient(t, m)

	cfg := config.DiscordConfig{
		AutoReconnect:         true,
		ReconnectBaseInterval: 5 * time.Second,
		MaxReconnectInterval:  10 * time.Second,
		HealthCheckInterval:   50 * time.Millisecond,
		HealthCheckTimeout:    5 * time.Second,
	}

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	done := make(chan error, 1)
	go func() { done <- c.RunWithReconnect(ctx, cfg) }()

	waitAccept(t, m)

	select {
	case f := <-m.recv:
		if f.opcode != OpPing {
			t.Errorf("expected health check ping, got opcode %d", f.opcode)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("RunWithReconnect did not start health check")
	}

	cancel()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("RunWithReconnect did not stop after context cancel")
	}
}
