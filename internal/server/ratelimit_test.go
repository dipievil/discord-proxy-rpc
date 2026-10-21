package server

import (
	"runtime"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

// newTestRateLimiter builds a limiter and stops its cleanup goroutine when the
// test finishes, so tests never leak goroutines.
func newTestRateLimiter(t *testing.T, rate int, window time.Duration) *RateLimiter {
	t.Helper()
	rl := NewRateLimiter(rate, window)
	t.Cleanup(rl.Stop)
	return rl
}

func TestRateLimiterAllowsWithinLimit(t *testing.T) {
	rl := newTestRateLimiter(t, 3, time.Minute)

	ip := "192.168.1.1"
	for i := 0; i < 3; i++ {
		if !rl.Allow(ip) {
			t.Errorf("request %d should be allowed", i+1)
		}
	}
}

func TestRateLimiterBlocksOverLimit(t *testing.T) {
	rl := newTestRateLimiter(t, 2, time.Minute)

	ip := "192.168.1.2"
	if !rl.Allow(ip) {
		t.Fatal("first request should be allowed")
	}
	if !rl.Allow(ip) {
		t.Fatal("second request should be allowed")
	}
	if rl.Allow(ip) {
		t.Fatal("third request should be blocked")
	}
}

func TestRateLimiterResetsAfterWindow(t *testing.T) {
	rl := newTestRateLimiter(t, 1, 50*time.Millisecond)

	ip := "192.168.1.3"
	if !rl.Allow(ip) {
		t.Fatal("first request should be allowed")
	}
	if rl.Allow(ip) {
		t.Fatal("second request should be blocked")
	}

	waitFor(t, 2*time.Second, "rate limit window to reset", func() bool {
		return rl.Allow(ip)
	})
}

func TestRateLimiterDifferentIPs(t *testing.T) {
	rl := newTestRateLimiter(t, 1, time.Minute)

	if !rl.Allow("10.0.0.1") {
		t.Fatal("first IP should be allowed")
	}
	if rl.Allow("10.0.0.1") {
		t.Fatal("first IP should be blocked after limit")
	}
	if !rl.Allow("10.0.0.2") {
		t.Fatal("second IP should be allowed")
	}
}

func TestRateLimiterStopIsIdempotent(t *testing.T) {
	rl := NewRateLimiter(1, time.Minute)

	rl.Stop()
	rl.Stop()

	if !rl.Allow("10.0.0.1") {
		t.Error("Allow should keep working after Stop")
	}
}

func TestRateLimiterStopWithoutGoroutine(t *testing.T) {
	rl := NewRateLimiter(1, 0)

	done := make(chan struct{})
	go func() {
		rl.Stop()
		close(done)
	}()

	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("Stop blocked when no cleanup goroutine was started")
	}
}

func TestRateLimiterStopReleasesCleanupGoroutine(t *testing.T) {
	before := runtime.NumGoroutine()

	const limiters = 20
	for i := 0; i < limiters; i++ {
		NewRateLimiter(1, time.Minute).Stop()
	}

	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if runtime.NumGoroutine() <= before {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Errorf("goroutines = %d, want <= %d after Stop", runtime.NumGoroutine(), before)
}

func TestRateLimiterEvictsExpiredVisitors(t *testing.T) {
	rl := newTestRateLimiter(t, 1, 20*time.Millisecond)

	if !rl.Allow("192.168.1.4") {
		t.Fatal("first request should be allowed")
	}

	waitFor(t, 2*time.Second, "expired visitor to be evicted", func() bool {
		rl.mu.Lock()
		defer rl.mu.Unlock()
		return len(rl.visitors) == 0
	})
}

func TestConnectionTrackerAllowsWithinLimit(t *testing.T) {
	ct := NewConnectionTracker(3)

	for i := 0; i < 3; i++ {
		if !ct.TryAdd() {
			t.Fatalf("TryAdd() = false on reservation %d, want true", i+1)
		}
	}
	if ct.TryAdd() {
		t.Fatal("TryAdd() = true at limit, want false")
	}
	if ct.Count() != 3 {
		t.Errorf("Count = %d, want 3", ct.Count())
	}
}

func TestConnectionTrackerRemove(t *testing.T) {
	ct := NewConnectionTracker(2)

	ct.TryAdd()
	ct.TryAdd()
	if ct.TryAdd() {
		t.Fatal("should be at limit")
	}

	ct.Remove()
	if ct.Count() != 1 {
		t.Errorf("Count = %d, want 1", ct.Count())
	}
	if !ct.TryAdd() {
		t.Fatal("should allow a reservation after remove")
	}
}

func TestConnectionTrackerTryAddConcurrent(t *testing.T) {
	ct := NewConnectionTracker(5)

	const goroutines = 64
	var wg sync.WaitGroup
	var granted int64

	for i := 0; i < goroutines; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if ct.TryAdd() {
				atomic.AddInt64(&granted, 1)
			}
		}()
	}
	wg.Wait()

	if granted != 5 {
		t.Errorf("granted reservations = %d, want 5", granted)
	}
	if ct.Count() != 5 {
		t.Errorf("Count = %d, want 5", ct.Count())
	}
}

func TestConnectionTrackerRemoveFloor(t *testing.T) {
	ct := NewConnectionTracker(5)

	ct.Remove()
	if ct.Count() != 0 {
		t.Errorf("Count = %d, want 0 (remove below zero)", ct.Count())
	}
}

func TestExtractIP(t *testing.T) {
	tests := []struct {
		input string
		want  string
	}{
		{"192.168.1.1:8080", "192.168.1.1"},
		{"[::1]:8080", "::1"},
		{"10.0.0.1", "10.0.0.1"},
		{"::1", "::1"},
		{"[::1]", "::1"},
		{"[2001:db8::1]", "2001:db8::1"},
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			got := extractIP(tt.input)
			if got != tt.want {
				t.Errorf("extractIP(%q) = %q, want %q", tt.input, got, tt.want)
			}
		})
	}
}
