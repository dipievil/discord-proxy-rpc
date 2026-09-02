package ipc

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/discord-proxy-rpc/discord-proxy-rpc/internal/config"
	"go.uber.org/zap"
)

func TestBackoffSequence(t *testing.T) {
	rm := &ReconnectManager{
		baseInterval: 5 * time.Second,
		maxInterval:  60 * time.Second,
	}

	expected := []time.Duration{
		5 * time.Second,
		10 * time.Second,
		20 * time.Second,
		40 * time.Second,
		60 * time.Second,
	}

	for i, want := range expected {
		rm.mu.Lock()
		rm.attempt = i
		rm.mu.Unlock()

		interval := rm.NextInterval()
		minAllowed := time.Duration(float64(want) * 0.89)
		maxAllowed := time.Duration(float64(want) * 1.11)
		if interval < minAllowed || interval > maxAllowed {
			t.Errorf("attempt %d: interval %v not within 11%% of %v (range [%v, %v])",
				i, interval, want, minAllowed, maxAllowed)
		}
	}
}

func TestBackoffJitter(t *testing.T) {
	rm := &ReconnectManager{
		baseInterval: 10 * time.Second,
		maxInterval:  60 * time.Second,
	}

	for attempt := 0; attempt < 4; attempt++ {
		rm.mu.Lock()
		rm.attempt = attempt
		rm.mu.Unlock()

		base := cappedDouble(rm.baseInterval, rm.maxInterval, attempt)

		for range 200 {
			interval := rm.NextInterval()
			min := time.Duration(float64(base) * 0.9)
			max := time.Duration(float64(base) * 1.1)
			if interval < min || interval > max {
				t.Errorf("attempt %d: interval %v outside [%.0fs, %.0fs]",
					attempt, interval, min.Seconds(), max.Seconds())
				t.FailNow()
			}
		}
	}
}

func TestBackoffMaxCap(t *testing.T) {
	rm := &ReconnectManager{
		baseInterval: 5 * time.Second,
		maxInterval:  60 * time.Second,
	}

	for attempt := range 20 {
		rm.mu.Lock()
		rm.attempt = attempt
		rm.mu.Unlock()

		interval := rm.NextInterval()
		if interval > 66*time.Second {
			t.Errorf("attempt %d: interval %v exceeds max+10%% of 66s", attempt, interval)
		}
	}
}

func TestResetOnSuccess(t *testing.T) {
	rm := &ReconnectManager{
		baseInterval: 5 * time.Second,
		maxInterval:  60 * time.Second,
	}

	rm.mu.Lock()
	rm.attempt = 5
	rm.mu.Unlock()

	rm.Reset()

	rm.mu.Lock()
	attempt := rm.attempt
	rm.mu.Unlock()

	if attempt != 0 {
		t.Errorf("attempt = %d, want 0 after Reset", attempt)
	}
}

func TestAutoReconnectDisabled(t *testing.T) {
	m := newMockDiscord(t)
	c := newTestClient(t, m)

	cfg := config.DiscordConfig{
		AutoReconnect:         false,
		ReconnectBaseInterval: 5 * time.Second,
		MaxReconnectInterval:  60 * time.Second,
	}

	rm := NewReconnectManager(c, cfg, zap.NewNop())

	err := rm.Reconnect(context.Background())
	if err == nil {
		t.Fatal("expected error when auto-reconnect is disabled")
	}
	if !errors.Is(err, ErrAutoReconnectDisabled) {
		t.Errorf("error = %v, want ErrAutoReconnectDisabled", err)
	}
}

func TestReconnectStopsOnContextCancel(t *testing.T) {
	m := newMockDiscord(t)
	c := newTestClient(t, m)

	cfg := config.DiscordConfig{
		AutoReconnect:         true,
		ReconnectBaseInterval: 5 * time.Second,
		MaxReconnectInterval:  60 * time.Second,
	}

	ctx, cancel := context.WithCancel(context.Background())
	rm := NewReconnectManager(c, cfg, zap.NewNop())

	done := make(chan error, 1)
	go func() { done <- rm.Reconnect(ctx) }()

	time.Sleep(50 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected error from cancelled context")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Reconnect did not stop after context cancellation")
	}

	if c.State() != StateReconnecting {
		t.Errorf("state = %v, want reconnecting", c.State())
	}
}

func TestStopCancelsPendingTimer(t *testing.T) {
	m := newMockDiscord(t)
	c := newTestClient(t, m)

	cfg := config.DiscordConfig{
		AutoReconnect:         true,
		ReconnectBaseInterval: 5 * time.Second,
		MaxReconnectInterval:  60 * time.Second,
	}

	rm := NewReconnectManager(c, cfg, zap.NewNop())

	ctx := context.Background()
	done := make(chan error, 1)
	go func() { done <- rm.Reconnect(ctx) }()

	time.Sleep(50 * time.Millisecond)
	rm.Stop()

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("Stop did not cause Reconnect to return")
	}
}

func TestReconnectSuccessAfterFailure(t *testing.T) {
	cfg := config.DiscordConfig{
		AutoReconnect:         true,
		ReconnectBaseInterval: 20 * time.Millisecond,
		MaxReconnectInterval:  50 * time.Millisecond,
	}

	m2 := newMockDiscord(t)

	c := New("test-client-id", zap.NewNop())
	c.paths = []string{"/nonexistent/discord-ipc-99"}
	c.timeout = 10 * time.Millisecond

	rm := NewReconnectManager(c, cfg, zap.NewNop())

	c.paths = []string{m2.path}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	err := rm.Reconnect(ctx)
	if err != nil {
		t.Fatalf("expected successful reconnect, got %v", err)
	}
	if c.State() != StateConnected {
		t.Errorf("state = %v, want connected", c.State())
	}
}

func TestReconnectFailsThenStops(t *testing.T) {
	c := New("test-client-id", zap.NewNop())
	c.paths = []string{"/nonexistent/discord-ipc-99"}
	c.timeout = 10 * time.Millisecond

	cfg := config.DiscordConfig{
		AutoReconnect:         true,
		ReconnectBaseInterval: 20 * time.Millisecond,
		MaxReconnectInterval:  50 * time.Millisecond,
	}

	rm := NewReconnectManager(c, cfg, zap.NewNop())

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- rm.Reconnect(ctx) }()

	time.Sleep(200 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected error from cancelled context")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("Reconnect did not stop after context cancellation")
	}
}

func TestRunWithReconnectStopsOnContextCancel(t *testing.T) {
	m := newMockDiscord(t)
	c := newTestClient(t, m)

	cfg := config.DiscordConfig{
		AutoReconnect:         true,
		ReconnectBaseInterval: 5 * time.Second,
		MaxReconnectInterval:  10 * time.Second,
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- c.RunWithReconnect(ctx, cfg) }()

	waitAccept(t, m)
	m.closeConnection()
	m.ln.Close()

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected error from cancelled context")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("RunWithReconnect did not stop after context cancellation")
	}
}

func TestMaxReconnectAttempts(t *testing.T) {
	c := New("test-client-id", zap.NewNop())
	c.paths = []string{"/nonexistent/discord-ipc-99"}
	c.timeout = 10 * time.Millisecond

	cfg := config.DiscordConfig{
		AutoReconnect:         true,
		ReconnectBaseInterval: 1 * time.Millisecond,
		MaxReconnectInterval:  2 * time.Millisecond,
	}

	rm := NewReconnectManager(c, cfg, zap.NewNop())

	rm.mu.Lock()
	rm.attempt = maxReconnectAttempts
	rm.mu.Unlock()

	err := rm.Reconnect(context.Background())
	if err == nil {
		t.Fatal("expected error at max attempts")
	}
	if !errors.Is(err, ErrMaxReconnectAttempts) {
		t.Errorf("error = %v, want ErrMaxReconnectAttempts", err)
	}
}

func TestNextIntervalRespectsMaxCap(t *testing.T) {
	rm := &ReconnectManager{
		baseInterval: 5 * time.Second,
		maxInterval:  60 * time.Second,
	}

	for attempt := range 20 {
		rm.mu.Lock()
		rm.attempt = attempt
		rm.mu.Unlock()

		interval := rm.NextInterval()
		if interval > rm.maxInterval*11/10 {
			t.Errorf("attempt %d: interval %v exceeds max %v", attempt, interval, rm.maxInterval)
		}
	}
}

func TestBackoffDoesNotOverflowAtLargeAttempts(t *testing.T) {
	rm := &ReconnectManager{
		baseInterval: 5 * time.Second,
		maxInterval:  60 * time.Second,
	}

	for attempt := 0; attempt <= 2*maxReconnectAttempts; attempt++ {
		rm.mu.Lock()
		rm.attempt = attempt
		rm.mu.Unlock()

		interval := rm.NextInterval()
		if interval <= 0 {
			t.Fatalf("attempt %d: interval %v must be positive (overflow)", attempt, interval)
		}
		maxAllowed := rm.maxInterval * 11 / 10
		if interval > maxAllowed {
			t.Errorf("attempt %d: interval %v exceeds max+10%% %v", attempt, interval, maxAllowed)
		}
	}
}

func TestCappedDouble(t *testing.T) {
	cases := []struct {
		name   string
		base   time.Duration
		cap    time.Duration
		times  int
		want   time.Duration
	}{
		{"zero times", 5 * time.Second, 60 * time.Second, 0, 5 * time.Second},
		{"below cap", 5 * time.Second, 60 * time.Second, 2, 20 * time.Second},
		{"capped", 5 * time.Second, 60 * time.Second, 4, 60 * time.Second},
		{"base beyond cap", 100 * time.Second, 60 * time.Second, 3, 60 * time.Second},
		{"large times", 1 * time.Nanosecond, 60 * time.Second, 1000, 60 * time.Second},
		{"zero cap", 5 * time.Second, 0, 3, 0},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := cappedDouble(tc.base, tc.cap, tc.times); got != tc.want {
				t.Errorf("cappedDouble(%v, %v, %d) = %v, want %v", tc.base, tc.cap, tc.times, got, tc.want)
			}
		})
	}
}

func TestRunWithReconnectStopsOnCancelWhileIdle(t *testing.T) {
	m := newMockDiscord(t)
	c := newTestClient(t, m)

	cfg := config.DiscordConfig{
		AutoReconnect:         true,
		ReconnectBaseInterval: 5 * time.Second,
		MaxReconnectInterval:  10 * time.Second,
	}

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- c.RunWithReconnect(ctx, cfg) }()

	waitAccept(t, m)

	time.Sleep(100 * time.Millisecond)
	cancel()

	select {
	case err := <-done:
		if err == nil {
			t.Fatal("expected error from cancelled context")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("RunWithReconnect did not stop on cancel while session idle")
	}
}
