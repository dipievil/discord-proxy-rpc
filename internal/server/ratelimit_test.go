package server

import (
	"testing"
	"time"
)

func TestRateLimiterAllowsWithinLimit(t *testing.T) {
	rl := NewRateLimiter(3, time.Minute)

	ip := "192.168.1.1"
	for i := 0; i < 3; i++ {
		if !rl.Allow(ip) {
			t.Errorf("request %d should be allowed", i+1)
		}
	}
}

func TestRateLimiterBlocksOverLimit(t *testing.T) {
	rl := NewRateLimiter(2, time.Minute)

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
	rl := NewRateLimiter(1, 50*time.Millisecond)

	ip := "192.168.1.3"
	if !rl.Allow(ip) {
		t.Fatal("first request should be allowed")
	}
	if rl.Allow(ip) {
		t.Fatal("second request should be blocked")
	}

	time.Sleep(100 * time.Millisecond)

	if !rl.Allow(ip) {
		t.Fatal("request after window should be allowed")
	}
}

func TestRateLimiterDifferentIPs(t *testing.T) {
	rl := NewRateLimiter(1, time.Minute)

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

func TestConnectionTrackerAllowsWithinLimit(t *testing.T) {
	ct := NewConnectionTracker(3)

	if !ct.CanConnect() {
		t.Fatal("should allow connection")
	}
	ct.Add()
	if !ct.CanConnect() {
		t.Fatal("should allow connection")
	}
	ct.Add()
	if !ct.CanConnect() {
		t.Fatal("should allow connection")
	}
	ct.Add()
	if ct.CanConnect() {
		t.Fatal("should reject connection at limit")
	}
}

func TestConnectionTrackerRemove(t *testing.T) {
	ct := NewConnectionTracker(2)

	ct.Add()
	ct.Add()
	if ct.CanConnect() {
		t.Fatal("should be at limit")
	}

	ct.Remove()
	if !ct.CanConnect() {
		t.Fatal("should allow after remove")
	}
	if ct.Count() != 1 {
		t.Errorf("Count = %d, want 1", ct.Count())
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
	}

	for _, tt := range tests {
		got := extractIP(tt.input)
		if got != tt.want {
			t.Errorf("extractIP(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}
